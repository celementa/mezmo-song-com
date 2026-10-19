package payment

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var yooClient = &http.Client{Timeout: 30 * time.Second}

// APIBaseURL — база API ЮKassa. Отдельная переменная нужна тестам: пакет ходит
// в сеть, и без подмены базы хендлеры не проверить.
var APIBaseURL = "https://api.yookassa.ru"

const (
	ModeEmbedded = "embedded" // виджет на своём домене → confirmation_token
	ModeRedirect = "redirect" // уход на страницу ЮKassa → confirmation_url
)

// APIError — ответ ЮKassa с HTTP-статусом ошибки. Отдельный тип нужен, чтобы
// вызывающий отличал отказ API (4xx — запрос не пройдёт и при повторе) от сбоя сети.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("yookassa: status %d: %s", e.Status, e.Body)
}

type YooKassa struct {
	ShopID string
	Secret string
}

type CreateRequest struct {
	OrderID   string // сырой id заказа: уходит в metadata.order_id
	ValueRub  string // "990.00"
	Mode      string // ModeEmbedded | ModeRedirect
	ReturnURL string // только для ModeRedirect; для embedded return_url задаёт виджет на фронте
	Receipt   Receipt

	// Metadata заменяет дефолтную {"order_id": OrderID} — подписочные платежи
	// помечаются subscription_id/flow, чтобы вебхук не спутал их с заказами.
	Metadata map[string]string
	// SavePaymentMethod=true просит ЮKassa привязать карту для автосписаний
	// (первый платёж подписки).
	SavePaymentMethod bool
	// PaymentMethodID — автосписание по сохранённой карте: подтверждение не
	// нужно, confirmation в payload не уходит.
	PaymentMethodID string
	// IdempotenceKey — детерминированный ключ вместо случайного. Обязателен для
	// ребиллов: повторный тик планировщика не должен списать деньги дважды.
	IdempotenceKey string
}

type CreateResult struct {
	PaymentID         string
	Status            string // для автосписаний ответ приходит сразу: succeeded или canceled
	ConfirmationURL   string // непусто при ModeRedirect
	ConfirmationToken string // непусто при ModeEmbedded, живёт 1 час, одноразовый
}

type createPaymentRes struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Confirmation *struct {
		Type              string `json:"type"`
		ConfirmationURL   string `json:"confirmation_url"`
		ConfirmationToken string `json:"confirmation_token"`
	} `json:"confirmation"`
}

// Receipt holds the data required for 54-FZ fiscalization. At least one of
// Email/Phone must be set, otherwise YooKassa rejects the payment.
type Receipt struct {
	Email       string
	Phone       string
	Description string // item name, max 128 chars
	VatCode     int    // 1 = без НДС, 2 = 0%, 3 = 10%, 4 = 20%, 5 = 10/110, 6 = 20/120
}

// createPayload вынесен отдельно, чтобы payload проверялся тестом без сети.
// paymentLocale задаёт язык платёжной формы. Без него ЮKassa ориентируется на
// язык браузера/устройства, и покупатель из РФ вполне может получить форму
// по-английски (наблюдалось на проде 2026-08-07: «Complete the payment within…»
// на русской странице заказа).
const paymentLocale = "ru_RU"

func createPayload(req CreateRequest) map[string]any {
	meta := req.Metadata
	if meta == nil {
		meta = map[string]string{"order_id": req.OrderID}
	}
	payload := map[string]any{
		"amount":   map[string]string{"value": req.ValueRub, "currency": "RUB"},
		"capture":  true,
		"metadata": meta,
		"receipt":  buildReceipt(req.ValueRub, req.Receipt),
	}
	// Автосписание по сохранённой карте проходит без участия человека —
	// confirmation ЮKassa для него не принимает.
	if req.PaymentMethodID != "" {
		payload["payment_method_id"] = req.PaymentMethodID
		return payload
	}
	if req.Mode == ModeRedirect {
		payload["confirmation"] = map[string]string{"type": "redirect", "return_url": req.ReturnURL, "locale": paymentLocale}
	} else {
		payload["confirmation"] = map[string]string{"type": "embedded", "locale": paymentLocale}
	}
	if req.SavePaymentMethod {
		payload["save_payment_method"] = true
	}
	return payload
}

