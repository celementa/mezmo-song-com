package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

// setAPIBaseURL подменяет payment.APIBaseURL и восстанавливает исходное значение
// через t.Cleanup. Это общий мутируемый пакетный var — каждая подмена обязана
// сохранять и возвращать предыдущее значение, иначе тесты становятся флаки при
// параллельном запуске (t.Parallel()) или в другом порядке.
func setAPIBaseURL(t *testing.T, url string) {
	t.Helper()
	prev := payment.APIBaseURL
	payment.APIBaseURL = url
	t.Cleanup(func() { payment.APIBaseURL = prev })
}

// yooTestServer открывает Server с настроенными (фейковыми) ключами ЮKassa и
// httptest-заглушкой её API вместо реальной сети. Возвращает Server, БД и
// функцию генерации ответа /v3/payments — тесты подставляют своё поведение.
func yooTestServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	s, _ := testServer(t)
	s.Config = config.Config{
		PublicAppURL:      "http://localhost:8080",
		YooKassaShopID:    "shop_test",
		YooKassaSecretKey: "secret_test",
	}
	return s
}

func TestInitiatePaymentEmbeddedReturnsToken(t *testing.T) {
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_embedded_1",
			"status": "pending",
			"confirmation": map[string]string{
				"type":               "embedded",
				"confirmation_token": "ct-embedded-1",
			},
		})
	})
	seedOrder(t, s.DB, "ord_e1", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_e1/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_e1")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["confirmationToken"] != "ct-embedded-1" {
		t.Fatalf("confirmationToken = %v, want ct-embedded-1", out["confirmationToken"])
	}
	if _, has := out["confirmationUrl"]; has {
		t.Fatalf("response has confirmationUrl, want absent for embedded")
	}

	o, err := store.GetOrder(context.Background(), s.DB, "ord_e1")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if !store.KnownPaymentID(o, "pay_embedded_1") {
		t.Fatalf("payment id pay_embedded_1 not recorded on order")
	}
}

func TestInitiatePaymentRedirectReturnsURL(t *testing.T) {
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_redirect_1",
			"status": "pending",
			"confirmation": map[string]string{
				"type":             "redirect",
				"confirmation_url": "https://yoomoney.ru/checkout/payments/v2/contract?orderId=abc",
			},
		})
	})
	seedOrder(t, s.DB, "ord_r1", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_r1/initiate-payment", strings.NewReader(`{"mode":"redirect"}`))
	req.SetPathValue("id", "ord_r1")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["confirmationUrl"] != "https://yoomoney.ru/checkout/payments/v2/contract?orderId=abc" {
		t.Fatalf("confirmationUrl = %v, want the YooKassa url", out["confirmationUrl"])
	}
	if _, has := out["confirmationToken"]; has {
		t.Fatalf("response has confirmationToken, want absent for redirect")
	}
}

func TestInitiatePaymentDefaultModeIsEmbedded(t *testing.T) {
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_default_1",
			"status": "pending",
			"confirmation": map[string]string{
				"type":               "embedded",
				"confirmation_token": "ct-default-1",
			},
		})
	})
	seedOrder(t, s.DB, "ord_d1", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_d1/initiate-payment", strings.NewReader(``))
	req.SetPathValue("id", "ord_d1")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["confirmationToken"] != "ct-default-1" {
		t.Fatalf("confirmationToken = %v, want ct-default-1 (empty body must default to embedded)", out["confirmationToken"])
	}
}

