package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/store"
)

// subPaymentJSON — ответ GET /v3/payments/{id} для платежа подписки: metadata
// несёт subscription_id/flow, а не order_id, плюс payment_method с saved.
func subPaymentJSON(id, status string, paid bool, amount, subID, flow, methodID string) map[string]any {
	m := map[string]any{
		"id":     id,
		"status": status,
		"paid":   paid,
		"amount": map[string]string{"value": amount, "currency": "RUB"},
	}
	if subID != "" {
		m["metadata"] = map[string]string{"subscription_id": subID, "flow": flow}
	}
	if methodID != "" {
		m["payment_method"] = map[string]any{"id": methodID, "saved": true}
	}
	return m
}

func subWebhookBody(event, paymentID, subID string) string {
	return fmt.Sprintf(`{"event":%q,"object":{"id":%q,"metadata":{"subscription_id":%q}}}`, event, paymentID, subID)
}

// Вебхук первого платежа подписки: сверка с API, активация, дедуп ретрая.
// Чужой/подменённый платёж (не та сумма, не та подписка) отклоняется.
func TestSubscriptionWebhookActivates(t *testing.T) {
	s := refundTestServer(t)
	sub, err := store.UpsertPendingSubscription(t.Context(), s.DB, "w@t.ru", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	yooGetPaymentsServer(t, map[string]map[string]any{
		"pay_ok":    subPaymentJSON("pay_ok", "succeeded", true, "990.00", sub.ID, store.SubFlowFirst, "pm_77"),
		"pay_cheap": subPaymentJSON("pay_cheap", "succeeded", true, "1.00", sub.ID, store.SubFlowFirst, "pm_77"),
		"pay_alien": subPaymentJSON("pay_alien", "succeeded", true, "990.00", "sub_other", store.SubFlowFirst, ""),
	})

	// Не та сумма и не та подписка — отклонены, подписка не активирована.
	for _, pid := range []string{"pay_cheap", "pay_alien"} {
		rec := postWebhook(s, subWebhookBody("payment.succeeded", pid, sub.ID))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "verification_failed") {
			t.Fatalf("%s: code=%d body=%s, want verification_failed", pid, rec.Code, rec.Body.String())
		}
	}
	if got, _ := store.GetSubscription(t.Context(), s.DB, sub.ID); got.Status != store.SubPending {
		t.Fatalf("после отклонённых платежей статус %s, want pending", got.Status)
	}

	rec := postWebhook(s, subWebhookBody("payment.succeeded", "pay_ok", sub.ID))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetSubscription(t.Context(), s.DB, sub.ID)
	if got.Status != store.SubActive || got.YooMethodID.String != "pm_77" {
		t.Fatalf("status=%s method=%s, want active/pm_77", got.Status, got.YooMethodID.String)
	}
	until := got.AccessUntil.String

	// Ретрай вебхука — период не продлевается второй раз.
	postWebhook(s, subWebhookBody("payment.succeeded", "pay_ok", sub.ID))
	got2, _ := store.GetSubscription(t.Context(), s.DB, sub.ID)
	if got2.AccessUntil.String != until {
		t.Fatalf("ретрай продлил период: %s → %s", until, got2.AccessUntil.String)
	}
}

// subLogin выполняет вход в кабинет и возвращает cookie сессии.
func subLogin(t *testing.T, s *Server, email string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/subscription/session",
		bytes.NewReader([]byte(fmt.Sprintf(`{"email":%q}`, email))))
	rec := httptest.NewRecorder()
	s.handleSubscriptionLogin(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login: code=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == subSessionCookie {
			return c
		}
	}
	t.Fatal("login: no session cookie")
	return nil
}

