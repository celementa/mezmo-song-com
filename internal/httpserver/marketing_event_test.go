package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mezmo-song-com/internal/store"
)

// Событие API-триггера уходит на каждый заказ отдельно: тело содержит email,
// order_id, детерминированный event_id (order_id:event_type) и ссылки. У
// неоплаченного заказа есть checkout_url, у оплаченного — нет.
func TestMarketingEventPayload(t *testing.T) {
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- body
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	s := orderRefTestServer(t)
	s.Config.UnisenderTriggerURL = srv.URL
	s.Config.PublicAppURL = "https://example.test"
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_evt", store.PaymentPending)
	if err := store.SetOrderFlow(ctx, s.DB, "ord_evt", ArmCom500750s, 19); err != nil {
		t.Fatalf("set flow: %v", err)
	}

	s.marketingEvent("ord_evt", EventOrderCreated)
	var body map[string]any
	select {
	case body = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("событие не пришло")
	}
	ref := publicOrderRef(s.Config.WebhookSecret, "ord_evt")
	want := map[string]any{
		"email":          "a@b.ru",
		"order_id":       "ord_evt",
		"order_ref":      ref,
		"event_id":       "ord_evt:order_created",
		"event_type":     "order_created",
		"payment_status": "pending",
		"order_url":      "https://example.test/order/" + ref,
		"status_url":     "https://example.test/api/public/order/" + ref + "/status",
		"checkout_url":   "https://example.test/config?arm=variant3-500-750s&order=" + ref,
		"flow_arm":       "variant3-500-750s",
	}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("%s = %v, want %v (body %v)", k, body[k], v, body)
		}
	}
	if _, err := time.Parse(time.RFC3339, body["occurred_at"].(string)); err != nil {
		t.Fatalf("occurred_at = %v: %v", body["occurred_at"], err)
	}

	if err := store.MarkPaid(ctx, s.DB, "ord_evt"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	s.marketingEvent("ord_evt", EventOrderPaid)
	select {
	case body = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("второе событие не пришло")
	}
	if body["event_id"] != "ord_evt:order_paid" || body["payment_status"] != "paid" || body["checkout_url"] != nil {
		t.Fatalf("после оплаты: %v", body)
	}
}

// Без UNISENDER_TRIGGER_URL событие не отправляется — стенд и прод до
// подключения сценария работают как раньше.
func TestMarketingEventDisabledWithoutURL(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_off", store.PaymentPending)
	s.marketingEvent("ord_off", EventOrderCreated) // не должно паниковать и ничего не ждёт
}
