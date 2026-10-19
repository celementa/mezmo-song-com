package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mezmo-song-com/internal/store"
)

// yooPaymentsByID — заглушка GET /v3/payments/{id}: ответ на каждый id задаёт
// тест. Считает обращения, чтобы проверять, что сверка не трогает лишнего.
func yooPaymentsByID(t *testing.T, byID map[string]map[string]any) (*Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		resp, ok := byID[r.PathValue("id")]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)
	s, _ := testServer(t)
	s.Config.YooKassaShopID = "shop_test"
	s.Config.YooKassaSecretKey = "secret_test"
	s.Config.PublicAppURL = "http://localhost:8080"
	return s, &calls
}

func succeededPayment(id, orderID, amount string) map[string]any {
	return map[string]any{
		"id":       id,
		"status":   "succeeded",
		"paid":     true,
		"amount":   map[string]string{"value": amount, "currency": "RUB"},
		"metadata": map[string]string{"order_id": orderID},
	}
}

// Вебхук потерян: заказ pending, а в ЮKassa платёж succeeded. Сверка обязана
// провести оплату тем же путём, что и вебхук.
func TestReconcileSettlesPaidOrderMissedByWebhook(t *testing.T) {
	s, calls := yooPaymentsByID(t, map[string]map[string]any{
		"pay_lost": succeededPayment("pay_lost", "ord_lost", "990.00"),
	})
	ctx := context.Background()
	seedOrderAt(t, s.DB, "ord_lost", "pending", time.Now().Add(-30*time.Minute))
	if err := store.AddPaymentID(ctx, s.DB, "ord_lost", "pay_lost", ""); err != nil {
		t.Fatal(err)
	}

	if n := s.reconcilePayments(ctx, time.Now()); n != 1 {
		t.Fatalf("settled = %d, want 1", n)
	}
	o, err := store.GetOrder(ctx, s.DB, "ord_lost")
	if err != nil {
		t.Fatal(err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid", o.PaymentStatus)
	}
	if !o.ServerTrackedAt.Valid {
		t.Fatalf("server_tracked_at not set: conversions bus must fire from reconcile too")
	}
	// Повторный прогон — заказ уже paid, в выборку не попадает, API не дёргается.
	before := calls.Load()
	if n := s.reconcilePayments(ctx, time.Now()); n != 0 {
		t.Fatalf("second run settled = %d, want 0", n)
	}
	if calls.Load() != before {
		t.Fatalf("second run hit YooKassa API %d time(s), want 0", calls.Load()-before)
	}
}

// Свежий заказ (моложе reconcileMinAge) ещё может подтвердить штатный вебхук —
// сверка его не опрашивает.
func TestReconcileSkipsFreshOrders(t *testing.T) {
	s, calls := yooPaymentsByID(t, map[string]map[string]any{
		"pay_fresh": succeededPayment("pay_fresh", "ord_fresh", "990.00"),
	})
	ctx := context.Background()
	seedOrderAt(t, s.DB, "ord_fresh", "pending", time.Now())
	if err := store.AddPaymentID(ctx, s.DB, "ord_fresh", "pay_fresh", ""); err != nil {
		t.Fatal(err)
	}
	if n := s.reconcilePayments(ctx, time.Now()); n != 0 {
		t.Fatalf("settled = %d, want 0 for a fresh order", n)
	}
	if calls.Load() != 0 {
		t.Fatalf("fresh order hit YooKassa API %d time(s), want 0", calls.Load())
	}
}

// Чужой или недоплаченный платёж сверка отклоняет так же, как вебхук: заказ
// остаётся pending.
func TestReconcileDoesNotSettleUnderpaidPayment(t *testing.T) {
	s, _ := yooPaymentsByID(t, map[string]map[string]any{
		"pay_small": succeededPayment("pay_small", "ord_small", "100.00"),
	})
	ctx := context.Background()
	seedOrderAt(t, s.DB, "ord_small", "pending", time.Now().Add(-time.Hour))
	if err := store.AddPaymentID(ctx, s.DB, "ord_small", "pay_small", ""); err != nil {
		t.Fatal(err)
	}
	if n := s.reconcilePayments(ctx, time.Now()); n != 0 {
		t.Fatalf("settled = %d, want 0", n)
	}
	o, _ := store.GetOrder(ctx, s.DB, "ord_small")
	if o.PaymentStatus != store.PaymentPending {
		t.Fatalf("payment_status = %q, want pending", o.PaymentStatus)
	}
}

// Все платежи заказа ЮKassa отменила, а payment.canceled не дошёл — сверка
// закрывает заказ, чтобы не опрашивать его неделю.
func TestReconcileCancelsOrderWhenAllPaymentsCanceled(t *testing.T) {
	s, _ := yooPaymentsByID(t, map[string]map[string]any{
		"pay_c1": {"id": "pay_c1", "status": "canceled", "paid": false},
		"pay_c2": {"id": "pay_c2", "status": "canceled", "paid": false},
	})
	ctx := context.Background()
	seedOrderAt(t, s.DB, "ord_c", "pending", time.Now().Add(-2*time.Hour))
	for _, id := range []string{"pay_c1", "pay_c2"} {
		if err := store.AddPaymentID(ctx, s.DB, "ord_c", id, ""); err != nil {
			t.Fatal(err)
		}
	}
	s.reconcilePayments(ctx, time.Now())
	o, _ := store.GetOrder(ctx, s.DB, "ord_c")
	if o.PaymentStatus != store.PaymentCanceled {
		t.Fatalf("payment_status = %q, want canceled", o.PaymentStatus)
	}
}

// Первый платёж подписки оплачен, вебхук не дошёл: подписка pending с
// pending_payment_id. Сверка активирует её и записывает платёж.
func TestReconcileSettlesSubscriptionPayment(t *testing.T) {
	ctx := context.Background()
	s, _ := testServer(t)
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, "sub@test.ru", "Тест")
	if err != nil {
		t.Fatal(err)
	}
	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_sub": {
			"id":       "pay_sub",
			"status":   "succeeded",
			"paid":     true,
			"amount":   map[string]string{"value": "990.00", "currency": "RUB"},
			"metadata": map[string]string{"subscription_id": sub.ID, "flow": store.SubFlowFirst},
		},
	})
	s.Config.YooKassaShopID = "shop_test"
	s.Config.YooKassaSecretKey = "secret_test"
	if err := store.SetSubscriptionPendingPayment(ctx, s.DB, sub.ID, "pay_sub"); err != nil {
		t.Fatal(err)
	}

	if n := s.reconcilePayments(ctx, time.Now()); n != 1 {
		t.Fatalf("settled = %d, want 1", n)
	}
	got, err := store.GetSubscription(ctx, s.DB, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.SubActive {
		t.Fatalf("status = %q, want active", got.Status)
	}
	if got.PendingPaymentID.Valid && got.PendingPaymentID.String != "" {
		t.Fatalf("pending_payment_id = %q, want cleared after settle", got.PendingPaymentID.String)
	}
	// Второй прогон: платёж уже записан, ожидающих нет.
	if n := s.reconcilePayments(ctx, time.Now()); n != 0 {
		t.Fatalf("second run settled = %d, want 0", n)
	}
}
