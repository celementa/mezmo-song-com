// Stripe на стороне сервера: выставление встроенной формы, вебхук и подписки.
//
// Деньги за заказ зачитываются только по вебхуку checkout.session.completed и
// только после перечитывания сессии из API: подпись доказывает, что запрос
// прислал Stripe, но не доказывает сумму, валюту и статус оплаты.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

// stripeConfirmationMode — значение orders.yoo_confirmation.mode для Stripe.
// Колонка общая: в ней уже лежат подтверждения ЮKassa, и отдельная таблица
// ради одного провайдера только раздвоила бы дедуп повторных кликов.
const stripeConfirmationMode = "stripe_embedded"

func (s *Server) stripe() payment.Stripe {
	return payment.Stripe{SecretKey: s.Config.StripeSecretKey}
}

// stripeConfigured — можно ли принимать деньги через Stripe. Publishable key
// нужен фронту, webhook secret — серверу; без любого из трёх оплата не
// замкнётся, поэтому провайдер считается ненастроенным.
func (s *Server) stripeConfigured() bool {
	return s.Config.StripeSecretKey != "" &&
		s.Config.StripePublishableKey != "" &&
		s.Config.StripeWebhookSecret != ""
}

// stripePriceID — price_… для плеча и плана. Цены в Dashboard уже разбиты на
// разовую и подписку, поэтому сумма на сайте и в Stripe не может разъехаться.
func (s *Server) stripePriceID(o store.Order, plan string) string {
	// Плечи 500/750s продают $19/$15, все остальные — $25/$25. Имя плеча —
	// единственный признак тарифа, который есть в заказе.
	priced500 := strings.Contains(o.FlowVariant.String, "500-750s")
	if plan == PaymentPlanSubscription {
		if priced500 {
			return s.Config.StripePriceV3Subscription
		}
		return s.Config.StripePrice990Subscription
	}
	if priced500 {
		return s.Config.StripePriceV3Onetime
	}
	return s.Config.StripePrice990Onetime
}

func (s *Server) stripeLocale(o store.Order) string {
	if o.Lang() == store.LangEN {
		return "en"
	}
	return "ru"
}

