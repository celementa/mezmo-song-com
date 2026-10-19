package httpserver

// Партнёрская воронка Les Roses: песню продают допником в корзине их магазина
// на InSales, оплачивает покупатель там же — вместе с букетом. Mezmo в оплате
// не участвует: к нам приходит уже оплаченный заказ без анкеты, покупатель
// получает письмо со ссылкой /order/{ref} и рассказывает историю у нас.
//
// Источник правды о деньгах — InSales. Вебхук от него не подписан, поэтому его
// тело — только сигнал «посмотри заказ N»: сам заказ всегда перечитывается
// через API нашим ключом. Тот же код гоняет фоновая сверка, чтобы потерянный
// вебхук не означал потерянную песню (как с ЮKassa в reconcile.go).
//
// A/B «блок в корзине / попап перед оформлением» различается вариантом товара:
// у песни два варианта с артикулами MEZMO-CART и MEZMO-POPUP, плечо читается
// из строки заказа. Показы и клики шлёт сниппет корзины (handleLesrosesEvent).

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mezmo-song-com/internal/insales"
	"mezmo-song-com/internal/store"
)

const (
	lesrosesSKUCart  = "MEZMO-CART"
	lesrosesSKUPopup = "MEZMO-POPUP"
	// lesrosesSongPrice — цена песни в магазине партнёра; берётся для ручных
	// заказов, у вебхучных — фактическая цена строки заказа.
	lesrosesSongPrice = 990

	emailLesrosesLink     = "lesroses_link"
	emailLesrosesReminder = "lesroses_reminder"

	lesrosesSyncInterval = 10 * time.Minute
	// lesrosesSyncWindow — насколько назад смотрит сверка: вебхук мог потеряться
	// при рестарте контейнера, а InSales мог не ретраить.
	lesrosesSyncWindow     = 48 * time.Hour
	lesrosesFirstReminder  = 24 * time.Hour
	lesrosesSecondReminder = 72 * time.Hour
)

// insalesCanceled — статусы исполнения InSales, при которых заказ считается
// отменённым или возвращённым.
var insalesCanceled = map[string]bool{"declined": true, "returning": true, "returned": true}

// partnerOf — партнёр, продавший заказ; пусто у наших собственных заказов.
func partnerOf(o store.Order) string {
	if o.Flow.String == store.FlowLesroses {
		return store.FlowLesroses
	}
	return ""
}

// isPartnerOrder — то же по id; ошибка чтения считается «не партнёрский».
func (s *Server) isPartnerOrder(id string) bool {
	o, err := store.GetOrder(context.Background(), s.DB, id)
	return err == nil && partnerOf(o) != ""
}

func (s *Server) insalesClient() insales.Client {
	return insales.Client{BaseURL: s.Config.InsalesShopURL, ID: s.Config.InsalesAPIID, Password: s.Config.InsalesAPIPassword}
}

func (s *Server) registerLesroses(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/partners/lesroses/webhook", s.handleLesrosesWebhook)
	mux.HandleFunc("POST /api/public/partners/lesroses/event", s.handleLesrosesEvent)
	mux.HandleFunc("OPTIONS /api/public/partners/lesroses/event", s.handleLesrosesEvent)
	mux.HandleFunc("POST /api/public/order/{id}/brief-progress", s.handleBriefProgress)
	mux.HandleFunc("GET /api/admin/lesroses/orders", s.handleLesrosesOrders)
	mux.HandleFunc("GET /api/admin/lesroses/orders.csv", s.handleLesrosesOrdersCSV)
	mux.HandleFunc("POST /api/admin/lesroses/orders", s.handleLesrosesManualOrder)
}

// briefSteps — экранов в анкете партнёрского заказа (PartnerBrief.tsx).
const briefSteps = 4

// handleBriefProgress — отметка с анкеты партнёрского заказа: страница открыта
// и покупатель на шаге step (1..briefSteps). Пишет страница, а не переход по
// ссылке, поэтому почтовые сканеры ссылок (они JS не исполняют) прогресс не
// рисуют. Фоновый пинг: на любую ошибку 204, анкете он не мешает.
func (s *Server) handleBriefProgress(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)
	var body struct {
		Step int `json:"step"`
	}
	if err := readJSON(r, &body); err != nil || body.Step < 1 || body.Step > briefSteps {
		return
	}
	id, ok := s.resolveOrderID(r.Context(), r.PathValue("id"))
	if !ok {
		return
	}
	o, err := store.GetOrder(r.Context(), s.DB, id)
	// Только пока анкета ждёт ответов: после отправки шаги уже ничего не значат.
	if err != nil || partnerOf(o) == "" || o.Status != "created" || hasSongConfig(o) {
		return
	}
	if err := store.RecordBriefProgress(r.Context(), s.DB, id, body.Step, time.Now()); err != nil {
		log.Printf("[lesroses] brief-progress order=%s: %v", id, err)
	}
}

