// Stripe — международная ветка mezmo-song.com: Checkout Session в режиме
// embedded_page (встроенная форма на нашем домене), вебхук и подписки.
//
// Клиент намеренно на net/http, без stripe-go: проект уже так работает с
// ЮKassa и UnitPay, а нужная часть API — четыре запроса и проверка подписи.
package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// StripeAPIBaseURL — база API. Переменная, а не константа: тесты подменяют её.
var StripeAPIBaseURL = "https://api.stripe.com"

var stripeClient = &http.Client{Timeout: 30 * time.Second}

type Stripe struct {
	SecretKey string
}

// StripeAPIError — отказ API (4xx/5xx). Отдельный тип, чтобы вызывающий
// отличал отказ от сетевого сбоя.
type StripeAPIError struct {
	Status int
	Body   string
}

func (e *StripeAPIError) Error() string {
	return fmt.Sprintf("stripe: status %d: %s", e.Status, e.Body)
}

const (
	StripeModePayment      = "payment"
	StripeModeSubscription = "subscription"
)

// StripeCheckoutRequest — параметры сессии. Amount не передаётся: сумма живёт
// в price_… из Dashboard, чтобы цена на сайте и в Stripe не разъехались.
type StripeCheckoutRequest struct {
	OrderID       string
	Mode          string
	PriceID       string
	CustomerEmail string
	ReturnURL     string
	Locale        string // ru | en
	TrialDays     int    // >0 — первый платёж подписки через N дней (0 ₽ сегодня)
	// SavePaymentMethod — сохранить карту для будущих списаний (разовая
	// покупка): Checkout создаёт Customer, а PaymentIntent — off_session setup.
	SavePaymentMethod bool
	Metadata          map[string]string
	// IdempotencyKey — детерминированный ключ на попытку: двойной клик не
	// создаёт две сессии.
	IdempotencyKey string
}

type StripeCheckoutSession struct {
	ID            string            `json:"id"`
	Mode          string            `json:"mode"`
	Status        string            `json:"status"`         // open, complete, expired
	PaymentStatus string            `json:"payment_status"` // paid, unpaid, no_payment_required
	Currency      string            `json:"currency"`       // usd
	AmountTotal   int64             `json:"amount_total"`   // в минорных единицах
	ClientSecret  string            `json:"client_secret"`  // для embedded_page
	URL           string            `json:"url"`            // только hosted
	Metadata      map[string]string `json:"metadata"`
	Subscription  string            `json:"subscription"`
	Customer      string            `json:"customer"`
	CustomerEmail string            `json:"customer_email"`
	ClientRefID   string            `json:"client_reference_id"`
	PaymentIntent string            `json:"payment_intent"`
	Livemode      bool              `json:"livemode"`
	ExpiresAt     int64             `json:"expires_at"`
}

func (s Stripe) base() string {
	if StripeAPIBaseURL != "" {
		return strings.TrimSuffix(StripeAPIBaseURL, "/")
	}
	return "https://api.stripe.com"
}

func (s Stripe) do(method, path string, form url.Values, idempotencyKey string) ([]byte, error) {
	if s.SecretKey == "" {
		return nil, fmt.Errorf("stripe: missing secret key")
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, s.base()+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.SecretKey, "")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := stripeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, &StripeAPIError{Status: resp.StatusCode, Body: string(raw)}
	}
	return raw, nil
}

// CreateCheckoutSession создаёт сессию встроенной формы. Возвращает
// client_secret — его монтирует Stripe.js на фронте.
func (s Stripe) CreateCheckoutSession(req StripeCheckoutRequest) (StripeCheckoutSession, error) {
	if req.Mode != StripeModePayment && req.Mode != StripeModeSubscription {
		return StripeCheckoutSession{}, fmt.Errorf("stripe: unknown mode %q", req.Mode)
	}
	if strings.TrimSpace(req.PriceID) == "" {
		return StripeCheckoutSession{}, fmt.Errorf("stripe: missing price id")
	}
	if strings.TrimSpace(req.ReturnURL) == "" {
		return StripeCheckoutSession{}, fmt.Errorf("stripe: missing return_url")
	}

	form := url.Values{}
	form.Set("mode", req.Mode)
	form.Set("ui_mode", "embedded_page")
	form.Set("line_items[0][price]", req.PriceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("return_url", req.ReturnURL)
	form.Set("client_reference_id", req.OrderID)
	form.Set("locale", firstNonEmpty(req.Locale, "auto"))
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", v)
	}
	// order_id дублируется в metadata платежа/подписки: вебхук invoice.paid
	// приходит без session, и заказ по нему иначе не найти.
	if req.Mode == StripeModeSubscription {
		form.Set("subscription_data[metadata][order_id]", req.OrderID)
		if req.TrialDays > 0 {
			form.Set("subscription_data[trial_period_days]", strconv.Itoa(req.TrialDays))
		}
	} else {
		form.Set("payment_intent_data[metadata][order_id]", req.OrderID)
		if req.SavePaymentMethod {
			// Customer нужен, чтобы после разовой оплаты привязать подписку
			// без повторного ввода карты. setup_future_usage просит Stripe
			// сохранить способ оплаты для off-session списаний.
			form.Set("customer_creation", "always")
			form.Set("payment_intent_data[setup_future_usage]", "off_session")
		}
	}
	if email := strings.TrimSpace(req.CustomerEmail); email != "" {
		form.Set("customer_email", email)
	}

	raw, err := s.do(http.MethodPost, "/v1/checkout/sessions", form, req.IdempotencyKey)
	if err != nil {
		return StripeCheckoutSession{}, err
	}
	var out StripeCheckoutSession
	if err := json.Unmarshal(raw, &out); err != nil {
		return StripeCheckoutSession{}, err
	}
	if out.ClientSecret == "" {
		return StripeCheckoutSession{}, fmt.Errorf("stripe: session %s has no client_secret", out.ID)
	}
	return out, nil
}