// Кабинет: вход по email только при живом доступе; создание песни упирается в
// суточный лимит; отмена из кабинета сохраняет доступ.
func TestSubscriptionCabinetFlow(t *testing.T) {
	s, db := testServer(t)
	s.Config = config.Config{PublicAppURL: "http://localhost:8080", WebhookSecret: "test-secret"}

	// Входа без подписки нет.
	req := httptest.NewRequest("POST", "/api/subscription/session", bytes.NewReader([]byte(`{"email":"no@t.ru"}`)))
	rec := httptest.NewRecorder()
	s.handleSubscriptionLogin(rec, req)
	if rec.Code != 403 {
		t.Fatalf("login without subscription: code=%d, want 403", rec.Code)
	}

	sub, _ := store.UpsertPendingSubscription(t.Context(), db, "cab@t.ru", "")
	if _, err := store.RecordSubscriptionPayment(t.Context(), db, sub.ID, "p1", "990.00", store.SubFlowFirst, "pm"); err != nil {
		t.Fatal(err)
	}
	cookie := subLogin(t, s, "cab@t.ru")

	// Лимит 2 в сутки для теста.
	if err := store.SetSetting(t.Context(), db, store.SettingSubscriptionDailyLimit, "2"); err != nil {
		t.Fatal(err)
	}

	draft := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/subscription/orders/draft",
			bytes.NewReader([]byte(`{"recipientName":"Ира","yourName":"Аня"}`)))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.handleSubscriptionOrderDraft(rec, req)
		return rec
	}
	for i := 0; i < 2; i++ {
		if rec := draft(); rec.Code != 200 {
			t.Fatalf("draft %d: code=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := draft(); rec.Code != 429 {
		t.Fatalf("сверх лимита: code=%d, want 429", rec.Code)
	}

	// Заказы видны в кабинете и не видны без cookie.
	req = httptest.NewRequest("GET", "/api/subscription/orders", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.handleSubscriptionOrders(rec, req)
	var ordersResp struct {
		Orders []map[string]any `json:"orders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ordersResp); err != nil || len(ordersResp.Orders) != 2 {
		t.Fatalf("orders: n=%d err=%v body=%s", len(ordersResp.Orders), err, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleSubscriptionOrders(rec, httptest.NewRequest("GET", "/api/subscription/orders", nil))
	if rec.Code != 401 {
		t.Fatalf("orders without cookie: code=%d, want 401", rec.Code)
	}

	// Отмена из кабинета: статус canceled, но /me по прежней сессии ещё работает.
	req = httptest.NewRequest("POST", "/api/subscription/cancel", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.handleSubscriptionCancel(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cancel: code=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/api/subscription/me", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.handleSubscriptionMe(rec, req)
	var me struct {
		Canceled bool   `json:"canceled"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || !me.Canceled {
		t.Fatalf("me after cancel: %s (err=%v)", rec.Body.String(), err)
	}
}

// Публичная отмена по токену из письма: info отдаёт email, отмена только POST-ом.
func TestSubscriptionCancelByToken(t *testing.T) {
	s, db := testServer(t)
	sub, _ := store.UpsertPendingSubscription(t.Context(), db, "tok@t.ru", "")
	if _, err := store.RecordSubscriptionPayment(t.Context(), db, sub.ID, "p1", "990.00", store.SubFlowFirst, "pm"); err != nil {
		t.Fatal(err)
	}
	fresh, _ := store.GetSubscription(t.Context(), db, sub.ID)

	rec := httptest.NewRecorder()
	s.handleSubscriptionCancelInfo(rec, httptest.NewRequest("GET", "/api/subscription/cancel-info?t="+fresh.CancelToken, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "tok@t.ru") {
		t.Fatalf("cancel-info: code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleSubscriptionCancelInfo(rec, httptest.NewRequest("GET", "/api/subscription/cancel-info?t=bad", nil))
	if rec.Code != 404 {
		t.Fatalf("cancel-info bad token: code=%d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleSubscriptionCancel(rec, httptest.NewRequest("POST", "/api/subscription/cancel",
		bytes.NewReader([]byte(fmt.Sprintf(`{"token":%q}`, fresh.CancelToken)))))
	if rec.Code != 200 {
		t.Fatalf("cancel by token: code=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetSubscription(t.Context(), db, sub.ID)
	if got.Status != store.SubCanceled {
		t.Fatalf("status=%s, want canceled", got.Status)
	}
	if !got.HasAccess(time.Now().UTC()) {
		t.Fatal("доступ до конца периода обязан сохраниться")
	}
}

// Email из subscription.test_emails активируется без оплаты даже при настроенной
// ЮKassa (тест флоу на проде до включения рекуррентки); сеть не трогается.
func TestSubscriptionStartTestEmailAllowlist(t *testing.T) {
	s := refundTestServer(t) // ключи ЮKassa заданы — обычный email ушёл бы в оплату
	if err := store.SetSetting(t.Context(), s.DB, store.SettingSubscriptionTestEmails, "QA@Test.ru, second@test.ru"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.handleSubscriptionStart(rec, httptest.NewRequest("POST", "/api/subscription/start",
		bytes.NewReader([]byte(`{"email":"qa@test.ru","name":"QA"}`))))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"activated":true`) {
		t.Fatalf("test-email start: code=%d body=%s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), s.DB, "qa@test.ru")
	if err != nil || sub.Status != store.SubActive {
		t.Fatalf("test-email sub: status=%s err=%v", sub.Status, err)
	}
}

// Без ключей ЮKassa /start активирует подписку сразу (dev-режим).
func TestSubscriptionStartMockMode(t *testing.T) {
	s, db := testServer(t)
	rec := httptest.NewRecorder()
	s.handleSubscriptionStart(rec, httptest.NewRequest("POST", "/api/subscription/start",
		bytes.NewReader([]byte(`{"email":"mock@t.ru","name":"Аня"}`))))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"activated":true`) {
		t.Fatalf("mock start: code=%d body=%s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), db, "mock@t.ru")
	if err != nil || sub.Status != store.SubActive {
		t.Fatalf("mock sub: status=%s err=%v", sub.Status, err)
	}

	// Повторная покупка на активную подписку — 409.
	rec = httptest.NewRecorder()
	s.handleSubscriptionStart(rec, httptest.NewRequest("POST", "/api/subscription/start",
		bytes.NewReader([]byte(`{"email":"mock@t.ru"}`))))
	if rec.Code != 409 {
		t.Fatalf("second start: code=%d, want 409", rec.Code)
	}
}

// Подарочный месяц: доступ есть, карты и плановых списаний нет, цена продления
// зафиксирована; повторно тому же адресу не выдаётся, неоплаченному — тоже.
func TestAdminSubscriptionGift(t *testing.T) {
	s, db := testServer(t)
	seedOrder(t, db, "ord_gift", store.PaymentPaid)
	gift := func(orderID string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleAdminSubscriptionGift(rec, httptest.NewRequest("POST", "/api/admin/subscriptions/gift",
			bytes.NewReader([]byte(fmt.Sprintf(`{"orderId":%q,"priceRub":500}`, orderID)))))
		return rec
	}
	if rec := gift("ord_gift"); rec.Code != 200 {
		t.Fatalf("gift: code=%d body=%s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), db, "a@b.ru")
	if err != nil {
		t.Fatal(err)
	}
	if !sub.HasAccess(time.Now().UTC()) || sub.Status != store.SubCanceled || sub.YooMethodID.String != "" || sub.NextChargeAt.Valid || subPrice(sub) != 500 {
		t.Fatalf("gifted sub: status=%s method=%q next=%v price=%d", sub.Status, sub.YooMethodID.String, sub.NextChargeAt, subPrice(sub))
	}
	due, _ := store.ListDueSubscriptionCharges(t.Context(), db, time.Now().AddDate(0, 2, 0), subRetryCooldown, subGiveUpAfter)
	if len(due) != 0 {
		t.Fatalf("подарочная подписка не должна попадать в списания: %d", len(due))
	}
	if rec := gift("ord_gift"); rec.Code != 409 {
		t.Fatalf("повторный подарок: code=%d, want 409", rec.Code)
	}
	seedOrder(t, db, "ord_unpaid", "pending")
	if rec := gift("ord_unpaid"); rec.Code != 409 {
		t.Fatalf("неоплаченный заказ: code=%d, want 409", rec.Code)
	}
	// Продление человек оформляет сам — и по цене подарка, а не общей.
	if _, err := store.UpsertPendingSubscription(t.Context(), db, "a@b.ru", ""); err != nil {
		t.Fatalf("оформление продления должно быть доступно: %v", err)
	}
}

// activeSubForJobs заводит активную подписку с данным способом оплаты и
// списанием через chargeIn от сейчас (отрицательное — срок уже наступил).
func activeSubForJobs(t *testing.T, s *Server, email, methodID string, chargeIn time.Duration) store.Subscription {
	t.Helper()
	ctx := t.Context()
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, "Тест")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSubscriptionPayment(ctx, s.DB, sub.ID, "pay-"+sub.ID, "990.00", store.SubFlowFirst, methodID); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(chargeIn).Format(time.RFC3339)
	if _, err := s.DB.ExecContext(ctx, `UPDATE subscriptions SET next_charge_at = ?, access_until = ? WHERE id = ?`, at, at, sub.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.GetSubscription(ctx, s.DB, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

// Подписке без оплаты (заглушка вместо карты) планировщик не шлёт напоминание
// и не пытается списать — раньше это было письмо «спишется 990 ₽» и почасовые
// 400 от ЮKassa. Настоящая подписка рядом по-прежнему в обеих выборках.
func TestSubscriptionJobsSkipMockMethod(t *testing.T) {
	s := refundTestServer(t)
	ctx := t.Context()
	now := time.Now().UTC()
	activeSubForJobs(t, s, "mock-remind@t.ru", store.MockPaymentMethodID, 24*time.Hour)
	activeSubForJobs(t, s, "mock-flow@t.ru", store.MockPaymentMethodID+"-ord_1", 24*time.Hour)
	real := activeSubForJobs(t, s, "real-remind@t.ru", "pm-real", 24*time.Hour)

	reminders, err := store.ListSubscriptionsNeedingReminder(ctx, s.DB, now, subReminderLead)
	if err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 1 || reminders[0].ID != real.ID {
		t.Fatalf("reminders = %+v, want only %s", reminders, real.ID)
	}

	activeSubForJobs(t, s, "mock-due@t.ru", store.MockPaymentMethodID, -time.Hour)
	realDue := activeSubForJobs(t, s, "real-due@t.ru", "pm-real-2", -time.Hour)
	due, err := store.ListDueSubscriptionCharges(ctx, s.DB, now, subRetryCooldown, subGiveUpAfter)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != realDue.ID {
		t.Fatalf("due = %+v, want only %s", due, realDue.ID)
	}
}

// 4xx от ЮKassa на автосписании — не сбой сети: попытка помечается, и
// следующий тик не повторяет запрос (раньше — каждый час до 30 дней).
func TestSubscriptionRebillAPIRejectMarksAttempt(t *testing.T) {
	s := refundTestServer(t)
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v3/payments", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"type":"error","code":"invalid_request","parameter":"payment_method_id"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	sub := activeSubForJobs(t, s, "revoked@t.ru", "pm-revoked", -time.Hour)
	s.runSubscriptionJobsOnce(t.Context())
	s.runSubscriptionJobsOnce(t.Context())
	if calls != 1 {
		t.Fatalf("payment create calls = %d, want 1", calls)
	}
	fresh, err := store.GetSubscription(t.Context(), s.DB, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != store.SubPastDue {
		t.Fatalf("status = %s, want %s", fresh.Status, store.SubPastDue)
	}
}

// --- T-009: цена ценового плеча на /subscribe ---

// seedArmOrder — оплаченный заказ ценового плеча с заданным email и, если
// methodID непуст, сохранённой картой ЮKassa: ровно то состояние, из которого
// апсейл воронки ведёт человека на /subscribe?order=<ref>&email=<email>.
func seedArmOrder(t *testing.T, s *Server, id, email, arm, methodID string) string {
	t.Helper()
	o := store.Order{
		ID:               id,
		Status:           "created",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    store.PaymentPaid,
		InputPayloadJSON: `{"recipientName":"Ира","yourName":"Аня"}`,
		CustomerJSON:     fmt.Sprintf(`{"email":%q}`, email),
		BasePrice:        750,
		CreatedAt:        seedOrderDefaultCreatedAt,
	}
	if err := store.CreateOrder(t.Context(), s.DB, o); err != nil {
		t.Fatalf("seed order %s: %v", id, err)
	}
	// CreateOrder не пишет ветку/валюту/способ оплаты — их задаёт платёж.
	if _, err := s.DB.ExecContext(t.Context(),
		`UPDATE orders SET flow_variant = ?, currency = 'RUB', yoo_payment_method_id = ? WHERE id = ?`,
		arm, methodID, id); err != nil {
		t.Fatalf("seed order %s fields: %v", id, err)
	}
	return id
}

// captureYooCreate поднимает заглушку POST /v3/payments и складывает payload
// последнего запроса в dst: по нему проверяется сумма и return_url платежа.
func captureYooCreate(t *testing.T, dst *map[string]any, created *int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v3/payments", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		*dst = payload
		*created++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pay_arm_1",
			"status": "pending",
			"confirmation": map[string]string{
				"type":             "redirect",
				"confirmation_url": "https://yoomoney.test/checkout/pay_arm_1",
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)
}

// Старт подписки с orderRef ценового плеча: платёж выставляется на цену плеча
// (500 ₽), а не на общую (990 ₽), цена фиксируется в subscriptions.price_rub,
// return_url ведёт сразу на русскую страницу возврата.
func TestSubscriptionStartUsesOrderArmPrice(t *testing.T) {
	var got map[string]any
	created := 0
	s := refundTestServer(t)
	s.Config.WebhookSecret = "test-secret"
	captureYooCreate(t, &got, &created)

	orderID := seedArmOrder(t, s, "ord_arm_start", "arm@b.c", ArmCom500750s, "")
	ref := publicOrderRef(s.Config.WebhookSecret, orderID)

	rec := httptest.NewRecorder()
	s.handleSubscriptionStart(rec, httptest.NewRequest("POST", "/api/subscription/start",
		bytes.NewReader([]byte(fmt.Sprintf(`{"email":"arm@b.c","mode":"redirect","orderRef":%q}`, ref)))))
	if rec.Code != 200 {
		t.Fatalf("start: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if created != 1 {
		t.Fatalf("платежей создано %d, want 1", created)
	}
	amount, _ := got["amount"].(map[string]any)
	if amount["value"] != "500.00" || amount["currency"] != "RUB" {
		t.Fatalf("платёж на %v %v, want 500.00 RUB (цена плеча, а не общая 990)", amount["value"], amount["currency"])
	}
	conf, _ := got["confirmation"].(map[string]any)
	if ret, _ := conf["return_url"].(string); !strings.Contains(ret, "/ru/subscribe/return?sid=") {
		t.Fatalf("return_url = %q, want .../ru/subscribe/return?sid=...", ret)
	}

	sub, err := store.GetSubscriptionByEmail(t.Context(), s.DB, "arm@b.c")
	if err != nil {
		t.Fatal(err)
	}
	if sub.PriceRub != 500 {
		t.Fatalf("subscriptions.price_rub = %d, want 500", sub.PriceRub)
	}
	if !strings.Contains(rec.Body.String(), `"sid":"`+sub.ID+`"`) {
		t.Fatalf("ответ без sid подписки: %s", rec.Body.String())
	}
}

// Чужой email в теле при заказе плеча — 409 order_email_mismatch: ни платежа,
// ни записи подписки (цену и email задаёт заказ, а не тело запроса).
func TestSubscriptionStartRejectsOrderEmailMismatch(t *testing.T) {
	var got map[string]any
	created := 0
	s := refundTestServer(t)
	s.Config.WebhookSecret = "test-secret"
	captureYooCreate(t, &got, &created)

	orderID := seedArmOrder(t, s, "ord_arm_alien", "owner@b.c", ArmCom500750s, "")
	ref := publicOrderRef(s.Config.WebhookSecret, orderID)

	rec := httptest.NewRecorder()
	s.handleSubscriptionStart(rec, httptest.NewRequest("POST", "/api/subscription/start",
		bytes.NewReader([]byte(fmt.Sprintf(`{"email":"other@b.c","mode":"redirect","orderRef":%q}`, ref)))))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "order_email_mismatch") {
		t.Fatalf("чужой email: code=%d body=%s, want 409 order_email_mismatch", rec.Code, rec.Body.String())
	}
	if created != 0 {
		t.Fatalf("платёж создан при чужом email: %d", created)
	}
	for _, email := range []string{"other@b.c", "owner@b.c"} {
		if _, err := store.GetSubscriptionByEmail(t.Context(), s.DB, email); err == nil {
			t.Fatalf("подписка %s создана несмотря на 409", email)
		}
	}
}

// Прямой заход на /subscribe без orderRef — по-прежнему общая цена 990 ₽.
func TestSubscriptionStartWithoutOrderUsesDefaultPrice(t *testing.T) {
	var got map[string]any
	created := 0
	s := refundTestServer(t)
	captureYooCreate(t, &got, &created)

	rec := httptest.NewRecorder()
	s.handleSubscriptionStart(rec, httptest.NewRequest("POST", "/api/subscription/start",
		bytes.NewReader([]byte(`{"email":"plain@b.c","mode":"redirect"}`))))
	if rec.Code != 200 {
		t.Fatalf("start: code=%d body=%s", rec.Code, rec.Body.String())
	}
	amount, _ := got["amount"].(map[string]any)
	if amount["value"] != "990.00" {
		t.Fatalf("платёж на %v, want 990.00 (общая цена без orderRef)", amount["value"])
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), s.DB, "plain@b.c")
	if err != nil {
		t.Fatal(err)
	}
	if subPrice(sub) != 990 || sub.PriceRub != 0 {
		t.Fatalf("subPrice=%d price_rub=%d, want 990/0", subPrice(sub), sub.PriceRub)
	}
}

// Активация по сохранённой карте (апсейл «0 ₽ сегодня») фиксирует рублёвую цену
// плеча: 500 ₽, а не $15 из arm.SubscriptionPrice — иначе кабинет показывал бы
// 15 ₽/мес.
func TestOrderSubscribeUsesRubArmPrice(t *testing.T) {
	s := refundTestServer(t) // ключи ЮKassa заданы: способ оплаты берётся из заказа
	seedArmOrder(t, s, "ord_arm_sub", "saved@b.c", ArmCom500750s, "pm_saved_77")

	req := httptest.NewRequest("POST", "/api/orders/ord_arm_sub/subscribe", nil)
	req.SetPathValue("id", "ord_arm_sub")
	rec := httptest.NewRecorder()
	s.handleOrderSubscribe(rec, req)
	if rec.Code != 200 {
		t.Fatalf("subscribe: code=%d body=%s", rec.Code, rec.Body.String())
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), s.DB, "saved@b.c")
	if err != nil {
		t.Fatal(err)
	}
	if sub.PriceRub != 500 || subPrice(sub) != 500 {
		t.Fatalf("price_rub=%d subPrice=%d, want 500 (рубли плеча, не $15)", sub.PriceRub, subPrice(sub))
	}
	if sub.Status != store.SubActive || sub.YooMethodID.String != "pm_saved_77" {
		t.Fatalf("status=%s method=%q, want active/pm_saved_77", sub.Status, sub.YooMethodID.String)
	}
}
