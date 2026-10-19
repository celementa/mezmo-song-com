package httpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/marketing"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/relay"
	"mezmo-song-com/internal/storage"
	"mezmo-song-com/internal/store"

	"database/sql"
)

type Server struct {
	DB      *sql.DB
	Config  config.Config
	Storage *storage.Client // треки на диске (AUDIO_DIR); nil только в тестах
}

// BuildSHA — коммит сборки (go build -ldflags "-X …BuildSHA=…"). /health
// отдаёт его заголовком X-Build: по нему CI ждёт, что поднялся новый бинарь.
var BuildSHA = "dev"

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Build", BuildSHA)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /api/public/settings", s.handlePublicSettings)
	mux.HandleFunc("POST /api/public/track", s.handleTrack)
	mux.HandleFunc("GET /api/public/flow", s.handlePublicFlow)
	mux.HandleFunc("POST /api/public/flow/step", s.handleFlowStep)
	mux.HandleFunc("POST /api/public/discount/validate", s.handleDiscountValidate)
	mux.HandleFunc("POST /api/order-drafts/upsert", s.handleOK)
	mux.HandleFunc("POST /api/orders/draft", s.handleOrdersDraft)
	mux.HandleFunc("GET /api/orders/{id}", s.handleOrderGet)
	mux.HandleFunc("POST /api/orders/{id}/confirm", s.handleOrderConfirm)
	mux.HandleFunc("POST /api/orders/{id}/apply-discount", s.handleApplyDiscount)
	mux.HandleFunc("POST /api/orders/{id}/initiate-payment", s.handleInitiatePayment)
	// Шаги варианта 1: бриф после сбора email, генерация до оплаты и апсейлы.
	mux.HandleFunc("PATCH /api/orders/{id}/brief", s.handleOrderBrief)
	mux.HandleFunc("POST /api/orders/{id}/generate-preview", s.handleGeneratePreview)
	mux.HandleFunc("POST /api/orders/{id}/speedup", s.handleBuySpeedup)
	mux.HandleFunc("POST /api/orders/{id}/upsell/stems", s.handleUpsellStems)
	mux.HandleFunc("GET /api/public/order/{id}/status", s.handlePublicOrderStatus)
	mux.HandleFunc("GET /api/public/order/{id}/resume", s.handlePublicOrderResume)
	mux.HandleFunc("GET /api/public/order/{id}/content", s.handlePublicOrderContent)
	mux.HandleFunc("GET /api/public/order/{id}/audio/{index}", s.handleAudioProxy)
	mux.HandleFunc("GET /api/public/order/{id}/preview", s.handlePreviewAudio)
	mux.HandleFunc("POST /api/public/order/{id}/mark-purchase-tracked", s.handleMarkPurchaseTracked)
	mux.HandleFunc("POST /api/public/order/{id}/engagement", s.handleOrderEngagement)
	mux.HandleFunc("POST /api/public/order/{id}/regenerate", s.handleRegenerate)
	mux.HandleFunc("GET /api/admin/orders", s.handleAdminOrders)
	mux.HandleFunc("GET /api/admin/orders.csv", s.handleAdminOrdersCSV)
	mux.HandleFunc("GET /api/admin/analytics", s.handleAdminAnalytics)
	mux.HandleFunc("POST /api/admin/orders/draft", s.handleAdminOrdersDraft)
	mux.HandleFunc("GET /api/admin/settings", s.handleAdminSettingsGet)
	mux.HandleFunc("PUT /api/admin/settings", s.handleAdminSettingsPut)
	mux.HandleFunc("GET /api/admin/emails", s.handleAdminEmailsGet)
	mux.HandleFunc("PUT /api/admin/emails", s.handleAdminEmailsPut)
	mux.HandleFunc("GET /api/admin/emails/log", s.handleAdminEmailLog)
	s.registerFlow(mux)
	mux.HandleFunc("POST /api/webhooks/yookassa", s.handleYooWebhook)
	mux.HandleFunc("GET /api/webhooks/unitpay", s.handleUnitPayWebhook)
	mux.HandleFunc("POST /api/webhooks/song-ready", s.handleSongReady)
	mux.HandleFunc("POST /api/webhooks/kie", s.handleKieWebhook)
	mux.HandleFunc("GET /api/webhooks/unisender-go", s.handleUnisenderGoWebhook)
	mux.HandleFunc("POST /api/webhooks/unisender-go", s.handleUnisenderGoWebhook)
	s.registerSubscription(mux)
	s.registerLesroses(mux)
}

func (s *Server) handleOK(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true})
}

// attributionSource приводит метки визита/заказа к одному источнику для отчётов:
// ref → utm_source → домен реферера → «прямой». Свой же домен в реферере считается
// прямым заходом, иначе внутренние переходы выглядели бы как внешний трафик.
func attributionSource(attr map[string]any, selfHost string) (source, campaign string) {
	campaign, _ = attr["utm_campaign"].(string)
	// Персональная метка партнёра (`?ref=nickname`) — отдельный источник
	// `ref:nickname`, чтобы у каждого инфлюенсера была своя строка воронки в
	// /admin/analytics. Приоритет выше utm_source: такую ссылку мы выдаём
	// руками конкретному человеку, это самое точное, что известно о трафике.
	if tag, _ := attr["ref"].(string); strings.TrimSpace(tag) != "" {
		tag = strings.TrimSpace(tag)
		// Значение приходит из URL и становится ключом группировки в отчёте —
		// длинный мусор обрезаем, чтобы не разносить таблицу.
		if r := []rune(tag); len(r) > 64 {
			tag = string(r[:64])
		}
		return "ref:" + tag, campaign
	}
	if src, _ := attr["utm_source"].(string); strings.TrimSpace(src) != "" {
		return strings.TrimSpace(src), campaign
	}
	if ref, _ := attr["referrer"].(string); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Host != "" {
			host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
			if host != strings.TrimPrefix(strings.ToLower(selfHost), "www.") {
				return host, campaign
			}
		}
	}
	return sourceDirect, campaign
}

const sourceDirect = "прямой"

// selfHost — домен самого сайта, чтобы отличать внутренние переходы от внешних.
func (s *Server) selfHost() string {
	if u, err := url.Parse(s.Config.PublicAppURL); err == nil {
		return u.Host
	}
	return ""
}

// handleTrack сохраняет визит на лендинг вместе с источником трафика — это
// знаменатель конверсии в /admin/analytics. Пинг фоновый, поэтому на любых
// проблемах отвечаем 200 и просто ничего не пишем.
func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path        string         `json:"path"`
		SessionID   string         `json:"sessionId"`
		ThemeSlug   *string        `json:"themeSlug"`
		Attribution map[string]any `json:"attribution"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.SessionID) == "" {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}

	source, campaign := attributionSource(body.Attribution, s.selfHost())
	now := time.Now().UTC()
	v := store.Visit{
		SessionID: strings.TrimSpace(body.SessionID),
		Day:       now.Format("2006-01-02"),
		Path:      firstNonEmpty(strings.TrimSpace(body.Path), "/"),
		Source:    source,
		CreatedAt: now,
	}
	if body.ThemeSlug != nil && *body.ThemeSlug != "" {
		v.ThemeSlug = sql.NullString{String: *body.ThemeSlug, Valid: true}
	}
	if campaign != "" {
		v.Campaign = sql.NullString{String: campaign, Valid: true}
	}
	if len(body.Attribution) > 0 {
		if attrBytes, err := json.Marshal(body.Attribution); err == nil {
			v.AttributionJSON = sql.NullString{String: string(attrBytes), Valid: true}
		}
	}
	// Плечо A/B-теста пишется в визит, потому что визит — знаменатель конверсии.
	// Считать её от заказов нельзя: плечо с более коротким путём до заказа
	// выигрывало бы просто за счёт того, что заказы в нём заводятся раньше.
	if arm, _ := s.armForRequest(r.Context(), r, v.SessionID); arm.ID != "" {
		v.FlowVariant = sql.NullString{String: arm.ID, Valid: true}
	}
	if err := store.RecordVisit(r.Context(), s.DB, v); err != nil {
		log.Printf("record visit: %v", err)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func firstNonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (s *Server) handlePublicSettings(w http.ResponseWriter, r *http.Request) {
	resp := publicSettingsResponse(s.Config)
	// trackTestOrders — единственная настройка из БД в публичном ответе: фронт
	// решает, отбивать ли браузерные события для тестовых (INTERNAL) заказов.
	resp["trackTestOrders"] = store.GetSetting(r.Context(), s.DB, settingTrackTestOrders, "0") == "1"
	// Главная по рекламной ссылке с плечом (/?arm=variant1-500) показывает цену
	// этого плеча, а не базовую: дальше по CTA параметр доедет до /config, и
	// воронка продаст по той же цене. Неизвестное или выключенное имя — базовая
	// цена, как и раньше (в воронке такая ссылка тоже уходит в обычную ротацию).
	if name := r.URL.Query().Get("arm"); name != "" {
		if named, ok := armByName(name); ok && named.Route == "" {
			if arm, ok := s.enabledArm(r.Context(), named.ID); ok {
				resp["paymentAmount"] = s.armPrice(r.Context(), arm)
			}
		}
	}
	writeJSON(w, 200, resp)
}

// settingTrackTestOrders — «1»: браузерная аналитика отбивает и тестовые
// (INTERNAL) заказы; «0» (дефолт): тестовые заказы в аналитику не попадают.
const settingTrackTestOrders = "analytics.track_test_orders"

// maxRegenerationSetting — верхняя граница для полей лимита доработок в админке.
// Каждая доработка — это платный прогон OpenRouter и Kie, поэтому трёхзначные
// значения почти наверняка опечатка; безлимит задаётся нулём, а не большим числом.
const maxRegenerationSetting = 99

func (s *Server) adminSettings(ctx context.Context) map[string]any {
	client, internal := store.RegenerationLimits(ctx, s.DB)
	dailyBudget := store.PreviewDailyBudget(ctx, s.DB)
	previewEnabled := store.PreviewEnabled(ctx, s.DB)
	saveCard := store.SaveCardInFunnel(ctx, s.DB)
	freePerVisitor, freePerIP := store.FreeGenerationLimits(ctx, s.DB)
	speedup, stems := store.UpsellPrices(ctx, s.DB)

	// Плечи отдаются все известные с ценой и признаком «в ротации»: страница
	// настроек — единственное место, где сплит включается и выключается.
	arms := make([]map[string]any, 0, len(KnownFlowArms))
	enabled := map[string]bool{}
	for _, arm := range s.enabledArms(ctx) {
		enabled[arm.ID] = true
	}
	weights := store.FlowArmWeights(ctx, s.DB)
	for _, id := range KnownFlowArms {
		arm := flowArms[id]
		arms = append(arms, map[string]any{
			"id":      id,
			"label":   arm.Label,
			"link":    s.armLink(id),
			"price":   store.FlowArmPrice(ctx, s.DB, id, s.armFallbackPrice(arm)),
			"weight":  armWeight(id, weights),
			"enabled": enabled[id],
		})
	}

	return map[string]any{
		"trackTestOrders":       store.GetSetting(ctx, s.DB, settingTrackTestOrders, "0") == "1",
		"regenerationsClient":   client,
		"regenerationsInternal": internal,
		"flowArms":              arms,
		"previewEnabled":        previewEnabled,
		"saveCardInFunnel":      saveCard,
		"previewDailyBudget":    dailyBudget,
		"freeGenPerVisitor":     freePerVisitor,
		"freeGenPerIP":          freePerIP,
		"deliveryDelayMinutes":  int(store.DeliveryDelay(ctx, s.DB).Minutes()),
		"speedupPrice":          speedup,
		"stemsPrice":            stems,
	}
}

// Границы полей флоу на /admin/settings. Смысл тот же, что у
// maxRegenerationSetting: отсечь опечатку, которая тратит деньги или ломает
// обещание клиенту, а не выразить «настоящий» предел.
const (
	maxFlowPrice            = 100000 // ₽ за песню
	maxPreviewPerVisitor    = 50     // бесплатных генераций на посетителя в сутки
	maxPreviewDailyBudget   = 100000 // генераций до оплаты в сутки на весь сайт
	maxDeliveryDelayMinutes = 7 * 24 * 60
	maxUpsellPrice          = 100000
	maxArmWeight            = 1000
)

// flowArmPatch — правка одного плеча со страницы настроек.
type flowArmPatch struct {
	ID      string `json:"id"`
	Price   *int   `json:"price"`
	Weight  *int   `json:"weight"`
	Enabled *bool  `json:"enabled"`
}

// applyFlowArms сохраняет цены, веса и состав ротации.
//
// Патч сливается с текущим состоянием, а не заменяет его: страница настроек
// присылает одно плечо, когда меняют его цену, и собирать состав ротации только
// из присланного означало бы выключить остальные плечи заодно с правкой цены.
//
// Пустой состав включённых плеч не сохраняется: сайт без плеча не работает, а
// «снять все галочки» на странице настроек — почти наверняка промах мимо
// чекбокса, а не осознанное решение.
func (s *Server) applyFlowArms(ctx context.Context, patches []flowArmPatch) error {
	weights := store.FlowArmWeights(ctx, s.DB)
	enabledNow := map[string]bool{}
	for _, arm := range s.enabledArms(ctx) {
		enabledNow[arm.ID] = true
	}

	for _, patch := range patches {
		if _, known := flowArms[patch.ID]; !known {
			return errOutOfRange
		}
		if patch.Price != nil {
			if *patch.Price <= 0 || *patch.Price > maxFlowPrice {
				return errOutOfRange
			}
			if err := store.SetSetting(ctx, s.DB, store.SettingFlowArmPriceKey(patch.ID), strconv.Itoa(*patch.Price)); err != nil {
				return err
			}
		}
		if patch.Weight != nil {
			if *patch.Weight <= 0 || *patch.Weight > maxArmWeight {
				return errOutOfRange
			}
			weights[patch.ID] = *patch.Weight
		}
		if patch.Enabled != nil {
			enabledNow[patch.ID] = *patch.Enabled
		}
	}

	// Порядок в обеих настройках — из KnownFlowArms, а не из порядка ключей
	// карты: строка настройки не должна меняться от запуска к запуску.
	enabled := make([]string, 0, len(KnownFlowArms))
	pairs := make([]string, 0, len(KnownFlowArms))
	for _, id := range KnownFlowArms {
		pairs = append(pairs, id+":"+strconv.Itoa(armWeight(id, weights)))
		if enabledNow[id] {
			enabled = append(enabled, id)
		}
	}
	if len(enabled) == 0 {
		return errOutOfRange
	}
	if err := store.SetSetting(ctx, s.DB, store.SettingFlowArmWeights, strings.Join(pairs, ",")); err != nil {
		return err
	}
	return store.SetSetting(ctx, s.DB, store.SettingFlowArmsEnabled, strings.Join(enabled, ","))
}

// errOutOfRange — значение поля настроек вне допустимых границ.
var errOutOfRange = errors.New("out of range")

func (s *Server) handleAdminSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.adminSettings(r.Context()))
}

// handleAdminSettingsPut принимает частичный патч: присылается только то, что
// поменяли на странице, остальные настройки остаются как есть.
func (s *Server) handleAdminSettingsPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackTestOrders       *bool          `json:"trackTestOrders"`
		RegenerationsClient   *int           `json:"regenerationsClient"`
		RegenerationsInternal *int           `json:"regenerationsInternal"`
		FlowArms              []flowArmPatch `json:"flowArms"`
		PreviewEnabled        *bool          `json:"previewEnabled"`
		SaveCardInFunnel      *bool          `json:"saveCardInFunnel"`
		FreeGenPerVisitor     *int           `json:"freeGenPerVisitor"`
		FreeGenPerIP          *int           `json:"freeGenPerIP"`
		PreviewDailyBudget    *int           `json:"previewDailyBudget"`
		DeliveryDelayMinutes  *int           `json:"deliveryDelayMinutes"`
		SpeedupPrice          *int           `json:"speedupPrice"`
		StemsPrice            *int           `json:"stemsPrice"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_request"})
		return
	}
	empty := body.TrackTestOrders == nil && body.RegenerationsClient == nil && body.RegenerationsInternal == nil &&
		len(body.FlowArms) == 0 && body.PreviewDailyBudget == nil &&
		body.FreeGenPerVisitor == nil && body.FreeGenPerIP == nil && body.PreviewEnabled == nil &&
		body.SaveCardInFunnel == nil &&
		body.DeliveryDelayMinutes == nil && body.SpeedupPrice == nil && body.StemsPrice == nil
	if empty {
		writeJSON(w, 400, map[string]string{"error": "bad_request"})
		return
	}

	ctx := r.Context()
	if body.TrackTestOrders != nil {
		value := "0"
		if *body.TrackTestOrders {
			value = "1"
		}
		if err := store.SetSetting(ctx, s.DB, settingTrackTestOrders, value); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}
	if body.PreviewEnabled != nil {
		value := "0"
		if *body.PreviewEnabled {
			value = "1"
		}
		if err := store.SetSetting(ctx, s.DB, store.SettingPreviewEnabled, value); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}
	if body.SaveCardInFunnel != nil {
		value := "0"
		if *body.SaveCardInFunnel {
			value = "1"
		}
		if err := store.SetSetting(ctx, s.DB, store.SettingSaveCardInFunnel, value); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}
	limits := map[string]*int{
		store.SettingRegenerationsClient:   body.RegenerationsClient,
		store.SettingRegenerationsInternal: body.RegenerationsInternal,
	}
	for key, v := range limits {
		if v == nil {
			continue
		}
		if *v < 0 || *v > maxRegenerationSetting {
			writeJSON(w, 400, map[string]string{"error": "out_of_range"})
			return
		}
		if err := store.SetSetting(ctx, s.DB, key, strconv.Itoa(*v)); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}

	// Краны флоу. 0 у любого из них — «выключено / без ограничений», поэтому
	// нижняя граница нулевая, а не единица.
	flowLimits := []struct {
		key   string
		value *int
		max   int
	}{
		{store.SettingFreeGenerationsPerDay, body.FreeGenPerVisitor, maxPreviewPerVisitor},
		{store.SettingFreeGenerationsPerIPPerDay, body.FreeGenPerIP, maxPreviewDailyBudget},
		{store.SettingPreviewDailyBudget, body.PreviewDailyBudget, maxPreviewDailyBudget},
		{store.SettingDeliveryDelayMinutes, body.DeliveryDelayMinutes, maxDeliveryDelayMinutes},
		{store.SettingUpsellSpeedupPrice, body.SpeedupPrice, maxUpsellPrice},
		{store.SettingUpsellStemsPrice, body.StemsPrice, maxUpsellPrice},
	}
	for _, item := range flowLimits {
		if item.value == nil {
			continue
		}
		if *item.value < 0 || *item.value > item.max {
			writeJSON(w, 400, map[string]string{"error": "out_of_range"})
			return
		}
		if err := store.SetSetting(ctx, s.DB, item.key, strconv.Itoa(*item.value)); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}
	if len(body.FlowArms) > 0 {
		if err := s.applyFlowArms(ctx, body.FlowArms); err != nil {
			if errors.Is(err, errOutOfRange) {
				writeJSON(w, 400, map[string]string{"error": "out_of_range"})
				return
			}
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}
	writeJSON(w, 200, s.adminSettings(ctx))
}

