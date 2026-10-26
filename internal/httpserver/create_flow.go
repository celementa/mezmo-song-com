// Ручки новых воронок заказа (/create): v1 «оплата поздно» — анонимная
// генерация до оплаты с дневной квотой, v2 «оплата в начале» — заказ по одному
// email с доездом вопросов после оплаты. Плюс допники (ускорение/стемы) и
// письма-триггеры. Регистрируются из Server.Register.
package httpserver

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

// flowExtra — допник воронки. Название нужно чеку 54-ФЗ, цена приходит из
// настроек: те же поля, что у апсейлов варианта 1 (upsell.speedup_price /
// upsell.stems_price), чтобы одна цена не жила в двух местах.
type flowExtra struct {
	Title string // строка чека 54-ФЗ
}

const (
	extraSpeed = "speed"
	extraStems = "stems"
)

var flowExtras = map[string]flowExtra{
	extraSpeed: {Title: "Ускорение создания песни"},
	extraStems: {Title: "Инструментал и вокал отдельными файлами"},
}

// extraPrice — цена допника из настроек. 0 означает «не продаётся»: шаг не
// показывается и ручки отказывают.
//
// У ускорения есть второе условие: оно продаёт снятие задержки доставки, а
// задержку получают только заказы воронки v1 (в v2 песню слушают внутри
// воронки сразу после оплаты, и задержка сломала бы сам шаг прослушивания).
// Значит в v2 ускорять нечего — цена там 0, сколько бы ни стояло в настройках.
func (s *Server) extraPrice(ctx context.Context, o store.Order, id string) int {
	speedup, stems := store.UpsellPrices(ctx, s.DB)
	switch id {
	case extraSpeed:
		if o.Flow.String != "v1" || store.DeliveryDelay(ctx, s.DB) <= 0 {
			return 0
		}
		return speedup
	case extraStems:
		return stems
	default:
		return 0
	}
}

// extraPrices — каталог допников этого заказа для фронта: цена 0 = шаг скрыт.
func (s *Server) extraPrices(ctx context.Context, o store.Order) map[string]int {
	out := make(map[string]int, len(flowExtras))
	for id := range flowExtras {
		out[id] = s.extraPrice(ctx, o, id)
	}
	return out
}

// hasSongConfig — заполнены ли вопросы о песне (v2 оплачивается до них:
// у свежеоплаченного v2-заказа payload ещё пуст, и генерацию запускать рано).
func hasSongConfig(o store.Order) bool {
	var payload map[string]any
	_ = json.Unmarshal([]byte(o.InputPayloadJSON), &payload)
	recipient, _ := payload["recipientName"].(string)
	return strings.TrimSpace(recipient) != ""
}

// extrasSelectedSum — сумма допников, выбранных до оплаты: входит в основной
// платёж. Считается по ценам из настроек, поэтому смена цены в админке между
// выбором и оплатой меняет и сумму — это осознанно: платёж создаётся по той же
// формуле, а его сверка в вебхуке ловит расхождение.
func (s *Server) extrasSelectedSum(ctx context.Context, o store.Order) int {
	sum := 0
	for _, id := range store.ExtrasSelected(o) {
		sum += s.extraPrice(ctx, o, id)
	}
	return sum
}

// extraRevenue — деньги за допники заказа по ТЕКУЩИМ ценам настроек. Гейты
// extraPrice (только v1, только при задержке) здесь не применяются: оплаченный
// допник — это деньги, даже если сейчас он не продаётся. Историческая цена не
// хранится, поэтому смена цены в админке перепишет и прошлую выручку — то же
// допущение, что в запросах дашборда; иначе admin и Grafana дали бы разные суммы.
func (s *Server) extraRevenue(ctx context.Context, o store.Order) int {
	speedup, stems := store.UpsellPrices(ctx, s.DB)
	price := func(id string) int {
		switch id {
		case extraSpeed:
			return speedup
		case extraStems:
			return stems
		}
		return 0
	}
	sum := 0
	for _, id := range store.ExtrasSelected(o) {
		sum += price(id)
	}
	for _, e := range store.ExtrasPaid(o) {
		sum += price(e.ID)
	}
	return sum
}

func (s *Server) registerFlow(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/flow/v1/draft", s.handleFlowV1Draft)
	mux.HandleFunc("POST /api/flow/v2/start", s.handleFlowV2Start)
	mux.HandleFunc("POST /api/orders/{id}/email", s.handleOrderSetEmail)
	mux.HandleFunc("POST /api/orders/{id}/config", s.handleOrderSetConfig)
	mux.HandleFunc("POST /api/orders/{id}/extras/select", s.handleExtraSelect)
	mux.HandleFunc("POST /api/orders/{id}/extras/initiate", s.handleExtraInitiate)
	s.registerFlowSubscribe(mux)
}

