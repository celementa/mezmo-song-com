package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/store"
)

// Плечо назначается детерминированно от сессии: перезагрузка страницы не должна
// перекидывать человека между плечами — вместе с плечом у него менялась бы цена
// посреди оформления.
func TestPickArmIsStablePerSession(t *testing.T) {
	arms := []flowArm{flowArms[ArmCom990], flowArms[ArmCom500750s]}
	weights := map[string]int{ArmCom990: 40, ArmCom500750s: 20}
	for _, session := range []string{"s-1", "s-2", "abc", ""} {
		first := pickArm(session, arms, weights)
		for i := 0; i < 20; i++ {
			if got := pickArm(session, arms, weights); got.ID != first.ID {
				t.Fatalf("сессия %q попала в %s, раньше была в %s", session, got.ID, first.ID)
			}
		}
	}
}

// Веса должны реально делить трафик: контроль общий для двух тестов, поэтому
// сплит 40/20 задуман неравным и обязан таким и оставаться.
func TestPickArmRespectsWeights(t *testing.T) {
	arms := []flowArm{flowArms[ArmCom990], flowArms[ArmCom500750s]}
	weights := map[string]int{ArmCom990: 40, ArmCom500750s: 20}
	counts := map[string]int{}
	const n = 3000
	for i := 0; i < n; i++ {
		counts[pickArm("session-"+strconv.Itoa(i), arms, weights).ID]++
	}
	base := float64(counts[ArmCom990]) / float64(n)
	if base < 0.6 || base > 0.73 {
		t.Fatalf("доля %s = %.3f, ожидалось около 0.667 (40 из 60)", ArmCom990, base)
	}
}

// Одно включённое плечо — сплита нет, весь трафик идёт по нему. Это дефолт: тест
// включается осознанно в админке.
func TestSingleArmTakesAllTraffic(t *testing.T) {
	arms := []flowArm{flowArms[ArmCom990]}
	for i := 0; i < 50; i++ {
		if got := pickArm("s"+strconv.Itoa(i), arms, nil); got.ID != ArmCom990 {
			t.Fatalf("получено плечо %s, ожидалось единственное включённое", got.ID)
		}
	}
}