// lesrosesSong ищет песню в заказе InSales: плечо по артикулу и цену строки.
func lesrosesSong(o insales.Order) (arm string, price int, ok bool) {
	for _, l := range o.OrderLines {
		switch strings.ToUpper(strings.TrimSpace(l.LineSKU())) {
		case lesrosesSKUCart:
			arm = store.ArmLesrosesCart
		case lesrosesSKUPopup:
			arm = store.ArmLesrosesPopup
		default:
			continue
		}
		price = int(math.Round(l.SalePrice))
		if price <= 0 {
			price = lesrosesSongPrice
		}
		return arm, price, true
	}
	return "", 0, false
}

// Исход обработки заказа InSales — для логов, ответа вебхука и тестов.
const (
	lrNoSong        = "no_song"
	lrUnpaid        = "unpaid"
	lrCreated       = "created"
	lrExists        = "exists"
	lrCanceled      = "canceled"        // доступ закрыт
	lrCancelNoted   = "cancel_noted"    // отмена записана, песню уже делали — доступ оставлен
	lrCancelNoOrder = "cancel_no_order" // отменили до того, как заказ дошёл до нас
)

// syncLesrosesOrder доводит наше состояние до заказа InSales. Идемпотентна:
// вебхук, его повтор и сверка могут прийти по одному заказу сколько угодно раз.
func (s *Server) syncLesrosesOrder(ctx context.Context, o insales.Order) (string, error) {
	arm, price, ok := lesrosesSong(o)
	if !ok {
		return lrNoSong, nil
	}
	number := strconv.FormatInt(o.Number, 10)

	if insalesCanceled[o.FulfillmentStatus] {
		id, err := store.PartnerOrderIDByNumber(ctx, s.DB, store.FlowLesroses, number)
		if errors.Is(err, store.ErrNotFound) {
			return lrCancelNoOrder, nil
		}
		if err != nil {
			return "", err
		}
		closed, err := store.MarkPartnerCanceled(ctx, s.DB, id)
		if err != nil {
			return "", err
		}
		if closed {
			return lrCanceled, nil
		}
		return lrCancelNoted, nil
	}

	if o.FinancialStatus != "paid" {
		return lrUnpaid, nil
	}
	created, _, err := s.createLesrosesOrder(ctx, store.PartnerOrderRef{
		PartnerID: strconv.FormatInt(o.ID, 10),
		Number:    number,
	}, o.Client.Email, arm, price)
	if err != nil {
		return "", err
	}
	if created {
		return lrCreated, nil
	}
	return lrExists, nil
}

// createLesrosesOrder заводит оплаченный заказ без анкеты и шлёт ссылку на неё.
// Генерация не запускается: она стартует из POST /api/orders/{id}/config, когда
// покупатель ответит на вопросы (тот же путь, что у плеча «оплата в начале»).
func (s *Server) createLesrosesOrder(ctx context.Context, ref store.PartnerOrderRef, email, arm string, price int) (created bool, id string, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	customer, _ := json.Marshal(map[string]any{"email": email})
	o := store.Order{
		ID:               newOrderID(),
		Status:           "created",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    store.PaymentPaid,
		InputPayloadJSON: "{}",
		CustomerJSON:     string(customer),
		BasePrice:        price,
		Flow:             sql.NullString{String: store.FlowLesroses, Valid: true},
		FunnelStage:      sql.NullString{String: store.StagePaid, Valid: true},
		FlowVariant:      sql.NullString{String: arm, Valid: true},
		CreatedAt:        time.Now(),
	}
	created, id, err = store.CreatePartnerOrder(ctx, s.DB, o, ref)
	if err != nil || !created {
		return created, id, err
	}
	log.Printf("[lesroses] order №%s → %s arm=%s price=%d email=%q", ref.Number, id, arm, price, email)
	if email == "" {
		log.Printf("[lesroses] order %s: у заказа InSales нет email — ссылку отправить некуда, выдать из кабинета", id)
		return created, id, nil
	}
	go s.sendLesrosesEmail(context.Background(), emailLesrosesLink, id, email, ref.Number)
	return created, id, nil
}