// initiateStripe выставляет встроенную форму Stripe для заказа. Подписка
// (plan=subscription) оплачивает первый месяц и сама включает безлимит.
func (s *Server) initiateStripe(w http.ResponseWriter, r *http.Request, o store.Order, returnURL string) {
	ctx := r.Context()
	if !s.stripeConfigured() {
		writeJSON(w, 503, map[string]string{"error": "payments_unavailable"})
		return
	}
	plan := PaymentPlanOneTime
	if o.PaymentPlan.Valid && o.PaymentPlan.String == PaymentPlanSubscription {
		plan = PaymentPlanSubscription
	}
	priceID := s.stripePriceID(o, plan)
	if priceID == "" {
		log.Printf("[pay] stripe order=%s: price id для плана %s не настроен", o.ID, plan)
		writeJSON(w, 503, map[string]string{"error": "price_unavailable"})
		return
	}
	mode := payment.StripeModePayment
	if plan == PaymentPlanSubscription {
		mode = payment.StripeModeSubscription
	}
	email, _ := orderCustomer(o)

	// Повторный клик не должен открывать вторую оплачиваемую сессию. Живую
	// сессию перечитываем из API: confirmation могла остаться от прошлой ветки.
	if c, ok := store.LoadConfirmation(o); ok && c.Mode == stripeConfirmationMode &&
		time.Since(c.IssuedAt) < confirmationTTL && c.PaymentID != "" && c.Token != "" {
		if sess, err := s.stripe().GetCheckoutSession(c.PaymentID); err == nil && sess.Status == "open" {
			log.Printf("[pay] stripe order=%s session=%s reused", o.ID, sess.ID)
			writeJSON(w, 200, map[string]any{
				"provider":       ProviderStripe,
				"clientSecret":   c.Token,
				"publishableKey": s.Config.StripePublishableKey,
				"sessionId":      c.PaymentID,
				"returnUrl":      returnURL,
			})
			return
		}
	}

	bucket := time.Now().UTC().Unix() / 600 // десятиминутное окно для idempotency
	result, err := s.stripe().CreateCheckoutSession(payment.StripeCheckoutRequest{
		OrderID:           o.ID,
		Mode:              mode,
		PriceID:           priceID,
		CustomerEmail:     email,
		ReturnURL:         returnURL,
		Locale:            s.stripeLocale(o),
		Metadata:          map[string]string{"order_id": o.ID, "plan": plan, "branch": o.PaymentBranch.String},
		SavePaymentMethod: mode == payment.StripeModePayment,
		IdempotencyKey:    fmt.Sprintf("order-%s-%s-%d", o.ID, plan, bucket),
	})
	if err != nil {
		log.Printf("[pay] stripe order=%s: create session err=%v", o.ID, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	_ = store.SetPaymentProvider(ctx, s.DB, o.ID, ProviderStripe)
	if err := store.SaveConfirmation(ctx, s.DB, o.ID, store.IssuedConfirmation{
		PaymentID: result.ID,
		Mode:      stripeConfirmationMode,
		Token:     result.ClientSecret,
		URL:       returnURL,
		IssuedAt:  time.Now().UTC(),
	}); err != nil {
		log.Printf("[pay] stripe order=%s: save confirmation err=%v", o.ID, err)
	}
	log.Printf("[pay] stripe order=%s session=%s mode=%s amount=%d %s created",
		o.ID, result.ID, mode, s.expectedPaymentRubles(ctx, o), s.orderCurrency(o))
	writeJSON(w, 200, map[string]any{
		"provider":       ProviderStripe,
		"clientSecret":   result.ClientSecret,
		"publishableKey": s.Config.StripePublishableKey,
		"sessionId":      result.ID,
		"returnUrl":      returnURL,
	})
}

// handleStripeWebhook принимает события Stripe. Ответ 200 после проверки
// подписи: на бизнес-отказ (чужая сессия, неверная сумма) ретраить бессмысленно,
// 500 — только когда API/БД временно недоступны.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(400)
		return
	}
	if err := payment.VerifyStripeSignature(body, r.Header.Get("Stripe-Signature"), s.Config.StripeWebhookSecret, time.Now()); err != nil {
		log.Printf("[pay] stripe webhook: подпись не сошлась: %v", err)
		w.WriteHeader(400)
		return
	}
	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		w.WriteHeader(400)
		return
	}

	ctx := r.Context()
	switch event.Type {
	case "checkout.session.completed":
		var sess payment.StripeCheckoutSession
		if err := json.Unmarshal(event.Data.Object, &sess); err != nil || sess.ID == "" {
			log.Printf("[pay] stripe webhook: bad session object event=%s", event.ID)
			w.WriteHeader(200)
			return
		}
		outcome := s.settleStripeSession(ctx, sess.ID)
		if outcome == settleRetry {
			w.WriteHeader(500)
			return
		}
	case "invoice.paid":
		s.handleStripeInvoicePaid(ctx, event.Data.Object)
	case "customer.subscription.deleted":
		var sub payment.StripeSubscription
		if err := json.Unmarshal(event.Data.Object, &sub); err == nil && sub.ID != "" {
			if err := store.CancelStripeSubscription(ctx, s.DB, sub.ID); err != nil {
				log.Printf("[sub] stripe %s: cancel err=%v", sub.ID, err)
			}
		}
	case "charge.refunded":
		// Возвраты Stripe разбираются вручную: автоматическое закрытие доступа
		// требует перечитать charge/refund, а не доверять телу вебхука. Событие
		// принимаем, чтобы Stripe не ретраил его бесконечно.
		log.Printf("[pay] stripe webhook: charge.refunded получен, разбор вручную")
	default:
		log.Printf("[pay] stripe webhook: событие %s проигнорировано", event.Type)
	}
	w.WriteHeader(200)
}

