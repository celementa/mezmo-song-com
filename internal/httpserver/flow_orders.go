// Ручки и фоновые шаги варианта 1: сбор брифа после создания заказа, генерация
// до оплаты, 15-секундный отрывок, апсейлы и письма флоу.
//
// Ручки появляются по новым шагам флоу, а не по плечам теста: v1_base и
// v1_price ходят по одним и тем же адресам и отличаются только ценой, которую
// сервер берёт из плеча (SetOrderFlow) — из тела запроса цена не принимается
// никогда, иначе покупатель выбирал бы её сам.
package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"mezmo-song-com/internal/audioclip"
	"mezmo-song-com/internal/store"
)

// previewClipDuration — длина отрывка из документа (шаг 7).
const previewClipDuration = 15 * time.Second

// previewClipStart — с какой доли трека берётся отрывок. Начало песни это
// обычно интро, поэтому берём из первой трети: это ближе к «самому
// трогательному моменту», чем первые 15 секунд.
const previewClipStart = 0.3

// previewObjectKey — ключ отрывка в MinIO. Как и у полных треков, в ключе номер
// прогона: доработка не должна отдавать отрывок прошлой версии из кэша.
func previewObjectKey(orderID string, regeneration int) string {
	return "orders/" + orderID + "/r" + strconv.Itoa(regeneration) + "/preview.mp3"
}

// publicPreviewURL — ссылка на отрывок для фронта.
func publicPreviewURL(ref string, regeneration int) string {
	return "/api/public/order/" + ref + "/preview?v=" + strconv.Itoa(regeneration)
}

