package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/storage"
	"mezmo-song-com/internal/store"
)

// refundTestServer открывает Server с настроенными (фейковыми) ключами ЮKassa —
// достаточно для payment.canceled (гейт по orders.yoo_payment_id, без сети) и для
// refund.succeeded, если тест дополнительно подменит payment.APIBaseURL.
func refundTestServer(t *testing.T) *Server {
	t.Helper()
	s, _ := testServer(t)
	s.Config = config.Config{
		PublicAppURL:      "http://localhost:8080",
		YooKassaShopID:    "shop_test",
		YooKassaSecretKey: "secret_test",
	}
	return s
}

// paymentJSON строит тело ответа GET /v3/payments/{id}, как его отдаёт ЮKassa.
// refunded == "" — поле refunded_amount не сериализуется вовсе (как у платежа
// без возвратов); orderID == "" — metadata отсутствует.
func paymentJSON(id, status string, paid bool, amount, refunded, orderID string) map[string]any {
	m := map[string]any{
		"id":     id,
		"status": status,
		"paid":   paid,
		"amount": map[string]string{"value": amount, "currency": "RUB"},
	}
	if refunded != "" {
		m["refunded_amount"] = map[string]string{"value": refunded}
	}
	if orderID != "" {
		m["metadata"] = map[string]string{"order_id": orderID}
	}
	return m
}

// yooGetPaymentsServer поднимает httptest-заглушку GET /v3/payments/{id} и
// подменяет payment.APIBaseURL на неё (восстанавливается в t.Cleanup). Payment
// canceled-ветка не ходит в сеть вовсе, поэтому эта заглушка нужна только тестам
// на refund.succeeded.
func yooGetPaymentsServer(t *testing.T, byID map[string]map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		resp, ok := byID[id]
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
}

func postWebhook(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/webhooks/yookassa", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleYooWebhook(rec, req)
	return rec
}

// assertSingleJSONResponse проверяет, что тело ответа содержит РОВНО один
// JSON-объект. Ловит регресс, при котором ветка switch пишет свой writeJSON без
// return, и управление проваливается в общий writeJSON в конце
// handleYooWebhook — тогда тело содержит два JSON-объекта подряд, а настоящий
// http.ResponseWriter вдобавок пишет в лог "superfluous response.WriteHeader call".
// httptest.ResponseRecorder такую ошибку не логирует и не паникует, поэтому
// проверять нужно именно содержимое тела, а не rec.Code.
func assertSingleJSONResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("decode response body: %v (body=%s)", err, rec.Body.String())
	}
	if dec.More() {
		t.Fatalf("response body has more than one JSON value (double write): %s", rec.Body.String())
	}
}

// --- payment.canceled ---

// Заказ pending, id платежа из вебхука совпадает с текущим (orders.yoo_payment_id) —
// отмена применяется без единого сетевого вызова.
func TestYooWebhookCanceledMarksPendingOrder(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_c1", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_c1", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.canceled","object":{"id":"pay_1","metadata":{"order_id":"ord_c1"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	// Регресс: ветка "payment.canceled" писала writeJSON без return и проваливалась
	// в общий writeJSON в конце handleYooWebhook — ответ уходил дважды.
	assertSingleJSONResponse(t, rec)

	o, err := store.GetOrder(ctx, s.DB, "ord_c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentCanceled {
		t.Fatalf("payment_status = %q, want %q", o.PaymentStatus, store.PaymentCanceled)
	}
}