// handleStripeInvoicePaid продлевает доступ по продлению подписки. Это НЕ
// покупка: конверсию не шлём (требование аналитики), поэтому здесь только
// access_until.
func (s *Server) handleStripeInvoicePaid(ctx context.Context, raw json.RawMessage) {
	var invoice struct {
		Subscription string `json:"subscription"`
	}
	if err := json.Unmarshal(raw, &invoice); err != nil || invoice.Subscription == "" {
		return
	}
	sub, err := s.stripe().GetSubscription(invoice.Subscription)
	if err != nil {
		log.Printf("[sub] stripe %s: get subscription err=%v", invoice.Subscription, err)
		return
	}
	periodEnd := time.Unix(sub.CurrentPeriodEnd, 0).UTC()
	if err := store.ExtendStripeSubscription(ctx, s.DB, sub.ID, periodEnd); err != nil {
		log.Printf("[sub] stripe %s: extend err=%v", sub.ID, err)
	}
}

// settleStripeSession зачитывает оплату заказа. Сессия перечитывается из API:
// сумма, валюта и payment_status из тела вебхука не доказательство.
func (s *Server) settleStripeSession(ctx context.Context, sessionID string) settleOutcome {
	sess, err := s.stripe().GetCheckoutSession(sessionID)
	if err != nil {
		log.Printf("[pay] stripe session=%s: get err=%v", sessionID, err)
		return settleRetry
	}
	orderID := strings.TrimSpace(sess.Metadata["order_id"])
	if orderID == "" {
		orderID = strings.TrimSpace(sess.ClientRefID)
	}
	if orderID == "" {
		log.Printf("[pay] stripe session=%s: нет order_id в metadata — чужая сессия", sessionID)
		return settleOK
	}
	prev, err := store.GetOrder(ctx, s.DB, orderID)
	if errors.Is(err, store.ErrNotFound) {
		log.Printf("[pay] stripe session=%s: заказ %s не найден", sessionID, orderID)
		return settleOK
	}
	if err != nil {
		log.Printf("[pay] stripe session=%s: get order %s err=%v", sessionID, orderID, err)
		return settleRetry
	}

	if sess.PaymentStatus != "paid" && sess.PaymentStatus != "no_payment_required" {
		log.Printf("[pay] stripe session=%s order=%s: payment_status=%s — не зачитываем",
			sessionID, orderID, sess.PaymentStatus)
		return settleOK
	}
	wantCurrency := s.orderCurrency(prev)
	if !strings.EqualFold(sess.Currency, strings.ToLower(wantCurrency)) {
		log.Printf("[pay] stripe session=%s order=%s REJECTED: валюта %s, ожидалась %s",
			sessionID, orderID, sess.Currency, wantCurrency)
		return settleRejected
	}
	expected := s.expectedPaymentRubles(ctx, prev)
	// trial-подписка («0 сегодня») законно приходит с amount_total=0.
	if sess.AmountTotal != int64(expected)*100 && !(sess.Mode == payment.StripeModeSubscription && sess.AmountTotal == 0) {
		log.Printf("[pay] stripe session=%s order=%s REJECTED: сумма %d, ожидалось %d %s",
			sessionID, orderID, sess.AmountTotal, expected, wantCurrency)
		return settleRejected
	}

	paymentID := sess.PaymentIntent
	if paymentID == "" {
		paymentID = sess.ID
	}

	// Разовая оплата с save_payment_method: запоминаем Customer и карту, чтобы
	// апсейл подписки после неё не просил данные заново. Ошибка чтения PI не
	// должна мешать зачёту денег — это только улучшение следующего шага.
	if sess.Mode == payment.StripeModePayment && sess.PaymentIntent != "" {
		if pi, err := s.stripe().GetPaymentIntent(sess.PaymentIntent); err == nil {
			customer := pi.Customer
			if customer == "" {
				customer = sess.Customer
			}
			if customer != "" || pi.PaymentMethod != "" {
				if err := store.SetOrderStripePayment(ctx, s.DB, orderID, customer, pi.PaymentMethod); err != nil {
					log.Printf("[pay] stripe session=%s order=%s: save customer/pm err=%v", sessionID, orderID, err)
				} else {
					log.Printf("[pay] stripe session=%s order=%s: saved customer=%s pm=%s", sessionID, orderID, customer, pi.PaymentMethod)
				}
			}
		} else {
			log.Printf("[pay] stripe session=%s order=%s: get payment intent err=%v", sessionID, orderID, err)
		}
	}

	if sess.Mode == payment.StripeModeSubscription {
		if err := s.activateStripeOrderSubscription(ctx, prev, sess, expected); err != nil {
			log.Printf("[pay] stripe session=%s order=%s: activate subscription err=%v", sessionID, orderID, err)
			return settleRetry
		}
	}

	outcome := s.applyOrderPaid(ctx, prev, orderID, paymentID, "stripe webhook")
	if outcome == settleOK && sess.Mode == payment.StripeModeSubscription {
		if err := s.linkStripeOrderSubscription(ctx, prev, sess); err != nil {
			log.Printf("[pay] stripe session=%s order=%s: link subscription err=%v", sessionID, orderID, err)
		}
	}
	return outcome
}