func (s *Server) handleDiscountValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	_ = readJSON(r, &body)
	code := strings.TrimSpace(strings.ToUpper(body.Code))
	if code == "DEMO10" {
		writeJSON(w, 200, map[string]any{"valid": true, "discountType": "percentage", "percentageValue": 10})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": false, "error": "Промокод недействителен."})
}

// Единственный публичный промокод. Второе значение — известен ли код: неизвестные
// в БД не пишутся, иначе через ручку можно выставить себе служебный INTERNAL.
func publicDiscountAmount(code string, basePrice int) (int, bool) {
	if code == "DEMO10" {
		return int(float64(basePrice) * 0.1), true
	}
	return 0, false
}

// buildOrderFromBody constructs a base store.Order from the order-draft request
// body. The order is created as "paid" by default; callers apply their own
// payment-status rules (e.g. handleOrdersDraft flips to "pending" when YooKassa
// is configured).
func buildOrderFromBody(cfg config.Config, body map[string]any) store.Order {
	id := newOrderID()
	payloadBytes, _ := json.Marshal(body)

	customer := map[string]any{
		"whatsappNumber": body["whatsappNumber"],
		"email":          body["email"],
	}
	customerBytes, _ := json.Marshal(customer)

	o := store.Order{
		ID:               id,
		Status:           "created",
		DeliveryStatus:   "delivery_pending",
		PaymentStatus:    "paid",
		InputPayloadJSON: string(payloadBytes),
		CustomerJSON:     string(customerBytes),
		BasePrice:        priceFromConfig(cfg),
		CreatedAt:        time.Now(),
	}
	if theme, _ := body["themeSlug"].(string); theme != "" {
		o.ThemeSlug = sql.NullString{String: theme, Valid: true}
	}
	if gaID, _ := body["gaClientId"].(string); gaID != "" {
		o.GAClientID = sql.NullString{String: gaID, Valid: true}
	}
	if ymID, _ := body["ymClientId"].(string); ymID != "" {
		o.YMClientID = sql.NullString{String: ymID, Valid: true}
	}
	// Рекламные метки посадочной страницы (utm_*, yclid, rb_clickid) — источник
	// заказа в /admin/orders и основа для офлайн-конверсий в Директ/VK.
	if attr, _ := body["attribution"].(map[string]any); len(attr) > 0 {
		if attrBytes, err := json.Marshal(attr); err == nil {
			o.AttributionJSON = sql.NullString{String: string(attrBytes), Valid: true}
		}
	}
	return o
}

func (s *Server) handleOrdersDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}

	o := buildOrderFromBody(s.Config, body)

	// Плечо A/B-теста и цена берутся от сессии на сервере: sessionId — то же
	// самое значение, что уходит в /api/public/track, а цена из тела запроса не
	// читается вообще, иначе покупатель назначал бы её сам.
	sessionID, _ := body["sessionId"].(string)
	arm, price := s.armForRequest(ctx, r, strings.TrimSpace(sessionID))
	o.FlowVariant = sql.NullString{String: arm.ID, Valid: true}
	o.BasePrice = price

	if s.paymentsConfigured() {
		o.PaymentStatus = "pending"
	}

	if err := store.CreateOrder(ctx, s.DB, o); err != nil {
		log.Printf("create order: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if err := store.SetOrderFlow(ctx, s.DB, o.ID, arm.ID, price); err != nil {
		log.Printf("[flow] order=%s set flow err=%v", o.ID, err)
	}
	// Заказ в варианте 1 создаётся сразу после шага 3 (email), поэтому здесь же
	// уходит письмо-триггер: до генерации, как в документе.
	go s.sendWelcomeEmail(context.Background(), o.ID)
	s.marketingOrder(o.ID, nil)
	s.marketingEvent(o.ID, EventOrderCreated)

	writeJSON(w, 200, map[string]any{"orderId": publicOrderRef(s.Config.WebhookSecret, o.ID), "xenditInvoiceUrl": nil, "paymentStatus": o.PaymentStatus, "flowVariant": arm.ID, "basePrice": price})
}

