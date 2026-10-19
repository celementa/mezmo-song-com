package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mezmo-song-com/internal/store"
)

func flowMux(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	s, _ := testServer(t)
	mux := http.NewServeMux()
	s.Register(mux)
	return s, mux
}

func postJSON(t *testing.T, mux *http.ServeMux, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Воронка v1: три бесплатные генерации в день проходят, четвёртая — 429.
func TestFlowV1Draft_DailyLimit(t *testing.T) {
	_, mux := flowMux(t)

	var cookies []*http.Cookie
	for i := 0; i < 3; i++ {
		rec := postJSON(t, mux, "/api/flow/v1/draft", `{"recipientName":"Анна","occasion":"birthday"}`, cookies)
		if rec.Code != 200 {
			t.Fatalf("draft %d: code=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["generationSkipped"] == true {
			t.Fatalf("draft %d: генерация пропущена, хотя квота не исчерпана", i+1)
		}
		if len(cookies) == 0 {
			cookies = rec.Result().Cookies() // кука посетителя с первого ответа
			if len(cookies) == 0 {
				t.Fatal("first draft must set the visitor cookie")
			}
		}
	}

	// Квота исчерпана. Это НЕ отказ: заказ создаётся, генерация не запускается,
	// воронка идёт к оплате («сначала заплати, потом песня»). Прежний 429 без
	// заказа означал, что человек не может даже купить — за общим адресом
	// (офис, мобильный оператор) в это упирался и тот, кто ничего не тратил.
	rec := postJSON(t, mux, "/api/flow/v1/draft", `{"recipientName":"Анна"}`, cookies)
	if rec.Code != 200 {
		t.Fatalf("draft после исчерпания квоты: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["generationSkipped"] != true {
		t.Fatalf("ожидался generationSkipped=true, получено %v", body)
	}
	if body["reason"] != "daily_limit_reached" {
		t.Fatalf("reason=%v", body["reason"])
	}
	ref, _ := body["orderId"].(string)
	if ref == "" {
		t.Fatal("заказ не создан — покупатель не может даже оплатить песню")
	}
}

// Воронка v1: без имени получателя заказ не создаётся и квота не тратится.
func TestFlowV1Draft_RequiresRecipient(t *testing.T) {
	_, mux := flowMux(t)
	rec := postJSON(t, mux, "/api/flow/v1/draft", `{}`, nil)
	if rec.Code != 400 {
		t.Fatalf("code=%d", rec.Code)
	}
}

// Воронка v2: заказ стартует по одному email; конфиг доезжает после и
// принимается один раз.
func TestFlowV2_StartThenConfig(t *testing.T) {
	s, mux := flowMux(t)

	rec := postJSON(t, mux, "/api/flow/v2/start", `{"email":"BUYER@x.ru","attribution":{"utm_source":"vk"}}`, nil)
	if rec.Code != 200 {
		t.Fatalf("start: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var started struct {
		OrderID string `json:"orderId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &started)
	if started.OrderID == "" {
		t.Fatal("no orderId")
	}

	id, ok := resolveOrderRef(s.Config.WebhookSecret, started.OrderID)
	if !ok {
		t.Fatalf("bad ref %q", started.OrderID)
	}
	o, err := store.GetOrder(t.Context(), s.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	if o.Flow.String != "v2" || !strings.Contains(o.CustomerJSON, "buyer@x.ru") {
		t.Fatalf("order=%+v", o)
	}
	if hasSongConfig(o) {
		t.Fatal("v2 order must start without song config")
	}
	// Без ключей ЮKassa заказ создаётся paid (dev-режим) — конфиг запускает генерацию.
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("dev-mode payment status=%s", o.PaymentStatus)
	}

	rec = postJSON(t, mux, "/api/orders/"+started.OrderID+"/config", `{"recipientName":"Мама","occasion":"юбилей"}`, nil)
	if rec.Code != 200 {
		t.Fatalf("config: code=%d body=%s", rec.Code, rec.Body.String())
	}
	o, _ = store.GetOrder(t.Context(), s.DB, id)
	if !hasSongConfig(o) || o.Status != "processing" {
		t.Fatalf("after config: status=%s payload=%s", o.Status, o.InputPayloadJSON)
	}

	// Повторный конфиг после старта генерации отклоняется.
	rec = postJSON(t, mux, "/api/orders/"+started.OrderID+"/config", `{"recipientName":"Другая"}`, nil)
	if rec.Code != 409 {
		t.Fatalf("second config: code=%d", rec.Code)
	}
}

// Допники: продаются только когда их есть чем выполнить, выбор до оплаты — у
// pending-заказа, после оплаты — отдельным платежом (в dev-режиме помечается
// оплаченным сразу).
//
// Доступность допника решают настройки: цена 0 = «выполнить нечем», шаг скрыт и
// ручка отказывает. У ускорения условие жёстче — оно продаёт снятие задержки
// доставки, поэтому требует и включённую задержку, и воронку v1 (в v2 песню
// слушают внутри воронки сразу после оплаты, задержка сломала бы сам шаг).
func TestFlowExtras(t *testing.T) {
	s, mux := flowMux(t)
	ctx := t.Context()
	// Мир, в котором оба допника есть чем выполнить.
	if err := store.SetSetting(ctx, s.DB, store.SettingDeliveryDelayMinutes, "120"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(ctx, s.DB, store.SettingUpsellStemsPrice, "190"); err != nil {
		t.Fatal(err)
	}
	seedFlowOrder(t, s.DB, "ord_extras", "v1")

	rec := postJSON(t, mux, "/api/orders/ord_extras/extras/select", `{"id":"speed"}`, nil)
	if rec.Code != 200 {
		t.Fatalf("select: code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = postJSON(t, mux, "/api/orders/ord_extras/extras/select", `{"id":"nope"}`, nil)
	if rec.Code != 400 {
		t.Fatalf("unknown extra: code=%d", rec.Code)
	}
	rec = postJSON(t, mux, "/api/orders/ord_extras/extras/initiate", `{"id":"stems"}`, nil)
	if rec.Code != 409 {
		t.Fatalf("initiate before payment: code=%d", rec.Code)
	}

	if err := store.MarkPaid(ctx, s.DB, "ord_extras"); err != nil {
		t.Fatal(err)
	}
	rec = postJSON(t, mux, "/api/orders/ord_extras/extras/select", `{"id":"stems"}`, nil)
	if rec.Code != 409 {
		t.Fatalf("select after payment: code=%d", rec.Code)
	}
	rec = postJSON(t, mux, "/api/orders/ord_extras/extras/initiate", `{"id":"stems"}`, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"paid":true`) {
		t.Fatalf("initiate dev-mode: code=%d body=%s", rec.Code, rec.Body.String())
	}

	o, _ := store.GetOrder(ctx, s.DB, "ord_extras")
	if got := s.extrasSelectedSum(ctx, o); got != 290 {
		t.Fatalf("selected sum=%d", got)
	}
	paid := store.ExtrasPaid(o)
	if len(paid) != 1 || paid[0].ID != "stems" {
		t.Fatalf("paid extras=%+v", paid)
	}
}

// Выключенный допник не продаётся: стемы по умолчанию стоят 0, потому что
// отдавать их пока нечем (разделение дорожек у Kie.ai не подключено).
func TestFlowExtrasRefusesDisabled(t *testing.T) {
	s, mux := flowMux(t)
	seedFlowOrder(t, s.DB, "ord_off", "v1")
	if err := store.MarkPaid(t.Context(), s.DB, "ord_off"); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, mux, "/api/orders/ord_off/extras/initiate", `{"id":"stems"}`, nil)
	if rec.Code != 409 {
		t.Fatalf("стемы с нулевой ценой: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Ускорение не продаётся в воронке v2: там песню слушают внутри воронки сразу
// после оплаты, задержки доставки нет, и снимать ускорением нечего.
func TestFlowSpeedExtraUnavailableInV2(t *testing.T) {
	s, mux := flowMux(t)
	if err := store.SetSetting(t.Context(), s.DB, store.SettingDeliveryDelayMinutes, "120"); err != nil {
		t.Fatal(err)
	}
	seedFlowOrder(t, s.DB, "ord_v2speed", "v2")

	rec := postJSON(t, mux, "/api/orders/ord_v2speed/extras/select", `{"id":"speed"}`, nil)
	if rec.Code != 409 {
		t.Fatalf("ускорение в v2: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFlowSetEmail(t *testing.T) {
	s, mux := flowMux(t)
	seedOrder(t, s.DB, "ord_email", "pending")

	rec := postJSON(t, mux, "/api/orders/ord_email/email", `{"email":"Fan@Mail.ru","phone":"+79001234567"}`, nil)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	o, _ := store.GetOrder(t.Context(), s.DB, "ord_email")
	if !strings.Contains(o.CustomerJSON, "fan@mail.ru") || !strings.Contains(o.CustomerJSON, "+79001234567") {
		t.Fatalf("customer=%s", o.CustomerJSON)
	}
	if !o.EmailConsent {
		t.Fatal("consent must be recorded")
	}

	rec = postJSON(t, mux, "/api/orders/ord_email/email", `{"email":"not-an-email"}`, nil)
	if rec.Code != 400 {
		t.Fatalf("bad email: code=%d", rec.Code)
	}
}

// Ускорение обязано что-то делать: оплаченный допник снимает задержку
// доставки. Пока он этого не делал, воронка брала 290 ₽ за обещание «получите
// за 10–30 минут» на заказе, который всё равно ждал общий срок.
func TestSpeedExtraClearsDeliveryHold(t *testing.T) {
	s, mux := flowMux(t)
	ctx := t.Context()
	if err := store.SetSetting(ctx, s.DB, store.SettingDeliveryDelayMinutes, "120"); err != nil {
		t.Fatal(err)
	}
	seedFlowOrder(t, s.DB, "ord_speed_hold", "v1")

	// Оплата ставит задержку (в dev-режиме ЮKassa нет, поэтому вызываем то же,
	// что делает вебхук).
	if err := store.MarkPaid(ctx, s.DB, "ord_speed_hold"); err != nil {
		t.Fatal(err)
	}
	s.applyDeliveryHold(ctx, "ord_speed_hold")
	if o, _ := store.GetOrder(ctx, s.DB, "ord_speed_hold"); !o.DeliverAfter.Valid {
		t.Fatal("заказ воронки v1 не придержан — ускорять нечего, а допник продаётся")
	}

	rec := postJSON(t, mux, "/api/orders/ord_speed_hold/extras/initiate", `{"id":"speed"}`, nil)
	if rec.Code != 200 {
		t.Fatalf("доплата за ускорение: code=%d body=%s", rec.Code, rec.Body.String())
	}
	o, _ := store.GetOrder(ctx, s.DB, "ord_speed_hold")
	if o.DeliverAfter.Valid {
		t.Fatal("ускорение оплачено, а задержка осталась — покупатель заплатил ни за что")
	}
}

// Воронка v2 задержку не получает: там песню слушают внутри воронки сразу
// после оплаты, и придержанная доставка сломала бы её собственный шаг.
func TestV2FunnelIsNotHeld(t *testing.T) {
	s, _ := flowMux(t)
	ctx := t.Context()
	if err := store.SetSetting(ctx, s.DB, store.SettingDeliveryDelayMinutes, "120"); err != nil {
		t.Fatal(err)
	}
	seedFlowOrder(t, s.DB, "ord_v2_hold", "v2")
	if err := store.MarkPaid(ctx, s.DB, "ord_v2_hold"); err != nil {
		t.Fatal(err)
	}
	s.applyDeliveryHold(ctx, "ord_v2_hold")
	if o, _ := store.GetOrder(ctx, s.DB, "ord_v2_hold"); o.DeliverAfter.Valid {
		t.Fatal("заказ v2 придержан — шаг прослушивания внутри воронки заблокирован")
	}
}