// activateStripeOrderSubscription создаёт/обновляет локальную подписку по
// первой оплате Stripe. Продления потом ведёт Stripe, локальный планировщик её
// не трогает (provider=stripe исключён из выборок).
func (s *Server) activateStripeOrderSubscription(ctx context.Context, o store.Order, sess payment.StripeCheckoutSession, price int) error {
	email, _ := orderCustomer(o)
	if email == "" {
		return fmt.Errorf("no customer email")
	}
	name, _ := jsonField(o.InputPayloadJSON, "yourName")
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, name)
	if errors.Is(err, store.ErrSubscriptionActive) {
		return nil // уже есть живая подписка — заказ просто войдёт в неё
	}
	if err != nil {
		return err
	}
	periodEnd := store.AddCalendarMonth(time.Now().UTC())
	if sess.Subscription != "" {
		if remote, err := s.stripe().GetSubscription(sess.Subscription); err == nil && remote.CurrentPeriodEnd > 0 {
			periodEnd = time.Unix(remote.CurrentPeriodEnd, 0).UTC()
		}
	}
	currency := strings.ToUpper(sess.Currency)
	_, err = store.ActivateStripeSubscription(ctx, s.DB, sub.ID, sess.Subscription, sess.Customer, currency, price, periodEnd)
	return err
}

// linkStripeOrderSubscription привязывает оплаченный заказ к подписке, не
// меняя сумму: для аналитики первый платёж подписки — это покупка на $15/$25.
func (s *Server) linkStripeOrderSubscription(ctx context.Context, o store.Order, sess payment.StripeCheckoutSession) error {
	email, _ := orderCustomer(o)
	sub, err := store.GetSubscriptionByEmail(ctx, s.DB, email)
	if err != nil {
		return err
	}
	return store.LinkOrderSubscription(ctx, s.DB, o.ID, sub.ID)
}

