// Подписка из воронки заказа (документ v9–v12):
//
//   - «Безлимит за 0 ₽ сегодня» — апсейл после оплаты песни в плечах с
//     одинаковой ценой. Карта уже сохранена платежом за песню
//     (save_payment_method), поэтому подписка включается в один клик: сегодня
//     ничего не списывается, песня и есть первый месяц, первое списание — через
//     календарный месяц по сохранённой карте.
//   - «Оформить безлимит вместо разовой покупки» — экран сравнения ДО оплаты в
//     ценовом плече (разовая 990 ₽ против подписки 750 ₽/мес, v20). Подписка
//     оплачивается как обычно (первый платёж с сохранением карты), а заказ
//     после её активации становится песней по подписке.
package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mezmo-song-com/internal/marketing"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

func (s *Server) registerFlowSubscribe(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/orders/{id}/subscribe", s.requireSubscriptions(s.handleOrderSubscribe))
	mux.HandleFunc("POST /api/orders/{id}/subscribe-instead", s.requireSubscriptions(s.handleOrderSubscribeInstead))
}

// orderCustomer — email и телефон покупателя из заказа.
func orderCustomer(o store.Order) (email, phone string) {
	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	email, _ = cust["email"].(string)
	phone, _ = cust["whatsappNumber"].(string)
	return strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(phone)
}