// handleAdminOrdersDraft creates a fully-paid order for internal staff and kicks
// off song generation immediately, bypassing the payment step. It lives under
// /api/admin/* so the adminAuth Basic-Auth middleware gates it. Orders are tagged
// BasePrice=0 + DiscountCode="INTERNAL" so they're filterable in the admin list.
func (s *Server) handleAdminOrdersDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}

	o := buildOrderFromBody(s.Config, body)
	o.PaymentStatus = "paid"
	o.BasePrice = 0
	o.DiscountCode = sql.NullString{String: "INTERNAL", Valid: true}

	if err := store.CreateOrder(ctx, s.DB, o); err != nil {
		log.Printf("create admin order: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}

	// Drive generation directly: the /order/:id page only polls status and never
	// calls /confirm, so the admin endpoint must confirm + trigger the relay itself.
	transitioned, err := store.ConfirmOrder(ctx, s.DB, o.ID)
	if err != nil {
		log.Printf("confirm admin order %s: %v", o.ID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if transitioned {
		go s.triggerRelay(context.Background(), o.ID)
	}

	writeJSON(w, 200, map[string]any{"orderId": publicOrderRef(s.Config.WebhookSecret, o.ID)})
}

func (s *Server) handleOrderGet(w http.ResponseWriter, r *http.Request) {
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
	_ = store.MaybeDemoComplete(ctx, s.DB, id)
	o, _ = store.GetOrder(ctx, s.DB, id)

	writeJSON(w, 200, orderSummary(o, publicOrderRef(s.Config.WebhookSecret, o.ID)))
}

func (s *Server) handleOrderConfirm(w http.ResponseWriter, r *http.Request) {
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
	if o.PaymentStatus != "paid" {
		log.Printf("[pay] confirm order=%s rejected: payment_status=%s", id, o.PaymentStatus)
		writeJSON(w, 402, map[string]string{"error": "not_paid"})
		return
	}
	// Заказы воронки /create и партнёрские (Les Roses) оплачиваются ДО анкеты:
	// генерацию запускает /api/orders/{id}/config, а не этот вызов — иначе
	// песня пишется по пустой анкете.
	if o.Flow.Valid && !hasSongConfig(o) {
		log.Printf("[pay] confirm order=%s rejected: song config is empty", id)
		writeJSON(w, 409, map[string]string{"error": "needs_config"})
		return
	}
	s.applyDeliveryHold(ctx, id)
	transitioned, err := store.ConfirmOrder(ctx, s.DB, id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if transitioned {
		log.Printf("[pay] confirm order=%s via client: generation triggered", id)
		go s.triggerRelay(context.Background(), id)
	} else {
		log.Printf("[pay] confirm order=%s via client: already processing, skip", id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) triggerRelay(ctx context.Context, orderID string) {
	log.Printf("relay start for order %s model=%s kie=%v", orderID, s.Config.OpenRouterModel, s.Config.KieAPIKey != "")
	o, err := store.GetOrder(ctx, s.DB, orderID)
	if err != nil {
		return
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(o.InputPayloadJSON), &payload)
	getStr := func(k string) string {
		if v, ok := payload[k].(string); ok {
			return v
		}
		return ""
	}
	mp, _ := payload["musicPreferences"].(map[string]any)
	style := ""
	voiceStyle := ""
	if mp != nil {
		if g, ok := mp["genre"].(string); ok {
			style = g
		}
		if v, ok := mp["voiceStyle"].(string); ok {
			voiceStyle = v
		}
	}
	vocalGender := vocalGenderFor(payload, voiceStyle)
	kieCallback := strings.TrimSuffix(s.Config.PublicAppURL, "/") +
		"/api/webhooks/kie?orderId=" + orderID + "&secret=" + s.Config.WebhookSecret
	rel := relay.Client{
		OpenRouterAPIKey: s.Config.OpenRouterAPIKey,
		OpenRouterModel:  s.Config.OpenRouterModel,
		OpenRouterProxy:  s.Config.OpenRouterProxy,
		KieAPIKey:        s.Config.KieAPIKey,
		CallbackURL:      kieCallback,
	}
	lyrics, err := rel.Trigger(ctx, relay.OrderPayload{
		OrderID:         o.ID,
		Sender:          getStr("yourName"),
		Recipient:       getStr("recipientName"),
		Occasion:        getStr("occasion"),
		Style:           style,
		Emotion:         getStr("emotion"),
		VocalGender:     vocalGender,
		Story:           getStr("story"),
		Relationship:    getStr("relationship"),
		RecipientGender: recipientGender(payload),
	})
	if err != nil {
		log.Printf("relay error for order %s: %v", orderID, err)
		_ = store.CompleteWithDemoAudio(ctx, s.DB, orderID, lyrics)
		return
	}
	log.Printf("relay success for order %s lyricsLen=%d", orderID, len(lyrics))
	if s.Config.KieAPIKey != "" {
		// Kie.ai будет колбэчить когда аудио готово
		if lyrics != "" {
			_ = store.SaveLyrics(ctx, s.DB, orderID, lyrics)
		}
	} else {
		// Без Kie.ai — завершаем с демо-аудио и реальным текстом
		_ = store.CompleteWithDemoAudio(ctx, s.DB, orderID, lyrics)
	}
}

func (s *Server) handleApplyDiscount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	var body struct {
		DiscountCode string `json:"discountCode"`
	}
	_ = readJSON(r, &body)
	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if o.XenditInvoiceURL.Valid && o.XenditInvoiceURL.String != "" {
		writeJSON(w, 409, map[string]string{"error": "payment_already_initiated"})
		return
	}
	code := strings.TrimSpace(strings.ToUpper(body.DiscountCode))
	// В БД попадают только известные промокоды. Иначе публичная ручка позволяла
	// записать любой код, включая INTERNAL: он снимает лимит доработок и убирает
	// заказ из воронки в /admin/analytics.
	amount, known := publicDiscountAmount(code, o.BasePrice)
	if !known {
		writeJSON(w, 400, map[string]string{"error": "invalid_code"})
		return
	}
	_ = store.ApplyDiscount(ctx, s.DB, id, code, amount)
	writeJSON(w, 200, map[string]any{"ok": true, "paymentAmount": o.BasePrice - amount, "xenditInvoiceUrl": nil})
}

const (
	// confirmationTTL — сколько считаем выданное подтверждение живым. Токен виджета
	// живёт 1 час (дока ЮKassa); берём запас 10 минут, чтобы покупатель не получил
	// токен, который умрёт, пока он вводит карту.
	confirmationTTL = 50 * time.Minute
	// maxPaymentHistory — сколько последних платежей заказа перечитываем из API.
	// Практически их 1-2 (в этом и смысл задачи); ограничение защищает ручку от
	// превращения в десяток сетевых вызовов на исторических заказах.
	maxPaymentHistory = 5
)

// paymentHistoryIDs возвращает не больше limit последних id платежей, заведённых
// по заказу (yoo_payment_ids; при пустой истории — фолбэк на legacy-колонку
// yoo_payment_id, тот же приём, что в store.KnownPaymentID).
func paymentHistoryIDs(o store.Order, limit int) []string {
	var ids []string
	if o.YooPaymentIDs.Valid && o.YooPaymentIDs.String != "" {
		_ = json.Unmarshal([]byte(o.YooPaymentIDs.String), &ids)
	}
	if len(ids) == 0 && o.YooPaymentID.Valid && o.YooPaymentID.String != "" {
		ids = []string{o.YooPaymentID.String}
	}
	if len(ids) > limit {
		ids = ids[len(ids)-limit:]
	}
	return ids
}

// expectedPaymentRubles — сколько покупатель должен заплатить за заказ: цена
// плеча минус скидка плюс допники, выбранные ДО оплаты (они входят в основной
// платёж, см. create_flow.go).
//
// Формула одна на все точки, где сумма встречается: создание платежа, сверка
// вебхука и переиспользование выданного подтверждения. Раньше их было три
// копии, и именно расхождение между «сумма, на которую создан платёж» и
// «сумма, которую ждёт вебхук» приводило к отклонению настоящей оплаты как
// amount_too_small.
func (s *Server) expectedPaymentRubles(ctx context.Context, o store.Order) int {
	amount := o.BasePrice
	if o.DiscountAmount.Valid {
		amount -= int(o.DiscountAmount.Int64)
	}
	if amount < 0 {
		amount = 0
	}
	return amount + s.extrasSelectedSum(ctx, o)
}

// livePaymentGate решает судьбу новой попытки оплаты ДО создания платежа:
//
//	alreadyPaid — в истории есть succeeded+paid платёж: деньги уже взяты, вебхук
//	              просто не доехал. Ручка отвечает 409 и второго платежа не заводит.
//	              Помечать заказ оплаченным здесь НЕЛЬЗЯ — это работа вебхука с
//	              полной сверкой (verifyYooPayment).
//	reuse       — подтверждение, которое можно отдать повторно: тот же режим, младше
//	              confirmationTTL, платёж всё ещё pending.
//
// Любая ошибка чтения API трактуется как «переиспользовать нечего» — покупатель
// обязан иметь возможность заплатить.
func (s *Server) livePaymentGate(ctx context.Context, o store.Order, mode string) (alreadyPaid bool, reuse *store.IssuedConfirmation) {
	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
	expected := s.expectedPaymentRubles(ctx, o)

	statuses := map[string]payment.Payment{}
	for _, id := range paymentHistoryIDs(o, maxPaymentHistory) {
		p, err := yk.GetPayment(id)
		if err != nil {
			log.Printf("[pay] gate order=%s payment=%s get err=%v", o.ID, id, err)
			continue
		}
		statuses[id] = p
		if p.Status == "succeeded" && p.Paid {
			return true, nil
		}
	}

	c, ok := store.LoadConfirmation(o)
	if !ok || c.Mode != mode {
		return false, nil
	}
	if time.Since(c.IssuedAt) >= confirmationTTL {
		log.Printf("[pay] gate order=%s payment=%s confirmation expired (age=%s)", o.ID, c.PaymentID, time.Since(c.IssuedAt).Round(time.Second))
		return false, nil
	}
	if c.Mode == payment.ModeEmbedded && c.Token == "" {
		return false, nil
	}
	if c.Mode == payment.ModeRedirect && c.URL == "" {
		return false, nil
	}
	p, ok := statuses[c.PaymentID]
	if !ok {
		return false, nil
	}
	if p.Status != "pending" {
		log.Printf("[pay] gate order=%s payment=%s not pending (status=%s)", o.ID, c.PaymentID, p.Status)
		return false, nil
	}
	// Сумма могла измениться после выдачи подтверждения: покупатель вернулся со
	// сводки назад и добавил допник (ускорение или стемы до оплаты). Отдать
	// прежнее подтверждение значит списать старую сумму, а вебхук ждёт новую и
	// отклонит платёж как amount_too_small — деньги списаны, заказ не оплачен.
	// Поэтому при расхождении подтверждение не переиспользуется: заводим новый
	// платёж на актуальную сумму.
	if got, ok := rublesToKopecks(p.Amount); !ok || got != int64(expected)*100 {
		log.Printf("[pay] gate order=%s payment=%s amount mismatch (payment=%s expected=%d ₽) — подтверждение не переиспользуем",
			o.ID, c.PaymentID, p.Amount, expected)
		return false, nil
	}
	return false, &c
}

func (s *Server) handleInitiatePayment(w http.ResponseWriter, r *http.Request) {
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
	if o.PaymentStatus != "pending" {
		writeJSON(w, 409, map[string]string{"error": "not_pending"})
		return
	}

	finalPrice := s.expectedPaymentRubles(ctx, o)
	if finalPrice <= 0 {
		if err := store.MarkPaid(ctx, s.DB, id); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		writeJSON(w, 200, map[string]any{"paid": true})
		return
	}

	var body struct {
		Mode string `json:"mode"`
	}
	_ = readJSON(r, &body) // пустое тело допустимо: mode остаётся "" → embedded
	mode := payment.ModeEmbedded
	if body.Mode == payment.ModeRedirect {
		mode = payment.ModeRedirect
	}

	// Куда вернуть покупателя после оплаты. Redirect-платежу адрес уходит в
	// ЮKassa, embedded-платежу — фронту вместе с токеном: return_url виджету
	// задаёт он, а знать правила возврата в двух местах незачем.
	//
	// Заказы воронок /create возвращаются В ВОРОНКУ, а не на /thanks. Иначе
	// покупатель v2 («оплата в начале») платил на втором шаге из тринадцати и
	// уходил на страницу «спасибо», где про песню его никто уже не спрашивал:
	// payload оставался пустым, вебхук генерацию не запускал, а открытие
	// /order/{ref} подтверждало заказ и генерировало песню по ПУСТОМУ брифу.
	// У v1 тем же способом отрезался весь хвост после оплаты (подписка, стемы,
	// «готово»). Воронка сама умеет продолжить с нужного шага по ?order=.
	ref := publicOrderRef(s.Config.WebhookSecret, id)
	returnURL := strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/thanks?orderId=" + ref
	if o.Flow.Valid && o.Flow.String != "" {
		returnURL = s.flowResumeURL(ref) + "&paid=1"
	} else if o.FlowVariant.Valid && o.FlowVariant.String != "" {
		// Вариант 1 (/config): шаги после оплаты — апсейл подписки, стемы,
		// готово — живут на странице заказа. Промежуточный /thanks с кнопкой
		// «Открыть страницу заказа» был лишним кликом между оплатой и ними.
		returnURL = s.publicOrderURL(id)
	}
	if s.unitpayConfigured() {
		s.initiateUnitPay(w, r, o, returnURL)
		return
	}
	// Токен виджета одноразовый и живёт час, а платёж с capture: true оплачивается
	// сразу. Значит, пока прежний платёж жив, второй — это второе списание за одну
	// песню. Отменить прежний нельзя (ЮKassa не отменяет pending, проверено живым
	// запросом в T-018), поэтому мы просто не заводим новый: отдаём то же подтверждение.
	if s.Config.YooKassaShopID != "" && s.Config.YooKassaSecretKey != "" {
		alreadyPaid, reuse := s.livePaymentGate(ctx, o, mode)
		if alreadyPaid {
			log.Printf("[pay] initiate order=%s REFUSED: заказ уже оплачен ранее заведённым платежом, ждём вебхук", id)
			writeJSON(w, 409, map[string]string{"error": "already_paid"})
			return
		}
		if reuse != nil {
			// Ответ обязан быть неотличим от свежего платежа: фронт (T-012) про
			// переиспользование ничего не знает и знать не должен.
			log.Printf("[pay] initiate order=%s payment=%s reused (%s, age=%s)", id, reuse.PaymentID, reuse.Mode, time.Since(reuse.IssuedAt).Round(time.Second))
			if reuse.Mode == payment.ModeRedirect {
				writeJSON(w, 200, map[string]any{"confirmationUrl": reuse.URL})
			} else {
				writeJSON(w, 200, map[string]any{"confirmationToken": reuse.Token, "paymentId": reuse.PaymentID, "returnUrl": returnURL})
			}
			return
		}
	}

	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}

	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	custEmail, _ := cust["email"].(string)
	custPhone, _ := cust["whatsappNumber"].(string)
	rcpt := payment.Receipt{
		Email:       strings.TrimSpace(custEmail),
		Phone:       strings.TrimSpace(custPhone),
		Description: "Персональная песня",
		VatCode:     s.Config.YooKassaVatCode,
	}

	// Дошли сюда — либо гейт выше решил, что переиспользовать нечего (истории нет,
	// подтверждение протухло/не того режима, или сам платёж уже не pending), либо
	// ключей ЮKassa нет вовсе (mock-режим). В любом случае заводим новый платёж.
	//
	// Сохранение карты у платежа за песню — под краном `payments.save_card_in_funnel`
	// и по умолчанию ВЫКЛЮЧЕНО. Сохранять способ оплаты умеет только карта, поэтому
	// просьба о сохранении убирает с формы ЮKassa СБП, SberPay и Mir Pay — а это
	// большая доля платежей. Включённый кран возвращает апсейл «безлимит за 0 ₽
	// сегодня» в один клик (flow_subscribe.go), ценой того, что песню можно
	// оплатить только картой. Если ЮKassa сохранение не приняла (автоплатежи не
	// включены, метод не поддерживает), платёж заводится заново без него —
	// потерять апсейл лучше, чем продажу.
	saveCard := (o.FlowVariant.Valid || o.Flow.Valid) && store.SaveCardInFunnel(ctx, s.DB)
	req := payment.CreateRequest{
		OrderID:           id,
		ValueRub:          rubPaymentAmount(finalPrice),
		Mode:              mode,
		ReturnURL:         returnURL,
		Receipt:           rcpt,
		SavePaymentMethod: saveCard,
	}
	result, err := yk.Create(req)
	if err != nil && req.SavePaymentMethod {
		log.Printf("[pay] initiate order=%s create with save_payment_method err=%v — retry without", id, err)
		req.SavePaymentMethod = false
		result, err = yk.Create(req)
	}
	if err != nil {
		log.Printf("[pay] initiate order=%s create payment err=%v", id, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	if err := store.AddPaymentID(ctx, s.DB, id, result.PaymentID, result.ConfirmationURL); err != nil {
		log.Printf("add payment id: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if err := store.SetPaymentProvider(ctx, s.DB, id, ProviderYooKassa); err != nil {
		log.Printf("[pay] initiate order=%s set provider err=%v", id, err)
	}
	// Ошибка сохранения не должна ломать оплату: платёж уже создан у ЮKassa,
	// покупателю всё равно нужно отдать подтверждение. Без сохранённого токена
	// следующий вызов просто заведёт новый платёж — это не хуже поведения T-011.
	if err := store.SaveConfirmation(ctx, s.DB, id, store.IssuedConfirmation{
		PaymentID: result.PaymentID,
		Mode:      mode,
		Token:     result.ConfirmationToken,
		URL:       result.ConfirmationURL,
		IssuedAt:  time.Now().UTC(),
	}); err != nil {
		log.Printf("[pay] initiate order=%s save confirmation err=%v", id, err)
	}
	if mode == payment.ModeRedirect {
		log.Printf("[pay] initiate order=%s payment=%s amount=%d created (redirect)", id, result.PaymentID, finalPrice)
		writeJSON(w, 200, map[string]any{"confirmationUrl": result.ConfirmationURL})
		return
	}
	log.Printf("[pay] initiate order=%s payment=%s amount=%d created (embedded)", id, result.PaymentID, finalPrice)
	writeJSON(w, 200, map[string]any{"confirmationToken": result.ConfirmationToken, "paymentId": result.PaymentID, "returnUrl": returnURL})
}

func (s *Server) handlePublicOrderStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	_ = store.MaybeDemoComplete(ctx, s.DB, id)
	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}

	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	hasEmail := false
	if v, ok := cust["email"].(string); ok && strings.TrimSpace(v) != "" {
		hasEmail = true
	}

	invURL := ""
	if o.XenditInvoiceURL.Valid {
		invURL = o.XenditInvoiceURL.String
	}

	// paymentAmount идёт в purchase-трекинг (GTM/пиксели) — отдаём фактически
	// оплаченную сумму: та же формула, что у создания платежа и сверки вебхука.
	paymentAmount := s.expectedPaymentRubles(ctx, o)

	// Допники нового флоу: каталог с ценами + состояние по этому заказу.
	extraPrices := s.extraPrices(ctx, o)
	paidExtraIDs := []string{}
	for _, e := range store.ExtrasPaid(o) {
		paidExtraIDs = append(paidExtraIDs, e.ID)
	}

	writeJSON(w, 200, map[string]any{
		"id":                  publicOrderRef(s.Config.WebhookSecret, o.ID),
		"trackingId":          o.ID,
		"status":              o.Status,
		"paymentStatus":       o.PaymentStatus,
		"xenditInvoiceUrl":    nullStr(invURL),
		"recipientName":       jsonRecipient(o.InputPayloadJSON),
		"createdAt":           o.CreatedAt.UTC().Format(time.RFC3339Nano),
		"config":              deliveryCfg(),
		"confirmedAt":         o.CreatedAt.UTC().Format(time.RFC3339Nano),
		"deliveryScheduledAt": nullStr(o.DeliverAfter.String),
		"deliveryDelayHours":  int(store.DeliveryDelay(ctx, s.DB).Hours()),
		"deliveryHeld":        store.DeliveryHeld(o, time.Now()),
		"speedupPurchased":    o.SpeedupPurchased,
		"upsellKind":          nullStr(o.UpsellKind.String),
		"previewReady":        o.PreviewAudioURL.Valid && o.PreviewAudioURL.String != "",
		"flowVariant":         nullStr(o.FlowVariant.String),
		"resumeUrl":           nullStr(s.resumeURL(o)),
		// Песня по подписке или подписка уже оформлена с этого заказа — апсейл
		// подписки после оплаты не показывается.
		"subscribed":               o.SubscriptionID.Valid && o.SubscriptionID.String != "",
		"savedCard":                o.YooMethodID.Valid && o.YooMethodID.String != "",
		"revisionWaitHours":        24,
		"orderProcessingPage":      map[string]string{"headline": "Готовим вашу песню", "subtitle": "Это может занять несколько минут."},
		"processingUpsellItems":    []any{},
		"revisionUpsell":           nil,
		"purchasedUpsells":         []any{},
		"purchaseTrackedAt":        nullStr(o.PurchaseTrackedAt.String),
		"isTest":                   o.DiscountCode.Valid && o.DiscountCode.String == "INTERNAL",
		"paymentAmount":            paymentAmount,
		"discountCode":             nullStr(o.DiscountCode.String), // воронка покажет «промокод применён», сумму возьмёт из paymentAmount
		"pixelConfirmScript":       nil,
		"tiktokPixelConfirmScript": nil,
		"hasEmail":                 hasEmail,
		"upsellMode":               nil,
		"acceptedUpsells":          []any{},
		"upsellInvoiceUrl":         nil,
		"themeSlug":                nullStr(o.ThemeSlug.String),
		"flow":                     nullStr(o.Flow.String),
		// Партнёрский заказ (Les Roses): оплачен у партнёра, апсейлов Mezmo и
		// отбивок покупки на странице нет; needsBrief — пора показать анкету.
		"partner":     nullStr(partnerOf(o)),
		"needsBrief":  partnerOf(o) != "" && o.PaymentStatus == store.PaymentPaid && o.Status == "created" && !hasSongConfig(o),
		"funnelStage": nullStr(o.FunnelStage.String),
		"extras": map[string]any{
			"prices":   extraPrices,
			"selected": store.ExtrasSelected(o),
			"paid":     paidExtraIDs,
		},
	})
}

func (s *Server) handlePublicOrderContent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	_ = store.MaybeDemoComplete(ctx, s.DB, id)
	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}

	var rawURLs []string
	_ = json.Unmarshal([]byte(o.AudioURLsJSON), &rawURLs)

	// Лимит доработок берётся из настроек (/admin/settings): у тестовых заказов
	// из /admin/ads он свой. 0 в настройке означает «без ограничений» и уезжает на
	// фронт как -1 — по отрицательному значению страница рисует «без ограничений».
	clientLimit, internalLimit := store.RegenerationLimits(ctx, s.DB)
	maxRegenerations := clientLimit
	if store.IsInternalOrder(o) {
		maxRegenerations = internalLimit
	}
	if maxRegenerations == 0 {
		maxRegenerations = -1
	}

	ref := publicOrderRef(s.Config.WebhookSecret, id)

	// Отрывок доступен и без оплаты, и при придержанной доставке — это и есть
	// шаг 7 «превью под замком».
	previewURL := any(nil)
	if o.PreviewAudioURL.Valid && o.PreviewAudioURL.String != "" {
		previewURL = publicPreviewURL(ref, o.RegenerationCount)
	}
	// Пока задержка не вышла, песня не отдаётся вообще: иначе платное ускорение
	// не ускоряет ничего и продаёт воздух.
	held := store.DeliveryHeld(o, time.Now())

	orders := []any{}
	if len(rawURLs) > 0 && o.Status == "completed" && !held {
		lyrics := ""
		if o.LyricsText.Valid {
			lyrics = o.LyricsText.String
		}

		// Прошлые версии текста: доработка перезатирает текущий, и без истории
		// удачный вариант терялся насовсем. Ошибка чтения не должна ронять выдачу
		// песни — тогда просто нет истории.
		versions := []any{}
		stored, err := store.ListLyricsVersions(ctx, s.DB, id)
		if err != nil {
			log.Printf("order %s list lyrics versions err=%v", id, err)
		}
		for _, v := range stored {
			versions = append(versions, map[string]any{
				"version":    v.Version,
				"lyricsText": v.Lyrics,
				"createdAt":  v.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}
		tracks := make([]string, len(rawURLs))
		for i, u := range rawURLs {
			if strings.HasPrefix(u, "orders/") {
				// stored as MinIO key — rewrite to proxy URL
				tracks[i] = publicAudioURL(ref, i, o.RegenerationCount)
			} else {
				tracks[i] = u
			}
		}
		orders = append(orders, map[string]any{
			"id":                  ref,
			"status":              "completed",
			"recipientName":       jsonRecipient(o.InputPayloadJSON),
			"createdAt":           time.Now().UTC().Format(time.RFC3339Nano),
			"tracks":              tracks,
			"lyricsText":          lyrics,
			"lyricsVersions":      versions,
			"regenerationCount":   o.RegenerationCount,
			"maxRegenerations":    maxRegenerations,
			"hasTestimonial":      false,
			"testimonialStatus":   nil,
			"revisionSubmittedAt": nil,
			"language":            "ru",
			"musicStyle":          "",
			"voiceStyle":          "",
		})
	}

	writeJSON(w, 200, map[string]any{
		"orders":              orders,
		"previewUrl":          previewURL,
		"paymentStatus":       o.PaymentStatus,
		"deliveryHeld":        held,
		"deliveryScheduledAt": nullStr(o.DeliverAfter.String),
	})
}

func (s *Server) handleMarkPurchaseTracked(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	// Заявка, а не уведомление: фронт пушит событие в dataLayer только получив
	// first=true. Ошибка → 500 и никакого события: отметка осталась пустой,
	// следующее открытие страницы заказа попробует снова (самолечение лучше,
	// чем потерянная или задвоенная покупка).
	first, err := store.MarkPurchaseTracked(ctx, s.DB, id, time.Now())
	if err != nil {
		log.Printf("[analytics] mark-purchase-tracked order=%s err=%v", id, err)
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	log.Printf("[analytics] mark-purchase-tracked order=%s first=%t", id, first)
	writeJSON(w, 200, map[string]any{"ok": true, "first": first})
}

func (s *Server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}

	var body struct {
		RevisionType string `json:"revisionType"`
		Description  string `json:"description"`
		NewLyrics    string `json:"newLyrics"`
		MusicStyle   string `json:"musicStyle"`
		VoiceStyle   string `json:"voiceStyle"`
		Language     string `json:"language"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_request"})
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
	// Доработка перезапускает платную генерацию (OpenRouter + Kie), поэтому доступна
	// только на оплаченном заказе. Внутренние заказы из /admin/ads создаются сразу
	// с payment_status = paid, так что под условие подходят.
	if o.PaymentStatus != "paid" {
		writeJSON(w, 400, map[string]string{"error": "not_paid"})
		return
	}

	_, err = store.IncrementRegeneration(ctx, s.DB, id)
	if err != nil {
		if err.Error() == "max_regenerations_reached" {
			writeJSON(w, 400, map[string]string{"error": "max_regenerations_reached"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}

	var payload map[string]any
	_ = json.Unmarshal([]byte(o.InputPayloadJSON), &payload)
	getStr := func(k string) string {
		if v, ok := payload[k].(string); ok {
			return v
		}
		return ""
	}
	mp, _ := payload["musicPreferences"].(map[string]any)
	style := ""
	voiceStyle := ""
	if mp != nil {
		if g, ok := mp["genre"].(string); ok {
			style = g
		}
		if v, ok := mp["voiceStyle"].(string); ok {
			voiceStyle = v
		}
	}
	if body.MusicStyle != "" {
		style = body.MusicStyle
	}
	// Пол вокала можно попросить не только дропдауном, но и словами в описании
	// правки. Явный дропдаун точнее — он побеждает.
	voiceChosen := ""
	if body.VoiceStyle != "" {
		voiceChosen = body.VoiceStyle
	} else if g := vocalGenderFromText(body.Description); g != "" {
		voiceChosen = g
	}
	if voiceChosen != "" {
		voiceStyle = voiceChosen
	}
	vocalGender := vocalGenderFor(payload, voiceStyle)
	// Выбранный голос запоминается в заказе, иначе выбор живёт одну доработку:
	// следующая без явного выбора откатилась бы к исходному значению
	// («Surprise me» → female) — и снова не тот голос.
	if voiceChosen != "" {
		if err := store.SetVoiceStyle(ctx, s.DB, id, vocalGender); err != nil {
			log.Printf("regenerate order=%s save voiceStyle err=%v", id, err)
		}
	}

	story := getStr("story")
	if body.Description != "" {
		story = story + "\n\nПравки: " + body.Description
	}
	// Присланный текст песни — это не «история» для новой генерации, а готовый
	// результат: его нужно спеть как есть. Раньше он подставлялся в story, уходил
	// в промпт и модель сочиняла по нему свой текст — вставить конкретный текст
	// было невозможно в принципе.
	customLyrics := strings.TrimSpace(body.NewLyrics)

	// Правка словами меняет существующий текст, а не сочиняет новую песню: модель
	// получает текущий текст и просьбу. Для revisionType=new_story это не так —
	// там человек сознательно просит другую историю, и генерация идёт с нуля.
	editInstruction := ""
	currentLyrics := strings.TrimSpace(o.LyricsText.String)
	if body.RevisionType == "describe" && currentLyrics != "" {
		editInstruction = strings.TrimSpace(body.Description)
	}

	kieCallback := strings.TrimSuffix(s.Config.PublicAppURL, "/") +
		"/api/webhooks/kie?orderId=" + id + "&secret=" + s.Config.WebhookSecret
	rel := relay.Client{
		OpenRouterAPIKey: s.Config.OpenRouterAPIKey,
		OpenRouterModel:  s.Config.OpenRouterModel,
		OpenRouterProxy:  s.Config.OpenRouterProxy,
		KieAPIKey:        s.Config.KieAPIKey,
		CallbackURL:      kieCallback,
	}
	go func() {
		log.Printf("regenerate start order=%s revisionType=%s customLyrics=%t vocalGender=%s", id, body.RevisionType, customLyrics != "", vocalGender)
		payload := relay.OrderPayload{
			OrderID:         o.ID,
			Sender:          getStr("yourName"),
			Recipient:       getStr("recipientName"),
			Occasion:        getStr("occasion"),
			Style:           style,
			Emotion:         getStr("emotion"),
			VocalGender:     vocalGender,
			Story:           story,
			Relationship:    getStr("relationship"),
			RecipientGender: recipientGender(payload),
		}
		var lyrics string
		var err error
		switch {
		case customLyrics != "":
			lyrics, err = rel.TriggerWithLyrics(context.Background(), payload, customLyrics)
		case editInstruction != "":
			lyrics, err = rel.TriggerEdit(context.Background(), payload, currentLyrics, editInstruction)
		default:
			lyrics, err = rel.Trigger(context.Background(), payload)
		}
		if err != nil {
			log.Printf("regenerate error order=%s: %v", id, err)
			_ = store.CompleteWithDemoAudio(context.Background(), s.DB, id, lyrics)
			return
		}
		log.Printf("regenerate success order=%s lyricsLen=%d", id, len(lyrics))
		if s.Config.KieAPIKey != "" {
			if lyrics != "" {
				_ = store.SaveLyrics(context.Background(), s.DB, id, lyrics)
			}
		} else {
			_ = store.CompleteWithDemoAudio(context.Background(), s.DB, id, lyrics)
		}
	}()

	writeJSON(w, 200, map[string]any{"ok": true})
}

// verifyYooPayment сверяет платёж, прочитанный из API ЮKassa, с нашим заказом.
// Пустая строка — всё сошлось, иначе причина отказа для лога.
func (s *Server) verifyYooPayment(ctx context.Context, p payment.Payment, o store.Order, webhookPaymentID string) string {
	if p.Status != "succeeded" || !p.Paid {
		return "not_succeeded"
	}
	// Платежи по заказу заводим только мы. Чужой платёж — пусть и настоящий, и
	// оплаченный — к этому заказу не относится. Сверяем по всей истории платежей:
	// embedded-токен часовой, поэтому у заказа их может быть несколько.
	if !store.KnownPaymentID(o, webhookPaymentID) {
		return "payment_id_mismatch"
	}
	if p.OrderID != o.ID {
		return "order_mismatch"
	}
	if p.Currency != "RUB" {
		return "currency_mismatch"
	}
	got, ok := rublesToKopecks(p.Amount)
	if !ok {
		return "amount_unparsable"
	}
	expected := s.expectedPaymentRubles(ctx, o)
	if got < int64(expected)*100 {
		return "amount_too_small"
	}
	return ""
}

// rublesToKopecks разбирает сумму ЮKassa ("990.00") в копейки, чтобы сравнивать
// целыми числами, а не float.
func rublesToKopecks(v string) (int64, bool) {
	whole, frac, _ := strings.Cut(strings.TrimSpace(v), ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, false
	}
	switch len(frac) {
	case 0:
		frac = "00"
	case 1:
		frac += "0"
	case 2:
	default:
		return 0, false
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return w*100 + f, true
}

// kopecksToRubles форматирует сумму в вид, который принимает ЮKassa ("790.00").
// Обратная к rublesToKopecks; float в деньгах не используем — округление съедает копейки.
func kopecksToRubles(k int64) string {
	return strconv.FormatInt(k/100, 10) + "." + fmt.Sprintf("%02d", k%100)
}

// livePayments перечитывает из API последние платежи заказа и возвращает живые
// успешные (succeeded && paid && refunded < amount) в порядке истории, плюс
// суммарно возвращённое по заказу в копейках.
//
// Список, а не bool: на эту же функцию опирается T-020 (автовозврат дублирующей
// оплаты) — ему нужно знать, какой именно платёж оставить. Порядок обязан быть
// историческим и стабильным, иначе решение T-020 перестанет быть детерминированным.
//
// Полный возврат НЕ меняет статус платежа: он остаётся succeeded и paid == true,
// растёт только refunded_amount. Поэтому «живой успешный платёж» — это
// succeeded && paid && refunded < amount, а не проверка статуса.
//
// Любая ошибка чтения API прерывает обход и возвращается вызывающей стороне:
// в отличие от livePaymentGate (где недоступность API не должна блокировать
// оплату), здесь на кону доступ к уже оплаченной песне — угадывать нельзя.
func (s *Server) livePayments(o store.Order) (live []payment.Payment, refundedTotal int64, err error) {
	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
	for _, id := range paymentHistoryIDs(o, maxPaymentHistory) {
		p, gerr := yk.GetPayment(id)
		if gerr != nil {
			return nil, 0, gerr
		}
		refAmt, _ := rublesToKopecks(p.Refunded) // "" → 0, возвратов не было
		refundedTotal += refAmt
		paidAmt, ok := rublesToKopecks(p.Amount)
		if p.Status == "succeeded" && p.Paid && ok && refAmt < paidAmt {
			live = append(live, p)
		}
	}
	return live, refundedTotal, nil
}

// livePaymentsLeft — обёртка для ветки refund.succeeded: остались ли деньги у нас.
func (s *Server) livePaymentsLeft(o store.Order) (hasLive bool, refundedTotal int64, err error) {
	live, refundedTotal, err := s.livePayments(o)
	if err != nil {
		return false, 0, err
	}
	return len(live) > 0, refundedTotal, nil
}

// refundExtraPayments возвращает лишние успешные платежи заказа. Оставляем самый
// ранний по истории — решение детерминированное, поэтому два конкурентных вебхука
// приходят к одному выводу, а детерминированный Idempotence-Key не даёт ЮKassa
// провести возврат дважды.
//
// Статус заказа НЕ трогаем: его подвинет вебхук refund.succeeded по правилам T-002
// («refunded только когда не осталось живого успешного платежа»). Поэтому автовозврат
// лишнего платежа не отнимает у покупателя песню — оставленный платёж жив.
//
// retry == true — платежи прочитать не удалось, а заказ уже был оплачен: единственная
// работа этого вебхука — возврат, значит имеет смысл ответить 500 и дождаться повтора.
func (s *Server) refundExtraPayments(ctx context.Context, o store.Order) (retry bool) {
	live, _, err := s.livePayments(o)
	if err != nil {
		log.Printf("[pay] refund-dup order=%s scan err=%v", o.ID, err)
		return o.PaymentStatus == store.PaymentPaid
	}
	if len(live) < 2 {
		return false
	}
	keep := live[0]
	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
	for _, p := range live[1:] {
		paidAmt, _ := rublesToKopecks(p.Amount)
		refAmt, _ := rublesToKopecks(p.Refunded)
		remainder := paidAmt - refAmt
		if remainder <= 0 {
			continue
		}
		r, err := yk.CreateRefund(p.ID, kopecksToRubles(remainder))
		if err != nil {
			log.Printf("[pay] refund-dup order=%s payment=%s FAILED: %v (оставлен %s)", o.ID, p.ID, err, keep.ID)
			continue
		}
		log.Printf("[pay] refund-dup order=%s payment=%s refunded %s руб. refund=%s status=%s (оставлен %s)",
			o.ID, p.ID, kopecksToRubles(remainder), r.ID, r.Status, keep.ID)
	}
	return false
}

func (s *Server) handleYooWebhook(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := readJSON(r, &payload); err != nil {
		log.Printf("[pay] yookassa webhook: decode err=%v", err)
		w.WriteHeader(400)
		return
	}
	obj, _ := payload["object"].(map[string]any)
	meta, _ := obj["metadata"].(map[string]any)
	orderID, _ := meta["order_id"].(string)
	event, _ := payload["event"].(string)
	// object.id — id самого объекта: для payment.succeeded/payment.canceled это id
	// платежа, но для refund.succeeded это id ВОЗВРАТА, а платёж, к которому он
	// относится, лежит в object.payment_id.
	objID, _ := obj["id"].(string)
	paymentID := objID
	if event == "refund.succeeded" {
		paymentID, _ = obj["payment_id"].(string)
	}
	log.Printf("[pay] yookassa webhook received: event=%s order=%s object=%s payment=%s", event, orderID, objID, paymentID)

	// Платежи подписки помечены metadata.subscription_id вместо order_id — у них
	// свой цикл (активация/продление), в конвейер заказов они не попадают.
	if subID, _ := meta["subscription_id"].(string); subID != "" {
		s.handleSubscriptionWebhook(w, r, event, paymentID, subID)
		return
	}

	// Платежи допников помечены metadata.extra — у них свой обработчик: они не
	// помечают заказ оплаченным и не попадают под автовозврат «лишних» платежей.
	if extraID, _ := meta["extra"].(string); extraID != "" && orderID != "" {
		s.handleExtraWebhook(w, r, event, paymentID, orderID, extraID)
		return
	}

	// refund.succeeded приходит с объектом *возврата*, не платежа: metadata.order_id
	// в нём может отсутствовать. Тогда заказ определяется по object.payment_id →
	// GetPayment → metadata.order_id платежа.
	if orderID == "" && event == "refund.succeeded" && paymentID != "" &&
		s.Config.YooKassaShopID != "" && s.Config.YooKassaSecretKey != "" {
		yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
		if p, err := yk.GetPayment(paymentID); err == nil {
			orderID = p.OrderID
		} else {
			log.Printf("[pay] yookassa webhook refund payment=%s: resolve order via GetPayment err=%v", paymentID, err)
		}
	}

	if orderID == "" {
		log.Printf("[pay] yookassa webhook: no order_id in metadata, ignoring")
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}

	switch event {
	case "payment.succeeded":
		switch s.settleSucceededPayment(r.Context(), orderID, paymentID, "yookassa webhook") {
		case settleNotFound, settleConflict, settleOK:
			writeJSON(w, 200, map[string]any{"ok": true})
		case settleNotVerifiable:
			writeJSON(w, 200, map[string]any{"ok": false, "error": "not_verifiable"})
		case settleRejected:
			writeJSON(w, 200, map[string]any{"ok": false, "error": "verification_failed"})
		case settleRetry:
			// Сеть или временная ошибка API ЮKassa: 500 — ЮKassa повторит вебхук,
			// и оплата подтвердится на следующей попытке.
			w.WriteHeader(500)
		}
		return
	case "payment.canceled":
		ctx := r.Context()
		if s.Config.YooKassaShopID == "" || s.Config.YooKassaSecretKey == "" {
			log.Printf("[pay] yookassa webhook order=%s: no credentials configured, cannot verify canceled — ignoring", orderID)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		o, err := store.GetOrder(ctx, s.DB, orderID)
		if err != nil {
			log.Printf("[pay] yookassa webhook order=%s: get order err=%v", orderID, err)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		// Отмена применяется, только если вебхук говорит про тот платёж, который мы
		// сами завели последним (orders.yoo_payment_id), и заказ ещё не оплачен.
		// Иначе устаревший/чужой payment.canceled мог бы «отменить» заказ, который
		// человек оплатил другим платежом — устаревшие платежи протухают сами и
		// canceled по ним прилетает штатно.
		if o.PaymentStatus != store.PaymentPending || !o.YooPaymentID.Valid || o.YooPaymentID.String != paymentID {
			log.Printf("[pay] yookassa webhook order=%s payment=%s canceled IGNORED: not current pending payment (status=%s current=%v)",
				orderID, paymentID, o.PaymentStatus, o.YooPaymentID)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		changed, err := store.MarkCanceled(ctx, s.DB, orderID)
		if err != nil {
			log.Printf("[pay] yookassa webhook order=%s MarkCanceled err=%v", orderID, err)
		}
		log.Printf("[pay] yookassa webhook order=%s payment=%s: canceled, changed=%v", orderID, paymentID, changed)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	case "refund.succeeded":
		ctx := r.Context()
		if s.Config.YooKassaShopID == "" || s.Config.YooKassaSecretKey == "" {
			log.Printf("[pay] yookassa webhook order=%s: no credentials configured, cannot verify refund — ignoring", orderID)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		o, err := store.GetOrder(ctx, s.DB, orderID)
		if err != nil {
			log.Printf("[pay] yookassa webhook order=%s: get order err=%v", orderID, err)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		if !store.KnownPaymentID(o, paymentID) {
			log.Printf("[pay] yookassa webhook order=%s payment=%s refund IGNORED: unknown payment", orderID, paymentID)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		hasLive, refundedTotal, err := s.livePaymentsLeft(o)
		if err != nil {
			// Сеть/временная ошибка API: 500 — ЮKassa повторит вебхук. Молча оставлять
			// заказ в неизвестном состоянии нельзя: на этом решении висит доступ к песне.
			log.Printf("[pay] yookassa webhook order=%s payment=%s: livePaymentsLeft err=%v", orderID, paymentID, err)
			w.WriteHeader(500)
			return
		}
		if err := store.SetRefundedKopecks(ctx, s.DB, orderID, refundedTotal); err != nil {
			log.Printf("[pay] yookassa webhook order=%s SetRefundedKopecks err=%v", orderID, err)
		}
		if hasLive {
			// Вернули лишний платёж (двойная оплата) или часть суммы — песня остаётся у клиента.
			log.Printf("[pay] refund order=%s: возвращено %d коп., но остался успешный платёж — доступ сохранён", orderID, refundedTotal)
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		changed, err := store.MarkRefunded(ctx, s.DB, orderID)
		if err != nil {
			log.Printf("[pay] yookassa webhook order=%s MarkRefunded err=%v", orderID, err)
		}
		log.Printf("[pay] refund order=%s: денег не осталось (возвращено %d коп.) → refunded, changed=%v", orderID, refundedTotal, changed)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	default:
		log.Printf("[pay] yookassa webhook order=%s: unhandled event=%s", orderID, event)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// settleOutcome — исход зачёта успешного платежа по заказу. Вебхук переводит
// его в HTTP-ответ ЮKassa, фоновая сверка — в лог.
type settleOutcome int

const (
	settleOK            settleOutcome = iota // оплата зачтена (или дубль — уже была)
	settleNotFound                           // заказа нет в БД
	settleNotVerifiable                      // нет ключей ЮKassa — сверить нельзя
	settleRejected                           // платёж не прошёл сверку с заказом
	settleConflict                           // успешная оплата на уже возвращённый заказ
	settleRetry                              // временная ошибка API — повторить позже
)

// settleSucceededPayment — единственный путь заказа в payment_status = paid.
// Его зовут вебхук ЮKassa (payment.succeeded) и фоновая сверка reconcile,
// когда вебхук не дошёл; src — кто зовёт, только для логов.
//
// Тело вебхука ничего не доказывает: ручка публичная, POST с чужим order_id
// может прислать кто угодно. Состояние платежа перечитывается из API ЮKassa и
// сверяется с заказом — только после этого начинаются побочные эффекты
// (оплата, генерация, письмо, конверсии, автовозврат дубля). Сверка
// выполняется даже на дубле: именно дубль — тот случай, ради которого мы идём
// в API, чтобы понять, есть ли вторая успешная оплата и вернуть лишнее.
func (s *Server) settleSucceededPayment(ctx context.Context, orderID, paymentID, src string) settleOutcome {
	prev, err := store.GetOrder(ctx, s.DB, orderID)
	if err != nil {
		log.Printf("[pay] %s order=%s: get order err=%v", src, orderID, err)
		return settleNotFound
	}
	if s.Config.YooKassaShopID == "" || s.Config.YooKassaSecretKey == "" {
		log.Printf("[pay] %s order=%s: no credentials configured, cannot verify — ignoring", src, orderID)
		return settleNotVerifiable
	}
	yk := payment.YooKassa{ShopID: s.Config.YooKassaShopID, Secret: s.Config.YooKassaSecretKey}
	p, err := yk.GetPayment(paymentID)
	if err != nil {
		log.Printf("[pay] %s order=%s payment=%s: verify err=%v", src, orderID, paymentID, err)
		return settleRetry
	}
	if reason := s.verifyYooPayment(ctx, p, prev, paymentID); reason != "" {
		log.Printf("[pay] %s order=%s payment=%s REJECTED: %s (api: status=%s paid=%v amount=%s %s meta_order=%s)",
			src, orderID, paymentID, reason, p.Status, p.Paid, p.Amount, p.Currency, p.OrderID)
		return settleRejected
	}
	log.Printf("[pay] %s order=%s payment=%s: verified against API (amount=%s %s)", src, orderID, paymentID, p.Amount, p.Currency)
	// Сохранённая карта — под апсейл подписки «0 ₽ сегодня». Пишется и на
	// дубле: первый вебхук мог прийти до того, как ЮKassa отдала saved=true.
	if p.PaymentMethodSaved && p.PaymentMethodID != "" {
		if err := store.SetOrderPaymentMethod(ctx, s.DB, orderID, p.PaymentMethodID); err != nil {
			log.Printf("[pay] %s order=%s: save payment method err=%v", src, orderID, err)
		}
	}

	if out := s.applyOrderPaid(ctx, prev, orderID, paymentID, src); out != settleOK {
		return out
	}

	// Возврат лишнего считается в обеих ветках: два вебхука, пришедшие до
	// MarkPaid, оба идут по «первой» ветке, и если проверять только на дубле,
	// дубль не заметит никто. prev — состояние заказа ДО этого события, поэтому
	// retry корректно отличает дубль (prev уже paid) от первой оплаты (prev ещё
	// не paid), не завязываясь на то, по какому платежу пришёл именно этот вебхук.
	if retry := s.refundExtraPayments(ctx, prev); retry {
		return settleRetry // перечитать платежи не удалось на дубле — повторить
	}
	return settleOK
}

// applyOrderPaid — побочные эффекты оплаты заказа, общие для всех провайдеров:
// перевод в paid, запуск генерации, конверсии, письмо. Вызывается только ПОСЛЕ
// того, как провайдер-специфичная сверка подтвердила платёж (ЮKassa —
// settleSucceededPayment, UnitPay — settleUnitPayPayment). prev — заказ до
// этого события: по нему отличается первая оплата от дубля уведомления.
func (s *Server) applyOrderPaid(ctx context.Context, prev store.Order, orderID, paymentID, src string) settleOutcome {
	if prev.PaymentStatus == store.PaymentRefunded {
		// Заказ уже вернули, а теперь по нему пришли деньги. Молча вернуть его в
		// paid нельзя (MarkPaid переводит в paid из любого статуса, кроме paid), но
		// и решать за человека, чем это было, мы не будем. Громко логируем.
		log.Printf("[pay] refund-dup order=%s payment=%s CONFLICT: успешная оплата пришла на возвращённый заказ — решение за человеком", orderID, paymentID)
		return settleConflict
	}

	if prev.PaymentStatus == store.PaymentPaid {
		// Gate side-effects on the first time we see this order paid. YooKassa
		// retries webhooks, so a duplicate must not send a second receipt / GA4
		// event or re-trigger generation.
		log.Printf("[pay] %s order=%s: already paid (duplicate), no action", src, orderID)
	} else {
		if err := store.MarkPaid(ctx, s.DB, orderID); err != nil {
			log.Printf("[pay] %s order=%s MarkPaid err=%v", src, orderID, err)
		} else {
			log.Printf("[pay] %s order=%s marked paid", src, orderID)
		}
		_ = store.SetFunnelStage(ctx, s.DB, orderID, store.StagePaid)
		// Обычная доставка придерживается на delivery.normal_delay_minutes —
		// именно снятие этой задержки продаёт ускорение на шаге 8.
		s.applyDeliveryHold(ctx, orderID)
		// Drive generation straight from the webhook so it no longer depends on the
		// customer returning to the site (the client-side /confirm often never fires).
		// ConfirmOrder is idempotent, so if /confirm also runs only one of them wins.
		// Воронка v2 оплачивается ДО вопросов о песне: payload на этот момент пуст,
		// генерацию запускает /api/orders/{id}/config, а не вебхук.
		if hasSongConfig(prev) {
			transitioned, err := store.ConfirmOrder(ctx, s.DB, orderID)
			if err != nil {
				log.Printf("[pay] %s order=%s ConfirmOrder err=%v", src, orderID, err)
			}
			if transitioned {
				log.Printf("[pay] %s order=%s: generation triggered", src, orderID)
				go s.triggerRelay(context.Background(), orderID)
			} else {
				log.Printf("[pay] %s order=%s: already processing, generation not re-triggered", src, orderID)
			}
		} else {
			log.Printf("[pay] %s order=%s: song config not filled yet (flow=v2), generation deferred", src, orderID)
		}
		// Server-side purchase tracking: fires for every paid order, even when the
		// customer never returns to the site (which is why the client-side event
		// under-counts). MarkServerTracked — атомарный гард: два конкурентных
		// вебхука не отправят конверсии дважды.
		if first, err := store.MarkServerTracked(ctx, s.DB, orderID, time.Now()); err != nil {
			log.Printf("[pay] %s order=%s MarkServerTracked err=%v", src, orderID, err)
		} else if first {
			go s.sendServerConversions(orderID)
			s.marketingOrder(orderID, map[string]string{marketing.FieldOrderPaid: "1"})
			s.marketingEvent(orderID, EventOrderPaid)
		}
		// Песня, сгенерированная ДО оплаты, уходит письмом прямо отсюда — одним
		// письмом вместе с благодарностью. Если она ещё не готова или доставка
		// придержана, уходит обычная квитанция, а письмо о песне отправит
		// колбэк Kie.ai или планировщик.
		go func(email string) {
			if s.deliverIfReady(context.Background(), orderID, src, "Спасибо, оплата получена!") {
				return
			}
			if email == "" {
				log.Printf("[pay] %s order=%s: no customer email, skip receipt", src, orderID)
				return
			}
			if s.Config.SMTPHost == "" {
				log.Printf("[pay] %s order=%s: SMTP_HOST not set, skip receipt", src, orderID)
				return
			}
			// Заказ воронки /create без анкеты (плечо «оплата в начале»):
			// человек оплатил и ушёл, песни не будет, пока он не допишет
			// историю — ссылка ведёт на продолжение воронки, а не на
			// страницу заказа, где анкеты нет.
			link := s.publicOrderURL(orderID)
			vars := map[string]string{"order_url": link, "needs_story": ""}
			body := "Спасибо! Заказ принят.\nСтраница заказа: " + link
			if o, err := store.GetOrder(context.Background(), s.DB, orderID); err == nil && o.Flow.Valid && !hasSongConfig(o) {
				link = s.flowResumeURL(publicOrderRef(s.Config.WebhookSecret, orderID))
				vars["needs_story"] = "1"
				body = "Спасибо, оплата получена!\nОсталось рассказать историю для песни — продолжите здесь: " + link
			}
			// Ссылка готовая, а не на выбор в шаблоне: условия simple в ссылках не работают.
			vars["next_url"] = link
			if err := s.sendEmail(context.Background(), emailPaymentReceived, emailTarget{OrderID: orderID}, email, "Оплата получена", body, vars); err != nil {
				log.Printf("[pay] %s order=%s: receipt email err=%v", src, orderID, err)
			} else {
				log.Printf("[pay] %s order=%s: receipt email sent to=%s", src, orderID, email)
			}
		}(extractCustomerEmail(orderID, s.DB))
	}

	return settleOK
}

// sendServerConversions — серверная «шина» конверсий: одна оплата с вебхука
// раскидывается по всем системам аналитики. Каждый синк независим и молчит,
// пока его env-переменные не заполнены.
func (s *Server) sendServerConversions(orderID string) {
	o, err := store.GetOrder(context.Background(), s.DB, orderID)
	if err != nil {
		log.Printf("server conversions: get order %s: %v", orderID, err)
		return
	}
	s.sendConversion(conversion{
		Label:       "order " + o.ID,
		TxID:        o.ID,
		Value:       orderFinalPrice(o),
		ItemID:      "song",
		ItemName:    "Персональная песня",
		GAClientID:  o.GAClientID.String,
		YMClientID:  o.YMClientID.String,
		Attribution: o.AttributionJSON.String,
	})
}

// conversion — одна покупка для всех систем аналитики. Заказ и платёж
// подписки сюда сводятся одинаково: шина не знает, что именно продано.
type conversion struct {
	Label       string // для логов: "order ord_x" / "subscription sub_x payment …"
	TxID        string // transaction_id: id заказа или id платежа подписки
	Value       int    // рубли
	ItemID      string
	ItemName    string
	GAClientID  string // из куки _ga; пусто — детерминированный фолбэк по TxID
	YMClientID  string // из куки _ym_uid; пусто — Метрике отправить некому
	Attribution string // JSON меток визита (rb_clickid для VK)
}

func (s *Server) sendConversion(c conversion) {
	s.sendGA4Purchase(c)
	s.sendMetrikaConversion(c)
	s.sendVKConversion(c)
}

func orderFinalPrice(o store.Order) int {
	value := o.BasePrice
	if o.DiscountAmount.Valid {
		value -= int(o.DiscountAmount.Int64)
	}
	if value < 0 {
		value = 0
	}
	return value
}

// sendGA4Purchase reports a purchase to GA4 via the Measurement Protocol.
// No-op unless GA4_MEASUREMENT_ID and GA4_API_SECRET are configured.
func (s *Server) sendGA4Purchase(c conversion) {
	if s.Config.GA4MeasurementID == "" || s.Config.GA4APISecret == "" {
		return
	}
	value := c.Value

	// GA4 requires a non-empty client_id. Prefer the browser's _ga client id
	// captured at draft time; fall back to a deterministic per-transaction id.
	clientID := c.TxID
	if strings.TrimSpace(c.GAClientID) != "" {
		clientID = strings.TrimSpace(c.GAClientID)
	}

	body := map[string]any{
		"client_id": clientID,
		"events": []map[string]any{{
			"name": "purchase",
			"params": map[string]any{
				"transaction_id": c.TxID,
				"value":          value,
				"currency":       s.siteCurrency(),
				"items": []map[string]any{{
					"item_id":   c.ItemID,
					"item_name": c.ItemName,
					"price":     value,
					"quantity":  1,
				}},
			},
		}},
	}
	payload, _ := json.Marshal(body)

	url := fmt.Sprintf("https://www.google-analytics.com/mp/collect?measurement_id=%s&api_secret=%s",
		s.Config.GA4MeasurementID, s.Config.GA4APISecret)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("ga4 purchase: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ga4 purchase: post: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("ga4 purchase: status %d for %s", resp.StatusCode, c.Label)
	}
}

// sendMetrikaConversion загружает покупку в Метрику через API офлайн-конверсий:
// цель + выручка, матчинг по ClientID из куки _ym_uid, снятой при создании
// заказа. No-op без METRIKA_COUNTER_ID/METRIKA_OAUTH_TOKEN или без ClientID.
// В настройках счётчика должны быть включены офлайн-конверсии.
func (s *Server) sendMetrikaConversion(c conversion) {
	if s.Config.MetrikaCounterID == "" || s.Config.MetrikaOAuthToken == "" {
		return
	}
	value := c.Value
	clientID := strings.TrimSpace(c.YMClientID)
	if clientID == "" {
		log.Printf("metrika conversion: %s has no ym client id, skip", c.Label)
		return
	}

	csv := fmt.Sprintf("ClientId,Target,DateTime,Price,Currency\n%s,purchase,%d,%d,RUB\n",
		clientID, time.Now().Unix(), value)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "conversions.csv")
	if err != nil {
		log.Printf("metrika conversion: multipart: %v", err)
		return
	}
	_, _ = fw.Write([]byte(csv))
	_ = mw.Close()

	endpoint := fmt.Sprintf("https://api-metrika.yandex.net/management/v1/counter/%s/offline_conversions/upload?client_id_type=CLIENT_ID",
		s.Config.MetrikaCounterID)
	req, err := http.NewRequest(http.MethodPost, endpoint, &buf)
	if err != nil {
		log.Printf("metrika conversion: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "OAuth "+s.Config.MetrikaOAuthToken)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("metrika conversion: post: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		log.Printf("metrika conversion: status %d for %s: %s", resp.StatusCode, c.Label, respBody)
	} else {
		log.Printf("metrika conversion: uploaded for %s", c.Label)
	}
}

// sendVKConversion отправляет цель в VK Ads (Top.Mail.Ru) серверным трекером.
// Матчинг по rb_clickid из attribution заказа — конверсия привязывается к
// рекламному клику. Без rb_clickid событие некому атрибуцировать — скип.
// No-op без VK_PIXEL_ID.
func (s *Server) sendVKConversion(c conversion) {
	if s.Config.VKPixelID == "" {
		return
	}
	value := c.Value
	rbClickID := ""
	if c.Attribution != "" {
		var attr map[string]string
		if err := json.Unmarshal([]byte(c.Attribution), &attr); err == nil {
			rbClickID = attr["rb_clickid"]
		}
	}
	if rbClickID == "" {
		log.Printf("vk conversion: %s has no rb_clickid, skip", c.Label)
		return
	}

	// Формат трекера: e=RG:<выручка>/<цель>; разделитель параметров — «;».
	endpoint := fmt.Sprintf("https://top-fwz1.mail.ru/tracker?id=%s;e=RG%%3A%d/%s;rb_clickid=%s",
		s.Config.VKPixelID, value, url.QueryEscape(s.Config.VKGoal), url.QueryEscape(rbClickID))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		log.Printf("vk conversion: get: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("vk conversion: status %d for %s", resp.StatusCode, c.Label)
	} else {
		log.Printf("vk conversion: sent for %s", c.Label)
	}
}

func extractCustomerEmail(orderID string, db *sql.DB) string {
	o, err := store.GetOrder(context.Background(), db, orderID)
	if err != nil {
		return ""
	}
	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	if v, ok := cust["email"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// handleKieWebhook receives Kie.ai callback: POST /api/webhooks/kie?orderId=...&secret=...
func (s *Server) handleKieWebhook(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("secret")
	if secret == "" || secret != s.Config.WebhookSecret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	orderID := r.URL.Query().Get("orderId")
	if orderID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			CallbackType string `json:"callbackType"`
			TaskID       string `json:"task_id"`
			Data         []struct {
				ID       string `json:"id"` // audioId трека — нужен для replace-section и Persona
				AudioURL string `json:"audio_url"`
				Title    string `json:"title"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := readJSON(r, &body); err != nil {
		log.Printf("kie webhook order=%s decode err=%v", orderID, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	log.Printf("kie webhook order=%s code=%d msg=%q callbackType=%s tracks=%d", orderID, body.Code, body.Msg, body.Data.CallbackType, len(body.Data.Data))
	cbType := strings.ToLower(body.Data.CallbackType)
	if cbType == "complete" && (body.Code != 200 || len(body.Data.Data) == 0 || body.Data.Data[0].AudioURL == "") {
		log.Printf("kie webhook order=%s generation failed code=%d msg=%q tracks=%d", orderID, body.Code, body.Msg, len(body.Data.Data))
		writeJSON(w, 200, map[string]any{"ok": false, "error": "generation_failed"})
		go func() {
			o, err := store.GetOrder(context.Background(), s.DB, orderID)
			if err != nil {
				return
			}
			lyrics := ""
			if o.LyricsText.Valid {
				lyrics = o.LyricsText.String
			}
			_ = store.CompleteWithDemoAudio(context.Background(), s.DB, orderID, lyrics)
			log.Printf("kie webhook order=%s fallback demo audio applied", orderID)
		}()
		return
	}
	if cbType != "complete" {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	// Respond immediately so Kie.ai doesn't time out and cancel our context.
	writeJSON(w, 200, map[string]any{"ok": true})

	tracks := body.Data.Data
	kieTaskID := body.Data.TaskID
	go func() {
		ctx := context.Background()
		o, err := store.GetOrder(ctx, s.DB, orderID)
		if err != nil {
			log.Printf("kie webhook order=%s get order err=%v", orderID, err)
			return
		}

		// Идентификаторы прогона сохраняем до загрузки треков: они нужны для
		// replace-section и Persona, и потерять их из-за упавшей выгрузки в MinIO
		// значит потерять навсегда — второго колбэка по этой задаче не будет.
		audioIDs := make([]string, 0, len(tracks))
		for _, track := range tracks {
			if track.ID != "" {
				audioIDs = append(audioIDs, track.ID)
			}
		}
		if kieTaskID != "" || len(audioIDs) > 0 {
			if err := store.SaveKieIDs(ctx, s.DB, orderID, kieTaskID, audioIDs); err != nil {
				log.Printf("kie webhook order=%s save kie ids err=%v", orderID, err)
			} else {
				log.Printf("kie webhook order=%s kie ids saved task=%s audio=%v", orderID, kieTaskID, audioIDs)
			}
		}
		lyrics := ""
		if o.LyricsText.Valid {
			lyrics = o.LyricsText.String
		}

		var audioURLs []string
		for i, track := range tracks {
			url := track.AudioURL
			if s.Storage != nil {
				key := audioObjectKey(orderID, o.RegenerationCount, i+1)
				if err := s.Storage.DownloadAndStore(ctx, key, track.AudioURL, "audio/mpeg"); err != nil {
					log.Printf("minio upload error order=%s track=%d: %v", orderID, i+1, err)
				} else {
					log.Printf("minio upload ok order=%s track=%d key=%s", orderID, i+1, key)
					url = key
				}
			}
			audioURLs = append(audioURLs, url)
		}

		_ = store.SaveRelayResult(ctx, s.DB, orderID, audioURLs, lyrics, "")
		log.Printf("kie webhook order=%s saved tracks=%d", orderID, len(audioURLs))

		// 15-секундный отрывок для шага 7: собирается из первого трека сразу,
		// потому что в варианте 1 его слушают ДО оплаты.
		if len(audioURLs) > 0 {
			s.buildPreviewClip(ctx, orderID, audioURLs[0], o.RegenerationCount)
		}

		// Письмо уходит только оплаченному заказу с вышедшей задержкой; иначе
		// его отправит вебхук оплаты или планировщик (deliverIfReady).
		s.deliverIfReady(ctx, orderID, "kie webhook", "")
	}()
}

func (s *Server) handleSongReady(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if s.Config.WebhookSecret == "" || token != s.Config.WebhookSecret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var body struct {
		OrderID   string   `json:"orderId"`
		Status    string   `json:"status"`
		AudioURLs []string `json:"audioUrls"`
		Title     string   `json:"title"`
		Lyrics    string   `json:"lyrics"`
	}
	if err := readJSON(r, &body); err != nil {
		w.WriteHeader(400)
		return
	}
	ctx := r.Context()
	if body.Status == "ready" && len(body.AudioURLs) > 0 {
		_ = store.SaveRelayResult(ctx, s.DB, body.OrderID, body.AudioURLs, body.Lyrics, body.Title)
		go s.deliverIfReady(context.Background(), body.OrderID, "song-ready", "")
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(v)
}

// ordersPerPage — размер страницы списка заказов в админке. Таблица растёт без
// предела, поэтому и выборка, и поиск живут на сервере.
const ordersPerPage = 50

func (s *Server) handleAdminOrders(w http.ResponseWriter, r *http.Request) {
	page := 1
	if n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page"))); err == nil && n > 1 {
		page = n
	}
	orders, total, err := store.ListOrdersPage(r.Context(), s.DB, store.OrderPage{
		Query:  r.URL.Query().Get("q"),
		Filter: r.URL.Query().Get("filter"),
		Limit:  ordersPerPage,
		Offset: (page - 1) * ordersPerPage,
	})
	if err != nil {
		log.Printf("list orders: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}

	out := make([]map[string]any, 0, len(orders))
	for _, o := range orders {
		out = append(out, s.adminOrderRow(r.Context(), o).json())
	}
	writeJSON(w, 200, map[string]any{
		"orders":  out,
		"total":   total,
		"page":    page,
		"perPage": ordersPerPage,
	})
}

// sourceUnknown — заказы, созданные до того, как метки начали сохраняться.
// Отличается от «прямой»: там метки собирались и их не было, здесь их некому
// было собрать.
const sourceUnknown = "неизвестно"

// handleAdminAnalytics — сводка для /admin/analytics: список покупок с источником
// и дневная воронка визиты → заказы → покупки по источникам трафика.
func (s *Server) handleAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	days := 14
	if q := strings.TrimSpace(r.URL.Query().Get("days")); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	sinceDay := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	visitStats, err := store.VisitStats(r.Context(), s.DB, sinceDay)
	if err != nil {
		log.Printf("visit stats: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	// Период ограничивается в SQL: аналитике нужны последние N дней, а таблица
	// заказов растёт неограниченно и целиком в память больше не помещается.
	orders, err := store.ListOrdersSince(r.Context(), s.DB, sinceDay)
	if err != nil {
		log.Printf("list orders for analytics: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}

	// bucket — дневной срез по источнику. refunds/refundedRevenue считаются отдельно:
	// возврат не выручка, но и не небытие — оператор должен видеть, что деньги вернули.
	type bucket struct {
		day             string
		source          string
		visitors        int
		orders          int
		purchases       int // только payment_status == paid
		revenue         int // только payment_status == paid
		refunds         int // payment_status == refunded
		refundedRevenue int // сумма возвращённых заказов
	}
	buckets := map[string]*bucket{}
	bucketFor := func(day, source string) *bucket {
		k := day + "|" + source
		b := buckets[k]
		if b == nil {
			b = &bucket{day: day, source: source}
			buckets[k] = b
		}
		return b
	}
	for _, vs := range visitStats {
		bucketFor(vs.Day, vs.Source).visitors += vs.Visitors
	}

	// Плечи A/B — второй разрез того же периода: рекламе нужен процент по плечам,
	// не открывая Grafana. Визиты берутся из visits (конверсия считается от визита),
	// заказы и деньги — из тех же orders, что и разрез по источникам.
	type armBucket struct{ visits, orders, purchases, revenue int }
	armStats := map[string]*armBucket{}
	armFor := func(id string) *armBucket {
		b := armStats[id]
		if b == nil {
			b = &armBucket{}
			armStats[id] = b
		}
		return b
	}
	armVisits, err := store.VisitArmStats(r.Context(), s.DB, sinceDay)
	if err != nil {
		log.Printf("visit arm stats: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	for _, av := range armVisits {
		armFor(av.Arm).visits += av.Visitors
	}

	selfHost := s.selfHost()
	purchases := make([]map[string]any, 0, 16)
	for _, o := range orders {
		day := o.CreatedAt.UTC().Format("2006-01-02")
		// Внутренние заказы из /admin/ads в воронку не идут — это не продажи.
		if o.DiscountCode.String == "INTERNAL" || o.BasePrice == 0 {
			continue
		}
		// Партнёрские продажи (Les Roses) — не наш трафик и не наши деньги
		// через ЮKassa: у них своя воронка в блоке partners, здесь они
		// исказили бы и источники, и плечи A/B.
		if partnerOf(o) != "" {
			continue
		}

		var attr map[string]any
		if o.AttributionJSON.Valid {
			_ = json.Unmarshal([]byte(o.AttributionJSON.String), &attr)
		}
		source, campaign := attributionSource(attr, selfHost)
		if len(attr) == 0 {
			source = sourceUnknown
		}

		b := bucketFor(day, source)
		b.orders++
		// Формула суммы — та же, что в SQL дашборда (grafana/dashboards/ab-flows.json):
		// цена минус скидка плюс допники. Без допников две аналитики расходились бы
		// ровно на их сумму, и «правильного» числа не было бы ни там, ни здесь.
		amount := o.BasePrice - int(o.DiscountAmount.Int64) + s.extraRevenue(r.Context(), o)
		switch o.PaymentStatus {
		case store.PaymentPaid:
			b.purchases++
			b.revenue += amount
		case store.PaymentRefunded:
			b.refunds++
			b.refundedRevenue += amount
		}
		if arm := strings.TrimSpace(o.FlowVariant.String); arm != "" {
			ab := armFor(arm)
			ab.orders++
			if o.PaymentStatus == store.PaymentPaid {
				ab.purchases++
				ab.revenue += amount
			}
		}

		// в purchases[] попадают и paid, и refunded — с полем refunded, чтобы фронт мог
		// отрисовать возврат зачёркнутым, а не потерять его.
		if o.PaymentStatus == store.PaymentPaid || o.PaymentStatus == store.PaymentRefunded {
			var cust map[string]any
			_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
			email, _ := cust["email"].(string)
			purchases = append(purchases, map[string]any{
				"kind":          "order",
				"orderId":       o.ID,
				"publicRef":     publicOrderRef(s.Config.WebhookSecret, o.ID),
				"createdAt":     o.CreatedAt.UTC().Format(time.RFC3339Nano),
				"email":         nullStr(email),
				"amount":        amount,
				"source":        source,
				"campaign":      nullStr(campaign),
				"details":       attr,
				"paymentStatus": o.PaymentStatus,
				"refunded":      o.PaymentStatus == store.PaymentRefunded,
			})
		}
	}

	// Платежи подписок — такая же выручка, как разовые покупки, но живут в
	// subscription_payments, а не в orders: песни подписчиков — заказы на 0 ₽.
	// Без этого блока дашборд расходился с кабинетом ЮKassa ровно на подписки
	// (2026-09-16: 6 оплат в ЮKassa, 3 в админке). В дневные срезы по источнику
	// они входят покупкой и выручкой, в purchases[] — строкой kind=subscription.
	subPayments, err := store.ListSubscriptionPaymentsSince(r.Context(), s.DB, sinceDay)
	if err != nil {
		log.Printf("list subscription payments for analytics: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	for _, p := range subPayments {
		var attr map[string]any
		if p.Attribution.Valid {
			_ = json.Unmarshal([]byte(p.Attribution.String), &attr)
		}
		source, campaign := attributionSource(attr, selfHost)
		if len(attr) == 0 {
			source = sourceUnknown
		}
		b := bucketFor(p.CreatedAt.UTC().Format("2006-01-02"), source)
		b.purchases++
		b.revenue += p.AmountRub()
		purchases = append(purchases, map[string]any{
			"kind":          "subscription",
			"flow":          p.Flow,
			"paymentId":     p.PaymentID,
			"orderId":       "",
			"publicRef":     "",
			"createdAt":     p.CreatedAt.UTC().Format(time.RFC3339Nano),
			"email":         nullStr(p.Email),
			"amount":        p.AmountRub(),
			"source":        source,
			"campaign":      nullStr(campaign),
			"details":       attr,
			"paymentStatus": store.PaymentPaid,
			"refunded":      false,
		})
	}
	// Заказы пришли из SQL по убыванию даты, подписки дописаны в конец — общий
	// список сортируется заново.
	sort.SliceStable(purchases, func(i, j int) bool {
		return purchases[i]["createdAt"].(string) > purchases[j]["createdAt"].(string)
	})

	daily := make([]map[string]any, 0, len(buckets))
	for _, b := range buckets {
		daily = append(daily, map[string]any{
			"day":             b.day,
			"source":          b.source,
			"visitors":        b.visitors,
			"orders":          b.orders,
			"purchases":       b.purchases,
			"revenue":         b.revenue,
			"refunds":         b.refunds,
			"refundedRevenue": b.refundedRevenue,
		})
	}
	// Свежие дни сверху, внутри дня — источники по объёму трафика.
	sort.Slice(daily, func(i, j int) bool {
		di, dj := daily[i]["day"].(string), daily[j]["day"].(string)
		if di != dj {
			return di > dj
		}
		vi, vj := daily[i]["visitors"].(int), daily[j]["visitors"].(int)
		if vi != vj {
			return vi > vj
		}
		return daily[i]["source"].(string) < daily[j]["source"].(string)
	})

	// Итог за весь период — чтобы факт возврата был виден даже когда daily пуст
	// (нет визитов/заказов в периоде).
	totalRefunds, totalRefundedRevenue := 0, 0
	for _, b := range buckets {
		totalRefunds += b.refunds
		totalRefundedRevenue += b.refundedRevenue
	}

	// Порядок плеч фиксирован (как в админке), незнакомые id — в конец: плечо
	// могли выключить в коде, а данные по нему в БД остались.
	armOrder := append([]string{}, KnownFlowArms...)
	for id := range armStats {
		if !slices.Contains(armOrder, id) {
			armOrder = append(armOrder, id)
		}
	}
	arms := make([]map[string]any, 0, len(armStats))
	for _, id := range armOrder {
		b := armStats[id]
		if b == nil {
			continue
		}
		arms = append(arms, map[string]any{
			"arm":       id,
			"visits":    b.visits,
			"orders":    b.orders,
			"purchases": b.purchases,
			"revenue":   b.revenue,
		})
	}

	writeJSON(w, 200, map[string]any{
		"days":            days,
		"since":           sinceDay,
		"arms":            arms,
		"purchases":       purchases,
		"daily":           daily,
		"refunds":         totalRefunds,
		"refundedRevenue": totalRefundedRevenue,
		"subscriptions":   s.subscriptionAnalytics(r.Context(), selfHost),
		"partners":        s.partnerAnalytics(r.Context(), sinceDay),
	})
}

// subscriptionAnalytics — блок подписок для /admin/analytics: сколько подписок,
// откуда пришли (атрибуция оформления), сколько длятся и сколько отписок.
// Считается по всем подпискам, а не по периоду: их на порядки меньше заказов,
// а «сколько всего активных» — вопрос про текущее состояние, не про срез дней.
func (s *Server) subscriptionAnalytics(ctx context.Context, selfHost string) map[string]any {
	subs, err := store.ListSubscriptions(ctx, s.DB)
	if err != nil {
		log.Printf("list subscriptions for analytics: %v", err)
		return map[string]any{"error": "db"}
	}

	// «Оплаченных» — только подписки с реальным платежом ЮKassa и не из тестового
	// allowlist: pending без оплаты и тестовые активации в эту цифру не входят.
	paidIDs, err := store.PaidSubscriptionIDs(ctx, s.DB)
	if err != nil {
		log.Printf("paid subscriptions for analytics: %v", err)
		return map[string]any{"error": "db"}
	}

	now := time.Now().UTC()
	counts := map[string]int{}
	bySource := map[string]int{}
	paid := 0
	var durationDays float64
	durationN := 0
	items := make([]map[string]any, 0, len(subs))

	for _, sub := range subs {
		counts[sub.Status]++
		if paidIDs[sub.ID] && !store.IsSubscriptionTestEmail(ctx, s.DB, sub.Email) {
			paid++
		}

		var attr map[string]any
		if sub.AttributionJSON.Valid {
			_ = json.Unmarshal([]byte(sub.AttributionJSON.String), &attr)
		}
		source, campaign := attributionSource(attr, selfHost)
		if len(attr) == 0 {
			source = sourceUnknown
		}
		bySource[source]++

		// Длительность: для отменённых — от оформления до конца оплаченного
		// периода, для живых — до «сейчас». pending (не оплатили) не считается.
		days := 0.0
		if sub.Status != store.SubPending {
			end := now
			if sub.Status == store.SubCanceled {
				if au := sub.AccessUntilTime(); !au.IsZero() {
					end = au
				}
			}
			if end.After(sub.CreatedAt) {
				days = end.Sub(sub.CreatedAt).Hours() / 24
				durationDays += days
				durationN++
			}
		}

		items = append(items, map[string]any{
			"email":        sub.Email,
			"status":       sub.Status,
			"source":       source,
			"campaign":     nullStr(campaign),
			"createdAt":    sub.CreatedAt.Format(time.RFC3339),
			"accessUntil":  nullStr(sub.AccessUntil.String),
			"durationDays": int(days),
		})
	}

	sources := make([]map[string]any, 0, len(bySource))
	for src, n := range bySource {
		sources = append(sources, map[string]any{"source": src, "count": n})
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i]["count"].(int) != sources[j]["count"].(int) {
			return sources[i]["count"].(int) > sources[j]["count"].(int)
		}
		return sources[i]["source"].(string) < sources[j]["source"].(string)
	})

	avgDuration := 0
	if durationN > 0 {
		avgDuration = int(durationDays / float64(durationN))
	}

	return map[string]any{
		"total":           len(subs),
		"paid":            paid,
		"active":          counts[store.SubActive],
		"pastDue":         counts[store.SubPastDue],
		"pending":         counts[store.SubPending],
		"cancellations":   counts[store.SubCanceled],
		"avgDurationDays": avgDuration,
		"bySource":        sources,
		"items":           items,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newOrderID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "ord_" + hex.EncodeToString(b[:])
}

func orderSummary(o store.Order, ref string) map[string]any {
	var payload map[string]any
	_ = json.Unmarshal([]byte(o.InputPayloadJSON), &payload)

	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)

	discount := 0
	if o.DiscountAmount.Valid {
		discount = int(o.DiscountAmount.Int64)
	}
	dc := ""
	if o.DiscountCode.Valid {
		dc = o.DiscountCode.String
	}

	inv := ""
	if o.XenditInvoiceURL.Valid {
		inv = o.XenditInvoiceURL.String
	}

	return map[string]any{
		"id":               ref,
		"status":           o.Status,
		"deliveryStatus":   o.DeliveryStatus,
		"paymentStatus":    o.PaymentStatus,
		"inputPayload":     payload,
		"customer":         cust,
		"themeSlug":        nullStr(o.ThemeSlug.String),
		"xenditInvoiceUrl": nullStr(inv),
		"basePrice":        o.BasePrice,
		"discountCode":     nullStr(dc),
		"discountAmount":   discount,
	}
}

func jsonRecipient(raw string) string {
	var payload map[string]any
	_ = json.Unmarshal([]byte(raw), &payload)
	if v, ok := payload["recipientName"].(string); ok {
		return v
	}
	return ""
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func rubPaymentAmount(rub int) string {
	return strconv.Itoa(rub) + ".00"
}

// priceFromConfig — цена по умолчанию в валюте сайта (USD). Имя переменной
// осталось от mezmo.ru; на .com цены плеч задаются в /admin/settings.
func priceFromConfig(c config.Config) int {
	if v := strings.TrimSpace(os.Getenv("PAYMENT_AMOUNT_RUBLES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 25
}

func originalPriceFromConfig(c config.Config) int {
	if v := strings.TrimSpace(os.Getenv("ORIGINAL_AMOUNT_RUBLES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 75
}

func deliveryCfg() map[string]string {
	return map[string]string{
		"pageTitle":          "Ваша песня",
		"headerText":         "Спасибо за заказ!",
		"processingMessage":  "Генерируем трек…",
		"downloadButtonText": "Скачать MP3",
		"lyricsButtonText":   "Текст песни",
		"successMessage":     "Ваша персональная песня готова!",
		"accentColor":        "#D4AF37",
		"logoUrl":            "",
		"trackSubtitleText":  "",
		"unlockButtonText":   "",
	}
}

// PublicSettings returns the same payload as GET /api/public/settings. It is
// exported so the entrypoint can inline it into index.html as window.__SETTINGS__,
// letting the SPA render the real template on first paint (no API round-trip → LCP).
func PublicSettings(c config.Config) map[string]any {
	return publicSettingsResponse(c)
}

func publicSettingsResponse(c config.Config) map[string]any {
	base := priceFromConfig(c)
	original := originalPriceFromConfig(c)
	return map[string]any{
		"design":                    "gold",
		"defaultThemeSlug":          nil,
		"splitTestActive":           false,
		"emailEnabled":              true,
		"emailOtpEnabled":           false,
		"agreementEnabled":          false,
		"instantEnabled":            true,
		"deliveryDelayHours":        0,
		"manualConfirmationEnabled": false,
		"paymentsEnabled":           true,
		"paymentAmount":             base,
		"originalAmount":            original,
		"metaPixelId":               nil,
		"tiktokPixelId":             nil,
		"headScripts":               nil,
		"maintenanceEnabled":        false,
		"maintenanceHeadline":       nil,
		"maintenanceMessage":        nil,
		"maintenanceEmail":          nil,
		"maintenanceInstagramUrl":   nil,
		"maintenanceFollowText":     nil,
		"metaPixelWishlistId":       nil,
		"metaPixelStep1Script":      nil,
		"metaPixelStep4Script":      nil,
		"tiktokPixelStep1Script":    nil,
		"tiktokPixelStep4Script":    nil,
		"publicSiteConfig":          publicSiteConfigRU(),
	}
}

func publicSiteConfigRU() map[string]any {
	return map[string]any{
		"logoUrl": "/logo.svg",
		"footer": map[string]string{
			"email":       "support@mezmo.ru",
			"companyName": "Mezmo",
			"disclaimer":  "",
		},
		"colors": map[string]string{
			"accentColor":     "#D4AF37",
			"buttonColor":     "#D4AF37",
			"buttonTextColor": "#1A1918",
			"accentTextColor": "#1A1918",
			"bgColor1":        "#FDFBF7",
			"bgColor2":        "#FFFFFF",
		},
		"landing": map[string]any{
			"heroHeadline": map[string]string{
				"line1": "Она включила запись —",
				"line2": "и растрогалась на первой строчке",
			},
			"heroSubtext": "Персональная песня с историей и именем близкого. Текст и музыку создаёт ИИ, доставляем сразу — не через сутки.",
			"footerCta": map[string]string{
				"headline": "Подарите то, что запомнят навсегда",
				"subtitle": "Тысячи людей уже увидели искреннюю благодарность в глазах тех, кому сделали такой подарок. Ваша очередь.",
			},
			"heroMedia": map[string]any{
				"imageUrl":          "/images/hero-poster.jpg",
				"videoUrl":          "/hero.mp4",
				"mode":              "video",
				"videoBelowPrice":   false,
				"videoThumbnailUrl": "",
			},
			"heroOverlay": map[string]string{
				"quote":           "Не дослушал до конца припева…",
				"authorName":      "Марина",
				"authorLocation":  "Москва",
				"authorAvatarUrl": "https://images.unsplash.com/photo-1530254541043-129f4c372200?w=200&h=200&fit=crop",
			},
			"heroPlayer": map[string]any{
				"enabled":           true,
				"playingBadgeText":  "Сейчас",
				"cornerBadgeText":   "Спецпредложение",
				"verifiedBadgeText": "Проверенный заказ",
				"quote":             "Сдерживался — не получилось.",
				"authorName":        "Александр",
				"authorSubline":     "СПб • 2026",
				"authorAvatarUrl":   "https://images.unsplash.com/photo-1530254541043-129f4c372200?w=200&h=200&fit=crop",
				"audioUrl":          nil,
			},
			"audioSamples": map[string]any{
				"nowPlaying": map[string]any{
					"name":         "Для Анны — моя любовь",
					"quote":        "Каждый наш вечер, с того неловкого первого свидания…",
					"time":         "3:23",
					"metaText":     "Поп-баллада",
					"audioUrl":     "/audio/sample-anna.mp3",
					"thumbnailUrl": nil,
				},
				"playlist": []map[string]any{
					{
						"title":        "Для Максима — 10 лет вместе",
						"subtitle":     "Акустика • 3:20",
						"ctaLabel":     "СЛУШАТЬ",
						"audioUrl":     "/audio/sample-maxim.mp3",
						"thumbnailUrl": nil,
					},
					{
						"title":        "Для Оли — это наша история",
						"subtitle":     "Поп • 3:29",
						"ctaLabel":     "СЛУШАТЬ",
						"audioUrl":     "/audio/sample-olya.mp3",
						"thumbnailUrl": nil,
					},
				},
			},
		},
		"statsBar": map[string]any{
			"items": []map[string]string{
				{"val": "96%", "label": "получателей растрогались"},
				{"val": "12 400+", "label": "песен создано"},
				{"val": "10–30 мин", "label": "до готовой песни"},
				{"val": "4.9 ★", "label": "средняя оценка"},
			},
		},
		"trustBadges": map[string]string{
			"badge1": "★★★★★ 12 400+ песен",
			"badge2": "10–30 минут",
			"badge3": "Возврат 100%",
		},
		"heroCheckmarks": []string{"Студийное качество", "Быстрая генерация", "Файл для скачивания"},
		"miscText": map[string]string{
			"heroTagline":         "",
			"heroChipsHeadline":   "Для кого песня?",
			"heroCtaButtonText":   "Создать свою песню",
			"ctaButtonText":       "Создать свою песню",
			"heroStarLine":        "★★★★★ 12 400+ песен",
			"mobileCtaQuotaBadge": "",
		},
		"howItWorksSection": map[string]any{
			"label":           "Просто",
			"headline":        "Три шага к незабываемому моменту",
			"backgroundColor": "#030712",
			"ctaButtonText":   "Создать свою песню",
			"steps": []map[string]string{
				{"icon": "✍️", "title": "Расскажите историю", "desc": "3 минуты на заполнение формы"},
				{"icon": "🎵", "title": "Создаём песню", "desc": "Текст и музыку создаёт ИИ"},
				{"icon": "📧", "title": "Доставляем на почту", "desc": "Менее чем за 24 часа"},
			},
		},
		"faqSection": map[string]any{
			"headline": "Частые вопросы",
			"items": []map[string]string{
				{"q": "Используете ли вы настоящих авторов?", "a": "Да, каждую песню пишет живой человек — профессиональный автор. ИИ помогает с мелодией и аранжировкой."},
				{"q": "Сколько ждать готовую песню?", "a": "Обычно 10–30 минут. Срок может быть дольше в часы пиков."},
				{"q": "Есть ли гарантия?", "a": "Да, возврат 100%, если не понравится. Дорабатываем бесплатно, пока не станет ок."},
				{"q": "Какие жанры доступны?", "a": "Поп-баллада, акустика, рок и другие. Выбираете при оформлении заказа."},
				{"q": "Это не просто ИИ без участия человека?", "a": "Живой автор пишет слова под вашу историю, ИИ помогает с музыкой. Не шаблон, индивидуальная песня."},
				{"q": "Можно подарить на любой праздник?", "a": "Да, подходит для дня рождения, годовщины, предложения, выпускного, просто так. Любой повод — главное история."},
			},
		},
		"reviews": map[string]any{
			"sectionLabel":    "Реакции получателей",
			"sectionHeadline": "Говорят наши клиенты",
			"sectionSubtext":  "2 847+ довольных клиентов",
			"items": []map[string]any{
				{
					"style":      "image",
					"imageUrl":   "/images/review-telegram-galya.png",
					"authorName": "Галя • Telegram",
				},
				{
					"style":      "image",
					"imageUrl":   "/images/review-whatsapp-proposal.png",
					"authorName": "Дмитрий Р. • WhatsApp",
				},
				{
					"style":      "image",
					"imageUrl":   "/images/review-max-olga.png",
					"authorName": "Ольга В. • MAX",
				},
			},
		},
		"promoBanner": map[string]any{
			"enabled":             false,
			"countdownLabel":      "🎵 Специальное предложение",
			"countdownTargetDate": "2027-02-14",
			"promoBadgeText":      "🎁 Скидка для новых клиентов",
			"evergreenEnabled":    false,
			"evergreenCycleHours": 24,
		},
		"upsell": map[string]string{"upsellMode": "post-payment"},
	}
}

// handleAudioProxy streams an audio file from MinIO for the given order and track index.
func (s *Server) handleAudioProxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	indexStr := r.PathValue("index")
	idx, err := strconv.Atoi(indexStr)
	if err != nil || idx < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound || o.PaymentStatus != "paid" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// Ссылка на полный трек могла попасть в кэш до начала задержки: пока она не
	// вышла, прокси молчит. Отрывок живёт на отдельной ручке и остаётся доступен.
	if store.DeliveryHeld(o, time.Now()) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var urls []string
	_ = json.Unmarshal([]byte(o.AudioURLsJSON), &urls)
	if idx >= len(urls) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	key := urls[idx]
	if !strings.HasPrefix(key, "orders/") || s.Storage == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	obj, info, err := s.Storage.GetObject(ctx, key)
	if err != nil {
		log.Printf("minio get error order %s key %s: %v", id, key, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer obj.Close()

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	// Минута, а не час: ссылка меняется вместе с номером прогона (publicAudioURL),
	// но содержимое одного прогона может быть перезаписано и без него — например
	// когда по заказу приходит второй колбэк Kie.ai. Короткий срок оставляет кэш
	// для перемотки внутри прослушивания и не позволяет застрять на прошлой песне.
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = io.Copy(w, obj)
}
