package payment

// UnitPay — приём оплаты mezmo-song.com (Celementa FZCO). Документация:
// https://help.unitpay.money (initPayment, getPayment, HTTP-уведомления).
//
// Платёж создаётся через API initPayment, а не ссылкой на форму: API сразу
// отдаёт paymentId, и по нему фоновая сверка (reconcile.go) находит оплату,
// уведомление о которой до сервера не дошло.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var unitpayClient = &http.Client{Timeout: 30 * time.Second}

// UnitPayAPIBase — база API. Переменная, а не константа: тесты и локальный стенд
// подменяют её заглушкой.
var UnitPayAPIBase = "https://unitpay.money"

// UnitPayIPs — адреса, с которых UnitPay шлёт уведомления (справочник «IP
// адреса системы»). Уведомление с другого адреса обработчик отклоняет.
var UnitPayIPs = []string{"31.186.100.49", "51.250.20.9", "217.198.170.141"}

// Статусы платежа в ответе getPayment.
const (
	UnitPayStatusSuccess = "success"
	UnitPayStatusWait    = "wait"
	UnitPayStatusRefund  = "refund"
)

type UnitPay struct {
	ProjectID string
	SecretKey string
	// Test=true — тестовый режим API (params[test]=1): только RUB и только карта,
	// данные платежа не сохраняются. Login — email кабинета UnitPay, без него
	// тестовый режим запрос не принимает.
	Test  bool
	Login string
}

// UnitPayAPIError — отказ API ({"error": {...}}). Отдельный тип, чтобы отличать
// отказ (повтор не поможет) от сбоя сети.
type UnitPayAPIError struct {
	Code    int
	Message string
}

func (e *UnitPayAPIError) Error() string {
	return fmt.Sprintf("unitpay: error %d: %s", e.Code, e.Message)
}

type UnitPayInitRequest struct {
	Account     string // сырой id заказа: вернётся в уведомлении как params[account]
	Sum         string // "25.00"
	Currency    string // ISO 4217: USD; в тестовом режиме только RUB
	Desc        string
	PaymentType string // код платёжной системы; пусто — card
	ResultURL   string // куда вернуть покупателя после оплаты (домен проекта)
	BackURL     string // куда вернуть, если он ушёл с формы без оплаты
	Locale      string // ru | en
	IP          string // IP покупателя
}

type UnitPayInitResult struct {
	PaymentID   string
	RedirectURL string
}

// UnitPayPayment — платёж из getPayment: то, с чем сверяется заказ.
type UnitPayPayment struct {
	PaymentID     string
	Status        string // success | wait | error | error_pay | error_check | refund | secure
	Account       string
	OrderSum      string
	OrderCurrency string
	PaymentType   string
}

func (u UnitPay) configured() error {
	if u.ProjectID == "" || u.SecretKey == "" {
		return fmt.Errorf("unitpay: missing credentials")
	}
	return nil
}

// call выполняет GET /api?method=…&params[k]=v и разбирает result в out.
func (u UnitPay) call(method string, params map[string]string, out any) error {
	q := url.Values{"method": {method}}
	for k, v := range params {
		if v != "" {
			q.Set("params["+k+"]", v)
		}
	}
	q.Set("params[secretKey]", u.SecretKey)
	if u.Test {
		q.Set("params[test]", "1")
		if u.Login != "" {
			q.Set("params[login]", u.Login)
		}
	}
	resp, err := unitpayClient.Get(UnitPayAPIBase + "/api?" + q.Encode())
	if err != nil {
		return fmt.Errorf("unitpay %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("unitpay %s: read: %w", method, err)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("unitpay %s: status %d: %s", method, resp.StatusCode, truncate(string(body), 300))
	}
	if env.Error != nil {
		return &UnitPayAPIError{Code: env.Error.Code, Message: env.Error.Message}
	}
	if len(env.Result) == 0 {
		return fmt.Errorf("unitpay %s: empty result", method)
	}
	return json.Unmarshal(env.Result, out)
}

// InitPayment создаёт платёж и отдаёт адрес платёжной формы.
func (u UnitPay) InitPayment(req UnitPayInitRequest) (UnitPayInitResult, error) {
	if err := u.configured(); err != nil {
		return UnitPayInitResult{}, err
	}
	paymentType := req.PaymentType
	if paymentType == "" {
		paymentType = "card"
	}
	var out struct {
		Type        string     `json:"type"`
		PaymentID   flexString `json:"paymentId"`
		RedirectURL string     `json:"redirectUrl"`
		Message     string     `json:"message"`
	}
	err := u.call("initPayment", map[string]string{
		"paymentType": paymentType,
		"projectId":   u.ProjectID,
		"account":     req.Account,
		"sum":         req.Sum,
		"currency":    req.Currency,
		"desc":        req.Desc,
		"resultUrl":   req.ResultURL,
		"backUrl":     req.BackURL,
		"locale":      req.Locale,
		"ip":          req.IP,
	}, &out)
	if err != nil {
		return UnitPayInitResult{}, err
	}
	if out.RedirectURL == "" || out.PaymentID == "" {
		return UnitPayInitResult{}, fmt.Errorf("unitpay initPayment: type=%q без redirectUrl/paymentId: %s", out.Type, out.Message)
	}
	return UnitPayInitResult{PaymentID: string(out.PaymentID), RedirectURL: out.RedirectURL}, nil
}

// GetPayment читает состояние платежа из API — источник истины для сверки.
func (u UnitPay) GetPayment(paymentID string) (UnitPayPayment, error) {
	if err := u.configured(); err != nil {
		return UnitPayPayment{}, err
	}
	if paymentID == "" {
		return UnitPayPayment{}, fmt.Errorf("unitpay: empty payment id")
	}
	var out struct {
		PaymentID     flexString `json:"paymentId"`
		Status        string     `json:"status"`
		Account       string     `json:"account"`
		OrderSum      flexString `json:"orderSum"`
		OrderCurrency string     `json:"orderCurrency"`
		PaymentType   string     `json:"paymentType"`
	}
	if err := u.call("getPayment", map[string]string{"paymentId": paymentID}, &out); err != nil {
		return UnitPayPayment{}, err
	}
	return UnitPayPayment{
		PaymentID:     string(out.PaymentID),
		Status:        out.Status,
		Account:       out.Account,
		OrderSum:      string(out.OrderSum),
		OrderCurrency: out.OrderCurrency,
		PaymentType:   out.PaymentType,
	}, nil
}

// UnitPaySignature — подпись уведомления: sha256(method{up}значения params,
// отсортированные по ключу, без signature{up}secretKey).
func UnitPaySignature(method string, params map[string]string, secretKey string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k != "signature" && k != "sign" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+2)
	parts = append(parts, method)
	for _, k := range keys {
		parts = append(parts, params[k])
	}
	parts = append(parts, secretKey)
	sum := sha256.Sum256([]byte(strings.Join(parts, "{up}")))
	return hex.EncodeToString(sum[:])
}

// UnitPayVerify проверяет подпись уведомления в константное время.
func UnitPayVerify(method string, params map[string]string, secretKey string) bool {
	want := UnitPaySignature(method, params, secretKey)
	got := strings.ToLower(params["signature"])
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// flexString принимает и число, и строку: paymentId в initPayment приходит
// числом, а в getPayment — строкой; суммы — то "5.00", то 5.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	if string(b) == "null" {
		*f = ""
		return nil
	}
	*f = flexString(b)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