// Оплаченный заказ отменить вебхуком нельзя — устаревший платёж протухает сам,
// и payment.canceled по нему прилетает штатно даже после успешной оплаты.
func TestYooWebhookCanceledDoesNotTouchPaidOrder(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_c2", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_c2", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_c2"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.canceled","object":{"id":"pay_1","metadata":{"order_id":"ord_c2"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_c2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (unchanged)", o.PaymentStatus)
	}
}

// Устаревший (не текущий) платёж из истории заказа не отменяет заказ, даже если
// заказ ещё pending.
func TestYooWebhookCanceledIgnoresForeignPaymentID(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_c3", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_c3", "pay_1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_c3", "pay_2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	// Текущий платёж теперь pay_2; pay_1 — устаревший в истории.

	rec := postWebhook(s, `{"event":"payment.canceled","object":{"id":"pay_1","metadata":{"order_id":"ord_c3"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_c3")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPending {
		t.Fatalf("payment_status = %q, want pending (stale payment id must not cancel)", o.PaymentStatus)
	}
}

// --- refund.succeeded ---

// Единственный платёж в истории полностью возвращён (refunded_amount == amount) —
// живых успешных платежей не осталось, заказ становится refunded.
func TestRefundClosesAccessWhenNoPaymentLeft(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_r1", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_r1", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_r1"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_1": paymentJSON("pay_1", "succeeded", true, "990.00", "990.00", "ord_r1"),
	})

	rec := postWebhook(s, `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"pay_1","status":"succeeded","amount":{"value":"990.00","currency":"RUB"},"metadata":{"order_id":"ord_r1"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	// Регресс: ветка "refund.succeeded" (путь без живого платежа, после MarkRefunded)
	// писала writeJSON без return и проваливалась в общий writeJSON в конце
	// handleYooWebhook — ответ уходил дважды.
	assertSingleJSONResponse(t, rec)

	o, err := store.GetOrder(ctx, s.DB, "ord_r1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentRefunded {
		t.Fatalf("payment_status = %q, want %q", o.PaymentStatus, store.PaymentRefunded)
	}
	if o.RefundedKopecks != 99000 {
		t.Fatalf("refunded_kopecks = %d, want 99000", o.RefundedKopecks)
	}
}

// Главный сценарий правки: два платежа в истории, первый полностью возвращён,
// второй жив (succeeded, paid, refunded_amount == 0) — доступ сохраняется, заказ
// остаётся paid, refunded_kopecks фиксирует сумму первого.
func TestRefundKeepsAccessWhenAnotherPaymentAlive(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_r2", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_r2", "pay_1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_r2", "pay_2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_r2"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_1": paymentJSON("pay_1", "succeeded", true, "990.00", "990.00", "ord_r2"),
		"pay_2": paymentJSON("pay_2", "succeeded", true, "990.00", "", "ord_r2"),
	})

	rec := postWebhook(s, `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"pay_1","status":"succeeded","amount":{"value":"990.00","currency":"RUB"},"metadata":{"order_id":"ord_r2"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_r2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (второй платёж жив)", o.PaymentStatus)
	}
	if o.RefundedKopecks != 99000 {
		t.Fatalf("refunded_kopecks = %d, want 99000", o.RefundedKopecks)
	}
}

// Частичный возврат единственного платежа: refunded_amount < amount, платёж
// остаётся живым успешным — доступ сохраняется.
func TestPartialRefundKeepsAccess(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_r3", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_r3", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_r3"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_1": paymentJSON("pay_1", "succeeded", true, "990.00", "500.00", "ord_r3"),
	})

	rec := postWebhook(s, `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"pay_1","status":"succeeded","amount":{"value":"500.00","currency":"RUB"},"metadata":{"order_id":"ord_r3"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_r3")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (частичный возврат не закрывает доступ)", o.PaymentStatus)
	}
	if o.RefundedKopecks != 50000 {
		t.Fatalf("refunded_kopecks = %d, want 50000", o.RefundedKopecks)
	}
}

// В теле возврата нет metadata.order_id: заказ определяется по object.payment_id →
// GetPayment → metadata.order_id платежа.
func TestRefundResolvesOrderByPaymentWhenNoMetadata(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_r4", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_r4", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_r4"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_1": paymentJSON("pay_1", "succeeded", true, "990.00", "990.00", "ord_r4"),
	})

	// Нет metadata на объекте возврата — только payment_id.
	rec := postWebhook(s, `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"pay_1","status":"succeeded","amount":{"value":"990.00","currency":"RUB"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_r4")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentRefunded {
		t.Fatalf("payment_status = %q, want %q (order must be resolved via payment_id)", o.PaymentStatus, store.PaymentRefunded)
	}
}

// Платёж, которого нет в истории заказа, к заказу не относится — вебхук ничего
// не меняет, даже если сам платёж настоящий.
func TestRefundIgnoresUnknownPayment(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_r5", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_r5", "pay_real", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_r5"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_real":    paymentJSON("pay_real", "succeeded", true, "990.00", "", "ord_r5"),
		"pay_unknown": paymentJSON("pay_unknown", "succeeded", true, "990.00", "990.00", "ord_r5"),
	})

	rec := postWebhook(s, `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"pay_unknown","status":"succeeded","amount":{"value":"990.00","currency":"RUB"},"metadata":{"order_id":"ord_r5"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_r5")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (unknown payment must not affect order)", o.PaymentStatus)
	}
	if o.RefundedKopecks != 0 {
		t.Fatalf("refunded_kopecks = %d, want 0 (unrelated payment ignored)", o.RefundedKopecks)
	}
}