// Регресс на server.go:534-541: скидка съедает всю сумму — заказ помечается
// оплаченным без обращения к ЮKassa вообще.
func TestInitiatePaymentZeroAmountStillMarksPaid(t *testing.T) {
	s, db := testServer(t)
	s.Config = config.Config{PublicAppURL: "http://localhost:8080"}
	seedOrder(t, db, "ord_zero", "pending")
	if err := store.ApplyDiscount(context.Background(), db, "ord_zero", "DEMO10", 990); err != nil {
		t.Fatalf("apply discount: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_zero/initiate-payment", strings.NewReader(`{}`))
	req.SetPathValue("id", "ord_zero")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["paid"] != true {
		t.Fatalf("paid = %v, want true", out["paid"])
	}
	o, _ := store.GetOrder(context.Background(), db, "ord_zero")
	if o.PaymentStatus != "paid" {
		t.Fatalf("payment_status = %q, want paid", o.PaymentStatus)
	}
}

// Токен виджета часовой: пользователь может успешно оплатить не последнюю, а
// одну из более ранних попыток. Вебхук обязан зачесть деньги в этом случае.
func TestWebhookAcceptsFirstOfSeveralPayments(t *testing.T) {
	callCount := 0
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		id := "pay_a"
		if callCount == 2 {
			id = "pay_b"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     id,
			"status": "pending",
			"confirmation": map[string]string{
				"type":               "embedded",
				"confirmation_token": "ct-" + id,
			},
		})
	})
	seedOrder(t, s.DB, "ord_first", "pending")

	// Две попытки оплаты — два платежа в истории заказа.
	for range 2 {
		req := httptest.NewRequest("POST", "/api/orders/ord_first/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
		req.SetPathValue("id", "ord_first")
		rec := httptest.NewRecorder()
		s.handleInitiatePayment(rec, req)
		if rec.Code != 200 {
			t.Fatalf("initiate-payment status = %d, body=%s", rec.Code, rec.Body.String())
		}
	}

	// GetPayment должен теперь отвечать succeeded для проверяемого вебхуком платежа.
	getPaymentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_a",
			"status": "succeeded",
			"paid":   true,
			"amount": map[string]string{"value": "990.00", "currency": "RUB"},
			"metadata": map[string]string{
				"order_id": "ord_first",
			},
		})
	}))
	t.Cleanup(getPaymentServer.Close)
	setAPIBaseURL(t, getPaymentServer.URL)

	body := `{"event":"payment.succeeded","object":{"id":"pay_a","metadata":{"order_id":"ord_first"}}}`
	req := httptest.NewRequest("POST", "/api/webhooks/yookassa", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleYooWebhook(rec, req)

	o, err := store.GetOrder(context.Background(), s.DB, "ord_first")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.PaymentStatus != "paid" {
		t.Fatalf("payment_status = %q, want paid (first of several payments must be accepted)", o.PaymentStatus)
	}
}

// Платёж, который заказ никогда не заводил, к нему не относится, даже если он
// настоящий и оплачен.
func TestWebhookRejectsForeignPayment(t *testing.T) {
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_ours",
			"status": "pending",
			"confirmation": map[string]string{
				"type":               "embedded",
				"confirmation_token": "ct-ours",
			},
		})
	})
	seedOrder(t, s.DB, "ord_foreign", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_foreign/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_foreign")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)
	if rec.Code != 200 {
		t.Fatalf("initiate-payment status = %d, body=%s", rec.Code, rec.Body.String())
	}

	getPaymentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_someone_elses",
			"status": "succeeded",
			"paid":   true,
			"amount": map[string]string{"value": "990.00", "currency": "RUB"},
			"metadata": map[string]string{
				"order_id": "ord_foreign",
			},
		})
	}))
	t.Cleanup(getPaymentServer.Close)
	setAPIBaseURL(t, getPaymentServer.URL)

	body := `{"event":"payment.succeeded","object":{"id":"pay_someone_elses","metadata":{"order_id":"ord_foreign"}}}`
	webhookReq := httptest.NewRequest("POST", "/api/webhooks/yookassa", strings.NewReader(body))
	webhookRec := httptest.NewRecorder()
	s.handleYooWebhook(webhookRec, webhookReq)

	o, err := store.GetOrder(context.Background(), s.DB, "ord_foreign")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.PaymentStatus != "pending" {
		t.Fatalf("payment_status = %q, want pending (foreign payment must be rejected)", o.PaymentStatus)
	}
}