// GetCheckoutSession перечитывает сессию из API. Вебхук доверяет только
// подписи, но сумму, валюту и статус обязан проверить по API — тело вебхука
// не доказывает, что деньги пришли.
func (s Stripe) GetCheckoutSession(id string) (StripeCheckoutSession, error) {
	raw, err := s.do(http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(id), nil, "")
	if err != nil {
		return StripeCheckoutSession{}, err
	}
	var out StripeCheckoutSession
	if err := json.Unmarshal(raw, &out); err != nil {
		return StripeCheckoutSession{}, err
	}
	return out, nil
}

// StripeSubscription — минимальная часть объекта подписки, нужная для доступа:
// статус и конец оплаченного периода.
type StripeSubscription struct {
	ID               string            `json:"id"`
	Status           string            `json:"status"` // active, trialing, past_due, canceled…
	Currency         string            `json:"currency"`
	CurrentPeriodEnd int64             `json:"current_period_end"`
	CancelAt         int64             `json:"cancel_at"`
	Metadata         map[string]string `json:"metadata"`
	Customer         string            `json:"customer"`
}

func (s Stripe) GetSubscription(id string) (StripeSubscription, error) {
	raw, err := s.do(http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, "")
	if err != nil {
		return StripeSubscription{}, err
	}
	var out StripeSubscription
	if err := json.Unmarshal(raw, &out); err != nil {
		return StripeSubscription{}, err
	}
	return out, nil
}

func (s Stripe) CancelSubscription(id string) error {
	form := url.Values{}
	form.Set("cancel_at_period_end", "true")
	_, err := s.do(http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), form, "")
	return err
}

// VerifyStripeSignature проверяет заголовок Stripe-Signature: HMAC-SHA256 от
// "t.payload" с секретом endpoint. Допуск по времени — 5 минут: этого хватает
// на ретраи Stripe и не даёт переигрывать старый запрос бесконечно.
func VerifyStripeSignature(payload []byte, header, secret string, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("stripe: webhook secret is empty")
	}
	var timestamp int64
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp, _ = strconv.ParseInt(value, 10, 64)
		case "v1":
			signatures = append(signatures, value)
		}
	}
	if timestamp == 0 || len(signatures) == 0 {
		return fmt.Errorf("stripe: malformed signature header")
	}
	if delta := now.Unix() - timestamp; delta > 300 || delta < -300 {
		return fmt.Errorf("stripe: signature timestamp outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	for _, got := range signatures {
		if hmac.Equal([]byte(got), []byte(want)) {
			return nil
		}
	}
	return fmt.Errorf("stripe: signature mismatch")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// StripePaymentIntent — часть PaymentIntent, нужная после оплаты: customer и
// сохранённый способ оплаты. Checkout Session отдаёт customer, но payment_method
// надёжнее взять из PaymentIntent.
type StripePaymentIntent struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Customer      string `json:"customer"`
	PaymentMethod string `json:"payment_method"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
}

func (s Stripe) GetPaymentIntent(id string) (StripePaymentIntent, error) {
	raw, err := s.do(http.MethodGet, "/v1/payment_intents/"+url.PathEscape(id), nil, "")
	if err != nil {
		return StripePaymentIntent{}, err
	}
	var out StripePaymentIntent
	if err := json.Unmarshal(raw, &out); err != nil {
		return StripePaymentIntent{}, err
	}
	return out, nil
}

// StripeSubscriptionRequest — прямая подписка по сохранённой карте, без
// повторной формы. TrialDays=30 даёт «0 ₽ сегодня».
type StripeSubscriptionRequest struct {
	Customer             string
	PriceID              string
	DefaultPaymentMethod string
	TrialDays            int
	Metadata             map[string]string
	IdempotencyKey       string
}

func (s Stripe) CreateSubscription(req StripeSubscriptionRequest) (StripeSubscription, error) {
	if req.Customer == "" || req.PriceID == "" {
		return StripeSubscription{}, fmt.Errorf("stripe: customer and price are required")
	}
	form := url.Values{}
	form.Set("customer", req.Customer)
	form.Set("items[0][price]", req.PriceID)
	if req.DefaultPaymentMethod != "" {
		form.Set("default_payment_method", req.DefaultPaymentMethod)
	}
	if req.TrialDays > 0 {
		form.Set("trial_period_days", strconv.Itoa(req.TrialDays))
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", v)
	}
	raw, err := s.do(http.MethodPost, "/v1/subscriptions", form, req.IdempotencyKey)
	if err != nil {
		return StripeSubscription{}, err
	}
	var out StripeSubscription
	if err := json.Unmarshal(raw, &out); err != nil {
		return StripeSubscription{}, err
	}
	return out, nil
}