// newIdempotenceKey — свой ключ на каждую попытку оплаты: токен виджета
// одноразовый и часовой, поэтому повторная попытка — это всегда новый платёж,
// а не переиспользование прежнего (тот же Idempotence-Key с другим телом даёт
// у ЮKassa 400).
func newIdempotenceKey(orderID string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return orderID + "-" + hex.EncodeToString(b[:])
}

// Create заводит платёж. Каждый вызов идёт со свежим Idempotence-Key: токен виджета
// одноразовый и часовой, поэтому повторная попытка оплаты — это всегда новый платёж,
// а не переиспользование прежнего.
func (y YooKassa) Create(req CreateRequest) (CreateResult, error) {
	if y.ShopID == "" || y.Secret == "" {
		return CreateResult{}, fmt.Errorf("yookassa: missing credentials")
	}
	raw, err := json.Marshal(createPayload(req))
	if err != nil {
		return CreateResult{}, err
	}

	httpReq, err := http.NewRequest(http.MethodPost, APIBaseURL+"/v3/payments", bytes.NewReader(raw))
	if err != nil {
		return CreateResult{}, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(y.ShopID + ":" + y.Secret))
	httpReq.Header.Set("Authorization", "Basic "+auth)
	httpReq.Header.Set("Content-Type", "application/json")
	idempo := req.IdempotenceKey
	if idempo == "" {
		idempo = newIdempotenceKey(req.OrderID)
	}
	httpReq.Header.Set("Idempotence-Key", idempo)

	resp, err := yooClient.Do(httpReq)
	if err != nil {
		return CreateResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return CreateResult{}, &APIError{Status: resp.StatusCode, Body: string(body)}
	}
	var out createPaymentRes
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CreateResult{}, err
	}
	res := CreateResult{PaymentID: out.ID, Status: out.Status}
	if out.Confirmation != nil {
		res.ConfirmationURL = out.Confirmation.ConfirmationURL
		res.ConfirmationToken = out.Confirmation.ConfirmationToken
	}
	return res, nil
}

// Payment is the subset of a YooKassa payment that a webhook must be checked against.
type Payment struct {
	ID       string
	Status   string // pending, waiting_for_capture, succeeded, canceled
	Paid     bool
	Amount   string // "990.00"
	Currency string
	OrderID  string // metadata.order_id
	Refunded string // refunded_amount.value; "" если возвратов не было. Полный возврат
	// НЕ меняет Status/Paid — платёж остаётся succeeded/true, растёт только эта сумма.

	// Metadata — все метки платежа как есть: у подписочных платежей вместо
	// order_id лежат subscription_id и flow.
	Metadata map[string]string
	// PaymentMethodID/PaymentMethodSaved — сохранённый способ оплаты. saved=true
	// появляется у первого платежа подписки (save_payment_method) — этот id и
	// используется для автосписаний.
	PaymentMethodID    string
	PaymentMethodSaved bool
}