// sendLesrosesEmail — письмо со ссылкой на анкету (первое и напоминания).
func (s *Server) sendLesrosesEmail(ctx context.Context, kind, orderID, email, number string) {
	if s.Config.SMTPHost == "" {
		log.Printf("[lesroses] mail order=%s: SMTP_HOST not set, skip %s", orderID, kind)
		return
	}
	link := s.publicOrderURL(orderID)
	numberLine := ""
	if number != "" {
		numberLine = " №" + number
	}
	var subject, body string
	switch kind {
	case emailLesrosesLink:
		subject = "Песня к букету из «Во Имя Розы»: расскажите историю"
		body = "Здравствуйте!\n\n" +
			"Спасибо за заказ" + numberLine + " в Les Roses. К букету вы добавили персональную песню от Mezmo.\n\n" +
			"Чтобы мы её написали, ответьте на несколько вопросов: для кого песня, какой повод, что вас связывает. Это займёт пару минут:\n" +
			link + "\n\n" +
			"Песня будет готова через 10–30 минут после ваших ответов и откроется по этой же ссылке — её можно переслать тому, кому она посвящена.\n\n" +
			"Если это письмо оказалось в «Спаме», отметьте его «Не спам» — тогда письмо с готовой песней придёт во «Входящие»."
	default:
		subject = "Песня к букету ждёт вашей истории"
		body = "Здравствуйте!\n\n" +
			"К заказу" + numberLine + " в Les Roses вы добавили персональную песню, но пока не рассказали, о ком она. " +
			"Без этого мы не можем её написать.\n\n" +
			"Ответьте на несколько вопросов — песня будет готова через 10–30 минут:\n" +
			link
	}
	vars := map[string]string{"brief_url": link, "order_number": number}
	if err := s.sendEmail(ctx, kind, emailTarget{OrderID: orderID}, email, subject, body, vars); err != nil {
		log.Printf("[lesroses] mail order=%s %s err=%v", orderID, kind, err)
		return
	}
	log.Printf("[lesroses] mail order=%s %s sent to=%s", orderID, kind, email)
}

// --- вебхук InSales ---

