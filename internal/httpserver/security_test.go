package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

func testServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Server{DB: db, Config: config.Config{}}, db
}

// seedOrderDefaultCreatedAt — фиксированная дата "до отсечки" (см. orderRefCutoff в
// orderref.go), а не time.Now(). Это осознанный выбор T-004 review-1: почти все тесты,
// сеющие заказ через seedOrder, ходят в публичные ручки по голому id и рассчитывают на
// legacy-путь resolveOrderID; при CreatedAt = time.Now() они были бы зелёными только
// пока «сегодня» раньше orderRefCutoff и покраснели бы 2026-08-15 без единой правки
// кода. Фиксированная дата убирает зависимость от календаря: legacy-путь остаётся
// legacy-путём независимо от того, когда запущен go test.
var seedOrderDefaultCreatedAt = orderRefCutoff.Add(-24 * time.Hour)

func seedOrder(t *testing.T, db *sql.DB, id, paymentStatus string) {
	t.Helper()
	seedOrderAt(t, db, id, paymentStatus, seedOrderDefaultCreatedAt)
}

// seedOrderAt — вариант seedOrder с заданным CreatedAt: нужен тестам T-004 на границу
// orderRefCutoff (обратная совместимость голого id против ref).
func seedOrderAt(t *testing.T, db *sql.DB, id, paymentStatus string, createdAt time.Time) {
	t.Helper()
	o := store.Order{
		ID:               id,
		Status:           "created",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    paymentStatus,
		InputPayloadJSON: `{"recipientName":"Ира"}`,
		CustomerJSON:     `{"email":"a@b.ru"}`,
		BasePrice:        990,
		CreatedAt:        createdAt,
	}
	if err := store.CreateOrder(context.Background(), db, o); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// Служебный INTERNAL нельзя выставить себе через публичную ручку скидки: он снимает
// лимит доработок и убирает заказ из воронки.
func TestApplyDiscountRejectsUnknownCode(t *testing.T) {
	s, db := testServer(t)
	seedOrder(t, db, "ord_x", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_x/apply-discount",
		strings.NewReader(`{"discountCode":"internal"}`))
	req.SetPathValue("id", "ord_x")
	rec := httptest.NewRecorder()
	s.handleApplyDiscount(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	o, err := store.GetOrder(context.Background(), db, "ord_x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.DiscountCode.Valid && o.DiscountCode.String != "" {
		t.Fatalf("discount_code = %q, want empty", o.DiscountCode.String)
	}
}

func TestApplyDiscountAcceptsDemo10(t *testing.T) {
	s, db := testServer(t)
	seedOrder(t, db, "ord_y", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_y/apply-discount",
		strings.NewReader(`{"discountCode":"demo10"}`))
	req.SetPathValue("id", "ord_y")
	rec := httptest.NewRecorder()
	s.handleApplyDiscount(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	o, _ := store.GetOrder(context.Background(), db, "ord_y")
	if o.DiscountCode.String != "DEMO10" || o.DiscountAmount.Int64 != 99 {
		t.Fatalf("got code=%q amount=%d, want DEMO10/99", o.DiscountCode.String, o.DiscountAmount.Int64)
	}
}

// Доработка тратит деньги на генерацию, поэтому неоплаченный заказ её не получает
// и счётчик доработок не двигается.
func TestRegenerateRequiresPaidOrder(t *testing.T) {
	s, db := testServer(t)
	seedOrder(t, db, "ord_z", "pending")

	req := httptest.NewRequest("POST", "/api/public/order/ord_z/regenerate",
		strings.NewReader(`{"revisionType":"lyrics"}`))
	req.SetPathValue("id", "ord_z")
	rec := httptest.NewRecorder()
	s.handleRegenerate(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	o, _ := store.GetOrder(context.Background(), db, "ord_z")
	if o.RegenerationCount != 0 {
		t.Fatalf("regeneration_count = %d, want 0", o.RegenerationCount)
	}
}

// Сверка вебхука с ответом API ЮKassa. Каждый отказ здесь — сценарий, при котором
// заказ раньше становился оплаченным без денег.
func TestVerifyYooPayment(t *testing.T) {
	// verifyYooPayment — метод: сумма допников приходит из настроек, а не из
	// константы в коде, поэтому нужен Server с БД.
	s, _ := testServer(t)
	ctx := t.Context()
	order := store.Order{
		ID:            "ord_1",
		BasePrice:     990,
		YooPaymentID:  sql.NullString{String: "pay_1", Valid: true},
		PaymentStatus: "pending",
	}
	ok := payment.Payment{Status: "succeeded", Paid: true, Amount: "990.00", Currency: "RUB", OrderID: "ord_1"}

	cases := []struct {
		name    string
		p       payment.Payment
		o       store.Order
		payment string
		want    string
	}{
		{"настоящая оплата", ok, order, "pay_1", ""},
		{"платёж ещё не прошёл", payment.Payment{Status: "pending", Paid: false, Amount: "990.00", Currency: "RUB", OrderID: "ord_1"}, order, "pay_1", "not_succeeded"},
		{"отменённый платёж", payment.Payment{Status: "canceled", Paid: false, Amount: "990.00", Currency: "RUB", OrderID: "ord_1"}, order, "pay_1", "not_succeeded"},
		{"чужой платёж вместо нашего", ok, order, "pay_other", "payment_id_mismatch"},
		{"платёж заведён под другой заказ", payment.Payment{Status: "succeeded", Paid: true, Amount: "990.00", Currency: "RUB", OrderID: "ord_2"}, order, "pay_1", "order_mismatch"},
		{"оплачено меньше цены", payment.Payment{Status: "succeeded", Paid: true, Amount: "1.00", Currency: "RUB", OrderID: "ord_1"}, order, "pay_1", "amount_too_small"},
		{"валюта не рубли", payment.Payment{Status: "succeeded", Paid: true, Amount: "990.00", Currency: "KZT", OrderID: "ord_1"}, order, "pay_1", "currency_mismatch"},
		{"сумма не парсится", payment.Payment{Status: "succeeded", Paid: true, Amount: "много", Currency: "RUB", OrderID: "ord_1"}, order, "pay_1", "amount_unparsable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.verifyYooPayment(ctx, c.p, c.o, c.payment); got != c.want {
				t.Fatalf("verifyYooPayment = %q, want %q", got, c.want)
			}
		})
	}
}

// Скидка уменьшает ожидаемую сумму: 990 − 99 = 891.
func TestVerifyYooPaymentWithDiscount(t *testing.T) {
	s, _ := testServer(t)
	ctx := t.Context()
	o := store.Order{
		ID:             "ord_1",
		BasePrice:      990,
		DiscountAmount: sql.NullInt64{Int64: 99, Valid: true},
	}
	p := payment.Payment{Status: "succeeded", Paid: true, Amount: "891.00", Currency: "RUB", OrderID: "ord_1"}
	if got := s.verifyYooPayment(ctx, p, o, "pay_1"); got != "" {
		t.Fatalf("verifyYooPayment = %q, want ok", got)
	}
	p.Amount = "890.99"
	if got := s.verifyYooPayment(ctx, p, o, "pay_1"); got != "amount_too_small" {
		t.Fatalf("verifyYooPayment = %q, want amount_too_small", got)
	}
}

func TestRublesToKopecks(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"990.00", 99000, true},
		{"990", 99000, true},
		{"990.5", 99050, true},
		{"0.01", 1, true},
		{"", 0, false},
		{"990.000", 0, false},
		{"-990.00", 0, false},
		{"много", 0, false},
	}
	for _, c := range cases {
		got, ok := rublesToKopecks(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("rublesToKopecks(%q) = %d,%v; want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Без ключей ЮKassa вебхук проверить нечем — заказ оплаченным не становится.
// Раньше именно этот путь помечал заказ оплаченным по одному лишь телу запроса.
func TestYooWebhookWithoutCredentialsDoesNotMarkPaid(t *testing.T) {
	s, db := testServer(t)
	seedOrder(t, db, "ord_w", "pending")

	body := `{"event":"payment.succeeded","object":{"id":"pay_fake","metadata":{"order_id":"ord_w"}}}`
	req := httptest.NewRequest("POST", "/api/webhooks/yookassa", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleYooWebhook(rec, req)

	o, err := store.GetOrder(context.Background(), db, "ord_w")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != "pending" {
		t.Fatalf("payment_status = %q, want pending", o.PaymentStatus)
	}
	if o.Status != "created" {
		t.Fatalf("status = %q, want created (генерация не должна стартовать)", o.Status)
	}
}

// Возврат не должен считаться выручкой (T-003), но обязан остаться виден оператору:
// отдельная строка в purchases[] с refunded=true и отдельный счётчик refunds в daily.
//
// В отличие от seedOrder, здесь CreatedAt должен быть настоящим time.Now(): проверяется
// окно ?days=7 в handleAdminAnalytics (store.ListOrdersSince), которое само считается от
// реального «сейчас» — это не связано с orderRefCutoff и не является календарной бомбой
// того же вида (окно скользит вместе с датой прогона теста).
func TestAdminAnalyticsRefundNotCountedAsRevenue(t *testing.T) {
	s, db := testServer(t)
	seedOrderAt(t, db, "ord_paid", store.PaymentPaid, time.Now())
	seedOrderAt(t, db, "ord_refunded", store.PaymentRefunded, time.Now())

	req := httptest.NewRequest("GET", "/api/admin/analytics?days=7", nil)
	rec := httptest.NewRecorder()
	s.handleAdminAnalytics(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Purchases []struct {
			OrderID       string `json:"orderId"`
			PaymentStatus string `json:"paymentStatus"`
			Refunded      bool   `json:"refunded"`
		} `json:"purchases"`
		Daily []struct {
			Purchases       int `json:"purchases"`
			Revenue         int `json:"revenue"`
			Refunds         int `json:"refunds"`
			RefundedRevenue int `json:"refundedRevenue"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Daily) != 1 {
		t.Fatalf("daily len = %d, want 1: %+v", len(resp.Daily), resp.Daily)
	}
	d := resp.Daily[0]
	if d.Revenue != 990 {
		t.Fatalf("daily[0].revenue = %d, want 990", d.Revenue)
	}
	if d.Purchases != 1 {
		t.Fatalf("daily[0].purchases = %d, want 1", d.Purchases)
	}
	if d.Refunds != 1 {
		t.Fatalf("daily[0].refunds = %d, want 1", d.Refunds)
	}
	if d.RefundedRevenue != 990 {
		t.Fatalf("daily[0].refundedRevenue = %d, want 990", d.RefundedRevenue)
	}

	var found bool
	for _, p := range resp.Purchases {
		if p.OrderID == "ord_refunded" {
			found = true
			if !p.Refunded {
				t.Fatalf("purchases[%s].refunded = false, want true", p.OrderID)
			}
			if p.PaymentStatus != store.PaymentRefunded {
				t.Fatalf("purchases[%s].paymentStatus = %q, want %q", p.OrderID, p.PaymentStatus, store.PaymentRefunded)
			}
		}
	}
	if !found {
		t.Fatalf("purchases[] does not contain refunded order: %+v", resp.Purchases)
	}
}