// GetPayment reads the authoritative payment state from YooKassa. The webhook
// body proves nothing — the endpoint is public and anyone can POST one — so the
// payment is always re-read here before an order is marked paid.
func (y YooKassa) GetPayment(paymentID string) (Payment, error) {
	if y.ShopID == "" || y.Secret == "" {
		return Payment{}, fmt.Errorf("yookassa: missing credentials")
	}
	if paymentID == "" {
		return Payment{}, fmt.Errorf("yookassa: empty payment id")
	}

	req, err := http.NewRequest(http.MethodGet, APIBaseURL+"/v3/payments/"+url.PathEscape(paymentID), nil)
	if err != nil {
		return Payment{}, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(y.ShopID + ":" + y.Secret))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := yooClient.Do(req)
	if err != nil {
		return Payment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Payment{}, fmt.Errorf("yookassa: status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Paid   bool   `json:"paid"`
		Amount struct {
			Value    string `json:"value"`
			Currency string `json:"currency"`
		} `json:"amount"`
		RefundedAmount struct {
			Value string `json:"value"`
		} `json:"refunded_amount"`
		Metadata      map[string]any `json:"metadata"`
		PaymentMethod struct {
			ID    string `json:"id"`
			Saved bool   `json:"saved"`
		} `json:"payment_method"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Payment{}, err
	}
	meta := make(map[string]string, len(out.Metadata))
	for k, v := range out.Metadata {
		if s, ok := v.(string); ok {
			meta[k] = s
		}
	}
	return Payment{
		ID:                 out.ID,
		Status:             out.Status,
		Paid:               out.Paid,
		Amount:             out.Amount.Value,
		Currency:           out.Amount.Currency,
		OrderID:            meta["order_id"],
		Refunded:           out.RefundedAmount.Value,
		Metadata:           meta,
		PaymentMethodID:    out.PaymentMethod.ID,
		PaymentMethodSaved: out.PaymentMethod.Saved,
	}, nil
}

// Refund — созданный возврат. Статус приходит сразу; succeeded означает, что
// ЮKassa возврат уже провела, pending — что он в обработке.
type Refund struct {
	ID     string
	Status string // pending | succeeded | canceled
}

// CreateRefund возвращает деньги по платежу: POST /v3/refunds.
//
// Ключ идемпотентности ДЕТЕРМИНИРОВАННЫЙ (в отличие от newIdempotenceKey при
// создании платежа): вебхуки ЮKassa ретраятся, и повторный вызов обязан быть тем
// же действием, а не вторым возвратом тех же денег.
//
// valueRub — сумма в формате ЮKassa ("790.00"), не больше остатка по платежу.
func (y YooKassa) CreateRefund(paymentID, valueRub string) (Refund, error) {
	if y.ShopID == "" || y.Secret == "" {
		return Refund{}, fmt.Errorf("yookassa: missing credentials")
	}
	raw, err := json.Marshal(map[string]any{
		"payment_id": paymentID,
		"amount":     map[string]string{"value": valueRub, "currency": "RUB"},
	})
	if err != nil {
		return Refund{}, err
	}

	httpReq, err := http.NewRequest(http.MethodPost, APIBaseURL+"/v3/refunds", bytes.NewReader(raw))
	if err != nil {
		return Refund{}, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(y.ShopID + ":" + y.Secret))
	httpReq.Header.Set("Authorization", "Basic "+auth)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Idempotence-Key", "refund:"+paymentID)

	resp, err := yooClient.Do(httpReq)
	if err != nil {
		return Refund{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Refund{}, fmt.Errorf("yookassa: refund status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Refund{}, err
	}
	return Refund{ID: out.ID, Status: out.Status}, nil
}

func buildReceipt(valueRub string, r Receipt) map[string]any {
	customer := map[string]string{}
	if r.Email != "" {
		customer["email"] = r.Email
	}
	if r.Phone != "" {
		customer["phone"] = normalizePhone(r.Phone)
	}

	desc := r.Description
	if desc == "" {
		desc = "Персональная песня"
	}
	if len([]rune(desc)) > 128 {
		desc = string([]rune(desc)[:128])
	}

	vat := r.VatCode
	if vat < 1 || vat > 6 {
		vat = 1
	}

	return map[string]any{
		"customer": customer,
		"items": []map[string]any{
			{
				"description":     desc,
				"quantity":        "1.00",
				"amount":          map[string]string{"value": valueRub, "currency": "RUB"},
				"vat_code":        vat,
				"payment_mode":    "full_payment",
				"payment_subject": "service",
			},
		},
	}
}

// normalizePhone strips everything but digits and a leading +, as YooKassa
// expects ITU-T E.164 (e.g. 79001234567).
func normalizePhone(s string) string {
	var b []rune
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b = append(b, c)
		}
	}
	return string(b)
}
