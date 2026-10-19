package payment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setAPIBaseURL подменяет пакетный var APIBaseURL и восстанавливает исходное
// значение через t.Cleanup — единый способ во всех тестах, которые его меняют,
// чтобы порядок запуска (в т.ч. t.Parallel()) не оставлял чужую подмену висеть.
func setAPIBaseURL(t *testing.T, url string) {
	t.Helper()
	prev := APIBaseURL
	APIBaseURL = url
	t.Cleanup(func() { APIBaseURL = prev })
}

func TestCreatePayloadEmbedded(t *testing.T) {
	req := CreateRequest{
		OrderID:  "ord_1",
		ValueRub: "990.00",
		Mode:     ModeEmbedded,
		Receipt:  Receipt{Email: "a@b.ru"},
	}
	payload := createPayload(req)

	confirmation, ok := payload["confirmation"].(map[string]string)
	if !ok {
		t.Fatalf("confirmation is not map[string]string: %#v", payload["confirmation"])
	}
	if confirmation["type"] != "embedded" {
		t.Fatalf("confirmation.type = %q, want embedded", confirmation["type"])
	}
	if _, has := confirmation["return_url"]; has {
		t.Fatalf("confirmation has return_url, want absent for embedded")
	}
	metadata, ok := payload["metadata"].(map[string]string)
	if !ok || metadata["order_id"] != "ord_1" {
		t.Fatalf("metadata.order_id = %#v, want ord_1", payload["metadata"])
	}
	if payload["capture"] != true {
		t.Fatalf("capture = %#v, want true", payload["capture"])
	}
	if payload["receipt"] == nil {
		t.Fatalf("receipt is missing")
	}
}

func TestCreatePayloadRedirect(t *testing.T) {
	req := CreateRequest{
		OrderID:   "ord_2",
		ValueRub:  "990.00",
		Mode:      ModeRedirect,
		ReturnURL: "https://mezmo.ru/thanks?orderId=ord_2",
		Receipt:   Receipt{Email: "a@b.ru"},
	}
	payload := createPayload(req)

	confirmation, ok := payload["confirmation"].(map[string]string)
	if !ok {
		t.Fatalf("confirmation is not map[string]string: %#v", payload["confirmation"])
	}
	if confirmation["type"] != "redirect" {
		t.Fatalf("confirmation.type = %q, want redirect", confirmation["type"])
	}
	if confirmation["return_url"] != req.ReturnURL {
		t.Fatalf("confirmation.return_url = %q, want %q", confirmation["return_url"], req.ReturnURL)
	}
}

func TestCreateParsesConfirmationToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "2d1c8e00-000f-5000-8000-1baf1f5f9f8f",
			"status": "pending",
			"confirmation": map[string]string{
				"type":               "embedded",
				"confirmation_token": "ct-2d1c8e00000f50008000",
			},
		})
	}))
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	yk := YooKassa{ShopID: "shop", Secret: "secret"}
	res, err := yk.Create(CreateRequest{OrderID: "ord_3", ValueRub: "990.00", Mode: ModeEmbedded, Receipt: Receipt{Email: "a@b.ru"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.ConfirmationToken != "ct-2d1c8e00000f50008000" {
		t.Fatalf("ConfirmationToken = %q, want ct-2d1c8e00000f50008000", res.ConfirmationToken)
	}
	if res.PaymentID != "2d1c8e00-000f-5000-8000-1baf1f5f9f8f" {
		t.Fatalf("PaymentID = %q, want the id from the response", res.PaymentID)
	}
}

func TestCreateSendsUniqueIdempotenceKey(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotence-Key"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pay_x", "status": "pending"})
	}))
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	yk := YooKassa{ShopID: "shop", Secret: "secret"}
	req := CreateRequest{OrderID: "ord_4", ValueRub: "990.00", Mode: ModeEmbedded, Receipt: Receipt{Email: "a@b.ru"}}
	if _, err := yk.Create(req); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	if _, err := yk.Create(req); err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d requests, want 2", len(keys))
	}
	if keys[0] == "" || keys[1] == "" {
		t.Fatalf("Idempotence-Key is empty: %v", keys)
	}
	if keys[0] == keys[1] {
		t.Fatalf("Idempotence-Key repeated across calls: %q", keys[0])
	}
}

// Форма оплаты должна открываться по-русски в обоих режимах. Без явной локали
// ЮKassa берёт язык браузера, и покупатель из РФ получал английскую форму.
func TestCreatePayloadSetsRussianLocale(t *testing.T) {
	for _, mode := range []string{ModeEmbedded, ModeRedirect} {
		t.Run(mode, func(t *testing.T) {
			payload := createPayload(CreateRequest{
				OrderID:   "ord_1",
				ValueRub:  "990.00",
				Mode:      mode,
				ReturnURL: "https://mezmo.ru/thanks?orderId=ord_1",
				Receipt:   Receipt{Email: "a@b.ru"},
			})
			confirmation, ok := payload["confirmation"].(map[string]string)
			if !ok {
				t.Fatalf("confirmation is not map[string]string: %#v", payload["confirmation"])
			}
			if confirmation["locale"] != "ru_RU" {
				t.Fatalf("confirmation.locale = %q, want ru_RU", confirmation["locale"])
			}
		})
	}
}