// yooRoutedServer маршрутизирует по методу+пути: гейт ходит на GET /v3/payments/{id},
// создание — на POST /v3/payments; оба нужны в одном тесте, а APIBaseURL один.
//
// refund — необязательный (variadic, чтобы не трогать существующие вызовы) обработчик
// POST /v3/refunds для тестов T-020 (автовозврат дублирующей оплаты); без него ручка
// отвечает 404 — тестам, где возврат не ожидается, сетевой вызов туда не нужен.
func yooRoutedServer(t *testing.T, get func(w http.ResponseWriter, paymentID string), create http.HandlerFunc, refund ...http.HandlerFunc) *Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		get(w, r.PathValue("id"))
	})
	mux.HandleFunc("POST /v3/payments", func(w http.ResponseWriter, r *http.Request) {
		if create == nil {
			w.WriteHeader(404)
			return
		}
		create(w, r)
	})
	mux.HandleFunc("POST /v3/refunds", func(w http.ResponseWriter, r *http.Request) {
		if len(refund) == 0 {
			w.WriteHeader(404)
			return
		}
		refund[0](w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	s, _ := testServer(t)
	s.Config = config.Config{
		PublicAppURL:      "http://localhost:8080",
		YooKassaShopID:    "shop_test",
		YooKassaSecretKey: "secret_test",
	}
	return s
}

func writePaymentJSON(w http.ResponseWriter, id, status string, paid bool) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id, "status": status, "paid": paid,
		"amount":   map[string]string{"value": "990.00", "currency": "RUB"},
		"metadata": map[string]string{"order_id": "irrelevant"},
	})
}

func writeCreatedPaymentJSON(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     id,
		"status": "pending",
		"confirmation": map[string]string{
			"type":               "embedded",
			"confirmation_token": "ct-" + id,
		},
	})
}