// Второй одинаковый вебхук (ретрай ЮKassa) не меняет ни статус, ни
// refunded_kopecks: сумма каждый раз пересчитывается из API, а не накапливается.
func TestRefundIsIdempotent(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_r6", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_r6", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_r6"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_1": paymentJSON("pay_1", "succeeded", true, "990.00", "990.00", "ord_r6"),
	})

	body := `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"pay_1","status":"succeeded","amount":{"value":"990.00","currency":"RUB"},"metadata":{"order_id":"ord_r6"}}}`
	if rec := postWebhook(s, body); rec.Code != 200 {
		t.Fatalf("first delivery: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if rec := postWebhook(s, body); rec.Code != 200 {
		t.Fatalf("second delivery (retry): status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_r6")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentRefunded {
		t.Fatalf("payment_status = %q, want %q", o.PaymentStatus, store.PaymentRefunded)
	}
	if o.RefundedKopecks != 99000 {
		t.Fatalf("refunded_kopecks = %d, want 99000 (must not double on retry)", o.RefundedKopecks)
	}
}

// --- Доступ после refunded/частичного возврата ---

// Заказ refunded не отдаёт аудио через прокси, даже если ключ в audio_urls валиден.
func TestRefundedOrderLosesAudioAccess(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_a1", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_a1", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_a1"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if err := store.SaveRelayResult(ctx, s.DB, "ord_a1", []string{"orders/ord_a1/track-1.mp3"}, "текст", "Песня"); err != nil {
		t.Fatalf("save relay result: %v", err)
	}
	if changed, err := store.MarkRefunded(ctx, s.DB, "ord_a1"); err != nil || !changed {
		t.Fatalf("mark refunded: changed=%v err=%v", changed, err)
	}

	req := httptest.NewRequest("GET", "/api/public/order/ord_a1/audio/0", nil)
	req.SetPathValue("id", "ord_a1")
	req.SetPathValue("index", "0")
	rec := httptest.NewRecorder()
	s.handleAudioProxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (refunded order must not stream audio)", rec.Code)
	}
}

// Заказ refunded не может уйти на доработку.
func TestRefundedOrderCannotRegenerate(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_a2", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_a2", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_a2"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if changed, err := store.MarkRefunded(ctx, s.DB, "ord_a2"); err != nil || !changed {
		t.Fatalf("mark refunded: changed=%v err=%v", changed, err)
	}

	req := httptest.NewRequest("POST", "/api/public/order/ord_a2/regenerate", strings.NewReader(`{"revisionType":"lyrics"}`))
	req.SetPathValue("id", "ord_a2")
	rec := httptest.NewRecorder()
	s.handleRegenerate(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["error"] != "not_paid" {
		t.Fatalf("error = %v, want not_paid", out["error"])
	}
}

// Заказ остаётся paid при частичном возврате (или возврате лишнего платежа) —
// доступ к аудио не закрывается самим фактом непустого refunded_kopecks.
// handleAudioProxy проходит гейт по payment_status и доходит до чтения из MinIO
// (что и требуется проверить: доступ не заблокирован на входе) — реальное
// хранилище в тесте недоступно (127.0.0.1:0), поэтому итоговый код 500, а не 404;
// именно разница между 404 (гейт) и 500 (гейт пройден, упало ниже) и является
// здесь проверяемым сигналом.
func TestRefundedButPaidOrderKeepsAudioAccess(t *testing.T) {
	s := refundTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_a3", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_a3", "pay_1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_a3"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if err := store.SaveRelayResult(ctx, s.DB, "ord_a3", []string{"orders/ord_a3/track-1.mp3"}, "текст", "Песня"); err != nil {
		t.Fatalf("save relay result: %v", err)
	}
	if err := store.SetRefundedKopecks(ctx, s.DB, "ord_a3", 50000); err != nil {
		t.Fatalf("set refunded kopecks: %v", err)
	}

	sc, err := storage.New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	s.Storage = sc

	req := httptest.NewRequest("GET", "/api/public/order/ord_a3/audio/0", nil)
	req.SetPathValue("id", "ord_a3")
	req.SetPathValue("index", "0")
	rec := httptest.NewRecorder()
	s.handleAudioProxy(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("status = 404, want access to pass the payment gate (paid order with partial refund must keep access)")
	}

	// Доработка тоже доступна.
	req2 := httptest.NewRequest("POST", "/api/public/order/ord_a3/regenerate", strings.NewReader(`{"revisionType":"lyrics"}`))
	req2.SetPathValue("id", "ord_a3")
	rec2 := httptest.NewRecorder()
	s.handleRegenerate(rec2, req2)
	if rec2.Code == 400 {
		var out map[string]string
		_ = json.Unmarshal(rec2.Body.Bytes(), &out)
		if out["error"] == "not_paid" {
			t.Fatalf("regenerate rejected as not_paid, want available for partially refunded but paid order")
		}
	}
}
