package httpserver

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mezmo-song-com/internal/marketing"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

// Подписка «безлимит песен»: 2000 ₽ списываются раз в календарный месяц по
// сохранённой карте ЮKassa. Здесь весь HTTP-слой подписки: оформление, кабинет
// (вход по email), отмена (страница по токену и кнопка в кабинете) и фоновый
// планировщик ребиллов. Состояние — в store/subscriptions.go.
const (
	subscriptionPriceRub    = 990 // общая цена; у подписки из ценового плеча своя (Subscription.PriceRub)
	subscriptionReceiptDesc = "Подписка на персональные песни (ежемесячное списание по оферте)"

	// Политика ребиллов: напоминание за 3 дня; при отказе банка ретрай раз в
	// 7 дней; через 30 дней после плановой даты подписка закрывается.
	subReminderLead  = 3 * 24 * time.Hour
	subRetryCooldown = 7 * 24 * time.Hour
	subGiveUpAfter   = 30 * 24 * time.Hour
	subSessionCookie = "sub_session"
	subDailyWindow   = 24 * time.Hour
)

func (s *Server) registerSubscription(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/subscription/start", s.requireSubscriptions(s.handleSubscriptionStart))
	mux.HandleFunc("POST /api/admin/subscriptions/rebill-now", s.handleAdminSubscriptionRebillNow)
	mux.HandleFunc("GET /api/admin/subscriptions/payments", s.handleAdminSubscriptionPayments)
	mux.HandleFunc("POST /api/admin/subscriptions/gift", s.handleAdminSubscriptionGift)
	mux.HandleFunc("GET /api/subscription/status", s.handleSubscriptionStatus)
	mux.HandleFunc("POST /api/subscription/session", s.handleSubscriptionLogin)
	mux.HandleFunc("POST /api/subscription/logout", s.handleSubscriptionLogout)
	mux.HandleFunc("GET /api/subscription/me", s.handleSubscriptionMe)
	mux.HandleFunc("GET /api/subscription/orders", s.handleSubscriptionOrders)
	mux.HandleFunc("POST /api/subscription/orders/draft", s.handleSubscriptionOrderDraft)
	mux.HandleFunc("POST /api/subscription/cancel-request", s.handleSubscriptionCancelRequest)
	mux.HandleFunc("GET /api/subscription/cancel-info", s.handleSubscriptionCancelInfo)
	mux.HandleFunc("POST /api/subscription/cancel", s.handleSubscriptionCancel)
}

// subscriptionsAvailable — можно ли продавать подписку. Рекуррентные списания
// реализованы только через ЮKassa; у UnitPay подписки включает их служба
// безопасности, и интеграции под них пока нет.
func (s *Server) subscriptionsAvailable() bool {
	return s.yooConfigured()
}

// requireSubscriptions закрывает ручки оформления подписки, пока продавать её
// нечем. Без этого гейта ручки ушли бы в dev-ветку «нет ключей ЮKassa —
// активировать без оплаты» и на mezmo-song.com раздавали бы подписку даром.
func (s *Server) requireSubscriptions(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.subscriptionsAvailable() && s.unitpayConfigured() {
			writeJSON(w, 503, map[string]string{"error": "subscriptions_unavailable"})
			return
		}
		h(w, r)
	}
}

func (s *Server) yooConfigured() bool {
	return s.Config.YooKassaShopID != "" && s.Config.YooKassaSecretKey != ""
}

func (s *Server) yoo() payment.YooKassa {
	return payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
}

// subPrice — цена подписки, ₽/мес: зафиксированная при оформлении или общая.
func subPrice(sub store.Subscription) int {
	if sub.PriceRub > 0 {
		return sub.PriceRub
	}
	return subscriptionPriceRub
}

