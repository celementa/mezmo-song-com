package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/store"
)

// Заказ воронки /create после оплаты обязан вернуться В ВОРОНКУ, а не на
// /thanks.
//
// Иначе покупатель v2 («оплата в начале») платил на втором шаге из тринадцати и
// уходил на «спасибо», где вопросы о песне ему уже никто не задавал: payload
// оставался пустым, вебхук генерацию не запускал, а открытие /order/{ref}
// подтверждало заказ и генерировало песню по ПУСТОМУ брифу. У v1 тем же
// способом отрезался весь хвост после оплаты (подписка, стемы, «готово»).
func TestInitiatePaymentReturnsFunnelOrderToFunnel(t *testing.T) {
	var gotReturnURL string
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Confirmation struct {
				ReturnURL string `json:"return_url"`
			} `json:"confirmation"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotReturnURL = body.Confirmation.ReturnURL
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "pay_flow_1",
			"status":       "pending",
			"confirmation": map[string]string{"type": "redirect", "confirmation_url": "https://yoo/pay"},
		})
	})
	seedFlowOrder(t, s.DB, "ord_flow_v2", "v2")

	initiateRedirect(t, s, "ord_flow_v2")

	if !strings.Contains(gotReturnURL, "/create?order=") {
		t.Fatalf("return_url = %q, ожидался возврат в воронку /create?order=…", gotReturnURL)
	}
	if strings.Contains(gotReturnURL, "/thanks") {
		t.Fatalf("return_url = %q — заказ воронки уводят на /thanks", gotReturnURL)
	}
}

// Заказы прежнего флоу по-прежнему возвращаются на /thanks: это единственная
// точка браузерной отбивки покупки для них.
func TestInitiatePaymentKeepsThanksForLegacyOrder(t *testing.T) {
	var gotReturnURL string
	s := yooTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Confirmation struct {
				ReturnURL string `json:"return_url"`
			} `json:"confirmation"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotReturnURL = body.Confirmation.ReturnURL
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "pay_legacy_1",
			"status":       "pending",
			"confirmation": map[string]string{"type": "redirect", "confirmation_url": "https://yoo/pay"},
		})
	})
	seedOrder(t, s.DB, "ord_legacy", store.PaymentPending)

	initiateRedirect(t, s, "ord_legacy")

	if !strings.Contains(gotReturnURL, "/thanks?orderId=") {
		t.Fatalf("return_url = %q, для заказа прежнего флоу ожидался /thanks", gotReturnURL)
	}
}

func initiateRedirect(t *testing.T, s *Server, orderID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/orders/"+orderID+"/initiate-payment", strings.NewReader(`{"mode":"redirect"}`))
	req.SetPathValue("id", orderID)
	s.handleInitiatePayment(rec, req)
	if rec.Code != 200 {
		t.Fatalf("initiate-payment: status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

// seedFlowOrder — заказ воронки /create (колонка flow заполнена).
func seedFlowOrder(t *testing.T, db *sql.DB, id, flow string) {
	t.Helper()
	seedOrder(t, db, id, store.PaymentPending)
	if _, err := db.ExecContext(context.Background(), `UPDATE orders SET flow = ? WHERE id = ?`, flow, id); err != nil {
		t.Fatalf("set flow: %v", err)
	}
}

// Подтверждение оплаты нельзя переиспользовать, если сумма заказа изменилась.
//
// Путь через обычный UI: покупатель дошёл до оплаты (подтверждение выдано),
// вернулся на сводку, добавил ускорение — и снова нажал «Оплатить». Прежнее
// подтверждение списало бы старую сумму, а вебхук ждёт новую и отклонил бы
// настоящую оплату как amount_too_small: деньги списаны, заказ не оплачен.
func TestInitiatePaymentDoesNotReuseConfirmationAfterAmountChange(t *testing.T) {
	var created int
	var lastAmount string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v3/payments", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Amount struct{ Value string } `json:"amount"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		created++
		lastAmount = body.Amount.Value
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           fmt.Sprintf("pay_%d", created),
			"status":       "pending",
			"amount":       map[string]string{"value": body.Amount.Value, "currency": "RUB"},
			"confirmation": map[string]string{"type": "redirect", "confirmation_url": "https://yoo/pay"},
		})
	})
	// Первый платёж остаётся pending и «висит» с прежней суммой.
	mux.HandleFunc("GET /v3/payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     r.PathValue("id"),
			"status": "pending",
			"paid":   false,
			"amount": map[string]string{"value": "990.00", "currency": "RUB"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setAPIBaseURL(t, srv.URL)

	s, _ := testServer(t)
	s.Config = config.Config{PublicAppURL: "http://localhost:8080", YooKassaShopID: "shop", YooKassaSecretKey: "secret"}
	seedFlowOrder(t, s.DB, "ord_amount", "v1")
	// Ускорение продаётся только при включённой задержке доставки — иначе его
	// цена 0 и сумма заказа не меняется (см. Server.extraPrice).
	if err := store.SetSetting(context.Background(), s.DB, store.SettingDeliveryDelayMinutes, "120"); err != nil {
		t.Fatalf("settings: %v", err)
	}

	initiateRedirect(t, s, "ord_amount")
	if created != 1 || lastAmount != "990.00" {
		t.Fatalf("первый платёж: создано=%d сумма=%s", created, lastAmount)
	}

	// Покупатель добавил ускорение — сумма заказа выросла.
	if err := store.SelectExtra(context.Background(), s.DB, "ord_amount", "speed"); err != nil {
		t.Fatalf("select extra: %v", err)
	}

	initiateRedirect(t, s, "ord_amount")
	if created != 2 {
		t.Fatalf("после смены суммы платежей создано %d — отдали прежнее подтверждение на старую сумму", created)
	}
	if lastAmount != "1280.00" {
		t.Fatalf("новый платёж на %s, ожидалось 1280.00 (990 + 290 за ускорение)", lastAmount)
	}
}