// Живой (< confirmationTTL) подтверждение того же режима, платёж всё ещё pending —
// ручка обязана отдать то же самое подтверждение и не звать ЮKassa на создание.
func TestInitiatePaymentReusesLiveToken(t *testing.T) {
	createCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { writePaymentJSON(w, id, "pending", false) },
		func(w http.ResponseWriter, r *http.Request) { createCalls++; writeCreatedPaymentJSON(w, "pay_new") },
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_reuse", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_reuse", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := store.SaveConfirmation(ctx, s.DB, "ord_reuse", store.IssuedConfirmation{
		PaymentID: "p1",
		Mode:      "embedded",
		Token:     "ct-p1",
		IssuedAt:  time.Now().UTC().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("seed confirmation: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_reuse/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_reuse")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["confirmationToken"] != "ct-p1" || out["paymentId"] != "p1" {
		t.Fatalf("got %v, want reused ct-p1/p1", out)
	}
	if createCalls != 0 {
		t.Fatalf("create calls = %d, want 0 (must reuse, not create)", createCalls)
	}
	o, err := store.GetOrder(ctx, s.DB, "ord_reuse")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	var ids []string
	_ = json.Unmarshal([]byte(o.YooPaymentIDs.String), &ids)
	if len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("yoo_payment_ids = %v, want [p1]", ids)
	}
}

// Подтверждение старше confirmationTTL нельзя переиспользовать: токен виджета
// живёт час, и покупатель мог бы получить умирающий токен.
func TestInitiatePaymentCreatesNewWhenConfirmationExpired(t *testing.T) {
	createCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { writePaymentJSON(w, id, "pending", false) },
		func(w http.ResponseWriter, r *http.Request) { createCalls++; writeCreatedPaymentJSON(w, "pay_new") },
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_expired", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_expired", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := store.SaveConfirmation(ctx, s.DB, "ord_expired", store.IssuedConfirmation{
		PaymentID: "p1",
		Mode:      "embedded",
		Token:     "ct-p1",
		IssuedAt:  time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed confirmation: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_expired/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_expired")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (expired confirmation must not be reused)", createCalls)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["paymentId"] != "pay_new" {
		t.Fatalf("paymentId = %v, want pay_new", out["paymentId"])
	}
	o, _ := store.GetOrder(ctx, s.DB, "ord_expired")
	var ids []string
	_ = json.Unmarshal([]byte(o.YooPaymentIDs.String), &ids)
	if len(ids) != 2 {
		t.Fatalf("yoo_payment_ids = %v, want 2 entries", ids)
	}
	got, ok := store.LoadConfirmation(o)
	if !ok || got.PaymentID != "pay_new" {
		t.Fatalf("new confirmation not saved: %+v ok=%v", got, ok)
	}
}

// Платёж, к которому относится сохранённое подтверждение, больше не pending —
// мёртвый токен покупателю не отдаём, заводим новый платёж.
func TestInitiatePaymentCreatesNewWhenPaymentNotPending(t *testing.T) {
	createCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { writePaymentJSON(w, id, "canceled", false) },
		func(w http.ResponseWriter, r *http.Request) { createCalls++; writeCreatedPaymentJSON(w, "pay_new") },
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_canceled", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_canceled", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := store.SaveConfirmation(ctx, s.DB, "ord_canceled", store.IssuedConfirmation{
		PaymentID: "p1", Mode: "embedded", Token: "ct-p1", IssuedAt: time.Now().UTC().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("seed confirmation: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_canceled/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_canceled")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (dead token must not be reused)", createCalls)
	}
}

// embedded и redirect дают разные типы подтверждения — покупатель, переключившийся
// на другой режим, не должен получить чужое (непригодное) подтверждение.
func TestInitiatePaymentDoesNotReuseAcrossModes(t *testing.T) {
	createCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { writePaymentJSON(w, id, "pending", false) },
		func(w http.ResponseWriter, r *http.Request) {
			createCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "pay_redirect_new",
				"status": "pending",
				"confirmation": map[string]string{
					"type":             "redirect",
					"confirmation_url": "https://yoomoney.ru/checkout/x",
				},
			})
		},
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_modes", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_modes", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := store.SaveConfirmation(ctx, s.DB, "ord_modes", store.IssuedConfirmation{
		PaymentID: "p1", Mode: "embedded", Token: "ct-p1", IssuedAt: time.Now().UTC().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("seed confirmation: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_modes/initiate-payment", strings.NewReader(`{"mode":"redirect"}`))
	req.SetPathValue("id", "ord_modes")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (redirect must not reuse an embedded confirmation)", createCalls)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["confirmationUrl"] != "https://yoomoney.ru/checkout/x" {
		t.Fatalf("confirmationUrl = %v, want the new redirect url", out["confirmationUrl"])
	}
}

// История платежей содержит успешно оплаченный — деньги уже взяты, вебхук просто
// не доехал. Второй платёж заводить нельзя; заказ помечать оплаченным здесь тоже
// нельзя, это работа вебхука с полной сверкой.
func TestInitiatePaymentRefusesWhenPreviousSucceeded(t *testing.T) {
	createCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { writePaymentJSON(w, id, "succeeded", true) },
		func(w http.ResponseWriter, r *http.Request) { createCalls++; writeCreatedPaymentJSON(w, "pay_new") },
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_paid", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_paid", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_paid/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_paid")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 409 {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["error"] != "already_paid" {
		t.Fatalf("error = %v, want already_paid", out["error"])
	}
	if createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", createCalls)
	}
	o, err := store.GetOrder(ctx, s.DB, "ord_paid")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.PaymentStatus != "pending" {
		t.Fatalf("payment_status = %q, want pending (ручка не помечает оплаченным)", o.PaymentStatus)
	}
}

// Доступность оплаты важнее дедупликации: не смогли прочитать статус платежа —
// заводим новый, а не блокируем покупателя.
func TestInitiatePaymentCreatesWhenGetPaymentFails(t *testing.T) {
	createCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { w.WriteHeader(500) },
		func(w http.ResponseWriter, r *http.Request) { createCalls++; writeCreatedPaymentJSON(w, "pay_new") },
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_geterr", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_geterr", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := store.SaveConfirmation(ctx, s.DB, "ord_geterr", store.IssuedConfirmation{
		PaymentID: "p1", Mode: "embedded", Token: "ct-p1", IssuedAt: time.Now().UTC().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("seed confirmation: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_geterr/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_geterr")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (API failure must fall back to creating a payment)", createCalls)
	}
}

// История заказа не должна превращаться в десяток сетевых вызовов на старых заказах.
func TestInitiatePaymentLimitsHistoryReads(t *testing.T) {
	getCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { getCalls++; writePaymentJSON(w, id, "canceled", false) },
		func(w http.ResponseWriter, r *http.Request) { writeCreatedPaymentJSON(w, "pay_new") },
	)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_history", "pending")
	for i := range 12 {
		if err := store.AddPaymentID(ctx, s.DB, "ord_history", fmt.Sprintf("p%d", i), ""); err != nil {
			t.Fatalf("seed history %d: %v", i, err)
		}
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_history/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_history")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if getCalls > 5 {
		t.Fatalf("GET /v3/payments/... calls = %d, want <= 5", getCalls)
	}
}