func (s *Server) handleLesrosesWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.insalesClient().Configured() || s.Config.LesrosesWebhookToken == "" {
		writeJSON(w, 503, map[string]string{"error": "lesroses_disabled"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.Config.LesrosesWebhookToken)) != 1 {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	// InSales присылает заказ целиком; верим в нём только id — остальное
	// перечитываем. Обёртку {"order": {...}} тоже понимаем на всякий случай.
	var body struct {
		ID    int64 `json:"id"`
		Order struct {
			ID int64 `json:"id"`
		} `json:"order"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	id := body.ID
	if id == 0 {
		id = body.Order.ID
	}
	if id == 0 {
		writeJSON(w, 400, map[string]string{"error": "no_order_id"})
		return
	}
	o, err := s.insalesClient().GetOrder(ctx, id)
	if errors.Is(err, insales.ErrNotFound) {
		log.Printf("[lesroses] webhook order=%d: нет в InSales, пропуск", id)
		writeJSON(w, 200, map[string]string{"outcome": "not_found"})
		return
	}
	if err != nil {
		// 5xx — пусть InSales повторит; сверка тоже подберёт.
		log.Printf("[lesroses] webhook order=%d: InSales API err=%v", id, err)
		writeJSON(w, 502, map[string]string{"error": "insales_unavailable"})
		return
	}
	outcome, err := s.syncLesrosesOrder(ctx, o)
	if err != nil {
		log.Printf("[lesroses] webhook order=%d №%d: err=%v", id, o.Number, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if outcome != lrNoSong {
		log.Printf("[lesroses] webhook order=%d №%d financial=%s fulfillment=%s → %s", id, o.Number, o.FinancialStatus, o.FulfillmentStatus, outcome)
	}
	writeJSON(w, 200, map[string]string{"outcome": outcome})
}

// --- фоновые джобы: сверка с InSales и напоминания про анкету ---

// StartLesrosesJobs — сверка заказов InSales (страховка от потерянного
// вебхука) и напоминания заполнить анкету. Оба шага идемпотентны.
func (s *Server) StartLesrosesJobs(ctx context.Context) {
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			s.runLesrosesJobsOnce(ctx, time.Now())
			timer.Reset(lesrosesSyncInterval)
		}
	}()
}

func (s *Server) runLesrosesJobsOnce(ctx context.Context, now time.Time) {
	if s.insalesClient().Configured() {
		s.reconcileLesroses(ctx, now)
	}
	s.sendBriefReminders(ctx, now)
}

// reconcileLesroses проходит заказы InSales, изменённые за окно сверки.
func (s *Server) reconcileLesroses(ctx context.Context, now time.Time) {
	since := now.Add(-lesrosesSyncWindow)
	counts := map[string]int{}
	for page := 1; page <= 50; page++ {
		orders, err := s.insalesClient().OrdersUpdatedSince(ctx, since, page)
		if err != nil {
			log.Printf("[lesroses] reconcile page=%d: InSales API err=%v", page, err)
			return
		}
		for _, o := range orders {
			outcome, err := s.syncLesrosesOrder(ctx, o)
			if err != nil {
				log.Printf("[lesroses] reconcile №%d err=%v", o.Number, err)
				continue
			}
			counts[outcome]++
		}
		if len(orders) < 100 {
			break
		}
	}
	// В логе только то, что сверка реально поправила: «exists» — штатный шум.
	if counts[lrCreated]+counts[lrCanceled]+counts[lrCancelNoted] > 0 {
		log.Printf("[lesroses] reconcile: created=%d canceled=%d cancel_noted=%d (вебхук их пропустил)",
			counts[lrCreated], counts[lrCanceled], counts[lrCancelNoted])
	}
}

func (s *Server) sendBriefReminders(ctx context.Context, now time.Time) {
	due, err := store.DueBriefReminders(ctx, s.DB, store.FlowLesroses, now, lesrosesFirstReminder, lesrosesSecondReminder)
	if err != nil {
		log.Printf("[lesroses] reminders: err=%v", err)
		return
	}
	for _, d := range due {
		if d.Email == "" {
			continue
		}
		claimed, err := store.ClaimBriefReminder(ctx, s.DB, d.ID, d.N)
		if err != nil || !claimed {
			continue
		}
		s.sendLesrosesEmail(ctx, emailLesrosesReminder, d.ID, d.Email, s.lesrosesNumber(ctx, d.ID))
	}
}

func (s *Server) lesrosesNumber(ctx context.Context, id string) string {
	var n string
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(partner_order_number, '') FROM orders WHERE id = ?`, id).Scan(&n)
	return n
}

// --- события сниппета корзины ---

var lesrosesEventArms = map[string]bool{"cart": true, "popup": true}
var lesrosesEvents = map[string]bool{store.PartnerEventShown: true, store.PartnerEventAdd: true, store.PartnerEventDecline: true}

// handleLesrosesEvent — показ/клик блока песни на сайте Les Roses. Фоновый
// пинг (navigator.sendBeacon): отвечает 204 на всё, лишь бы не мешать корзине.
func (s *Server) handleLesrosesEvent(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		for _, allowed := range s.Config.LesrosesOrigins {
			if strings.EqualFold(origin, allowed) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "POST")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Vary", "Origin")
				break
			}
		}
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// sendBeacon шлёт text/plain (без preflight), поэтому тип не проверяем.
	var body struct {
		SID   string `json:"sid"`
		Arm   string `json:"arm"`
		Event string `json:"event"`
	}
	if err := readJSON(r, &body); err == nil &&
		lesrosesEventArms[body.Arm] && lesrosesEvents[body.Event] &&
		len(body.SID) >= 8 && len(body.SID) <= 64 {
		if err := store.RecordPartnerEvent(r.Context(), s.DB, store.FlowLesroses, body.SID, body.Arm, body.Event, time.Now()); err != nil {
			log.Printf("[lesroses] event err=%v", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// partnerAnalytics — блок «Партнёры» в /admin/analytics: воронка Les Roses по
// плечам за тот же период. Ошибка не роняет всю аналитику — блок просто пуст.
func (s *Server) partnerAnalytics(ctx context.Context, sinceDay string) map[string]any {
	funnel, err := store.PartnerFunnel(ctx, s.DB, store.FlowLesroses, sinceDay)
	if err != nil {
		log.Printf("[lesroses] analytics err=%v", err)
		funnel = nil
	}
	return map[string]any{"lesroses": funnel}
}

// --- кабинет партнёра ---

// lesrosesState — состояние заказа для людей: что сейчас с песней.
func lesrosesState(r store.PartnerOrderRow) string {
	switch {
	case r.PaymentStatus == store.PaymentRefunded:
		return "canceled"
	case r.Status == "completed":
		return "ready"
	case r.Status == "processing":
		return "generating"
	case strings.TrimSpace(r.RecipientName) == "":
		return "awaiting_brief"
	}
	return "generating"
}

func lesrosesSinceDays(r *http.Request) (time.Time, int) {
	days := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 && n <= 366 {
		days = n
	}
	y, m, d := time.Now().UTC().AddDate(0, 0, -(days - 1)).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), days
}

func (s *Server) lesrosesRows(r *http.Request) ([]store.PartnerOrderRow, time.Time, int, error) {
	since, days := lesrosesSinceDays(r)
	rows, err := store.ListPartnerOrders(r.Context(), s.DB, store.FlowLesroses, since, r.URL.Query().Get("q"), 1000)
	return rows, since, days, err
}

func (s *Server) handleLesrosesOrders(w http.ResponseWriter, r *http.Request) {
	rows, since, days, err := s.lesrosesRows(r)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	funnel, err := store.PartnerFunnel(r.Context(), s.DB, store.FlowLesroses, since.Format("2006-01-02"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":         row.ID,
			"publicRef":  publicOrderRef(s.Config.WebhookSecret, row.ID),
			"url":        s.publicOrderURL(row.ID),
			"number":     row.Number,
			"insalesId":  row.PartnerID,
			"email":      row.Email,
			"arm":        strings.TrimPrefix(row.Arm, store.FlowLesroses+"_"),
			"state":      lesrosesState(row),
			"recipient":  row.RecipientName,
			"reminders":  row.BriefReminders,
			"canceledAt": nullStr(row.CanceledAt),
			"createdAt":  row.CreatedAt.UTC().Format(time.RFC3339),
			"brief": map[string]any{
				"openedAt": nullStr(row.BriefOpenedAt),
				"step":     row.BriefStep,
				"stepAt":   nullStr(row.BriefStepAt),
				"steps":    briefSteps,
			},
			"mail": map[string]any{
				"sent":          row.MailSent,
				"tracked":       row.MailTracked,
				"delivered":     row.MailDelivered,
				"problem":       row.MailProblem,
				"openedAt":      nullStr(row.MailOpenedAt),
				"openedHumanAt": nullStr(row.MailOpenedHumanAt),
			},
		})
	}
	writeJSON(w, 200, map[string]any{
		"orders":            out,
		"funnel":            funnel,
		"days":              days,
		"insalesConfigured": s.insalesClient().Configured(),
	})
}

var lesrosesStateTitle = map[string]string{
	"awaiting_brief": "ждём анкету",
	"generating":     "песня создаётся",
	"ready":          "песня готова",
	"canceled":       "отменён",
}

func (s *Server) handleLesrosesOrdersCSV(w http.ResponseWriter, r *http.Request) {
	rows, _, days, err := s.lesrosesRows(r)
	if err != nil {
		http.Error(w, "db", 500)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="lesroses-%dd.csv"`, days))
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM — чтобы Excel открыл кириллицу
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	_ = cw.Write([]string{"Дата (UTC)", "№ заказа Les Roses", "Email", "Вариант", "Статус песни", "Отменён в InSales", "Ссылка"})
	for _, row := range rows {
		_ = cw.Write([]string{
			row.CreatedAt.UTC().Format("2006-01-02 15:04"),
			row.Number, row.Email,
			strings.TrimPrefix(row.Arm, store.FlowLesroses+"_"),
			lesrosesStateTitle[lesrosesState(row)],
			row.CanceledAt,
			s.publicOrderURL(row.ID),
		})
	}
	cw.Flush()
}

var lesrosesNumberRe = regexp.MustCompile(`^\d{1,12}$`)

// handleLesrosesManualOrder — ручное создание по номеру заказа InSales: вебхук
// не дошёл или песню продали по телефону. Дубль по номеру не создаётся.
func (s *Server) handleLesrosesManualOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Number string `json:"number"`
		Email  string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	number := strings.TrimPrefix(strings.TrimSpace(body.Number), "№")
	if !lesrosesNumberRe.MatchString(number) {
		writeJSON(w, 400, map[string]string{"error": "bad_number"})
		return
	}
	email := strings.TrimSpace(body.Email)
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \r\n") {
		writeJSON(w, 400, map[string]string{"error": "bad_email"})
		return
	}
	created, id, err := s.createLesrosesOrder(r.Context(), store.PartnerOrderRef{Number: number}, email, store.ArmLesrosesManual, lesrosesSongPrice)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	resp := map[string]any{"created": created, "publicRef": publicOrderRef(s.Config.WebhookSecret, id), "url": s.publicOrderURL(id)}
	if !created {
		resp["error"] = "exists"
		writeJSON(w, 409, resp)
		return
	}
	writeJSON(w, 200, resp)
}