// handleOrderBrief дописывает ответы шагов 4–5 в заказ, созданный на шаге 3.
//
// Заказ создаётся сразу после email, а не в конце формы: в варианте 1 email
// собирается до генерации именно для того, чтобы у брошенной корзины остался
// адрес. Значит бриф доезжает позже, отдельными запросами.
func (s *Server) handleOrderBrief(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var patch map[string]any
	if err := readJSON(r, &patch); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	// Поля, которыми распоряжается сервер, из клиентского патча выкидываются:
	// иначе бриф стал бы способом переписать цену и плечо.
	for _, forbidden := range []string{"flowVariant", "basePrice", "price", "discountCode"} {
		delete(patch, forbidden)
	}
	updated, err := store.MergeInputPayload(ctx, s.DB, id, patch)
	if err != nil {
		log.Printf("[flow] brief order=%s err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !updated {
		// Заказа нет либо генерация уже началась — бриф менять поздно.
		writeJSON(w, 409, map[string]string{"error": "brief_locked"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// resumeURL — ссылка «продолжить оформление» неоплаченного заказа варианта 1:
// анкета /config восстанавливается по ref с сервера, а не из localStorage,
// поэтому работает из письма и с другого устройства. Пусто — продолжать нечего:
// заказ оплачен, генерация уже пошла, или это плечо/легаси-форма без анкеты.
func (s *Server) resumeURL(o store.Order) string {
	if o.PaymentStatus != store.PaymentPending || o.Status != "created" || !o.FlowVariant.Valid {
		return ""
	}
	arm, ok := flowArms[o.FlowVariant.String]
	if !ok || arm.Route != "" {
		return ""
	}
	ref := publicOrderRef(s.Config.WebhookSecret, o.ID)
	return "/config?arm=" + armLinkName(arm.ID) + "&order=" + url.QueryEscape(ref)
}

// handlePublicOrderResume отдаёт анкете /config ответы неоплаченного заказа:
// ссылка из письма «вы не завершили заказ» ведёт к тому же заказу, а не к новой
// анкете. Ключи — те, что фронт присылает в /api/orders/draft и /brief.
func (s *Server) handlePublicOrderResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if s.resumeURL(o) == "" {
		// Оплачен или генерация пошла — анкету менять поздно, фронт ведёт на /order.
		writeJSON(w, 409, map[string]string{"error": "not_resumable", "paymentStatus": o.PaymentStatus})
		return
	}
	var payload, cust map[string]any
	_ = json.Unmarshal([]byte(o.InputPayloadJSON), &payload)
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	str := func(m map[string]any, k string) string {
		v, _ := m[k].(string)
		return v
	}
	genre := ""
	if mp, ok := payload["musicPreferences"].(map[string]any); ok {
		genre = str(mp, "genre")
	}
	writeJSON(w, 200, map[string]any{
		"flowVariant": o.FlowVariant.String,
		"answers": map[string]string{
			"relationship":    str(payload, "relationship"),
			"recipientName":   str(payload, "recipientName"),
			"recipientGender": str(payload, "recipientGender"),
			"occasion":        str(payload, "occasion"),
			"email":           str(cust, "email"),
			"phone":           str(payload, "whatsappNumber"),
			"emotion":         str(payload, "emotion"),
			"genre":           genre,
			"story":           str(payload, "story"),
		},
	})
}

// handleGeneratePreview запускает генерацию песни до оплаты (шаг 6).
//
// Это единственное место в варианте 1, где мы тратим прогон OpenRouter + Kie.ai
// на того, кто ещё не заплатил, поэтому здесь расходуется общая квота
// бесплатных генераций (claimFreeGeneration — тот же механизм, что у воронок
// /create: кука посетителя, IP и суточный бюджет). Проверка повторяется, хотя
// GET /api/public/flow её уже делал read-only: между двумя вызовами кран мог
// закрыться, а расходовать квоту должна именно генерация.
func (s *Server) handleGeneratePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
	}
	_ = readJSON(r, &body)

	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if o.PreviewStartedAt.Valid {
		// Повторный запрос (перезагрузка страницы, двойной клик) — не ошибка:
		// генерация уже идёт, фронту достаточно продолжить опрос статуса.
		writeJSON(w, 200, map[string]any{"ok": true, "alreadyStarted": true})
		return
	}
	visitor := s.visitorID(w, r)
	if allowed, reason := s.claimFreeGeneration(ctx, visitor, clientIP(r)); !allowed {
		// Отказ мягкий и на этой стороне: фронт по ошибке ведёт покупателя к
		// оплате, а не показывает тупик (FlowRoute → goToCheckout).
		log.Printf("[flow] preview order=%s refused: %s (visitor=%s)", id, reason, visitor)
		writeJSON(w, 429, map[string]string{"error": reason})
		return
	}
	claimed, err := store.ClaimPreview(ctx, s.DB, id, "v:"+visitor, clientIP(r), time.Now())
	if err != nil {
		log.Printf("[flow] preview order=%s claim err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !claimed {
		writeJSON(w, 200, map[string]any{"ok": true, "alreadyStarted": true})
		return
	}
	transitioned, err := store.ConfirmOrder(ctx, s.DB, id)
	if err != nil {
		log.Printf("[flow] preview order=%s confirm err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if transitioned {
		log.Printf("[flow] preview order=%s: generation before payment triggered", id)
		go s.triggerRelay(context.Background(), id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handlePreviewAudio отдаёт 15-секундный отрывок. Оплата здесь не требуется —
// в этом весь смысл шага 7; полный трек по-прежнему живёт за handleAudioProxy,
// который без payment_status = paid не отвечает.
func (s *Server) handlePreviewAudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil || !o.PreviewAudioURL.Valid || o.PreviewAudioURL.String == "" || s.Storage == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	obj, info, err := s.Storage.GetObject(ctx, o.PreviewAudioURL.String)
	if err != nil {
		log.Printf("[flow] preview order=%s get object err=%v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = io.Copy(w, obj)
}

// buildPreviewClip нарезает отрывок из готового трека и складывает его
// отдельным объектом. Вызывается из колбэка Kie.ai после выгрузки треков.
//
// Отдельный объект, а не «проиграть 15 секунд полного файла на фронте»: во
// втором случае ссылка на всю песню видна в devtools, то есть замок на шаге 7
// не запирает ничего.
func (s *Server) buildPreviewClip(ctx context.Context, orderID, trackKey string, regeneration int) {
	if s.Storage == nil || !strings.HasPrefix(trackKey, "orders/") {
		return
	}
	full, err := s.Storage.GetBytes(ctx, trackKey)
	if err != nil {
		log.Printf("[flow] preview order=%s read track err=%v", orderID, err)
		return
	}
	clip, err := audioclip.Cut(full, previewClipStart, previewClipDuration)
	if err != nil {
		log.Printf("[flow] preview order=%s cut err=%v", orderID, err)
		return
	}
	key := previewObjectKey(orderID, regeneration)
	if err := s.Storage.Put(ctx, key, clip, "audio/mpeg"); err != nil {
		log.Printf("[flow] preview order=%s put err=%v", orderID, err)
		return
	}
	if err := store.SetPreviewAudio(ctx, s.DB, orderID, key); err != nil {
		log.Printf("[flow] preview order=%s save key err=%v", orderID, err)
		return
	}
	log.Printf("[flow] preview order=%s clip ready key=%s bytes=%d", orderID, key, len(clip))
}

// handleBuySpeedup — апсейл ускорения (шаг 8). Цена добавляется к base_price до
// оплаты, поэтому платёж остаётся один: второй платёж по заказу вернул бы
// автовозврат дублей (T-020).
func (s *Server) handleBuySpeedup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	price, _ := store.UpsellPrices(ctx, s.DB)
	// Без задержки обычной доставки ускорение не даёт покупателю ничего, и
	// продавать его нельзя — ручка отказывает, даже если цена задана.
	if price <= 0 || store.DeliveryDelay(ctx, s.DB) <= 0 {
		writeJSON(w, 409, map[string]string{"error": "speedup_disabled"})
		return
	}
	bought, err := store.BuySpeedup(ctx, s.DB, id, price)
	if err != nil {
		log.Printf("[flow] speedup order=%s err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !bought {
		// Уже куплено или заказ уже оплачен: цену оплаченного заказа менять
		// нельзя — покупатель заплатил другую сумму.
		writeJSON(w, 409, map[string]string{"error": "speedup_unavailable"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	log.Printf("[flow] speedup order=%s bought price=%d base=%d", id, price, o.BasePrice)
	writeJSON(w, 200, map[string]any{"ok": true, "basePrice": o.BasePrice, "paymentAmount": s.expectedPaymentRubles(ctx, o)})
}

// handleUpsellStems — апсейл инструментала и вокала отдельно (шаг 12).
//
// Создаётся ОТДЕЛЬНЫЙ заказ, а не второй платёж по родителю: refundExtraPayments
// считает два живых успешных платежа одного заказа дублем и возвращает лишний
// автоматически, то есть деньги за апсейл вернулись бы сами и молча (T-020).
func (s *Server) handleUpsellStems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	parentID, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	_, price := store.UpsellPrices(ctx, s.DB)
	if price <= 0 {
		writeJSON(w, 409, map[string]string{"error": "stems_disabled"})
		return
	}
	parent, err := store.GetOrder(ctx, s.DB, parentID)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if parent.PaymentStatus != store.PaymentPaid {
		writeJSON(w, 402, map[string]string{"error": "not_paid"})
		return
	}
	exists, err := store.HasUpsell(ctx, s.DB, parentID, store.UpsellStems)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if exists {
		writeJSON(w, 409, map[string]string{"error": "already_ordered"})
		return
	}

	// Тело дочернего заказа — бриф родителя плюс пометка апсейла: в админке
	// должно быть видно, к какой песне относятся стемы, без join'ов руками.
	var payload map[string]any
	if err := json.Unmarshal([]byte(parent.InputPayloadJSON), &payload); err != nil || payload == nil {
		payload = map[string]any{}
	}
	payload["upsell"] = store.UpsellStems
	payload["parentOrderId"] = parentID

	child := buildOrderFromBody(s.Config, payload)
	child.BasePrice = price
	if s.paymentsConfigured() {
		child.PaymentStatus = store.PaymentPending
	}
	if parent.FlowVariant.Valid {
		child.FlowVariant = parent.FlowVariant
	}
	if err := store.CreateOrder(ctx, s.DB, child); err != nil {
		log.Printf("[flow] stems parent=%s create err=%v", parentID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if err := store.SetUpsellParent(ctx, s.DB, child.ID, parentID, store.UpsellStems); err != nil {
		log.Printf("[flow] stems order=%s set parent err=%v", child.ID, err)
	}
	if parent.FlowVariant.Valid {
		if err := store.SetOrderFlow(ctx, s.DB, child.ID, parent.FlowVariant.String, price); err != nil {
			log.Printf("[flow] stems order=%s set flow err=%v", child.ID, err)
		}
	}
	log.Printf("[flow] stems order=%s parent=%s price=%d", child.ID, parentID, price)
	writeJSON(w, 200, map[string]any{
		"ok":            true,
		"orderId":       publicOrderRef(s.Config.WebhookSecret, child.ID),
		"paymentAmount": price,
	})
}

// applyDeliveryHold придерживает доставку после оплаты, если задержка включена
// и ускорение не куплено. Ускорение (шаг 8) продаёт снятие именно этой
// задержки: при delivery.normal_delay_minutes = 0 ускорять нечего, поэтому
// апсейл в этом случае и не показывается.
//
// Тестовые заказы (/admin/ads), песни подписчиков и заказы-апсейлы не
// задерживаются: там ждать нечего и некому.
func (s *Server) applyDeliveryHold(ctx context.Context, orderID string) {
	delay := store.DeliveryDelay(ctx, s.DB)
	if delay <= 0 {
		return
	}
	o, err := store.GetOrder(ctx, s.DB, orderID)
	if err != nil {
		return
	}
	if o.SpeedupPurchased || store.IsInternalOrder(o) || o.SubscriptionID.Valid || o.UpsellKind.Valid {
		return
	}
	// Воронка v2 задержку НЕ получает: там песню слушают внутри воронки сразу
	// после оплаты, и задержка заблокировала бы её собственный шаг
	// прослушивания. Поэтому в v2 и ускорение не продаётся (Server.extraPrice).
	if o.Flow.String == "v2" {
		return
	}
	// Воронка v1 задержку получает: до оплаты человек слышал только
	// 15-секундный отрывок, полную песню он ждёт как обычный покупатель — и
	// именно снятие этого ожидания продаёт допник «ускорение». Купленный
	// допник задержку снимает.
	if slices.Contains(store.ExtrasSelected(o), extraSpeed) || extraPaid(o, extraSpeed) {
		return
	}
	until := time.Now().UTC().Add(delay)
	if err := store.SetDeliverAfter(ctx, s.DB, orderID, until); err != nil {
		log.Printf("[flow] hold order=%s err=%v", orderID, err)
		return
	}
	log.Printf("[flow] hold order=%s until=%s", orderID, until.Format(time.RFC3339))
}

// sendWelcomeEmail — письмо-триггер после шага 3. Ставится заявкой
// (ClaimWelcomeEmail), поэтому повторный вызов письмо не задвоит.
func (s *Server) sendWelcomeEmail(ctx context.Context, orderID string) {
	if s.Config.SMTPHost == "" {
		return
	}
	email := extractCustomerEmail(orderID, s.DB)
	if email == "" {
		return
	}
	claimed, err := store.ClaimWelcomeEmail(ctx, s.DB, orderID, time.Now())
	if err != nil || !claimed {
		return
	}
	link := s.publicOrderURL(orderID)
	body := "Мы начали работу над вашей песней.\n\n" +
		"Как только черновик будет готов, вы услышите отрывок на этой странице:\n" +
		link + "\n"
	if err := s.sendEmail(ctx, emailOrderStarted, emailTarget{OrderID: orderID}, email, "Мы взялись за вашу песню", body,
		map[string]string{"order_url": link}); err != nil {
		log.Printf("[flow] welcome order=%s send err=%v", orderID, err)
	}
}

// flowJobsInterval — тик планировщика флоу. Пять минут, а не час как у
// подписок: задержка доставки продаётся с точностью до десятков минут, и
// опоздание письма на час съедало бы обещание «10–30 минут» целиком.
const flowJobsInterval = 5 * time.Minute

// StartFlowJobs запускает фоновый шаг флоу: досылку письма после снятия
// задержки. Шаг идемпотентен (заявка в БД), поэтому рестарты и совпавшие
// тики безопасны.
func (s *Server) StartFlowJobs(ctx context.Context) {
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			s.runFlowJobsOnce(ctx)
			timer.Reset(flowJobsInterval)
		}
	}()
}

func (s *Server) runFlowJobsOnce(ctx context.Context) {
	now := time.Now().UTC()

	// Задержка вышла — песня показывается и уходит письмо. Колбэк Kie.ai
	// пришёл раньше и письмо тогда сознательно не отправлял, второго колбэка не
	// будет, поэтому досылает планировщик.
	due, err := store.ListOrdersDueForDelivery(ctx, s.DB, now)
	if err != nil {
		log.Printf("[flow] jobs: due delivery err=%v", err)
	}
	for _, o := range due {
		s.deliverIfReady(ctx, o.ID, "delayed delivery", "")
	}

}

// readyForDelivery — можно ли прямо сейчас отправить письмо «Ваша песня готова».
// Единственное место, где сходятся все условия: заказ оплачен, песня готова,
// задержка доставки вышла, письмо ещё не уходило. Причина отказа — для лога.
//
// Оплата проверяется первой: в воронках с генерацией ДО оплаты песня
// готова у неоплаченного заказа, и письмо «слушайте и скачивайте» такому
// покупателю вело бы на страницу без песни и без кнопки оплаты.
func readyForDelivery(o store.Order, now time.Time) (bool, string) {
	switch {
	case o.PaymentStatus != store.PaymentPaid:
		return false, "not paid"
	case o.Status != "completed":
		return false, "song not ready"
	case o.DeliveryStatus == store.DeliveryDone:
		return false, "already delivered"
	case store.DeliveryHeld(o, now):
		return false, "delivery held until " + o.DeliverAfter.String
	}
	return true, ""
}

// deliverIfReady — единая точка отправки письма «Ваша песня готова». Зовётся из
// всех событий, после которых песню можно отдавать: колбэк Kie.ai, вебхук
// оплаты, тик планировщика по вышедшей задержке. Возвращает true, если письмо
// ушло. MarkDelivered ставится только после успешной отправки — это правило
// машины состояний доставки (см. CLAUDE.md): неотправленное письмо оставляет
// заказ в очереди на следующее событие.
//
// intro — первая строка письма; вебхук оплаты через неё благодарит за оплату,
// чтобы не слать два письма подряд.
func (s *Server) deliverIfReady(ctx context.Context, orderID, source, intro string) bool {
	o, err := store.GetOrder(ctx, s.DB, orderID)
	if err != nil {
		log.Printf("[deliver] %s order=%s get order err=%v", source, orderID, err)
		return false
	}
	if ok, reason := readyForDelivery(o, time.Now()); !ok {
		log.Printf("[deliver] %s order=%s email postponed: %s", source, orderID, reason)
		return false
	}
	email := extractCustomerEmail(orderID, s.DB)
	if email == "" {
		log.Printf("[deliver] %s order=%s skip email: no customer email", source, orderID)
		return false
	}
	if s.Config.SMTPHost == "" {
		log.Printf("[deliver] %s order=%s skip email: SMTP_HOST not set", source, orderID)
		return false
	}
	// Доработка проходит тот же цикл доставки, но письмо на заказ уходит одно:
	// человек заказал доработку со страницы и ждёт её там же, а повторные
	// «Ваша песня готова» с одной и той же ссылкой mail.ru принимал за спам.
	if o.DeliveredAt.Valid {
		log.Printf("[deliver] %s order=%s email skipped: already delivered once", source, orderID)
	} else {
		link := s.publicOrderURL(orderID)
		body := "Здравствуйте!\n\nВаша персональная песня готова. Слушайте и скачивайте по ссылке:\n" + link
		if intro != "" {
			body = intro + "\n\n" + body
		}
		if err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: orderID}, email, "Ваша песня готова", body,
			map[string]string{"song_url": link, "intro": intro}); err != nil {
			log.Printf("[deliver] %s order=%s send err=%v", source, orderID, err)
			return false
		}
	}
	if err := store.MarkDelivered(ctx, s.DB, orderID); err != nil {
		log.Printf("[deliver] %s order=%s mark err=%v", source, orderID, err)
		return true
	}
	log.Printf("[deliver] %s order=%s email sent to=%s", source, orderID, email)
	s.marketingEvent(orderID, EventSongReady)
	return true
}

// extraPaid — допник уже оплачен отдельным платежом.
func extraPaid(o store.Order, id string) bool {
	for _, e := range store.ExtrasPaid(o) {
		if e.ID == id {
			return true
		}
	}
	return false
}

// refOrEmpty — публичная ссылка на заказ по сырому id, если он задан. Нужна
// админке: ссылка на /order/{ref} по сырому parent_order_id даёт 404.
func refOrEmpty(secret string, id sql.NullString) string {
	if !id.Valid || id.String == "" {
		return ""
	}
	return publicOrderRef(secret, id.String)
}