// initiateStripeUpsell — «безлимит за 0 сегодня» после разовой покупки на
// международной ветке. Stripe Checkout в режиме subscription с trial 30 дней:
// карта привязывается сейчас, первое списание — через месяц, как у ЮKassa.
func (s *Server) initiateStripeUpsell(w http.ResponseWriter, r *http.Request, o store.Order) {
	if !s.stripeConfigured() {
		writeJSON(w, 503, map[string]string{"error": "payments_unavailable"})
		return
	}
	priceID := s.stripePriceID(o, PaymentPlanSubscription)
	if priceID == "" {
		writeJSON(w, 503, map[string]string{"error": "price_unavailable"})
		return
	}
	email, _ := orderCustomer(o)

	// Если карта уже сохранена разовой оплатой — создаём подписку напрямую,
	// без Checkout и повторного ввода данных. Trial 30 дней: «0 ₽ сегодня».
	if o.StripeCustomerID.Valid && o.StripeCustomerID.String != "" {
		price := s.expectedPaymentRubles(r.Context(), o)
		bucket := time.Now().UTC().Unix() / 600
		sub, err := s.stripe().CreateSubscription(payment.StripeSubscriptionRequest{
			Customer:             o.StripeCustomerID.String,
			PriceID:              priceID,
			DefaultPaymentMethod: o.StripePaymentMethodID.String,
			TrialDays:            30,
			Metadata:             map[string]string{"order_id": o.ID, "upsell": "subscription"},
			IdempotencyKey:       fmt.Sprintf("upsell-sub-%s-%d", o.ID, bucket),
		})
		if err == nil && (sub.Status == "trialing" || sub.Status == "active") {
			periodEnd := store.AddCalendarMonth(time.Now().UTC())
			if sub.CurrentPeriodEnd > 0 {
				periodEnd = time.Unix(sub.CurrentPeriodEnd, 0).UTC()
			}
			if err := s.activateStripeSubscriptionDirect(r.Context(), o, sub, price, periodEnd); err != nil {
				log.Printf("[sub] stripe upsell order=%s: direct activate err=%v", o.ID, err)
			} else {
				log.Printf("[sub] stripe upsell order=%s: direct subscription %s activated", o.ID, sub.ID)
				writeJSON(w, 200, map[string]any{"provider": ProviderStripe, "activated": true, "subscriptionId": sub.ID})
				return
			}
		} else if err != nil {
			log.Printf("[sub] stripe upsell order=%s: direct create err=%v — fallback to checkout", o.ID, err)
		}
	}
	bucket := time.Now().UTC().Unix() / 600
	result, err := s.stripe().CreateCheckoutSession(payment.StripeCheckoutRequest{
		OrderID:        o.ID,
		Mode:           payment.StripeModeSubscription,
		PriceID:        priceID,
		CustomerEmail:  email,
		ReturnURL:      s.publicOrderURL(o.ID),
		Locale:         s.stripeLocale(o),
		TrialDays:      30,
		Metadata:       map[string]string{"order_id": o.ID, "upsell": "subscription"},
		IdempotencyKey: fmt.Sprintf("upsell-%s-%d", o.ID, bucket),
	})
	if err != nil {
		log.Printf("[sub] stripe upsell order=%s: create session err=%v", o.ID, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	log.Printf("[sub] stripe upsell order=%s session=%s created (trial 30d)", o.ID, result.ID)
	writeJSON(w, 200, map[string]any{
		"provider":       ProviderStripe,
		"clientSecret":   result.ClientSecret,
		"publishableKey": s.Config.StripePublishableKey,
		"sessionId":      result.ID,
	})
}

// activateStripeSubscriptionDirect включает безлимит по уже сохранённой карте:
// Stripe Subscription создан с trial 30 дней, повторная форма не нужна.
func (s *Server) activateStripeSubscriptionDirect(ctx context.Context, o store.Order, remote payment.StripeSubscription, price int, periodEnd time.Time) error {
	email, _ := orderCustomer(o)
	if email == "" {
		return fmt.Errorf("no customer email")
	}
	name, _ := jsonField(o.InputPayloadJSON, "yourName")
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, name)
	if errors.Is(err, store.ErrSubscriptionActive) {
		existing, gerr := store.GetSubscriptionByEmail(ctx, s.DB, email)
		if gerr != nil {
			return gerr
		}
		return store.LinkOrderSubscription(ctx, s.DB, o.ID, existing.ID)
	}
	if err != nil {
		return err
	}
	if _, err := store.ActivateStripeSubscription(ctx, s.DB, sub.ID, remote.ID, remote.Customer, "USD", price, periodEnd); err != nil {
		return err
	}
	return store.LinkOrderSubscription(ctx, s.DB, o.ID, sub.ID)
}
