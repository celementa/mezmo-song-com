package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"mezmo-song-com/internal/store"
)

func seedBranchOrder(t *testing.T, s *Server, id, arm string) {
	t.Helper()
	seedOrder(t, s.DB, id, store.PaymentPending)
	if err := store.SetOrderFlow(context.Background(), s.DB, id, arm, 19); err != nil {
		t.Fatalf("set flow: %v", err)
	}
}

func postBranch(t *testing.T, s *Server, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/orders/"+id+"/payment-branch", strings.NewReader(body))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.handleSetPaymentBranch(rec, req)
	return rec
}

// Выбор ветки — единственное место, где появляется цена. Рублёвая и долларовая
// цены берутся из разных настроек, а не пересчитываются по курсу.
func TestSetPaymentBranchChoosesPriceAndCurrency(t *testing.T) {
	s := orderRefTestServer(t)
	seedBranchOrder(t, s, "ord_br1", ArmCom500750s)

	rec := postBranch(t, s, "ord_br1", `{"branch":"ru"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Branch, Currency, Provider string
		Amount                     int
		SubscriptionAmount         int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Branch != "ru" || out.Currency != "RUB" || out.Provider != "yookassa" {
		t.Fatalf("ветка = %+v, ждали ru/RUB/yookassa", out)
	}
	if out.Amount != 750 || out.SubscriptionAmount != 500 {
		t.Fatalf("цены = %d/%d, ждали 750/500", out.Amount, out.SubscriptionAmount)
	}

	o, err := store.GetOrder(context.Background(), s.DB, "ord_br1")
	if err != nil {
		t.Fatal(err)
	}
	if o.PaymentBranch.String != "ru" || o.Currency.String != "RUB" || o.BasePrice != 750 {
		t.Fatalf("заказ = branch %q currency %q base %d, ждали ru/RUB/750",
			o.PaymentBranch.String, o.Currency.String, o.BasePrice)
	}
}

func TestSetPaymentBranchSwitchesToIntl(t *testing.T) {
	s := orderRefTestServer(t)
	seedBranchOrder(t, s, "ord_br2", ArmCom990)

	rec := postBranch(t, s, "ord_br2", `{"branch":"intl"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Currency           string
		Provider           string
		Amount             int
		SubscriptionAmount int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Currency != "USD" || out.Provider != "stripe" {
		t.Fatalf("ветка = %+v, ждали USD/stripe", out)
	}
	if out.Amount != 25 || out.SubscriptionAmount != 25 {
		t.Fatalf("цены = %d/%d, ждали 25/25", out.Amount, out.SubscriptionAmount)
	}
}

// «← Назад» разрешает сменить ветку до оплаты, но не после: иначе можно
// поменять валюту уже оплаченного заказа.
func TestSetPaymentBranchRejectsPaidOrder(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_br_paid", store.PaymentPaid)
	if err := store.SetOrderFlow(context.Background(), s.DB, "ord_br_paid", ArmCom500750s, 19); err != nil {
		t.Fatal(err)
	}
	if rec := postBranch(t, s, "ord_br_paid", `{"branch":"ru"}`); rec.Code != 409 {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

// Экран «подписка / разовая» показывает обе цены и после того, как план уже
// выбран: выбрав подписку, покупатель не должен увидеть её цену в карточке
// разовой покупки (было «500/500» вместо «500/750»).
func TestSetPaymentBranchKeepsOneTimePriceForCompare(t *testing.T) {
	s := orderRefTestServer(t)
	seedBranchOrder(t, s, "ord_br_sub", ArmCom500750s)

	rec := postBranch(t, s, "ord_br_sub", `{"branch":"ru","plan":"subscription"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Amount             int
		OneTimeAmount      int
		SubscriptionAmount int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Amount != 500 || out.OneTimeAmount != 750 || out.SubscriptionAmount != 500 {
		t.Fatalf("цены = amount %d / one-time %d / sub %d, ждали 500/750/500",
			out.Amount, out.OneTimeAmount, out.SubscriptionAmount)
	}

	// Возврат на разовую покупку возвращает и сумму заказа.
	rec = postBranch(t, s, "ord_br_sub", `{"branch":"ru","plan":"onetime"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Amount != 750 || out.OneTimeAmount != 750 {
		t.Fatalf("после возврата к разовой: amount %d / one-time %d, ждали 750/750", out.Amount, out.OneTimeAmount)
	}
}

// Ветка, которая не умеет подписку (UnitPay), не должна принимать план
// subscription: иначе заказ остаётся с неоплачиваемым планом, а человек узнаёт
// об этом только на шаге оплаты (503 subscriptions_unavailable).
func TestSetPaymentBranchRejectsSubscriptionOnUnitPayBranch(t *testing.T) {
	s := orderRefTestServer(t)
	// Ключи ЮKassa есть — значит подписка продаётся в принципе, и гейт ниже
	// проверяет именно провайдера ветки, а не глобальную доступность.
	s.Config.YooKassaShopID = "shop"
	s.Config.YooKassaSecretKey = "secret"
	if err := store.SetSetting(context.Background(), s.DB, store.SettingPaymentIntlProvider, "unitpay"); err != nil {
		t.Fatal(err)
	}
	seedBranchOrder(t, s, "ord_br_unit", ArmCom500750s)

	if rec := postBranch(t, s, "ord_br_unit", `{"branch":"intl","plan":"subscription"}`); rec.Code != 409 {
		t.Fatalf("подписка в ветке UnitPay: status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
	// Разовая покупка в той же ветке — по-прежнему можно.
	if rec := postBranch(t, s, "ord_br_unit", `{"branch":"intl","plan":"onetime"}`); rec.Code != 200 {
		t.Fatalf("разовая в ветке UnitPay: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	// Ветка ЮKassa подписку принимает.
	if rec := postBranch(t, s, "ord_br_unit", `{"branch":"ru","plan":"subscription"}`); rec.Code != 200 {
		t.Fatalf("подписка в ветке ЮKassa: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetPaymentBranchValidatesInput(t *testing.T) {
	s := orderRefTestServer(t)
	seedBranchOrder(t, s, "ord_br3", ArmCom990)
	if rec := postBranch(t, s, "ord_br3", `{"branch":"crypto"}`); rec.Code != 400 {
		t.Fatalf("неизвестная ветка: status = %d, want 400", rec.Code)
	}
	if rec := postBranch(t, s, "ord_br3", `{}`); rec.Code != 400 {
		t.Fatalf("пустая ветка: status = %d, want 400", rec.Code)
	}
}