// Цену назначает сервер по плечу. Тело запроса на неё влиять не может — иначе
// покупатель ставил бы себе цену сам, подставив basePrice или чужое плечо.
func TestDraftTakesPriceFromArmNotFromBody(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, s.DB, store.SettingFlowArmsEnabled, ArmCom990+","+ArmCom500750s); err != nil {
		t.Fatalf("settings: %v", err)
	}

	// Сессия, которую распределение отдаёт «ценовому» плечу.
	arms := []flowArm{flowArms[ArmCom990], flowArms[ArmCom500750s]}
	session := ""
	for i := 0; i < 500 && session == ""; i++ {
		candidate := "sess-" + strconv.Itoa(i)
		if pickArm(candidate, arms, store.FlowArmWeights(ctx, s.DB)).ID == ArmCom500750s {
			session = candidate
		}
	}
	if session == "" {
		t.Fatal("не нашлась сессия для плеча " + ArmCom500750s)
	}

	body := `{"sessionId":"` + session + `","email":"a@b.ru","basePrice":1,"price":1,"flowVariant":"` + ArmCom990 + `"}`
	req := httptest.NewRequest("POST", "/api/orders/draft", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleOrdersDraft(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OrderID     string `json:"orderId"`
		FlowVariant string `json:"flowVariant"`
		BasePrice   int    `json:"basePrice"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FlowVariant != ArmCom500750s {
		t.Fatalf("flowVariant = %q, ожидалось %q (плечо считается от сессии, а не из тела)", resp.FlowVariant, ArmCom500750s)
	}
	if resp.BasePrice != flowArms[ArmCom500750s].DefaultPrice {
		t.Fatalf("basePrice = %d, ожидалась цена плеча %d", resp.BasePrice, flowArms[ArmCom500750s].DefaultPrice)
	}

	id, ok := s.resolveOrderID(ctx, resp.OrderID)
	if !ok {
		t.Fatal("ref не резолвится")
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.BasePrice != flowArms[ArmCom500750s].DefaultPrice || o.FlowVariant.String != ArmCom500750s {
		t.Fatalf("в БД base_price=%d flow_variant=%q — цена или плечо взяты не из плеча", o.BasePrice, o.FlowVariant.String)
	}
}

// Второй бесплатной генерации одному человеку не даём: это единственное место,
// где прогон Kie.ai тратится на того, кто ещё не заплатил. Механизм квоты общий
// с воронками /create — кука посетителя, а не sessionId.
func TestPreviewRefusedSecondTimeForSameVisitor(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, s.DB, store.SettingFreeGenerationsPerDay, "1"); err != nil {
		t.Fatalf("settings: %v", err)
	}
	seedOrder(t, s.DB, "ord_prev1", store.PaymentPending)
	seedOrder(t, s.DB, "ord_prev2", store.PaymentPending)

	first, cookies := previewRequest(t, s, "ord_prev1", nil)
	if first != 200 {
		t.Fatalf("первая генерация: status = %d, ожидалось 200", first)
	}
	if len(cookies) == 0 {
		t.Fatal("первый запрос не выдал куку посетителя — квоту не к чему привязать")
	}
	if second, _ := previewRequest(t, s, "ord_prev2", cookies); second != 429 {
		t.Fatalf("вторая генерация того же посетителя: status = %d, ожидалось 429", second)
	}
	// Заказ, у которого превью не стартовало, не должен уехать в processing.
	o, err := store.GetOrder(ctx, s.DB, "ord_prev2")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.PreviewStartedAt.Valid {
		t.Fatal("отказ по лимиту всё равно отметил старт превью")
	}
	// Другой человек (своя кука) генерацию получает — лимит на человека, а не
	// на всех.
	if third, _ := previewRequest(t, s, "ord_prev2", nil); third != 200 {
		t.Fatalf("другой посетитель: status = %d, ожидалось 200", third)
	}
}

// Суточный бюджетный кран закрывает превью для всех, а не только для того, кто
// его исчерпал: он защищает счёт, а не отдельного посетителя.
func TestPreviewRefusedWhenDailyBudgetSpent(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, s.DB, store.SettingPreviewDailyBudget, "1"); err != nil {
		t.Fatalf("settings: %v", err)
	}
	seedOrder(t, s.DB, "ord_b1", store.PaymentPending)
	seedOrder(t, s.DB, "ord_b2", store.PaymentPending)

	if code, _ := previewRequest(t, s, "ord_b1", nil); code != 200 {
		t.Fatalf("первая генерация: status = %d", code)
	}
	if code, _ := previewRequest(t, s, "ord_b2", nil); code != 429 {
		t.Fatalf("другой посетитель после исчерпания бюджета: status = %d, ожидалось 429", code)
	}
	// Кран закрыт — шаги генерации и превью выпадают из флоу, но оплата остаётся.
	rec := httptest.NewRecorder()
	s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid=sess-3", nil))
	var flow struct {
		Steps        []string `json:"steps"`
		PreviewReady bool     `json:"previewReady"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if flow.PreviewReady {
		t.Fatal("previewReady = true при исчерпанном бюджете")
	}
	for _, step := range flow.Steps {
		if step == StepPreview || step == StepGenerating {
			t.Fatalf("шаг %s остался во флоу при закрытом кране", step)
		}
	}
	if !containsStep(flow.Steps, StepPayment) {
		t.Fatal("деградация выбросила оплату — доступность покупки важнее превью")
	}
	_ = ctx
}

// Повторный запрос генерации по тому же заказу — не ошибка и не второй прогон.
func TestPreviewSecondCallOnSameOrderDoesNotRegenerate(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_twice", store.PaymentPending)

	code, cookies := previewRequest(t, s, "ord_twice", nil)
	if code != 200 {
		t.Fatalf("первый вызов: %d", code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/orders/ord_twice/generate-preview", strings.NewReader(`{}`))
	req.SetPathValue("id", "ord_twice")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	s.handleGeneratePreview(rec, req)
	if rec.Code != 200 {
		t.Fatalf("повторный вызов: %d", rec.Code)
	}
	var resp struct {
		AlreadyStarted bool `json:"alreadyStarted"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.AlreadyStarted {
		t.Fatal("повторный вызов не сообщил, что генерация уже идёт")
	}
}

// Ускорение снимает задержку и поднимает цену — но только у неоплаченного
// заказа и только один раз.
func TestSpeedupRaisesPriceOnceAndOnlyBeforePayment(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	enableDeliveryDelay(t, s)
	seedOrder(t, s.DB, "ord_sp", store.PaymentPending)
	if err := store.SetDeliverAfter(ctx, s.DB, "ord_sp", time.Now().Add(2*time.Hour)); err != nil {
		t.Fatalf("hold: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/orders/ord_sp/speedup", nil)
	req.SetPathValue("id", "ord_sp")
	s.handleBuySpeedup(rec, req)
	if rec.Code != 200 {
		t.Fatalf("покупка ускорения: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	o, err := store.GetOrder(ctx, s.DB, "ord_sp")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.BasePrice != 990+store.DefaultUpsellSpeedupPrice {
		t.Fatalf("base_price = %d, ожидалось %d", o.BasePrice, 990+store.DefaultUpsellSpeedupPrice)
	}
	if o.DeliverAfter.Valid {
		t.Fatal("ускорение куплено, а задержка доставки осталась")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/orders/ord_sp/speedup", nil)
	req2.SetPathValue("id", "ord_sp")
	s.handleBuySpeedup(rec2, req2)
	if rec2.Code != 409 {
		t.Fatalf("повторная покупка: status = %d, ожидалось 409", rec2.Code)
	}
	after, _ := store.GetOrder(ctx, s.DB, "ord_sp")
	if after.BasePrice != o.BasePrice {
		t.Fatalf("повторная покупка изменила цену: %d → %d", o.BasePrice, after.BasePrice)
	}
}

// Придержанная доставка не отдаёт песню ни страницей заказа, ни прокси трека:
// иначе платное ускорение не ускоряет ничего.
func TestHeldDeliveryHidesSongUntilDeadline(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_held", store.PaymentPaid)
	if err := store.SaveRelayResult(ctx, s.DB, "ord_held", []string{"orders/ord_held/r0/track-1.mp3"}, "текст", ""); err != nil {
		t.Fatalf("save result: %v", err)
	}
	if err := store.SetDeliverAfter(ctx, s.DB, "ord_held", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("hold: %v", err)
	}

	ref := publicOrderRef(s.Config.WebhookSecret, "ord_held")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/public/order/"+ref+"/content", nil)
	req.SetPathValue("id", ref)
	s.handlePublicOrderContent(rec, req)
	var content struct {
		Orders       []any `json:"orders"`
		DeliveryHeld bool  `json:"deliveryHeld"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &content); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(content.Orders) != 0 {
		t.Fatal("придержанный заказ отдал песню на страницу заказа")
	}
	if !content.DeliveryHeld {
		t.Fatal("страница не узнает о задержке: deliveryHeld = false")
	}

	audio := httptest.NewRecorder()
	areq := httptest.NewRequest("GET", "/api/public/order/"+ref+"/audio/0", nil)
	areq.SetPathValue("id", ref)
	areq.SetPathValue("index", "0")
	s.handleAudioProxy(audio, areq)
	if audio.Code != 404 {
		t.Fatalf("прокси трека при задержке: status = %d, ожидалось 404", audio.Code)
	}

	// Срок вышел — песня доступна без всякого участия планировщика.
	if err := store.SetDeliverAfter(ctx, s.DB, "ord_held", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("hold: %v", err)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/public/order/"+ref+"/content", nil)
	req2.SetPathValue("id", ref)
	s.handlePublicOrderContent(rec2, req2)
	var after struct {
		Orders []any `json:"orders"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &after)
	if len(after.Orders) != 1 {
		t.Fatalf("после срока песня не отдана: orders=%d", len(after.Orders))
	}
}

// Ускоренный заказ не придерживается вообще — иначе за 290 ₽ покупатель получал
// бы ту же задержку.
func TestApplyDeliveryHoldSkipsSpeedupAndInternal(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	enableDeliveryDelay(t, s)

	seedOrder(t, s.DB, "ord_h1", store.PaymentPaid)
	s.applyDeliveryHold(ctx, "ord_h1")
	if o, _ := store.GetOrder(ctx, s.DB, "ord_h1"); !o.DeliverAfter.Valid {
		t.Fatal("обычный заказ не придержан, хотя задержка включена")
	}

	seedOrder(t, s.DB, "ord_h2", store.PaymentPending)
	if ok, err := store.BuySpeedup(ctx, s.DB, "ord_h2", 290); err != nil || !ok {
		t.Fatalf("BuySpeedup: ok=%v err=%v", ok, err)
	}
	s.applyDeliveryHold(ctx, "ord_h2")
	if o, _ := store.GetOrder(ctx, s.DB, "ord_h2"); o.DeliverAfter.Valid {
		t.Fatal("заказ с купленным ускорением всё равно придержали")
	}

	seedOrder(t, s.DB, "ord_h3", store.PaymentPaid)
	if err := store.ApplyDiscount(ctx, s.DB, "ord_h3", store.InternalDiscountCode, 0); err != nil {
		t.Fatalf("discount: %v", err)
	}
	s.applyDeliveryHold(ctx, "ord_h3")
	if o, _ := store.GetOrder(ctx, s.DB, "ord_h3"); o.DeliverAfter.Valid {
		t.Fatal("тестовый заказ из /admin/ads придержали")
	}
}

// Апсейл после оплаты — ОТДЕЛЬНЫЙ заказ. Второй платёж по тому же заказу
// автовозврат дублей (T-020) вернул бы сам и молча.
func TestStemsUpsellCreatesSeparateOrder(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, s.DB, store.SettingUpsellStemsPrice, "190"); err != nil {
		t.Fatalf("settings: %v", err)
	}
	seedOrder(t, s.DB, "ord_parent", store.PaymentPaid)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/orders/ord_parent/upsell/stems", nil)
	req.SetPathValue("id", "ord_parent")
	s.handleUpsellStems(rec, req)
	if rec.Code != 200 {
		t.Fatalf("апсейл: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OrderID string `json:"orderId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	childID, ok := s.resolveOrderID(ctx, resp.OrderID)
	if !ok || childID == "ord_parent" {
		t.Fatalf("апсейл вернул ref %q — ожидался новый заказ, а не родитель", resp.OrderID)
	}
	child, err := store.GetOrder(ctx, s.DB, childID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.ParentOrderID.String != "ord_parent" || child.UpsellKind.String != store.UpsellStems {
		t.Fatalf("дочерний заказ не привязан: parent=%q kind=%q", child.ParentOrderID.String, child.UpsellKind.String)
	}
	if child.BasePrice != 190 {
		t.Fatalf("цена апсейла = %d, ожидалось 190", child.BasePrice)
	}
	// У родителя платёж по-прежнему один: апсейл в его историю не попал.
	parent, _ := store.GetOrder(ctx, s.DB, "ord_parent")
	if parent.BasePrice != 990 {
		t.Fatalf("цена родителя изменилась: %d", parent.BasePrice)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/orders/ord_parent/upsell/stems", nil)
	req2.SetPathValue("id", "ord_parent")
	s.handleUpsellStems(rec2, req2)
	if rec2.Code != 409 {
		t.Fatalf("повторный апсейл: status = %d, ожидалось 409", rec2.Code)
	}
}

// Бриф после шага 3 дописывает ответы, но цену и плечо через него не переписать,
// а после старта генерации он закрыт: иначе клиенту показывали бы одно, а пели
// по другому.
func TestBriefMergesFieldsButNotPriceAndLocksAfterGeneration(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_brief", store.PaymentPending)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/orders/ord_brief/brief",
		strings.NewReader(`{"story":"наша история","basePrice":1,"flowVariant":"variant3-500-750s"}`))
	req.SetPathValue("id", "ord_brief")
	s.handleOrderBrief(rec, req)
	if rec.Code != 200 {
		t.Fatalf("бриф: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	o, _ := store.GetOrder(ctx, s.DB, "ord_brief")
	if !strings.Contains(o.InputPayloadJSON, "наша история") {
		t.Fatalf("бриф не сохранён: %s", o.InputPayloadJSON)
	}
	if strings.Contains(o.InputPayloadJSON, "basePrice") || strings.Contains(o.InputPayloadJSON, "flowVariant") {
		t.Fatalf("бриф протащил служебные поля: %s", o.InputPayloadJSON)
	}
	if o.BasePrice != 990 {
		t.Fatalf("бриф изменил цену: %d", o.BasePrice)
	}

	if _, err := store.ConfirmOrder(ctx, s.DB, "ord_brief"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("PATCH", "/api/orders/ord_brief/brief", strings.NewReader(`{"story":"поздняя правка"}`))
	req2.SetPathValue("id", "ord_brief")
	s.handleOrderBrief(rec2, req2)
	if rec2.Code != 409 {
		t.Fatalf("бриф после старта генерации: status = %d, ожидалось 409", rec2.Code)
	}
}

// Ссылка «продолжить оформление» восстанавливает анкету по ref с сервера:
// ответы шагов 1–5 в ключах фронта, ссылка в статусе — только пока заказ не
// оплачен и генерация не пошла; после этого ручка отвечает 409.
func TestOrderResumeReturnsAnswersUntilPaidOrGenerating(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_resume", store.PaymentPending)
	if err := store.SetOrderFlow(ctx, s.DB, "ord_resume", ArmCom500750s, 990); err != nil {
		t.Fatalf("set flow: %v", err)
	}
	if _, err := store.MergeInputPayload(ctx, s.DB, "ord_resume", map[string]any{
		"relationship": "Мама", "occasion": "Юбилей", "emotion": "Любовь",
		"musicPreferences": map[string]any{"genre": "Pop"}, "story": "история",
	}); err != nil {
		t.Fatalf("brief: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/public/order/ord_resume/resume", nil)
	req.SetPathValue("id", "ord_resume")
	s.handlePublicOrderResume(rec, req)
	if rec.Code != 200 {
		t.Fatalf("resume: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		FlowVariant string            `json:"flowVariant"`
		Answers     map[string]string `json:"answers"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.FlowVariant != ArmCom500750s {
		t.Fatalf("flowVariant = %q", body.FlowVariant)
	}
	want := map[string]string{"relationship": "Мама", "recipientName": "Ира", "occasion": "Юбилей",
		"email": "a@b.ru", "emotion": "Любовь", "genre": "Pop", "story": "история"}
	for k, v := range want {
		if body.Answers[k] != v {
			t.Fatalf("answers[%s] = %q, want %q (all: %v)", k, body.Answers[k], v, body.Answers)
		}
	}

	o, _ := store.GetOrder(ctx, s.DB, "ord_resume")
	if got := s.resumeURL(o); got != "/config?arm=variant3-500-750s&order="+url.QueryEscape(publicOrderRef(s.Config.WebhookSecret, "ord_resume")) {
		t.Fatalf("resumeURL = %q", got)
	}

	if _, err := store.ConfirmOrder(ctx, s.DB, "ord_resume"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/public/order/ord_resume/resume", nil)
	req2.SetPathValue("id", "ord_resume")
	s.handlePublicOrderResume(rec2, req2)
	if rec2.Code != 409 {
		t.Fatalf("resume после старта генерации: status = %d, ожидалось 409", rec2.Code)
	}
	o2, _ := store.GetOrder(ctx, s.DB, "ord_resume")
	if s.resumeURL(o2) != "" {
		t.Fatalf("resumeURL после генерации должен быть пуст")
	}
}

// Письма флоу отправляются один раз: два тика планировщика или два вызова не
// должны задваивать письмо.
func TestFlowEmailClaimsAreOnce(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_mail", store.PaymentPaid)

	for i, want := range []bool{true, false} {
		got, err := store.ClaimWelcomeEmail(ctx, s.DB, "ord_mail", time.Now())
		if err != nil {
			t.Fatalf("claim welcome: %v", err)
		}
		if got != want {
			t.Fatalf("попытка %d: claim = %v, ожидалось %v", i+1, got, want)
		}
	}
}

// enableDeliveryDelay включает задержку обычной доставки: по умолчанию она
// выключена, поэтому тесты про придержанную доставку включают её явно.
func enableDeliveryDelay(t *testing.T, s *Server) {
	t.Helper()
	if err := store.SetSetting(context.Background(), s.DB, store.SettingDeliveryDelayMinutes, "120"); err != nil {
		t.Fatalf("settings: %v", err)
	}
}

// previewRequest запускает генерацию до оплаты. cookies — кука посетителя из
// прошлого ответа: квота считается по ней, поэтому «тот же человек» в тесте
// означает «те же куки», а не «та же sessionId».
func previewRequest(t *testing.T, s *Server, orderID string, cookies []*http.Cookie) (int, []*http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/orders/"+orderID+"/generate-preview", strings.NewReader(`{}`))
	req.SetPathValue("id", orderID)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	s.handleGeneratePreview(rec, req)
	got := rec.Result().Cookies()
	if len(got) == 0 {
		got = cookies
	}
	return rec.Code, got
}

func containsStep(steps []string, step string) bool {
	for _, s := range steps {
		if s == step {
			return true
		}
	}
	return false
}

// Дефолтный состав шагов: ускорение и стемы выключены (задержки доставки нет,
// стемы отдавать нечем), поэтому во флоу их нет.
//
// Мок сознательно отличается: там задержка и стемы включены, чтобы все экраны
// воронок были проходимы (см. комментарий у adminSettings в mock-api). Так что
// это тест прод-дефолтов, а не паритета с моком.
func TestFlowDefaultStepsHideDisabledUpsells(t *testing.T) {
	s := orderRefTestServer(t)
	rec := httptest.NewRecorder()
	s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid=any", nil))

	var flow struct {
		FlowVariant string   `json:"flowVariant"`
		Steps       []string `json:"steps"`
		Price       int      `json:"price"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{
		StepRecipient, StepOccasion, StepEmail, StepEmotion, StepStory,
		// Подписки без ЮKassa нет — апсейла после оплаты тоже.
		StepGenerating, StepPreview, StepPayment, StepDone,
	}
	if strings.Join(flow.Steps, ",") != strings.Join(want, ",") {
		t.Fatalf("шаги = %v, ожидалось %v", flow.Steps, want)
	}
	if flow.FlowVariant != ArmCom990 {
		t.Fatalf("плечо = %q, по умолчанию сплита нет и ожидалось %q", flow.FlowVariant, ArmCom990)
	}
	if flow.Price != 25 {
		t.Fatalf("цена = %d, ожидалось 25", flow.Price)
	}
}

// Правка цены одного плеча приходит со страницы настроек патчем из одного
// элемента. Она не должна выключать остальные плечи и не должна терять их веса:
// состав ротации сливается с текущим состоянием, а не собирается из патча.
func TestAdminSettingsArmPatchKeepsOtherArms(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()

	// Исходно включены оба плеча с разными весами.
	putSettings(t, s, `{"flowArms":[{"id":"`+ArmCom990+`","enabled":true,"weight":40},{"id":"`+ArmCom500750s+`","enabled":true,"weight":20}]}`, 200)

	// Патч только цены базового плеча.
	putSettings(t, s, `{"flowArms":[{"id":"`+ArmCom990+`","price":1100}]}`, 200)

	enabled := store.FlowArmsEnabled(ctx, s.DB, nil)
	if len(enabled) != 2 {
		t.Fatalf("после правки цены включённых плеч %v, ожидалось два", enabled)
	}
	weights := store.FlowArmWeights(ctx, s.DB)
	if weights[ArmCom990] != 40 || weights[ArmCom500750s] != 20 {
		t.Fatalf("веса сбились: %v", weights)
	}
	if price := store.FlowArmPrice(ctx, s.DB, ArmCom990, 990); price != 1100 {
		t.Fatalf("цена = %d, ожидалось 1100", price)
	}
}

// Выключить все плечи нельзя: сайт без плеча не работает.
func TestAdminSettingsRejectsEmptyArmRotation(t *testing.T) {
	s := orderRefTestServer(t)
	putSettings(t, s, `{"flowArms":[{"id":"`+ArmCom990+`","enabled":false},{"id":"`+ArmCom500750s+`","enabled":false}]}`, 400)
	if arms := s.enabledArms(context.Background()); len(arms) == 0 {
		t.Fatal("после отклонённого патча не осталось ни одного плеча")
	}
}

// Ускорение нельзя продавать, пока задержки обычной доставки нет: покупатель
// заплатил бы за снятие того, чего не существует.
func TestSpeedupRefusedWithoutDeliveryDelay(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_nodelay", store.PaymentPending)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/orders/ord_nodelay/speedup", nil)
	req.SetPathValue("id", "ord_nodelay")
	s.handleBuySpeedup(rec, req)
	if rec.Code != 409 {
		t.Fatalf("status = %d, ожидалось 409 (задержка выключена)", rec.Code)
	}
	if o, _ := store.GetOrder(context.Background(), s.DB, "ord_nodelay"); o.BasePrice != 990 {
		t.Fatalf("цена изменилась без задержки: %d", o.BasePrice)
	}
}

func putSettings(t *testing.T, s *Server, body string, wantCode int) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/admin/settings", strings.NewReader(body))
	s.handleAdminSettingsPut(rec, req)
	if rec.Code != wantCode {
		t.Fatalf("PUT settings: status = %d, ожидалось %d, body=%s", rec.Code, wantCode, rec.Body.String())
	}
}

// Рубильник превью: выключенная генерация до оплаты убирает шаги «пишем песню»
// и «слушайте» из воронки, но оплату оставляет. Нужен на случай, когда Kie.ai
// отвечает долго и превью превращается в бесконечное ожидание.
func TestPreviewDisabledSwitchDropsStepsButKeepsPayment(t *testing.T) {
	s := orderRefTestServer(t)
	if err := store.SetSetting(context.Background(), s.DB, store.SettingPreviewEnabled, "0"); err != nil {
		t.Fatalf("settings: %v", err)
	}

	rec := httptest.NewRecorder()
	s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid=off", nil))
	var flow struct {
		Steps         []string `json:"steps"`
		PreviewReady  bool     `json:"previewReady"`
		PreviewReason string   `json:"previewReason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if flow.PreviewReady {
		t.Fatal("previewReady = true при выключенном превью")
	}
	if flow.PreviewReason != "preview_disabled" {
		t.Fatalf("причина = %q, ожидалась preview_disabled", flow.PreviewReason)
	}
	for _, step := range flow.Steps {
		if step == StepPreview || step == StepGenerating {
			t.Fatalf("шаг %s остался во флоу при выключенном превью", step)
		}
	}
	if !containsStep(flow.Steps, StepPayment) {
		t.Fatal("выключение превью убрало оплату — продавать важнее, чем показывать")
	}

	// И воронка /create создаёт заказ без генерации, а не отказывает.
	seedOrder(t, s.DB, "ord_off_prev", store.PaymentPending)
	if code, _ := previewRequest(t, s, "ord_off_prev", nil); code != 429 {
		t.Fatalf("generate-preview при выключенном превью: %d, ожидалось 429", code)
	}
}

// Ссылка рекламы `?arm=variant3-500-750s` открывает это плечо и закрепляет его
// кукой: следующий запрос без ?arm= остаётся в том же плече и по той же цене.
func TestForcedArmPinsByLink(t *testing.T) {
	s := orderRefTestServer(t)
	// Форсится только включённое плечо: снятое с публикации недоступно и по ссылке.
	if err := store.SetSetting(context.Background(), s.DB, store.SettingFlowArmsEnabled, ArmCom990+","+ArmCom500750s); err != nil {
		t.Fatal(err)
	}

	// Сессия, которую ротация сама отправила бы в другое плечо, — иначе кука не нужна.
	session := ""
	arms := []flowArm{flowArms[ArmCom990], flowArms[ArmCom500750s]}
	for i := 0; i < 1000; i++ {
		if candidate := "sess-pin-" + strconv.Itoa(i); pickArm(candidate, arms, nil).ID == ArmCom990 {
			session = candidate
			break
		}
	}
	rec := httptest.NewRecorder()
	s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid="+session+"&arm=variant3-500-750s", nil))
	var flow struct {
		FlowVariant string `json:"flowVariant"`
		Price       int    `json:"price"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if flow.FlowVariant != ArmCom500750s {
		t.Fatalf("плечо по ссылке = %q, ожидалось %q", flow.FlowVariant, ArmCom500750s)
	}
	if flow.Price != 19 {
		t.Fatalf("цена плеча = %d, ожидалось 19", flow.Price)
	}

	var pinned *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == forcedArmCookie {
			pinned = c
		}
	}
	if pinned == nil {
		t.Fatal("плечо не закреплено кукой")
	}
	next := httptest.NewRequest("GET", "/api/public/flow?sid="+session, nil)
	next.AddCookie(pinned)
	rec2 := httptest.NewRecorder()
	s.handlePublicFlow(rec2, next)
	if err := json.Unmarshal(rec2.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	if flow.FlowVariant != ArmCom500750s {
		t.Fatalf("после закрепления плечо = %q, ожидалось %q", flow.FlowVariant, ArmCom500750s)
	}
}

// Ссылка с несуществующим или выключенным плечом уводит человека в обычную
// ротацию — молча. Счётчик arm_link_misses и есть способ это заметить: без
// него кампания месяц льёт трафик не в то плечо по не той цене.
func TestArmLinkMissesRecorded(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()

	// Опечатка в имени: variant3-500-750 вместо variant3-500-750s.
	rec := httptest.NewRecorder()
	s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid=miss-1&arm=variant3-500-750", nil))
	// Существующее плечо, выключенное в админке (в ротации по умолчанию только variant3-990).
	rec2 := httptest.NewRecorder()
	s.handlePublicFlow(rec2, httptest.NewRequest("GET", "/api/public/flow?sid=miss-2&arm=variant4-990", nil))

	rows, err := s.DB.QueryContext(ctx, `SELECT name, reason, hits FROM arm_link_misses ORDER BY name`)
	if err != nil {
		t.Fatalf("чтение счётчика: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, reason string
		var hits int
		if err := rows.Scan(&name, &reason, &hits); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = reason
		if hits != 1 {
			t.Fatalf("%s: hits = %d, ожидалась 1", name, hits)
		}
	}
	if got["variant3-500-750"] != store.ArmMissUnknown {
		t.Fatalf("опечатка в имени не записана как %q: %v", store.ArmMissUnknown, got)
	}
	if got["variant4-990"] != store.ArmMissDisabled {
		t.Fatalf("выключенное плечо не записано как %q: %v", store.ArmMissDisabled, got)
	}
}

// В адресную строку и в аналитику уходит имя плеча из рекламной ссылки; у .com
// оно совпадает с id.
func TestFlowResponseReturnsCanonicalArmName(t *testing.T) {
	s := orderRefTestServer(t)
	if err := store.SetSetting(context.Background(), s.DB, store.SettingFlowArmsEnabled,
		strings.Join(KnownFlowArms, ",")); err != nil {
		t.Fatalf("состав ротации: %v", err)
	}
	for _, name := range KnownFlowArms {
		rec := httptest.NewRecorder()
		s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid=name-"+name+"&arm="+name, nil))
		var body struct {
			FlowVariant string `json:"flowVariant"`
			ArmName     string `json:"armName"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if body.FlowVariant != name || body.ArmName != name {
			t.Fatalf("%s: flowVariant=%q armName=%q", name, body.FlowVariant, body.ArmName)
		}
	}
}

// Имя плеча — контракт с рекламщиками, унаследованный от .ru: числа в имени —
// рублёвые цены mezmo.ru (500 → $15, 750 → $19, 990 → $25), слева подписка,
// справа разовая, суффикс s — предвыбор на экране сравнения.
func TestArmLinkNamesMatchPrices(t *testing.T) {
	cases := []struct {
		arm            string
		oneTime, sub   int
		compareDefault string
		route          string
	}{
		{ArmCom990, 25, subscriptionPriceRub, "", ""},
		{ArmCom500750s, 19, 15, CompareOneTime, ""},
		{ArmComPrepay990, 25, subscriptionPriceRub, "", routeCreatePrepay},
		{ArmComPrepay500750s, 19, 15, CompareOneTime, routeCreatePrepay},
	}
	for _, c := range cases {
		arm, ok := flowArms[c.arm]
		if !ok {
			t.Fatalf("%s: плечо не найдено", c.arm)
		}
		if arm.DefaultPrice != c.oneTime || armSubscriptionPrice(arm) != c.sub || arm.CompareDefault != c.compareDefault || arm.Route != c.route {
			t.Errorf("%s: разовая=%d подписка=%d предвыбор=%q route=%q, ждали %d/%d/%q/%q",
				c.arm, arm.DefaultPrice, armSubscriptionPrice(arm), arm.CompareDefault, arm.Route, c.oneTime, c.sub, c.compareDefault, c.route)
		}
	}
}

// Пока подписку продать нечем (UnitPay без рекуррентки), ценовое плечо не
// показывает экран сравнения и апсейл подписки, а продаёт разовую цену.
func TestFlowHidesSubscriptionStepsWithoutProvider(t *testing.T) {
	s := orderRefTestServer(t)
	s.Config.UnitPayProjectID, s.Config.UnitPaySecretKey = "1", "sk"
	if err := store.SetSetting(context.Background(), s.DB, store.SettingFlowArmsEnabled, ArmCom500750s); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.handlePublicFlow(rec, httptest.NewRequest("GET", "/api/public/flow?sid=x", nil))
	var flow struct {
		Steps                []string `json:"steps"`
		Price                int      `json:"price"`
		SubscriptionsEnabled bool     `json:"subscriptionsEnabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slices.Contains(flow.Steps, StepCompare) || slices.Contains(flow.Steps, StepSubscription) || flow.SubscriptionsEnabled {
		t.Fatalf("подписочные шаги не скрыты: %+v", flow)
	}
	if flow.Price != 19 {
		t.Fatalf("цена = %d, ожидалась разовая 19", flow.Price)
	}

	req := httptest.NewRequest("POST", "/api/subscription/start", strings.NewReader(`{"email":"a@b.c"}`))
	rec = httptest.NewRecorder()
	s.requireSubscriptions(s.handleSubscriptionStart)(rec, req)
	if rec.Code != 503 {
		t.Fatalf("оформление подписки без провайдера: код %d, ожидался 503", rec.Code)
	}
}
