package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

const testUnitPaySecret = "up-secret"

func newUnitPayTestServer(t *testing.T, cfg config.Config) (*Server, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg.WebhookSecret = "secret"
	cfg.UnitPayProjectID = "445942"
	cfg.UnitPaySecretKey = testUnitPaySecret
	if cfg.UnitPayCurrency == "" {
		cfg.UnitPayCurrency = "USD"
	}
	// Пустой payload: анкета не заполнена, вебхук генерацию не запускает —
	// тест проверяет оплату, без фоновых горутин генерации.
	o := store.Order{
		ID:               "ord_up",
		Status:           "created",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    store.PaymentPending,
		InputPayloadJSON: `{}`,
		CustomerJSON:     `{"email":""}`,
		BasePrice:        25,
		CreatedAt:        time.Now(),
	}
	if err := store.CreateOrder(context.Background(), db, o); err != nil {
		t.Fatalf("create: %v", err)
	}
	return &Server{DB: db, Config: cfg}, o.ID
}

// unitpayNotify шлёт уведомление так, как его шлёт UnitPay: GET с params[...]
// и подписью; ip — адрес отправителя.
func unitpayNotify(t *testing.T, s *Server, method, ip string, params map[string]string, sign bool) map[string]any {
	t.Helper()
	if sign {
		params["signature"] = payment.UnitPaySignature(method, params, testUnitPaySecret)
	}
	q := url.Values{"method": {method}}
	for k, v := range params {
		q.Set("params["+k+"]", v)
	}
	req := httptest.NewRequest("GET", "/api/webhooks/unitpay?"+q.Encode(), nil)
	req.RemoteAddr = ip + ":40000"
	rec := httptest.NewRecorder()
	s.handleUnitPayWebhook(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("ответ не JSON: %q", rec.Body.String())
	}
	return out
}

func payParams(orderID, sum, currency string) map[string]string {
	return map[string]string{
		"account":       orderID,
		"unitpayId":     "777",
		"orderSum":      sum,
		"orderCurrency": currency,
		"payerSum":      sum,
		"payerCurrency": currency,
		"projectId":     "445942",
		"paymentType":   "card",
		"date":          "2026-10-19 12:00:00",
		"test":          "0",
	}
}