// handleOrderSubscribe — «безлимит за 0 ₽ сегодня» после оплаты песни.
//
// Условия: заказ оплачен, у него есть email и сохранённая карта. Без карты
// (платёж прошёл без save_payment_method — например, кошельком, который
// сохранение не поддерживает) ручка отвечает 409 no_saved_method, и фронт
// ведёт на обычное оформление /subscribe. Без ключей ЮKassa (стенд mock/go)
// подписка активируется по фиктивному способу — так проходится весь флоу.
func (s *Server) handleOrderSubscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
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
	email, _ := orderCustomer(o)
	if email == "" || !strings.Contains(email, "@") {
		writeJSON(w, 409, map[string]string{"error": "no_email"})
		return
	}
	methodID := o.YooMethodID.String
	if methodID == "" {
		if s.yooConfigured() {
			log.Printf("[sub] from-order order=%s refused: карта не сохранена", id)
			writeJSON(w, 409, map[string]string{"error": "no_saved_method"})
			return
		}
		methodID = store.MockPaymentMethodID + "-" + id
	}

	name, _ := jsonField(o.InputPayloadJSON, "yourName")
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, name)
	if errors.Is(err, store.ErrSubscriptionActive) {
		writeJSON(w, 409, map[string]string{"error": "already_active"})
		return
	}
	if err != nil {
		log.Printf("[sub] from-order order=%s: upsert err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if o.AttributionJSON.Valid {
		_ = store.SetSubscriptionAttribution(ctx, s.DB, sub.ID, o.AttributionJSON.String)
	}
	// Идентификаторы браузера заказа — для серверных конверсий по платежу подписки.
	_ = store.SetSubscriptionClientIDs(ctx, s.DB, sub.ID, o.GAClientID.String, o.YMClientID.String)
	// Цена продлений — из плеча заказа (variant1-750 → 750 ₽/мес): экран апсейла
	// обещает именно её, без этого подписка уходила по общей цене.
	if o.FlowVariant.Valid {
		if arm, ok := flowArms[o.FlowVariant.String]; ok && arm.SubscriptionPrice > 0 {
			if err := store.SetSubscriptionPrice(ctx, s.DB, sub.ID, arm.SubscriptionPrice); err != nil {
				log.Printf("[sub] from-order order=%s sub=%s: set price err=%v", id, sub.ID, err)
			}
		}
	}
	activated, err := store.ActivateSubscriptionWithMethod(ctx, s.DB, sub.ID, methodID, time.Now())
	if err != nil {
		log.Printf("[sub] from-order order=%s sub=%s: activate err=%v", id, sub.ID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !activated {
		writeJSON(w, 409, map[string]string{"error": "already_active"})
		return
	}
	if err := store.LinkOrderSubscription(ctx, s.DB, id, sub.ID); err != nil {
		log.Printf("[sub] from-order order=%s sub=%s: link err=%v", id, sub.ID, err)
	}
	if fresh, err := store.GetSubscription(ctx, s.DB, sub.ID); err == nil {
		go s.sendSubscriptionTrialEmail(fresh)
	}
	s.marketingContact(email, map[string]string{marketing.FieldSubscriber: "1"})
	log.Printf("[sub] from-order order=%s sub=%s email=%s activated: 0 ₽ сегодня, списание с %s",
		id, sub.ID, email, store.AddCalendarMonth(time.Now().UTC()).Format("2006-01-02"))
	writeJSON(w, 200, map[string]any{"sid": sub.ID, "activated": true})
}

// handleOrderSubscribeInstead — оформить подписку вместо разовой покупки
// (экран сравнения ценового плеча). Подписка оплачивается своим первым
// платежом; заказ привяжется к ней в вебхуке (fulfillOrderViaSubscription).
func (s *Server) handleOrderSubscribeInstead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	o, err := store.GetOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if o.PaymentStatus == store.PaymentPaid {
		writeJSON(w, 409, map[string]string{"error": "already_paid"})
		return
	}
	email, _ := orderCustomer(o)
	if email == "" || !strings.Contains(email, "@") {
		writeJSON(w, 409, map[string]string{"error": "no_email"})
		return
	}
	name, _ := jsonField(o.InputPayloadJSON, "yourName")
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, name)
	if errors.Is(err, store.ErrSubscriptionActive) {
		// Подписка уже есть — песня просто входит в неё, платить не нужно.
		existing, gerr := store.GetSubscriptionByEmail(ctx, s.DB, email)
		if gerr != nil {
			writeJSON(w, 409, map[string]string{"error": "already_active"})
			return
		}
		s.fulfillOrderViaSubscription(ctx, id, existing.ID)
		writeJSON(w, 200, map[string]any{"sid": existing.ID, "activated": true, "orderId": publicOrderRef(s.Config.WebhookSecret, id)})
		return
	}
	if err != nil {
		log.Printf("[sub] instead order=%s: upsert err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if o.AttributionJSON.Valid {
		_ = store.SetSubscriptionAttribution(ctx, s.DB, sub.ID, o.AttributionJSON.String)
	}
	// Цена подписки — из плеча заказа: ценовые плечи v20 продают подписку
	// дешевле общей, и продления обязаны идти по той же цене.
	if o.FlowVariant.Valid {
		if arm, ok := flowArms[o.FlowVariant.String]; ok && arm.SubscriptionPrice > 0 {
			if err := store.SetSubscriptionPrice(ctx, s.DB, sub.ID, arm.SubscriptionPrice); err != nil {
				log.Printf("[sub] instead order=%s sub=%s: set price err=%v", id, sub.ID, err)
			} else {
				sub.PriceRub = arm.SubscriptionPrice
			}
		}
	}

	ref := publicOrderRef(s.Config.WebhookSecret, id)
	// Dev-режим и тестовые адреса — как в handleSubscriptionStart: без оплаты.
	if !s.yooConfigured() || store.IsSubscriptionTestEmail(ctx, s.DB, email) {
		if _, err := store.RecordSubscriptionPayment(ctx, s.DB, sub.ID, "mock-"+sub.ID+"-"+time.Now().UTC().Format("20060102150405"), rubPaymentAmount(subPrice(sub)), store.SubFlowFirst, store.MockPaymentMethodID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		s.fulfillOrderViaSubscription(ctx, id, sub.ID)
		if fresh, err := store.GetSubscription(ctx, s.DB, sub.ID); err == nil {
			go s.sendSubscriptionWelcomeEmail(fresh)
		}
		s.marketingContact(email, map[string]string{marketing.FieldSubscriber: "1"})
		log.Printf("[sub] instead order=%s sub=%s: activated WITHOUT payment (dev mode or test email)", id, sub.ID)
		writeJSON(w, 200, map[string]any{"sid": sub.ID, "activated": true, "orderId": ref})
		return
	}

	// Возврат — на страницу заказа: там песня, а подписка уже активна вебхуком.
	returnURL := s.publicOrderURL(id)
	mode := paymentModeFromRequest(r)
	// Повторный заход на шаг оплаты (вернулись из приложения банка, перезагрузили
	// страницу, «назад» из /order) не должен заводить второй платёж: первый жив
	// час и оплачиваем, а автовозврата дублей у подписок нет. Тот же гейт, что у
	// разовых платежей (livePaymentGate), только сумма сверяется с ценой подписки —
	// в orders.yoo_confirmation могло остаться подтверждение разовой покупки.
	if reuse, ok := s.liveSubscriptionConfirmation(o, mode, subPrice(sub)); ok {
		log.Printf("[sub] instead order=%s sub=%s payment=%s reused (%s)", id, sub.ID, reuse.PaymentID, mode)
		writeJSON(w, 200, paymentStartResponse(mode, payment.CreateResult{PaymentID: reuse.PaymentID, ConfirmationToken: reuse.Token, ConfirmationURL: reuse.URL}, returnURL, map[string]any{"sid": sub.ID}))
		return
	}
	result, err := s.createSubscriptionPayment(sub, email, mode, returnURL, map[string]string{"order_id": id})
	if err != nil {
		log.Printf("[sub] instead order=%s sub=%s: create payment err=%v", id, sub.ID, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	if err := store.SaveConfirmation(ctx, s.DB, id, store.IssuedConfirmation{
		PaymentID: result.PaymentID,
		Mode:      mode,
		Token:     result.ConfirmationToken,
		URL:       result.ConfirmationURL,
		IssuedAt:  time.Now().UTC(),
	}); err != nil {
		log.Printf("[sub] instead order=%s save confirmation err=%v", id, err)
	}
	s.rememberSubscriptionPayment(ctx, sub.ID, result.PaymentID)
	log.Printf("[sub] instead order=%s sub=%s payment=%s created (%s)", id, sub.ID, result.PaymentID, mode)
	writeJSON(w, 200, paymentStartResponse(mode, result, returnURL, map[string]any{"sid": sub.ID}))
}

// liveSubscriptionConfirmation — выданное по заказу подтверждение первого платежа
// подписки, если его ещё можно отдать снова: тот же режим, моложе confirmationTTL,
// платёж в ЮKassa всё ещё pending и на цену подписки (а не разовой покупки).
// Любая неясность — false: заводим новый платёж, доступность оплаты важнее дедупа.
func (s *Server) liveSubscriptionConfirmation(o store.Order, mode string, priceRub int) (store.IssuedConfirmation, bool) {
	c, ok := store.LoadConfirmation(o)
	if !ok || c.Mode != mode || time.Since(c.IssuedAt) >= confirmationTTL {
		return c, false
	}
	if (c.Mode == payment.ModeEmbedded && c.Token == "") || (c.Mode == payment.ModeRedirect && c.URL == "") {
		return c, false
	}
	if !s.yooConfigured() {
		return c, false
	}
	p, err := s.yoo().GetPayment(c.PaymentID)
	if err != nil || p.Status != "pending" {
		return c, false
	}
	if got, ok := rublesToKopecks(p.Amount); !ok || got != int64(priceRub)*100 {
		return c, false
	}
	return c, true
}

// paymentModeFromRequest — режим подтверждения платежа из тела запроса
// ({"mode":"embedded"|"redirect"}). По умолчанию embedded: новые воронки
// платят виджетом на своём домене, редирект остаётся фолбэком фронта.
func paymentModeFromRequest(r *http.Request) string {
	var body struct {
		Mode string `json:"mode"`
	}
	_ = readJSON(r, &body)
	if body.Mode == payment.ModeRedirect {
		return payment.ModeRedirect
	}
	return payment.ModeEmbedded
}

// paymentStartResponse — ответ ручки, заводящей платёж: redirect-платежу
// confirmationUrl, embedded — токен виджета и адрес возврата для него.
func paymentStartResponse(mode string, result payment.CreateResult, returnURL string, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range extra {
		out[k] = v
	}
	if mode == payment.ModeRedirect {
		out["confirmationUrl"] = result.ConfirmationURL
		return out
	}
	out["confirmationToken"] = result.ConfirmationToken
	out["paymentId"] = result.PaymentID
	out["returnUrl"] = returnURL
	return out
}

// createSubscriptionPayment — первый платёж подписки с сохранением карты.
// extraMeta дописывается к metadata (order_id для подписки вместо разовой
// покупки); subscription_id и flow вебхук читает всегда.
func (s *Server) createSubscriptionPayment(sub store.Subscription, email, mode, returnURL string, extraMeta map[string]string) (payment.CreateResult, error) {
	// Ключ идемпотентности задаём сами: дефолтный newIdempotenceKey(sub.ID)
	// длиннее лимита ЮKassa в 64 символа. Каждая попытка — новый платёж.
	var kb [16]byte
	_, _ = rand.Read(kb[:])
	meta := map[string]string{"subscription_id": sub.ID, "flow": store.SubFlowFirst}
	for k, v := range extraMeta {
		meta[k] = v
	}
	return s.yoo().Create(payment.CreateRequest{
		OrderID:        sub.ID,
		ValueRub:       rubPaymentAmount(subPrice(sub)),
		Mode:           mode,
		ReturnURL:      returnURL,
		IdempotenceKey: "substart-" + hex.EncodeToString(kb[:]),
		Receipt: payment.Receipt{
			Email:       email,
			Description: subscriptionReceiptDesc,
			VatCode:     s.Config.YooKassaVatCode,
		},
		Metadata:          meta,
		SavePaymentMethod: true,
	})
}

// fulfillOrderViaSubscription делает заказ песней по подписке: оплаченным с
// меткой SUBSCRIPTION, и запускает по нему всё то же, что вебхук разовой
// оплаты — генерацию, стадию воронки, письмо о готовности. Задержка доставки
// не применяется: песни подписчиков не придерживаются (applyDeliveryHold).
func (s *Server) fulfillOrderViaSubscription(ctx context.Context, orderID, subID string) {
	attached, err := store.AttachOrderToSubscription(ctx, s.DB, orderID, subID)
	if err != nil {
		log.Printf("[sub] fulfill order=%s sub=%s: attach err=%v", orderID, subID, err)
		return
	}
	if !attached {
		log.Printf("[sub] fulfill order=%s sub=%s: заказ уже оплачен, не трогаем", orderID, subID)
		return
	}
	_ = store.SetFunnelStage(ctx, s.DB, orderID, store.StagePaid)
	s.marketingOrder(orderID, map[string]string{marketing.FieldOrderPaid: "1"})
	s.marketingEvent(orderID, EventOrderPaid)
	o, err := store.GetOrder(ctx, s.DB, orderID)
	if err != nil {
		return
	}
	if hasSongConfig(o) {
		transitioned, err := store.ConfirmOrder(ctx, s.DB, orderID)
		if err != nil {
			log.Printf("[sub] fulfill order=%s: confirm err=%v", orderID, err)
		}
		if transitioned {
			go s.triggerRelay(context.Background(), orderID)
		}
	}
	log.Printf("[sub] fulfill order=%s sub=%s: песня по подписке", orderID, subID)
	go s.deliverIfReady(context.Background(), orderID, "subscription", "Спасибо, подписка оформлена!")
}

// sendSubscriptionTrialEmail — приветствие подписки «0 ₽ сегодня»: первое
// списание через месяц, отсюда и отличие от обычного welcome-письма.
func (s *Server) sendSubscriptionTrialEmail(sub store.Subscription) {
	s.sendSubscriptionMail(emailSubWelcome, sub, "Подписка оформлена",
		"Здравствуйте!\n\nБезлимит песен включён. Сегодняшняя песня уже входит в подписку — сегодня мы ничего не списывали.\n"+
			"Первое списание "+rubLabel(subPrice(sub))+" — "+fmtRuDate(sub.NextChargeAt.String)+
			", напомним письмом за 3 дня.\n\nКабинет: "+s.subAccountURL()+
			"\nОтменить подписку в любой момент: "+s.subCancelURL(sub)+"\n",
		map[string]string{"trial": "1"})
}

func rubLabel(n int) string {
	return strconv.Itoa(n) + " ₽"
}

// jsonField — строковое поле из JSON заказа.
func jsonField(raw, key string) (string, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", false
	}
	v, ok := m[key].(string)
	return strings.TrimSpace(v), ok
}
