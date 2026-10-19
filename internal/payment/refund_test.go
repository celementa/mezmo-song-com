package payment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// В теле запроса на возврат — payment_id и amount {value, currency: "RUB"}.
func TestCreateRefundPayload(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ref_1", "status": "succeeded"})
	}))
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	yk := YooKassa{ShopID: "shop", Secret: "secret"}
	if _, err := yk.CreateRefund("pay_1", "790.00"); err != nil {
		t.Fatalf("CreateRefund: %v", err)
	}

	if body["payment_id"] != "pay_1" {
		t.Fatalf("payment_id = %#v, want pay_1", body["payment_id"])
	}
	amount, ok := body["amount"].(map[string]any)
	if !ok {
		t.Fatalf("amount is not an object: %#v", body["amount"])
	}
	if amount["value"] != "790.00" {
		t.Fatalf("amount.value = %#v, want 790.00", amount["value"])
	}
	if amount["currency"] != "RUB" {
		t.Fatalf("amount.currency = %#v, want RUB", amount["currency"])
	}
}

// Два вызова для одного платежа шлют одинаковый Idempotence-Key — ретрай вебхука
// не должен порождать второй возврат. Для другого платежа ключ другой.
func TestCreateRefundUsesDeterministicIdempotenceKey(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotence-Key"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ref_x", "status": "succeeded"})
	}))
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	yk := YooKassa{ShopID: "shop", Secret: "secret"}
	if _, err := yk.CreateRefund("pay_1", "790.00"); err != nil {
		t.Fatalf("CreateRefund #1: %v", err)
	}
	if _, err := yk.CreateRefund("pay_1", "790.00"); err != nil {
		t.Fatalf("CreateRefund #2 (retry): %v", err)
	}
	if _, err := yk.CreateRefund("pay_2", "990.00"); err != nil {
		t.Fatalf("CreateRefund for other payment: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("got %d requests, want 3", len(keys))
	}
	if keys[0] == "" {
		t.Fatalf("Idempotence-Key is empty")
	}
	if keys[0] != keys[1] {
		t.Fatalf("Idempotence-Key differs across retries for the same payment: %q vs %q", keys[0], keys[1])
	}
	if keys[0] == keys[2] {
		t.Fatalf("Idempotence-Key is the same for different payments: %q", keys[0])
	}
}

// Ошибка 400 от ЮKassa должна попасть в текст ошибки — это то, что окажется в
// логах прода при спорном списании.
func TestCreateRefundErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"type":"error","code":"invalid_request","description":"Refund amount exceeds payment amount"}`))
	}))
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	yk := YooKassa{ShopID: "shop", Secret: "secret"}
	_, err := yk.CreateRefund("pay_1", "9999.00")
	if err == nil {
		t.Fatalf("CreateRefund: want error, got nil")
	}
	if !strings.Contains(err.Error(), "Refund amount exceeds payment amount") {
		t.Fatalf("error = %q, want it to include the response body", err.Error())
	}
}