func paymentStatus(t *testing.T, s *Server, id string) string {
	t.Helper()
	o, err := store.GetOrder(context.Background(), s.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	return o.PaymentStatus
}

func TestUnitPayPayMarksOrderPaidOnce(t *testing.T) {
	s, id := newUnitPayTestServer(t, config.Config{})
	ip := payment.UnitPayIPs[0]

	out := unitpayNotify(t, s, "check", ip, payParams(id, "25.00", "USD"), true)
	if out["result"] == nil {
		t.Fatalf("check отклонён: %v", out)
	}
	if got := paymentStatus(t, s, id); got != store.PaymentPending {
		t.Fatalf("check не должен оплачивать заказ, status=%s", got)
	}

	out = unitpayNotify(t, s, "pay", ip, payParams(id, "25.00", "USD"), true)
	if out["result"] == nil {
		t.Fatalf("pay отклонён: %v", out)
	}
	if got := paymentStatus(t, s, id); got != store.PaymentPaid {
		t.Fatalf("status = %s, want paid", got)
	}
	o, _ := store.GetOrder(context.Background(), s.DB, id)
	if !store.KnownPaymentID(o, "777") {
		t.Fatal("unitpayId не попал в историю платежей заказа")
	}

	// Повтор того же уведомления — успешный ответ без повторных эффектов.
	if out := unitpayNotify(t, s, "pay", ip, payParams(id, "25.00", "USD"), true); out["result"] == nil {
		t.Fatalf("повтор pay отклонён: %v", out)
	}
}

func TestUnitPayRejectsForgedNotifications(t *testing.T) {
	cases := []struct {
		name   string
		ip     string
		params map[string]string
		sign   bool
	}{
		{"без подписи", payment.UnitPayIPs[0], payParams("ord_up", "25.00", "USD"), false},
		{"чужой IP", "203.0.113.7", payParams("ord_up", "25.00", "USD"), true},
		{"меньшая сумма", payment.UnitPayIPs[0], payParams("ord_up", "1.00", "USD"), true},
		{"другая валюта", payment.UnitPayIPs[0], payParams("ord_up", "25.00", "RUB"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, id := newUnitPayTestServer(t, config.Config{})
			out := unitpayNotify(t, s, "pay", tc.ip, tc.params, tc.sign)
			if out["error"] == nil {
				t.Fatalf("уведомление принято: %v", out)
			}
			if got := paymentStatus(t, s, id); got != store.PaymentPending {
				t.Fatalf("status = %s, want pending", got)
			}
		})
	}
}

// Тестовый режим UnitPay принимает только RUB: $25 выставляется и сверяется
// как 25 × 85.5 = 2137.5 → 2138 ₽.
func TestUnitPayTestModeSettlesInRubles(t *testing.T) {
	s, id := newUnitPayTestServer(t, config.Config{UnitPayTest: true, UnitPayTestRubRate: 85.5})
	o, _ := store.GetOrder(context.Background(), s.DB, id)
	if sum, cur := s.unitpayAmount(context.Background(), o); sum != "2138.00" || cur != "RUB" {
		t.Fatalf("amount = %s %s, want 2138.00 RUB", sum, cur)
	}
	out := unitpayNotify(t, s, "pay", payment.UnitPayIPs[1], payParams(id, "2138.00", "RUB"), true)
	if out["result"] == nil {
		t.Fatalf("pay отклонён: %v", out)
	}
	if got := paymentStatus(t, s, id); got != store.PaymentPaid {
		t.Fatalf("status = %s, want paid", got)
	}
}

// UnitPay подписывает уведомления боевым ключом проекта и в тестовом режиме,
// когда API ходит с тестовым: при заданном ключе уведомлений проверка идёт им.
func TestUnitPayVerifiesWithNotifyKey(t *testing.T) {
	s, id := newUnitPayTestServer(t, config.Config{})
	s.Config.UnitPaySecretKey = "sk_test_api"
	s.Config.UnitPayNotifySecretKey = testUnitPaySecret
	if out := unitpayNotify(t, s, "check", payment.UnitPayIPs[0], payParams(id, "25.00", "USD"), true); out["result"] == nil {
		t.Fatalf("уведомление с подписью ключом уведомлений отклонено: %v", out)
	}

	s.Config.UnitPaySecretKey = testUnitPaySecret
	s.Config.UnitPayNotifySecretKey = "other"
	if out := unitpayNotify(t, s, "check", payment.UnitPayIPs[0], payParams(id, "25.00", "USD"), true); out["error"] == nil {
		t.Fatalf("при заданном ключе уведомлений принята подпись ключом API: %v", out)
	}
}

// Виджет — та же форма с теми же параметрами на хосте виджета; чужой адрес
// формы виджетом не открывается, остаётся только переход.
func TestUnitPayStartResponseWidget(t *testing.T) {
	out := unitpayStartResponse("https://unitpay.money/pay/445942-07a7f/card?account=ord_up&signature=abc&test=1", "https://mezmo-song.com/order/r1", true)
	if got := out["widgetUrl"]; got != "https://widget.unitpay.money/pay/445942-07a7f/card?account=ord_up&signature=abc&test=1" {
		t.Fatalf("widgetUrl = %v", got)
	}
	if out["returnUrl"] != "https://mezmo-song.com/order/r1" {
		t.Fatalf("returnUrl = %v", out["returnUrl"])
	}
	if out := unitpayStartResponse("https://example.com/pay", "x", true); out["widgetUrl"] != nil {
		t.Fatalf("виджет для чужого адреса: %v", out)
	}
	if out := unitpayStartResponse("https://unitpay.money/pay/x/card?test=1", "x", false); out["widgetUrl"] != nil || out["confirmationUrl"] == nil {
		t.Fatalf("виджет в тестовом режиме: %v", out)
	}
}