// handleSubscriptionStart заводит первый платёж подписки с привязкой карты и
// отдаёт confirmationUrl для редиректа на ЮKassa. Без ключей ЮKassa (dev/mock)
// подписка активируется сразу.
func (s *Server) handleSubscriptionStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Email       string         `json:"email"`
		Name        string         `json:"name"`
		Mode        string         `json:"mode"`
		Attribution map[string]any `json:"attribution"`
		GAClientID  string         `json:"gaClientId"`
		YMClientID  string         `json:"ymClientId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	mode := payment.ModeEmbedded
	if body.Mode == payment.ModeRedirect {
		mode = payment.ModeRedirect
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeJSON(w, 400, map[string]string{"error": "email_required"})
		return
	}

	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, body.Name)
	if err == store.ErrSubscriptionActive {
		writeJSON(w, 409, map[string]string{"error": "already_active"})
		return
	}
	if err != nil {
		log.Printf("[sub] start email=%s: upsert err=%v", email, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	s.marketingContact(email, nil)

	// Источник прихода подписки — для дашборда /admin/analytics. Ошибка записи
	// оформление не ломает: атрибуция вспомогательна.
	if len(body.Attribution) > 0 {
		if raw, err := json.Marshal(body.Attribution); err == nil {
			if err := store.SetSubscriptionAttribution(ctx, s.DB, sub.ID, string(raw)); err != nil {
				log.Printf("[sub] start sub=%s: save attribution err=%v", sub.ID, err)
			}
		}
	}
	// Идентификаторы браузера — для серверных конверсий по платежам подписки.
	if body.GAClientID != "" || body.YMClientID != "" {
		if err := store.SetSubscriptionClientIDs(ctx, s.DB, sub.ID, body.GAClientID, body.YMClientID); err != nil {
			log.Printf("[sub] start sub=%s: save client ids err=%v", sub.ID, err)
		}
	}

	// Активация без оплаты: dev-режим (нет ключей ЮKassa) либо тестовый email
	// из настройки subscription.test_emails — так флоу проверяется на проде,
	// пока ЮKassa не включила рекуррентные платежи. Для остальных адресов
	// оплата обязательна, поэтому забытая настройка не раздаёт подписки всем.
	if !s.yooConfigured() || store.IsSubscriptionTestEmail(ctx, s.DB, email) {
		if _, err := store.RecordSubscriptionPayment(ctx, s.DB, sub.ID, "mock-"+sub.ID+"-"+time.Now().UTC().Format("20060102150405"), rubPaymentAmount(subPrice(sub)), store.SubFlowFirst, store.MockPaymentMethodID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		// Как и боевой вебхук: приветственное письмо со ссылками на кабинет и
		// отмену — стенд проходит флоу целиком.
		if fresh, err := store.GetSubscription(ctx, s.DB, sub.ID); err == nil {
			go s.sendSubscriptionWelcomeEmail(fresh)
		}
		s.marketingContact(email, map[string]string{marketing.FieldSubscriber: "1"})
		log.Printf("[sub] start email=%s: activated WITHOUT payment (dev mode or subscription.test_emails)", email)
		writeJSON(w, 200, map[string]any{"sid": sub.ID, "activated": true})
		return
	}

	returnURL := strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/subscribe/return?sid=" + sub.ID
	result, err := s.createSubscriptionPayment(sub, email, mode, returnURL, nil)
	if err != nil {
		log.Printf("[sub] start sub=%s: create payment err=%v", sub.ID, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	s.rememberSubscriptionPayment(ctx, sub.ID, result.PaymentID)
	log.Printf("[sub] start sub=%s email=%s payment=%s created (%s)", sub.ID, email, result.PaymentID, mode)
	writeJSON(w, 200, paymentStartResponse(mode, result, returnURL, map[string]any{"sid": sub.ID}))
}

// handleSubscriptionStatus — поллинг страницы возврата с ЮKassa. sid — это
// непубличный случайный id подписки, эндпоинт отдаёт только статус и срок.
// handleAdminSubscriptionPayments — платежи подписок за период для /admin/orders:
// в ЮKassa они выглядят как обычные платежи, а в списке заказов их нет — песни
// подписчиков идут заказами на 0 ₽. Без этого списка сверка «сколько денег
// пришло» с кабинетом ЮKassa не сходится.
func (s *Server) handleAdminSubscriptionPayments(w http.ResponseWriter, r *http.Request) {
	days := 30
	if q := strings.TrimSpace(r.URL.Query().Get("days")); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	payments, err := store.ListSubscriptionPaymentsSince(r.Context(), s.DB, since)
	if err != nil {
		log.Printf("list subscription payments: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	out := make([]map[string]any, 0, len(payments))
	total := 0
	for _, p := range payments {
		total += p.AmountRub()
		out = append(out, map[string]any{
			"paymentId":      p.PaymentID,
			"subscriptionId": p.SubscriptionID,
			"createdAt":      p.CreatedAt.UTC().Format(time.RFC3339),
			"email":          p.Email,
			"amount":         p.AmountRub(),
			"flow":           p.Flow,
			"subStatus":      p.SubStatus,
		})
	}
	writeJSON(w, 200, map[string]any{"days": days, "since": since, "total": total, "payments": out})
}

// handleAdminSubscriptionRebillNow — «списать сейчас»: переносит плановую дату
// на текущий момент и тут же прогоняет планировщик, то есть делает ровно то,
// что случилось бы в день списания. На стенде так проверяется рекуррентка без
// ожидания месяца; на проде — ручной повтор после отказа банка. Ответ — всё
// состояние подписки после прогона, чтобы видеть исход без похода в БД.
func (s *Server) handleAdminSubscriptionRebillNow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Email) == "" {
		writeJSON(w, 400, map[string]string{"error": "bad_request"})
		return
	}
	ctx := r.Context()
	sub, err := store.GetSubscriptionByEmail(ctx, s.DB, strings.ToLower(strings.TrimSpace(body.Email)))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	ok, err := store.ForceSubscriptionChargeDue(ctx, s.DB, sub.ID, time.Now().UTC())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !ok {
		writeJSON(w, 409, map[string]string{"error": "no_saved_method", "status": sub.Status})
		return
	}
	log.Printf("[sub] admin rebill-now sub=%s email=%s", sub.ID, sub.Email)
	s.runSubscriptionJobsOnce(ctx)
	fresh, err := store.GetSubscription(ctx, s.DB, sub.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"sid":               fresh.ID,
		"status":            fresh.Status,
		"accessUntil":       fresh.AccessUntil.String,
		"nextChargeAt":      fresh.NextChargeAt.String,
		"lastChargeAttempt": fresh.LastAttemptAt.String,
		"paymentMethodId":   fresh.YooMethodID.String,
	})
}

func (s *Server) handleSubscriptionStatus(w http.ResponseWriter, r *http.Request) {
	sid := strings.TrimSpace(r.URL.Query().Get("sid"))
	sub, err := store.GetSubscription(r.Context(), s.DB, sid)
	if err == store.ErrNotFound || sid == "" {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"status":      sub.Status,
		"active":      sub.HasAccess(time.Now().UTC()) && sub.Status == store.SubActive,
		"accessUntil": nullStr(sub.AccessUntil.String),
		"price":       subPrice(sub),
	})
}

// handleAdminSubscriptionGift — подарить покупателю оплаченного заказа месяц
// безлимита: без карты и без автосписаний, с письмом и ссылкой «продлить».
// Так исправляются покупки, которым подписка полагалась, а продали разовую:
// списывать без согласия нельзя, поэтому продление человек оформляет сам.
func (s *Server) handleAdminSubscriptionGift(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID  string `json:"orderId"`
		PriceRub int    `json:"priceRub"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.OrderID) == "" || body.PriceRub <= 0 {
		writeJSON(w, 400, map[string]string{"error": "bad_request"})
		return
	}
	ctx := r.Context()
	o, err := store.GetOrder(ctx, s.DB, strings.TrimSpace(body.OrderID))
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
	name, _ := jsonField(o.InputPayloadJSON, "yourName")
	sub, err := store.UpsertPendingSubscription(ctx, s.DB, email, name)
	if errors.Is(err, store.ErrSubscriptionActive) {
		writeJSON(w, 409, map[string]string{"error": "already_active"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	gifted, err := store.GiftSubscriptionAccess(ctx, s.DB, sub.ID, body.PriceRub, time.Now())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !gifted {
		writeJSON(w, 409, map[string]string{"error": "already_had_subscription", "status": sub.Status})
		return
	}
	fresh, err := store.GetSubscription(ctx, s.DB, sub.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	log.Printf("[sub] admin gift order=%s sub=%s email=%s price=%d until=%s", o.ID, fresh.ID, email, body.PriceRub, fresh.AccessUntil.String)
	mailed := s.sendSubscriptionGiftEmail(fresh) == nil
	writeJSON(w, 200, map[string]any{"sid": fresh.ID, "email": email, "accessUntil": fresh.AccessUntil.String, "mailed": mailed})
}

// --- Кабинет ---

func (s *Server) subCookieSecure() bool {
	return strings.HasPrefix(s.Config.PublicAppURL, "https://")
}

// handleSubscriptionLogin — вход по email без пароля: cookie выдаётся, только
// если у подписки с этим адресом ещё идёт оплаченный период. Модель как в
// библиотеке рун: адрес почты и есть доступ, ссылки на песни и так уходят на него.
func (s *Server) handleSubscriptionLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	sub, err := store.GetSubscriptionByEmail(ctx, s.DB, body.Email)
	if err == store.ErrNotFound {
		writeJSON(w, 403, map[string]string{"error": "no_subscription"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	now := time.Now().UTC()
	if !sub.HasAccess(now) {
		writeJSON(w, 403, map[string]string{"error": "access_expired"})
		return
	}
	expires := sub.AccessUntilTime()
	token, err := store.CreateSubscriptionSession(ctx, s.DB, sub.ID, expires)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     subSessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.subCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, 200, map[string]any{"ok": true, "accessUntil": sub.AccessUntil.String})
}

func (s *Server) handleSubscriptionLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(subSessionCookie); err == nil {
		store.DeleteSubscriptionSession(r.Context(), s.DB, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: subSessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.subCookieSecure(), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

// subFromSession достаёт подписку по cookie; nil — сессии нет или доступ истёк.
func (s *Server) subFromSession(r *http.Request) *store.Subscription {
	c, err := r.Cookie(subSessionCookie)
	if err != nil {
		return nil
	}
	sub, err := store.SubscriptionBySession(r.Context(), s.DB, c.Value)
	if err != nil {
		return nil
	}
	if !sub.HasAccess(time.Now().UTC()) {
		return nil
	}
	return &sub
}

func (s *Server) handleSubscriptionMe(w http.ResponseWriter, r *http.Request) {
	sub := s.subFromSession(r)
	if sub == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	limit := store.SubscriptionDailyLimit(ctx, s.DB)
	used := 0
	if limit > 0 {
		if n, err := store.CountSubscriptionOrdersSince(ctx, s.DB, sub.ID, time.Now().UTC().Add(-subDailyWindow)); err == nil {
			used = n
		}
	}
	writeJSON(w, 200, map[string]any{
		"email":        sub.Email,
		"name":         nullStr(sub.CustomerName.String),
		"status":       sub.Status,
		"accessUntil":  nullStr(sub.AccessUntil.String),
		"nextChargeAt": nullStr(sub.NextChargeAt.String),
		"canceled":     sub.Status == store.SubCanceled,
		"priceRub":     subPrice(*sub),
		"dailyLimit":   limit,
		"usedToday":    used,
	})
}

func (s *Server) handleSubscriptionOrders(w http.ResponseWriter, r *http.Request) {
	sub := s.subFromSession(r)
	if sub == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	orders, err := store.ListSubscriptionOrders(r.Context(), s.DB, sub.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	out := make([]map[string]any, 0, len(orders))
	for _, o := range orders {
		out = append(out, map[string]any{
			"orderId":        publicOrderRef(s.Config.WebhookSecret, o.ID),
			"recipientName":  jsonRecipient(o.InputPayloadJSON),
			"status":         o.Status,
			"deliveryStatus": o.DeliveryStatus,
			"isFallback":     store.IsFallbackAudio(o.AudioURLsJSON),
			"createdAt":      o.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, 200, map[string]any{"orders": out})
}

// handleSubscriptionOrderDraft создаёт песню в рамках подписки: заказ сразу
// paid (BasePrice=0, метка SUBSCRIPTION), генерация стартует немедленно — как
// внутренние заказы из /admin/ads, но с суточным лимитом.
func (s *Server) handleSubscriptionOrderDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sub := s.subFromSession(r)
	if sub == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	limit := store.SubscriptionDailyLimit(ctx, s.DB)
	if limit > 0 {
		used, err := store.CountSubscriptionOrdersSince(ctx, s.DB, sub.ID, time.Now().UTC().Add(-subDailyWindow))
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		if used >= limit {
			writeJSON(w, 429, map[string]any{"error": "daily_limit_reached", "dailyLimit": limit})
			return
		}
	}

	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	// Письма о готовности уходят на адрес подписки, что бы ни пришло из формы.
	body["email"] = sub.Email

	o := buildOrderFromBody(s.Config, body)
	o.PaymentStatus = "paid"
	o.BasePrice = 0
	o.DiscountCode = sql.NullString{String: store.SubscriptionDiscountCode, Valid: true}
	o.SubscriptionID = sql.NullString{String: sub.ID, Valid: true}

	if err := store.CreateOrder(ctx, s.DB, o); err != nil {
		log.Printf("[sub] create order sub=%s: %v", sub.ID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	transitioned, err := store.ConfirmOrder(ctx, s.DB, o.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if transitioned {
		go s.triggerRelay(context.Background(), o.ID)
	}
	log.Printf("[sub] order=%s created for sub=%s", o.ID, sub.ID)
	writeJSON(w, 200, map[string]any{"orderId": publicOrderRef(s.Config.WebhookSecret, o.ID)})
}

// --- Отмена ---

// handleSubscriptionCancelRequest шлёт ссылку отмены на email. Ответ всегда
// одинаковый: наличие подписки по адресу не раскрывается.
func (s *Server) handleSubscriptionCancelRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Email string `json:"email"`
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
	sub, err := store.GetSubscriptionByEmail(ctx, s.DB, email)
	if err == nil && sub.Status != store.SubCanceled {
		go s.sendSubscriptionCancelLinkEmail(sub)
	} else if err != nil && err != store.ErrNotFound {
		log.Printf("[sub] cancel-request email=%s: %v", email, err)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleSubscriptionCancelInfo — данные для страницы подтверждения отмены.
// Сама отмена только POST-ом: GET-ссылку из письма дёргают префетчеры почты,
// и молчаливая отмена по GET (как в рунах) снимала бы подписку без ведома человека.
func (s *Server) handleSubscriptionCancelInfo(w http.ResponseWriter, r *http.Request) {
	sub, err := store.GetSubscriptionByCancelToken(r.Context(), s.DB, r.URL.Query().Get("t"))
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"email":       sub.Email,
		"canceled":    sub.Status == store.SubCanceled,
		"accessUntil": nullStr(sub.AccessUntil.String),
	})
}

// handleSubscriptionCancel отменяет подписку: по токену из письма или по
// cookie кабинета. Доступ остаётся до конца оплаченного периода.
func (s *Server) handleSubscriptionCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Token string `json:"token"`
	}
	_ = readJSON(r, &body) // пустое тело допустимо: отмена из кабинета идёт по cookie

	subID := ""
	if tok := strings.TrimSpace(body.Token); tok != "" {
		sub, err := store.GetSubscriptionByCancelToken(ctx, s.DB, tok)
		if err == store.ErrNotFound {
			writeJSON(w, 404, map[string]string{"error": "not_found"})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		subID = sub.ID
	} else if sub := s.subFromSession(r); sub != nil {
		subID = sub.ID
	} else {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}

	email, accessUntil, ok, err := store.CancelSubscription(ctx, s.DB, subID)
	if err != nil {
		log.Printf("[sub] cancel sub=%s: %v", subID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !ok {
		// Уже отменена — для клиента это тот же успешный исход.
		sub, err := store.GetSubscription(ctx, s.DB, subID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "alreadyCanceled": true, "accessUntil": nullStr(sub.AccessUntil.String)})
		return
	}
	log.Printf("[sub] sub=%s email=%s canceled, access until %s", subID, email, accessUntil.Format(time.RFC3339))
	s.marketingContact(email, map[string]string{marketing.FieldSubscriber: "0", marketing.FieldSubscriberCanceled: "1"})
	au := ""
	if !accessUntil.IsZero() {
		au = accessUntil.Format(time.RFC3339)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "accessUntil": nullStr(au)})
}

// --- Вебхук ---

// handleSubscriptionWebhook обрабатывает события ЮKassa по платежам подписки
// (metadata.subscription_id вместо order_id). Как и для заказов, тело вебхука
// ничего не доказывает — состояние перечитывается из API.
func (s *Server) handleSubscriptionWebhook(w http.ResponseWriter, r *http.Request, event, paymentID, subID string) {
	log.Printf("[sub] webhook event=%s sub=%s payment=%s", event, subID, paymentID)
	if event != "payment.succeeded" {
		// canceled по первому платежу означает «человек не оплатил» — подписка
		// остаётся pending и просто не активируется; canceled по ребиллу
		// обрабатывает сам планировщик по ответу API. Возвраты подписки — вручную.
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	switch s.settleSubscriptionPayment(r.Context(), subID, paymentID, "webhook") {
	case settleOK:
		writeJSON(w, 200, map[string]any{"ok": true})
	case settleNotVerifiable:
		writeJSON(w, 200, map[string]any{"ok": false, "error": "not_verifiable"})
	case settleNotFound:
		writeJSON(w, 200, map[string]any{"ok": false, "error": "unknown_subscription"})
	case settleRejected:
		writeJSON(w, 200, map[string]any{"ok": false, "error": "verification_failed"})
	default:
		w.WriteHeader(500) // ЮKassa повторит
	}
}

// settleSubscriptionPayment — единственный путь платежа подписки в
// subscription_payments (активация, продление, письмо, конверсии). Зовут вебхук
// ЮKassa и фоновая сверка reconcile, если вебхук не дошёл; src — только для логов.
func (s *Server) settleSubscriptionPayment(ctx context.Context, subID, paymentID, src string) settleOutcome {
	if !s.yooConfigured() {
		return settleNotVerifiable
	}
	p, err := s.yoo().GetPayment(paymentID)
	if err != nil {
		log.Printf("[sub] %s sub=%s payment=%s: verify err=%v", src, subID, paymentID, err)
		return settleRetry
	}
	sub, err := store.GetSubscription(ctx, s.DB, subID)
	if err != nil {
		log.Printf("[sub] %s sub=%s payment=%s: get err=%v", src, subID, paymentID, err)
		return settleNotFound
	}
	if reason := verifySubscriptionPayment(p, subID, paymentID, subPrice(sub)); reason != "" {
		log.Printf("[sub] %s sub=%s payment=%s REJECTED: %s (api: status=%s paid=%v amount=%s %s)",
			src, subID, paymentID, reason, p.Status, p.Paid, p.Amount, p.Currency)
		return settleRejected
	}
	flow := p.Metadata["flow"]
	if flow != store.SubFlowRebill {
		flow = store.SubFlowFirst
	}
	first, err := store.RecordSubscriptionPayment(ctx, s.DB, subID, paymentID, p.Amount, flow, p.PaymentMethodID)
	if err != nil {
		log.Printf("[sub] %s sub=%s payment=%s: record err=%v", src, subID, paymentID, err)
		return settleRetry
	}
	if !first {
		log.Printf("[sub] %s sub=%s payment=%s: duplicate, no action", src, subID, paymentID)
		return settleOK
	}
	orderID := p.Metadata["order_id"]
	sub, err = store.GetSubscription(ctx, s.DB, subID)
	if err == nil {
		if flow == store.SubFlowFirst {
			go s.sendSubscriptionWelcomeEmail(sub)
		} else {
			go s.sendSubscriptionChargedEmail(sub)
		}
		s.marketingContact(sub.Email, map[string]string{marketing.FieldSubscriber: "1"})
		// Деньги подписки — такая же покупка для рекламы, как разовый заказ:
		// без конверсии Директ и VK не видят, что клик окупился.
		go s.sendSubscriptionConversions(sub, paymentID, p.Amount, orderID)
	}
	// Подписка вместо разовой покупки (ценовое плечо): заказ из metadata
	// становится песней по подписке.
	if orderID != "" && flow == store.SubFlowFirst {
		s.fulfillOrderViaSubscription(ctx, orderID, subID)
	}
	log.Printf("[sub] %s sub=%s payment=%s: recorded flow=%s method=%s", src, subID, paymentID, flow, p.PaymentMethodID)
	return settleOK
}

// sendSubscriptionConversions — платёж подписки в серверную шину конверсий
// (GA4 + Метрика + VK). transaction_id — id платежа: у подписки их много
// (первый и ребиллы), и каждый — отдельная покупка. Идентификаторы браузера
// берутся с подписки, а если их нет — с заказа, вместо которого оформлена
// подписка (там они сняты при создании драфта).
func (s *Server) sendSubscriptionConversions(sub store.Subscription, paymentID, amountValue, orderID string) {
	whole, _, _ := strings.Cut(amountValue, ".")
	value, _ := strconv.Atoi(strings.TrimSpace(whole))
	c := conversion{
		Label:       "subscription " + sub.ID + " payment " + paymentID,
		TxID:        paymentID,
		Value:       value,
		ItemID:      "subscription",
		ItemName:    "Подписка на песни",
		GAClientID:  sub.GAClientID.String,
		YMClientID:  sub.YMClientID.String,
		Attribution: sub.AttributionJSON.String,
	}
	if orderID != "" && (c.GAClientID == "" || c.YMClientID == "" || c.Attribution == "") {
		if o, err := store.GetOrder(context.Background(), s.DB, orderID); err == nil {
			if c.GAClientID == "" {
				c.GAClientID = o.GAClientID.String
			}
			if c.YMClientID == "" {
				c.YMClientID = o.YMClientID.String
			}
			if c.Attribution == "" {
				c.Attribution = o.AttributionJSON.String
			}
		}
	}
	s.sendConversion(c)
}

// verifySubscriptionPayment — те же проверки, что verifyYooPayment для заказов:
// платёж реально успешен, сумма и валюта ровно подписочные, metadata указывает
// на эту подписку.
func verifySubscriptionPayment(p payment.Payment, subID, webhookPaymentID string, priceRub int) string {
	if p.ID != webhookPaymentID {
		return "payment_id_mismatch"
	}
	if p.Status != "succeeded" || !p.Paid {
		return "not_succeeded"
	}
	if p.Metadata["subscription_id"] != subID {
		return "subscription_id_mismatch"
	}
	if p.Currency != "RUB" || p.Amount != rubPaymentAmount(priceRub) {
		return "amount_mismatch"
	}
	return ""
}

// --- Планировщик ребиллов ---

// StartSubscriptionJobs запускает фоновый цикл подписок: напоминания о
// списании, сами списания и закрытие безнадёжно просроченных. Тик раз в час;
// все шаги идемпотентны (детерминированные ключи и отметки в БД), поэтому
// рестарты сервера и совпавшие тики безопасны.
func (s *Server) StartSubscriptionJobs(ctx context.Context) {
	go func() {
		// Первый прогон вскоре после старта: сервер деплоится каждый день, и
		// ждать час после каждого рестарта значило бы систематически опаздывать.
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			s.runSubscriptionJobsOnce(ctx)
			timer.Reset(time.Hour)
		}
	}()
}

func (s *Server) runSubscriptionJobsOnce(ctx context.Context) {
	now := time.Now().UTC()

	// 1. Напоминания за subReminderLead до списания — требование честной
	// рекуррентки: человек успевает отменить до снятия денег.
	reminders, err := store.ListSubscriptionsNeedingReminder(ctx, s.DB, now, subReminderLead)
	if err != nil {
		log.Printf("[sub] jobs: list reminders err=%v", err)
	}
	for _, sub := range reminders {
		// Выключенное в админке напоминание считается отработанным: иначе
		// планировщик пытался бы его отправить каждый час до самого списания.
		if err := s.sendSubscriptionReminderEmail(sub); err == nil || errors.Is(err, errEmailDisabled) {
			if err := store.MarkReminderSent(ctx, s.DB, sub.ID, sub.NextChargeAt.String); err != nil {
				log.Printf("[sub] jobs: mark reminder sub=%s err=%v", sub.ID, err)
			}
		}
	}

	// 2. Списания. Без ключей ЮKassa планировщик молчит (dev-режим).
	if s.yooConfigured() {
		due, err := store.ListDueSubscriptionCharges(ctx, s.DB, now, subRetryCooldown, subGiveUpAfter)
		if err != nil {
			log.Printf("[sub] jobs: list due err=%v", err)
		}
		for _, sub := range due {
			s.chargeSubscription(ctx, sub, now)
		}
	}

	// 3. Просроченные дольше subGiveUpAfter закрываются насовсем.
	expired, err := store.ExpireOverdueSubscriptions(ctx, s.DB, now, subGiveUpAfter)
	if err != nil {
		log.Printf("[sub] jobs: expire err=%v", err)
	}
	for _, sub := range expired {
		// Подписка без оплаты просто доживает свой месяц: письмо «не удалось
		// продлить» про списание, которого не было, сбивает с толку.
		if store.IsMockPaymentMethod(sub.YooMethodID.String) {
			log.Printf("[sub] sub=%s email=%s: test subscription closed after its period", sub.ID, sub.Email)
			continue
		}
		log.Printf("[sub] sub=%s email=%s expired after %s of failed charges", sub.ID, sub.Email, subGiveUpAfter)
		go s.sendSubscriptionExpiredEmail(sub)
		s.marketingContact(sub.Email, map[string]string{marketing.FieldSubscriber: "0", marketing.FieldSubscriberCanceled: "1"})
	}
}

// chargeSubscription — одно автосписание по сохранённой карте. Ключ
// идемпотентности детерминирован в пределах суток: совпавшие тики и рестарты
// не списывают дважды, а ретрай в следующие сутки — уже новый платёж.
// Полный ключ (id + дата списания + день) длиннее лимита ЮKassa в 64 символа,
// поэтому в заголовок уходит его sha256-отпечаток.
func (s *Server) chargeSubscription(ctx context.Context, sub store.Subscription, now time.Time) {
	keySrc := fmt.Sprintf("sub-rebill-%s-%s-%s", sub.ID, sub.NextChargeAt.String, now.Format("20060102"))
	sum := sha256.Sum256([]byte(keySrc))
	idempo := "rb-" + hex.EncodeToString(sum[:20])
	result, err := s.yoo().Create(payment.CreateRequest{
		OrderID:  sub.ID,
		ValueRub: rubPaymentAmount(subPrice(sub)),
		Receipt: payment.Receipt{
			Email:       sub.Email,
			Description: subscriptionReceiptDesc,
			VatCode:     s.Config.YooKassaVatCode,
		},
		Metadata:        map[string]string{"subscription_id": sub.ID, "flow": store.SubFlowRebill},
		PaymentMethodID: sub.YooMethodID.String,
		IdempotenceKey:  idempo,
	})
	if err != nil {
		// 4xx — ЮKassa отвергла сам запрос (например, привязка карты отозвана):
		// повтор через час получит тот же ответ. Попытка помечается, следующая —
		// через subRetryCooldown; письма нет — это не отказ банка.
		var apiErr *payment.APIError
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
			log.Printf("[sub] rebill sub=%s: rejected by API, next try after %s: %v", sub.ID, subRetryCooldown, err)
			if err := store.MarkChargeAttemptFailed(ctx, s.DB, sub.ID, now); err != nil {
				log.Printf("[sub] rebill sub=%s: mark failed err=%v", sub.ID, err)
			}
			return
		}
		// Сеть или 5xx: не считаем это отказом банка, попытка не помечается —
		// следующий тик попробует снова.
		log.Printf("[sub] rebill sub=%s: create err=%v", sub.ID, err)
		return
	}
	log.Printf("[sub] rebill sub=%s payment=%s status=%s", sub.ID, result.PaymentID, result.Status)
	switch result.Status {
	case "succeeded":
		first, err := store.RecordSubscriptionPayment(ctx, s.DB, sub.ID, result.PaymentID, rubPaymentAmount(subPrice(sub)), store.SubFlowRebill, "")
		if err != nil {
			log.Printf("[sub] rebill sub=%s payment=%s: record err=%v", sub.ID, result.PaymentID, err)
			return
		}
		if first {
			if fresh, err := store.GetSubscription(ctx, s.DB, sub.ID); err == nil {
				go s.sendSubscriptionChargedEmail(fresh)
				go s.sendSubscriptionConversions(fresh, result.PaymentID, rubPaymentAmount(subPrice(sub)), "")
			}
			s.marketingContact(sub.Email, map[string]string{marketing.FieldSubscriber: "1"})
		}
	case "canceled":
		// Отказ банка: past_due, ретрай через subRetryCooldown, письмо человеку.
		if err := store.MarkChargeAttemptFailed(ctx, s.DB, sub.ID, now); err != nil {
			log.Printf("[sub] rebill sub=%s: mark failed err=%v", sub.ID, err)
		}
		go s.sendSubscriptionChargeFailedEmail(sub)
	default:
		// pending: исход решит вебхук payment.succeeded (или canceled — тогда
		// подписку подхватит следующий тик как просроченную попытку).
		log.Printf("[sub] rebill sub=%s payment=%s: pending, waiting for webhook", sub.ID, result.PaymentID)
		s.rememberSubscriptionPayment(ctx, sub.ID, result.PaymentID)
	}
}

// rememberSubscriptionPayment записывает заведённый платёж как ожидающий:
// фоновая сверка (reconcile) подхватит его, если вебхук ЮKassa не дойдёт.
// Ошибка оформление не ломает — это страховка, а не путь оплаты.
func (s *Server) rememberSubscriptionPayment(ctx context.Context, subID, paymentID string) {
	if err := store.SetSubscriptionPendingPayment(ctx, s.DB, subID, paymentID); err != nil {
		log.Printf("[sub] sub=%s payment=%s: remember pending payment err=%v", subID, paymentID, err)
	}
}

// --- Письма ---

func (s *Server) subAccountURL() string {
	return strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/account"
}

func (s *Server) subCancelURL(sub store.Subscription) string {
	return strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/subscription/cancel?t=" + sub.CancelToken
}

// sendSubscriptionMail — письмо подписчику. errEmailDisabled означает, что
// письмо выключено на /admin/emails. Подстановки шаблона о подписке (кабинет,
// отмена, цена, даты) собираются здесь для любого письма, extra — сверх них.
func (s *Server) sendSubscriptionMail(kind string, sub store.Subscription, subject, body string, extra map[string]string) error {
	if s.Config.SMTPHost == "" {
		log.Printf("[sub] mail %q to=%s skipped: SMTP not configured", subject, sub.Email)
		return errors.New("smtp not configured")
	}
	vars := map[string]string{
		"account_url":      s.subAccountURL(),
		"cancel_url":       s.subCancelURL(sub),
		"price":            strconv.Itoa(subPrice(sub)),
		"next_charge_date": fmtRuDate(sub.NextChargeAt.String),
		"access_until":     fmtRuDate(sub.AccessUntil.String),
	}
	for name, v := range extra {
		vars[name] = v
	}
	if err := s.sendEmail(context.Background(), kind, emailTarget{SubscriptionID: sub.ID}, sub.Email, subject, body, vars); err != nil {
		log.Printf("[sub] mail %q to=%s err=%v", subject, sub.Email, err)
		return err
	}
	log.Printf("[sub] mail %q sent to=%s", subject, sub.Email)
	return nil
}

func fmtRuDate(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Format("02.01.2006")
}

func (s *Server) sendSubscriptionGiftEmail(sub store.Subscription) error {
	renewURL := strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/subscribe?sid=" + url.QueryEscape(sub.ID) + "&email=" + url.QueryEscape(sub.Email)
	return s.sendSubscriptionMail(emailSubGift, sub, "Дарим вам месяц безлимита песен",
		"Здравствуйте!\n\nСпасибо за заказ песни. Мы дарим вам месяц безлимита: до "+fmtRuDate(sub.AccessUntil.String)+
			" можно создавать сколько угодно песен без доплат. Карту мы не привязывали — списаний не будет.\n\n"+
			"Кабинет (вход по этому email): "+s.subAccountURL()+
			"\n\nПонравится — продлите безлимит за "+rubLabel(subPrice(sub))+" в месяц: "+renewURL+"\n",
		map[string]string{"renew_url": renewURL})
}

func (s *Server) sendSubscriptionCancelLinkEmail(sub store.Subscription) {
	s.sendSubscriptionMail(emailSubCancelLink, sub, "Отмена подписки",
		fmt.Sprintf(`Вы запросили отмену подписки на персональные песни.

Чтобы подтвердить отмену, откройте страницу и нажмите кнопку:
%s

Если вы не запрашивали отмену — просто проигнорируйте это письмо,
подписка продолжит действовать.`, s.subCancelURL(sub)), nil)
}

func (s *Server) sendSubscriptionWelcomeEmail(sub store.Subscription) {
	s.sendSubscriptionMail(emailSubWelcome, sub, "Подписка оформлена",
		fmt.Sprintf(`Спасибо! Подписка на персональные песни активна.

Личный кабинет (вход по этому email): %s
Следующее списание %d ₽ — %s. Мы напомним письмом за 3 дня.

Отменить подписку можно в любой момент:
%s
После отмены доступ сохранится до конца оплаченного периода.`,
			s.subAccountURL(), subPrice(sub), fmtRuDate(sub.NextChargeAt.String), s.subCancelURL(sub)),
		map[string]string{"trial": ""})
}

func (s *Server) sendSubscriptionChargedEmail(sub store.Subscription) {
	s.sendSubscriptionMail(emailSubCharged, sub, "Подписка продлена",
		fmt.Sprintf(`Подписка на персональные песни продлена, списано %d ₽.

Личный кабинет: %s
Следующее списание — %s.

Отменить подписку: %s`,
			subPrice(sub), s.subAccountURL(), fmtRuDate(sub.NextChargeAt.String), s.subCancelURL(sub)), nil)
}

func (s *Server) sendSubscriptionReminderEmail(sub store.Subscription) error {
	return s.sendSubscriptionMail(emailSubReminder, sub, "Скоро списание по подписке",
		fmt.Sprintf(`Напоминаем: %s спишется %d ₽ за подписку на персональные песни.

Если подписка больше не нужна, отмените её до списания:
%s

Личный кабинет: %s`,
			fmtRuDate(sub.NextChargeAt.String), subPrice(sub), s.subCancelURL(sub), s.subAccountURL()), nil)
}

func (s *Server) sendSubscriptionChargeFailedEmail(sub store.Subscription) {
	s.sendSubscriptionMail(emailSubChargeFailed, sub, "Не удалось продлить подписку",
		fmt.Sprintf(`Списание %d ₽ за подписку не прошло — банк отклонил платёж.

Мы повторим попытку через несколько дней. Проверьте карту: доступ к кабинету
действует до %s, после этого подписка закроется.

Отменить подписку: %s`,
			subPrice(sub), fmtRuDate(sub.AccessUntil.String), s.subCancelURL(sub)), nil)
}

func (s *Server) sendSubscriptionExpiredEmail(sub store.Subscription) {
	subscribeURL := strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/subscribe"
	s.sendSubscriptionMail(emailSubExpired, sub, "Подписка завершена",
		fmt.Sprintf(`Продлить подписку не получилось, и она завершена. Все созданные песни
остаются доступны по ссылкам из писем.

Оформить подписку заново: %s`, subscribeURL),
		map[string]string{"subscribe_url": subscribeURL})
}