// Первая попытка оплаты не должна дорожать: без истории гейт не ходит в сеть вовсе.
func TestInitiatePaymentFirstAttemptMakesNoHistoryReads(t *testing.T) {
	getCalls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { getCalls++; writePaymentJSON(w, id, "pending", false) },
		func(w http.ResponseWriter, r *http.Request) { writeCreatedPaymentJSON(w, "pay_first") },
	)
	seedOrder(t, s.DB, "ord_fresh", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_fresh/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_fresh")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if getCalls != 0 {
		t.Fatalf("GET calls = %d, want 0 (first attempt has no history to check)", getCalls)
	}
}

// Локальная разработка (нет YOOKASSA_* в env) не должна ходить в сеть вовсе — ни
// гейтом, ни созданием платежа.
func TestInitiatePaymentWithoutCredentialsMakesNoAPICalls(t *testing.T) {
	calls := 0
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { calls++; writePaymentJSON(w, id, "pending", false) },
		func(w http.ResponseWriter, r *http.Request) { calls++; writeCreatedPaymentJSON(w, "pay_x") },
	)
	s.Config.YooKassaShopID = ""
	s.Config.YooKassaSecretKey = ""
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_nocreds", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_nocreds", "p1", ""); err != nil {
		t.Fatalf("seed history: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders/ord_nocreds/initiate-payment", strings.NewReader(`{"mode":"embedded"}`))
	req.SetPathValue("id", "ord_nocreds")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if calls != 0 {
		t.Fatalf("API calls = %d, want 0 (no credentials means no network at all)", calls)
	}
}

// Сохранение карты у платежа за песню — под краном, и по умолчанию выключено:
// сохранять способ оплаты умеет только карта, поэтому просьба о сохранении
// убирает с формы ЮKassa СБП, SberPay и Mir Pay. Тест сторожит именно дефолт:
// молчаливое возвращение флага снова оставило бы покупателю одну карту.
func TestFlowPaymentSavesCardOnlyWhenSettingOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setting  string
		wantSave bool
	}{
		{"дефолт — не просим сохранение", "", false},
		{"кран выключен", "0", false},
		{"кран включён", "1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotSave bool
			s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				gotSave, _ = body["save_payment_method"].(bool)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":           "pay_save_1",
					"status":       "pending",
					"confirmation": map[string]string{"type": "redirect", "confirmation_url": "https://yoo/pay"},
				})
			})
			if tc.setting != "" {
				if err := store.SetSetting(context.Background(), s.DB, store.SettingSaveCardInFunnel, tc.setting); err != nil {
					t.Fatalf("настройка: %v", err)
				}
			}
			// Заказ воронки: только у них сохранение вообще обсуждается.
			id := "ord_save_" + tc.setting + "x"
			seedOrder(t, s.DB, id, "pending")
			if _, err := s.DB.ExecContext(context.Background(),
				`UPDATE orders SET flow_variant = 'v1_base' WHERE id = ?`, id); err != nil {
				t.Fatalf("плечо заказа: %v", err)
			}

			req := httptest.NewRequest("POST", "/api/orders/"+id+"/initiate-payment", strings.NewReader(`{"mode":"redirect"}`))
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			s.handleInitiatePayment(rec, req)
			if rec.Code != 200 {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if gotSave != tc.wantSave {
				t.Fatalf("save_payment_method = %v, ожидалось %v", gotSave, tc.wantSave)
			}
		})
	}
}