// --- посетитель и квота бесплатных генераций (v1) ---

const visitorCookieName = "mzv"

// visitorID возвращает подписанный id посетителя из куки, при необходимости
// выпуская новый. Подпись (HMAC на WebhookSecret) не даёт скрутить лимит
// простым перебором значений куки — самодельная кука отбрасывается.
func (s *Server) visitorID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(visitorCookieName); err == nil {
		if id, ok := resolveOrderRef(s.Config.WebhookSecret, c.Value); ok {
			return id
		}
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	http.SetCookie(w, &http.Cookie{
		Name:     visitorCookieName,
		Value:    publicOrderRef(s.Config.WebhookSecret, id),
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   s.subCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	return id
}

// clientIP живёт в flow.go (A/B-флоу варианта 1) — здесь используется тот же хелпер.

// --- v1: анонимный драфт с генерацией до оплаты ---

// handleFlowV1Draft создаёт заказ воронки v1 и сразу запускает генерацию —
// песня слушается бесплатно до оплаты.
//
// Квота бесплатных генераций — три разных предохранителя, и путать их нельзя:
//
//  1. кука посетителя — лимит на ЧЕЛОВЕКА (3/сутки);
//  2. IP — страховка от скрипта в цикле, потолок на порядок выше (30/сутки), и
//     он не применяется, если адрес приватный: значит прокси не пробросил
//     X-Forwarded-For, адрес у всех посетителей одинаковый, и такой ключ
//     схлопнул бы весь сайт в один счётчик;
//  3. суточный бюджет на весь сайт (preview.daily_budget) — единственное, что
//     реально ограничивает счёт за OpenRouter/Kie.
//
// Исчерпание квоты НЕ отказ: заказ создаётся, генерация не запускается, и
// воронка идёт дальше к оплате («сначала заплати, потом песня»). Прежний
// 429 без создания заказа означал, что человек не может даже купить.
func (s *Server) handleFlowV1Draft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if recipient, _ := body["recipientName"].(string); strings.TrimSpace(recipient) == "" {
		writeJSON(w, 400, map[string]string{"error": "recipient_required"})
		return
	}

	visitor := s.visitorID(w, r)
	freeGeneration, quotaReason := s.claimFreeGeneration(ctx, visitor, clientIP(r))

	o := buildOrderFromBody(s.Config, body)
	o.Language = requestLang(r)
	o.Flow = sql.NullString{String: "v1", Valid: true}
	o.FunnelStage = sql.NullString{String: store.StageStarted, Valid: true}
	// Плечо A/B-теста считается сервером от сессии, как в /api/orders/draft.
	// Совпало с этим флоу (v2_base ведёт на /create?v=1) — заказ помечается
	// плечом и берёт его цену; пришли напрямую мимо ротации (?v= руками,
	// старая ссылка) — плечо не пишется, чтобы не пачкать аналитику теста.
	if sid, _ := body["sessionId"].(string); strings.TrimSpace(sid) != "" {
		if arm, price := s.armForRequest(ctx, r, strings.TrimSpace(sid)); arm.Route == routeCreateLatePayment {
			o.FlowVariant = sql.NullString{String: arm.ID, Valid: true}
			o.BasePrice = price
		}
	}
	if s.paymentsConfigured() {
		o.PaymentStatus = store.PaymentPending
	}

	if err := store.CreateOrder(ctx, s.DB, o); err != nil {
		log.Printf("[flow] v1 draft: create order err=%v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}

	// Бесплатной генерации не досталось — заказ всё равно живой, просто песня
	// будет после оплаты. Фронт по generationSkipped выкидывает шаги
	// «пишем песню» и «слушайте» и ведёт к оплате.
	if !freeGeneration {
		log.Printf("[flow] v1 draft order=%s created WITHOUT free generation: %s (visitor=%s ip=%s)",
			o.ID, quotaReason, visitor, clientIP(r))
		writeJSON(w, 200, map[string]any{
			"orderId":           publicOrderRef(s.Config.WebhookSecret, o.ID),
			"paymentStatus":     o.PaymentStatus,
			"generationSkipped": true,
			"reason":            quotaReason,
		})
		return
	}

	// Генерация стартует сразу, не дожидаясь оплаты. ConfirmOrder переводит
	// заказ в processing — вебхук оплаты позже увидит transitioned=false и
	// генерацию не перезапустит.
	transitioned, err := store.ConfirmOrder(ctx, s.DB, o.ID)
	if err != nil {
		log.Printf("[flow] v1 draft order=%s: confirm err=%v", o.ID, err)
	}
	if transitioned {
		// preview_started_at — общий счётчик генераций до оплаты для обеих
		// воронок: по нему считается суточный бюджет (preview.daily_budget).
		if _, err := store.ClaimPreview(ctx, s.DB, o.ID, "v:"+visitor, clientIP(r), time.Now()); err != nil {
			log.Printf("[flow] v1 draft order=%s: claim preview err=%v", o.ID, err)
		}
		go s.triggerRelay(context.Background(), o.ID)
	}
	log.Printf("[flow] v1 draft order=%s created, generation started (visitor=%s)", o.ID, visitor)
	writeJSON(w, 200, map[string]any{
		"orderId":       publicOrderRef(s.Config.WebhookSecret, o.ID),
		"paymentStatus": o.PaymentStatus,
	})
}

// --- v2: заказ по одному email, вопросы доезжают после оплаты ---

func (s *Server) handleFlowV2Start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Email       string         `json:"email"`
		Phone       string         `json:"phone"`
		SessionID   string         `json:"sessionId"`
		GAClientID  string         `json:"gaClientId"`
		YMClientID  string         `json:"ymClientId"`
		Attribution map[string]any `json:"attribution"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeJSON(w, 400, map[string]string{"error": "email_required"})
		return
	}

	customer := map[string]any{"email": email}
	if p := strings.TrimSpace(body.Phone); p != "" {
		customer["whatsappNumber"] = p
	}
	customerBytes, _ := json.Marshal(customer)

	o := store.Order{
		ID:               newOrderID(),
		Status:           "created",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    "paid",
		InputPayloadJSON: "{}",
		CustomerJSON:     string(customerBytes),
		BasePrice:        priceFromConfig(s.Config),
		Flow:             sql.NullString{String: "v2", Valid: true},
		FunnelStage:      sql.NullString{String: store.StageEmailCaptured, Valid: true},
		EmailConsent:     true,
		Language:         requestLang(r),
		CreatedAt:        time.Now(),
	}
	if s.paymentsConfigured() {
		o.PaymentStatus = store.PaymentPending
	}
	// Печать плеча — как в handleFlowV1Draft: только когда серверное
	// назначение сессии действительно этот флоу (v2_prepay → /create?v=2).
	if sid := strings.TrimSpace(body.SessionID); sid != "" {
		if arm, price := s.armForRequest(ctx, r, sid); arm.Route == routeCreatePrepay {
			o.FlowVariant = sql.NullString{String: arm.ID, Valid: true}
			o.BasePrice = price
		}
	}
	if body.GAClientID != "" {
		o.GAClientID = sql.NullString{String: body.GAClientID, Valid: true}
	}
	if body.YMClientID != "" {
		o.YMClientID = sql.NullString{String: body.YMClientID, Valid: true}
	}
	if len(body.Attribution) > 0 {
		if raw, err := json.Marshal(body.Attribution); err == nil {
			o.AttributionJSON = sql.NullString{String: string(raw), Valid: true}
		}
	}

	if err := store.CreateOrder(ctx, s.DB, o); err != nil {
		log.Printf("[flow] v2 start: create order err=%v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}

	ref := publicOrderRef(s.Config.WebhookSecret, o.ID)
	s.marketingContact(email, nil)
	s.marketingEvent(o.ID, EventOrderCreated)
	resumeURL := s.flowResumeURL(o.Lang(), ref)
	subject, text := "Ваша персональная песня — начало положено",
		"Здравствуйте!\n\nВы начали создание персональной песни на "+s.selfHost()+".\n"+
			"Продолжить оформление можно в любой момент по ссылке:\n"+resumeURL+
			"\n\nПесню пришлём на этот адрес, как только она будет готова."
	if o.Lang() == store.LangEN {
		subject, text = "Your personal song — off to a great start",
			"Hello!\n\nYou have started creating a personal song on "+s.selfHost()+".\n"+
				"You can continue your order at any time here:\n"+resumeURL+
				"\n\nWe will send the song to this address as soon as it is ready."
	}
	go s.sendFlowEmail(emailCreateStarted, o.ID, email, subject, text,
		map[string]string{"resume_url": resumeURL})

	log.Printf("[flow] v2 start order=%s email=%s", o.ID, email)
	writeJSON(w, 200, map[string]any{"orderId": ref, "paymentStatus": o.PaymentStatus})
}

// handleOrderSetEmail — шаг «Не потеряйте» воронки v1: привязывает email
// (телефон опционален) к заказу и шлёт письмо с превью песни.
func (s *Server) handleOrderSetEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var body struct {
		Email string `json:"email"`
		Phone string `json:"phone"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeJSON(w, 400, map[string]string{"error": "email_required"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err := store.SetOrderEmail(ctx, s.DB, id, email, strings.TrimSpace(body.Phone), true); err != nil {
		log.Printf("[flow] set email order=%s err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	// Стадия двигается только вперёд: оплаченный заказ не откатывается в email_captured.
	if !o.FunnelStage.Valid || o.FunnelStage.String == store.StageStarted {
		_ = store.SetFunnelStage(ctx, s.DB, id, store.StageEmailCaptured)
	}

	// Письмо с превью уходит только когда превью действительно было: при
	// выключенной генерации до оплаты обещать «слушайте по ссылке» нечего,
	// а песню анонсирует письмо после оплаты.
	s.marketingContact(email, nil)
	s.marketingEvent(id, EventOrderCreated)
	if o.PreviewStartedAt.Valid {
		ref := publicOrderRef(s.Config.WebhookSecret, id)
		songURL := s.flowResumeURL(o.Lang(), ref)
		shareURL := s.siteURL(o.Lang()) + "/listen/" + ref
		subject, body := "Ваша песня — слушайте по ссылке",
			"Здравствуйте!\n\nВаша персональная песня создана и ждёт вас:\n"+songURL+
				"\n\nПоделиться отрывком:\n"+shareURL
		if o.Lang() == store.LangEN {
			subject, body = "Your song is ready to listen",
				"Hello!\n\nYour personal song has been created and is waiting for you:\n"+songURL+
					"\n\nShare a snippet:\n"+shareURL
		}
		go s.sendFlowEmail(emailCreatePreview, id, email, subject, body,
			map[string]string{"song_url": songURL, "share_url": shareURL})
	}

	log.Printf("[flow] set email order=%s", id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleOrderSetConfig — воронка v2: вопросы о песне доезжают после оплаты.
// Если заказ уже оплачен, генерация стартует прямо отсюда (вебхук оплаты для
// v2-заказов генерацию не запускает — payload на тот момент пуст).
func (s *Server) handleOrderSetConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if recipient, _ := body["recipientName"].(string); strings.TrimSpace(recipient) == "" {
		writeJSON(w, 400, map[string]string{"error": "recipient_required"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	// Конфиг принимается один раз: генерация уже могла стартовать, и молчаливая
	// замена payload после неё только запутает доработки.
	if o.Status != "created" {
		writeJSON(w, 409, map[string]string{"error": "already_processing"})
		return
	}
	payloadBytes, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if err := store.SetOrderPayload(ctx, s.DB, id, string(payloadBytes)); err != nil {
		log.Printf("[flow] set config order=%s err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	_ = store.SetFunnelStage(ctx, s.DB, id, store.StageConfigFilled)

	started := false
	if o.PaymentStatus == store.PaymentPaid {
		transitioned, err := store.ConfirmOrder(ctx, s.DB, id)
		if err != nil {
			log.Printf("[flow] set config order=%s: confirm err=%v", id, err)
		}
		if transitioned {
			started = true
			go s.triggerRelay(context.Background(), id)
		}
	}
	log.Printf("[flow] set config order=%s generationStarted=%t", id, started)
	writeJSON(w, 200, map[string]any{"ok": true, "generationStarted": started})
}

// --- допники ---

// handleExtraSelect — выбор допника ДО оплаты (v1, шаг «Хотите быстрее?»):
// цена допника добавится к основному платежу в initiate-payment.
func (s *Server) handleExtraSelect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if _, known := flowExtras[body.ID]; !known {
		writeJSON(w, 400, map[string]string{"error": "unknown_extra"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if o.PaymentStatus != store.PaymentPending {
		writeJSON(w, 409, map[string]string{"error": "not_pending"})
		return
	}
	// Выключенный допник не продаётся: цена 0 значит «выполнить нечем».
	if s.extraPrice(ctx, o, body.ID) <= 0 {
		log.Printf("[flow] select extra order=%s extra=%s refused: выключен", id, body.ID)
		writeJSON(w, 409, map[string]string{"error": "extra_disabled"})
		return
	}
	if err := store.SelectExtra(ctx, s.DB, id, body.ID); err != nil {
		log.Printf("[flow] select extra order=%s extra=%s err=%v", id, body.ID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	log.Printf("[flow] select extra order=%s extra=%s", id, body.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleExtraInitiate — оплата допника ПОСЛЕ основного платежа отдельным
// redirect-платежом ЮKassa (автосписаний по сохранённой карте нет, пока не
// включена рекуррентка). Пока прежнее подтверждение живо, отдаётся оно же.
func (s *Server) handleExtraInitiate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	extra, known := flowExtras[body.ID]
	if !known {
		writeJSON(w, 400, map[string]string{"error": "unknown_extra"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if o.PaymentStatus != store.PaymentPaid {
		writeJSON(w, 409, map[string]string{"error": "not_paid"})
		return
	}
	price := s.extraPrice(ctx, o, body.ID)
	if price <= 0 {
		log.Printf("[flow] extra initiate order=%s extra=%s refused: выключен", id, body.ID)
		writeJSON(w, 409, map[string]string{"error": "extra_disabled"})
		return
	}
	for _, e := range store.ExtrasPaid(o) {
		if e.ID == body.ID {
			writeJSON(w, 200, map[string]any{"paid": true})
			return
		}
	}

	// Dev/mock-режим: без ключей ЮKassa допник считается оплаченным сразу —
	// фронт и mock-API проходят флоу целиком.
	if s.Config.YooKassaShopID == "" || s.Config.YooKassaSecretKey == "" {
		if err := store.MarkExtraPaid(ctx, s.DB, id, body.ID, "mock-extra-"+id+"-"+body.ID, time.Now()); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		s.applyExtraEffect(ctx, id, body.ID)
		writeJSON(w, 200, map[string]any{"paid": true})
		return
	}

	// Живое подтверждение того же допника переиспользуется: redirect-ссылка
	// ЮKassa живёт долго, а второй платёж — это риск двойного списания.
	for _, p := range store.ExtrasPending(o) {
		if p.ID != body.ID || p.URL == "" {
			continue
		}
		if issued, err := time.Parse(time.RFC3339, p.IssuedAt); err == nil && time.Since(issued) < time.Hour {
			log.Printf("[flow] extra initiate order=%s extra=%s payment=%s reused", id, body.ID, p.PaymentID)
			writeJSON(w, 200, map[string]any{"confirmationUrl": p.URL})
			return
		}
	}

	ref := publicOrderRef(s.Config.WebhookSecret, id)
	returnURL := s.flowResumeURL(o.Lang(), ref) + "&extra=" + body.ID
	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	custEmail, _ := cust["email"].(string)
	custPhone, _ := cust["whatsappNumber"].(string)

	var kb [16]byte
	_, _ = rand.Read(kb[:])
	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
	result, err := yk.Create(payment.CreateRequest{
		OrderID:        id,
		ValueRub:       rubPaymentAmount(price),
		Mode:           payment.ModeRedirect,
		ReturnURL:      returnURL,
		IdempotenceKey: "extra-" + hex.EncodeToString(kb[:]),
		Receipt: payment.Receipt{
			Email:       strings.TrimSpace(custEmail),
			Phone:       strings.TrimSpace(custPhone),
			Description: extra.Title,
			VatCode:     s.Config.YooKassaVatCode,
		},
		// metadata.extra отделяет платёж допника от основного платежа заказа:
		// вебхук по нему помечает допник, а не заказ.
		Metadata: map[string]string{"order_id": id, "extra": body.ID},
	})
	if err != nil {
		log.Printf("[flow] extra initiate order=%s extra=%s: create payment err=%v", id, body.ID, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	if err := store.SaveExtraPending(ctx, s.DB, id, store.PendingExtra{
		ID:        body.ID,
		PaymentID: result.PaymentID,
		URL:       result.ConfirmationURL,
		IssuedAt:  time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		// Платёж уже создан — подтверждение обязано дойти до покупателя, даже
		// если запомнить его не удалось (вебхук такой платёж отклонит как
		// неизвестный, но это лучше молчаливого 500 после создания платежа).
		log.Printf("[flow] extra initiate order=%s extra=%s: save pending err=%v", id, body.ID, err)
	}
	log.Printf("[flow] extra initiate order=%s extra=%s payment=%s created", id, body.ID, result.PaymentID)
	writeJSON(w, 200, map[string]any{"confirmationUrl": result.ConfirmationURL})
}

// handleExtraWebhook обрабатывает вебхук платежа допника (metadata.extra).
// Вызывается из handleYooWebhook ДО стандартного конвейера заказа: платёж
// допника не должен пометить заказ оплаченным или попасть под автовозврат
// «лишних» платежей.
func (s *Server) handleExtraWebhook(w http.ResponseWriter, r *http.Request, event, paymentID, orderID, extraID string) {
	ctx := r.Context()
	if event != "payment.succeeded" {
		log.Printf("[flow] extra webhook order=%s extra=%s event=%s: ignored", orderID, extraID, event)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if s.Config.YooKassaShopID == "" || s.Config.YooKassaSecretKey == "" {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "not_verifiable"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, orderID)
	if err != nil {
		log.Printf("[flow] extra webhook order=%s: get order err=%v", orderID, err)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	_, known := flowExtras[extraID]
	if !known || !store.KnownExtraPaymentID(o, paymentID) {
		log.Printf("[flow] extra webhook order=%s extra=%s payment=%s REJECTED: unknown extra/payment", orderID, extraID, paymentID)
		writeJSON(w, 200, map[string]any{"ok": false, "error": "verification_failed"})
		return
	}
	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
	p, err := yk.GetPayment(paymentID)
	if err != nil {
		log.Printf("[flow] extra webhook order=%s payment=%s: verify err=%v", orderID, paymentID, err)
		w.WriteHeader(500) // ЮKassa повторит вебхук
		return
	}
	got, parsed := rublesToKopecks(p.Amount)
	if p.Status != "succeeded" || !p.Paid || p.Currency != "RUB" ||
		p.Metadata["order_id"] != orderID || p.Metadata["extra"] != extraID ||
		!parsed || got < int64(s.extraPrice(ctx, o, extraID))*100 {
		log.Printf("[flow] extra webhook order=%s extra=%s payment=%s REJECTED: api mismatch (status=%s paid=%v amount=%s)",
			orderID, extraID, paymentID, p.Status, p.Paid, p.Amount)
		writeJSON(w, 200, map[string]any{"ok": false, "error": "verification_failed"})
		return
	}
	if err := store.MarkExtraPaid(ctx, s.DB, orderID, extraID, paymentID, time.Now()); err != nil {
		log.Printf("[flow] extra webhook order=%s extra=%s: mark paid err=%v", orderID, extraID, err)
		w.WriteHeader(500)
		return
	}
	s.applyExtraEffect(ctx, orderID, extraID)
	log.Printf("[flow] extra webhook order=%s extra=%s payment=%s: PAID", orderID, extraID, paymentID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// applyExtraEffect выполняет то, что покупатель купил допником. Пока это
// только ускорение: оно снимает задержку доставки, и без этого вызова допник
// был бы просто записью в БД — деньгами за обещание, которое никто не
// выполняет.
func (s *Server) applyExtraEffect(ctx context.Context, orderID, extraID string) {
	if extraID != extraSpeed {
		return
	}
	if err := store.ClearDeliveryHold(ctx, s.DB, orderID); err != nil {
		log.Printf("[flow] extra order=%s extra=%s: снять задержку не удалось err=%v", orderID, extraID, err)
		return
	}
	log.Printf("[flow] extra order=%s: ускорение оплачено, задержка доставки снята", orderID)
	// Снятая задержка обнуляет deliver_after, и планировщик досылки такой заказ
	// больше не увидит — письмо о готовой песне отправляется прямо отсюда.
	go s.deliverIfReady(context.Background(), orderID, "speedup extra", "")
}

// --- письма и джоба-догонка ---

// flowResumeURL — ссылка возврата в воронку /create с восстановлением заказа.
func (s *Server) flowResumeURL(lang, ref string) string {
	return s.siteURL(lang) + "/create?order=" + ref
}

// sendFlowEmail шлёт письмо-триггер воронки; отсутствие SMTP не ошибка (dev).
func (s *Server) sendFlowEmail(kind, orderID, email, subject, body string, vars map[string]string) {
	if s.Config.SMTPHost == "" {
		log.Printf("[flow] mail order=%s: SMTP_HOST not set, skip %q", orderID, subject)
		return
	}
	if err := s.sendEmail(context.Background(), kind, emailTarget{OrderID: orderID}, email, subject, body, vars); err != nil {
		log.Printf("[flow] mail order=%s %q err=%v", orderID, subject, err)
		return
	}
	log.Printf("[flow] mail order=%s %q sent to=%s", orderID, subject, email)
}
