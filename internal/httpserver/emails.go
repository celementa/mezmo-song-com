package httpserver

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"mezmo-song-com/internal/mail"
	"mezmo-song-com/internal/store"
)

// Реестр писем, которые шлёт сервер. Единственное место, где перечислены все
// письма: страница /admin/emails рисует тумблеры и подсказки из него, а
// sendEmail отказывается отправлять письмо, которого здесь нет. Новое письмо —
// новая запись здесь, иначе его не видно в админке и не выключить.
//
// Locked — письмо нельзя выключить: без него ломается сам продукт (человек не
// получит песню или не сможет отменить подписку), а не только коммуникация.
//
// Vars — подстановки, которые письмо передаёт в шаблон Unisender Go
// ({{order_url}} и т.п.), если на /admin/emails для него задан шаблон. Список
// обязан совпадать с тем, что передаёт место отправки: по нему верстают
// шаблон, а опечатка в имени молча оставит в письме пустое место.
type emailKind struct {
	Key     string     `json:"key"`
	Group   string     `json:"group"`
	Title   string     `json:"title"`
	Subject string     `json:"subject"`
	When    string     `json:"when"`
	Why     string     `json:"why"`
	Locked  bool       `json:"locked"`
	Vars    []emailVar `json:"vars"`
}

// emailVar — подстановка шаблона: имя ({{name}}) и что в ней лежит.
type emailVar struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// commonEmailVars — подстановки, которые sendEmail добавляет в каждое письмо.
var commonEmailVars = []emailVar{
	{"site_url", "Адрес сайта: https://mezmo.ru"},
	{"support_email", "Адрес поддержки: support@mezmo.ru"},
}

const (
	emailOrderStarted    = "order_started"
	emailCreateStarted   = "create_started"
	emailCreatePreview   = "create_preview"
	emailPaymentReceived = "payment_received"
	emailSongReady       = "song_ready"

	emailSubWelcome      = "sub_welcome"
	emailSubGift         = "sub_gift"
	emailSubReminder     = "sub_reminder"
	emailSubCharged      = "sub_charged"
	emailSubChargeFailed = "sub_charge_failed"
	emailSubExpired      = "sub_expired"
	emailSubCancelLink   = "sub_cancel_link"
)

var emailKinds = []emailKind{
	{
		Key: emailOrderStarted, Group: "Заказ", Title: "Начали работу (/config)",
		Subject: "Мы взялись за вашу песню",
		When:    "Воронка /config: человек ввёл email на шаге 3, до оплаты. Уходит один раз на заказ.",
		Why:     "Ссылка на страницу заказа — чтобы вернуться к оформлению с любого устройства, если закрыл вкладку.",
		Vars:    []emailVar{{"order_url", "Страница заказа: вернуться к оформлению с любого устройства"}},
	},
	{
		Key: emailCreateStarted, Group: "Заказ", Title: "Начало положено (/create, оплата в начале)",
		Subject: "Ваша персональная песня — начало положено",
		When:    "Воронка /create, плечо «оплата в начале»: человек ввёл email первым шагом.",
		Why:     "Ссылка «продолжить оформление» — брошенную корзину можно дожать.",
		Vars:    []emailVar{{"resume_url", "Продолжить оформление в /create"}},
	},
	{
		Key: emailCreatePreview, Group: "Заказ", Title: "Превью готово (/create)",
		Subject: "Ваша песня — слушайте по ссылке",
		When:    "Воронка /create, плечо «оплата поздно»: человек ввёл email после бесплатной генерации. Только если превью действительно было.",
		Why:     "Ссылка на готовое превью и отрывок, которым можно поделиться — возвращает к оплате.",
		Vars:    []emailVar{{"song_url", "Готовое превью песни"}, {"share_url", "Отрывок, которым можно поделиться"}},
	},
	{
		Key: emailPaymentReceived, Group: "Заказ", Title: "Оплата получена",
		Subject: "Оплата получена",
		When:    "Вебхук ЮKassa об оплате, когда песня ещё не готова (иначе вместо него уходит «Ваша песня готова» с благодарностью за оплату).",
		Why:     "Подтверждение, что деньги дошли. В воронке «оплата в начале» — единственная ссылка дописать историю: без неё оплативший и закрывший вкладку песню не получит.",
		Vars: []emailVar{{"next_url", "Куда вести: дописать историю, если её нет (needs_story), иначе страница заказа"},
			{"needs_story", "«1», если оплатили в /create до анкеты и история ещё не рассказана, иначе пусто"},
			{"order_url", "Страница заказа"}},
	},
	{
		Key: emailSongReady, Group: "Заказ", Title: "Песня готова",
		Subject: "Ваша песня готова",
		When:    "Заказ оплачен и песня сгенерирована (и вышла задержка доставки, если включена). Один раз на заказ — доработки письма не шлют.",
		Why:     "Главное письмо продукта: ссылка на песню. Кто платил через приложение банка, на сайт не вернулся и узнаёт о песне только отсюда.",
		Locked:  true,
		Vars:    []emailVar{{"song_url", "Страница песни: слушать и скачать"}, {"intro", "«Спасибо, оплата получена!» / «Спасибо, подписка оформлена!», если письмо уходит сразу после оплаты, иначе пусто"}},
	},

	{
		Key: emailSubWelcome, Group: "Подписка", Title: "Подписка оформлена",
		Subject: "Подписка оформлена",
		When:    "Первый платёж подписки прошёл (/subscribe, «подписка вместо разовой») или подписка «0 ₽ сегодня» включена по карте из заказа.",
		Why:     "Дата и сумма первого списания, ссылка на кабинет и на отмену. Ссылка на отмену в письмах — условие ЮKassa для рекуррентных платежей.",
		Vars:    []emailVar{{"account_url", "Личный кабинет"}, {"cancel_url", "Отмена подписки"}, {"price", "Сумма списания, ₽, числом"}, {"next_charge_date", "Дата следующего списания, ДД.ММ.ГГГГ"}, {"trial", "«1», если подписка «0 ₽ сегодня» (первое списание через месяц), иначе пусто"}},
	},
	{
		Key: emailSubGift, Group: "Подписка", Title: "Подарочный месяц",
		Subject: "Дарим вам месяц безлимита песен",
		When:    "Админ подарил месяц безлимита по заказу (кнопка в /admin/orders).",
		Why:     "Сообщает о подарке и даёт вход в кабинет — без письма человек о подарке не узнает.",
		Vars:    []emailVar{{"account_url", "Личный кабинет"}, {"access_until", "До какой даты действует подарок, ДД.ММ.ГГГГ"}, {"price", "Цена продления, ₽ в месяц"}, {"renew_url", "Продлить безлимит"}},
	},
	{
		Key: emailSubReminder, Group: "Подписка", Title: "Напоминание о списании",
		Subject: "Скоро списание по подписке",
		When:    "За 3 дня до автосписания. Выключенное считается отработанным и не повторяется.",
		Why:     "Человек успевает отменить до списания — меньше возвратов и чарджбэков. Выключать осознанно: это единственное письмо перед списанием.",
		Vars:    []emailVar{{"next_charge_date", "Дата списания, ДД.ММ.ГГГГ"}, {"price", "Сумма списания, ₽"}, {"cancel_url", "Отмена подписки"}, {"account_url", "Личный кабинет"}},
	},
	{
		Key: emailSubCharged, Group: "Подписка", Title: "Подписка продлена",
		Subject: "Подписка продлена",
		When:    "Автосписание за следующий месяц прошло.",
		Why:     "Подтверждение списания и ссылка на отмену. Чек 54-ФЗ ЮKassa присылает отдельно и независимо от этого письма.",
		Vars:    []emailVar{{"price", "Списанная сумма, ₽"}, {"next_charge_date", "Следующее списание, ДД.ММ.ГГГГ"}, {"account_url", "Личный кабинет"}, {"cancel_url", "Отмена подписки"}},
	},
	{
		Key: emailSubChargeFailed, Group: "Подписка", Title: "Списание не прошло",
		Subject: "Не удалось продлить подписку",
		When:    "Банк отклонил автосписание. Повтор раз в 7 дней, через 30 дней подписка закрывается.",
		Why:     "Человек может пополнить или сменить карту, пока доступ не закрылся.",
		Locked:  true,
		Vars:    []emailVar{{"price", "Сумма, которую не удалось списать, ₽"}, {"access_until", "До какой даты действует доступ, ДД.ММ.ГГГГ"}, {"cancel_url", "Отмена подписки"}},
	},
	{
		Key: emailSubExpired, Group: "Подписка", Title: "Подписка завершена",
		Subject: "Подписка завершена",
		When:    "30 дней неудачных автосписаний — подписка закрыта.",
		Why:     "Сообщает о закрытии и даёт ссылку оформить заново.",
		Vars:    []emailVar{{"subscribe_url", "Оформить подписку заново"}},
	},
	{
		Key: emailSubCancelLink, Group: "Подписка", Title: "Ссылка на отмену",
		Subject: "Отмена подписки",
		When:    "Человек сам ввёл email на странице /subscription/cancel.",
		Why:     "Это и есть отмена подписки без входа в кабинет: без письма страница /subscription/cancel не работает.",
		Locked:  true,
		Vars:    []emailVar{{"cancel_url", "Подтвердить отмену подписки"}},
	},
}

func emailKindByKey(key string) (emailKind, bool) {
	for _, k := range emailKinds {
		if k.Key == key {
			return k, true
		}
	}
	return emailKind{}, false
}

// errEmailDisabled — письмо выключено на /admin/emails; не сбой.
var errEmailDisabled = errors.New("email disabled in admin")

// sendMailFunc / sendTemplateFunc — подменяются в тестах.
var (
	sendMailFunc     = mail.SendPlain
	sendTemplateFunc = mail.SendTemplate
)

// emailTarget — к чему относится письмо (для журнала).
type emailTarget struct {
	OrderID        string
	SubscriptionID string
}

// sendEmail — единая точка отправки писем сервера: проверяет тумблер и пишет
// попытку в журнал. Проверку SMTP_HOST оставляет вызывающим — они по-разному
// логируют её отсутствие, а в журнал dev-стенда без почты писать нечего.
//
// Если на /admin/emails письму задан шаблон Unisender Go, оно уходит по нему
// (тема и текст из конструктора, vars — подстановками), иначе — subject и body
// через SMTP. Шаблон не принят (удалён, опечатка в id, сбой API) — письмо всё
// равно уходит нашим текстом, а причина пишется в журнал: письма о песне и об
// отмене подписки важнее вёрстки.
func (s *Server) sendEmail(ctx context.Context, kind string, target emailTarget, to, subject, body string, vars map[string]string) error {
	k, known := emailKindByKey(kind)
	if !known {
		// Ошибка программиста: письмо без записи в реестре нельзя выключить.
		log.Printf("[mail] unknown kind %q, subject %q — добавьте его в emailKinds", kind, subject)
	}
	entry := store.EmailLogEntry{Kind: kind, Recipient: to, Subject: subject, OrderID: target.OrderID, SubscriptionID: target.SubscriptionID}
	var err error
	if known && !k.Locked && !store.EmailEnabled(ctx, s.DB, kind) {
		entry.Status = store.EmailDisabled
		err = errEmailDisabled
		log.Printf("[mail] %s to=%s skipped: disabled in admin", kind, to)
	} else {
		// Через Unisender Go письмо несёт ключ строки журнала: по нему вебхук Go
		// дописывает «доставлено/открыто» (handleUnisenderGoWebhook).
		var tr mail.Tracking
		if s.Config.SMTPUnisenderGo {
			tr = mail.Tracking{Ref: randomRef(), Tag: kind}
		}
		sent := false
		if templateID := store.EmailTemplateID(ctx, s.DB, kind); templateID != "" && s.Config.SMTPUnisenderGo {
			err = sendTemplateFunc(s.Config, to, templateID, s.emailVars(vars), tr)
			switch {
			case err == nil:
				sent = true
				entry.Subject = "шаблон " + templateID
			case errors.Is(err, mail.ErrRecipientRejected):
				sent = true // не повод слать текстом: Go отбросит и его
			default:
				log.Printf("[mail] %s to=%s template %s err=%v — отправляю текстом", kind, to, templateID, err)
				entry.Error = "шаблон не отправился, ушло текстом: " + err.Error()
			}
		}
		if !sent {
			err = sendMailFunc(s.Config, []string{to}, subject, body, tr)
		}
		if err != nil {
			entry.Status, entry.Error = store.EmailFailed, err.Error()
		} else {
			entry.Status, entry.Ref = store.EmailSent, tr.Ref
		}
	}
	if logErr := store.LogEmail(ctx, s.DB, entry); logErr != nil {
		log.Printf("[mail] %s to=%s: journal err=%v", kind, to, logErr)
	}
	return err
}

// emailVars — подстановки письма плюс общие (commonEmailVars).
func (s *Server) emailVars(vars map[string]string) map[string]string {
	support := s.Config.MailReplyTo
	if support == "" {
		support = s.Config.SMTPFrom
	}
	out := map[string]string{
		"site_url":      strings.TrimSuffix(s.Config.PublicAppURL, "/"),
		"support_email": support,
	}
	for name, v := range vars {
		out[name] = v
	}
	return out
}

// --- /api/admin/emails ---

func (s *Server) adminEmailKinds(ctx context.Context) []map[string]any {
	out := make([]map[string]any, 0, len(emailKinds))
	for _, k := range emailKinds {
		out = append(out, map[string]any{
			"key": k.Key, "group": k.Group, "title": k.Title, "subject": k.Subject,
			"when": k.When, "why": k.Why, "locked": k.Locked, "vars": k.Vars,
			"enabled":    k.Locked || store.EmailEnabled(ctx, s.DB, k.Key),
			"templateId": store.EmailTemplateID(ctx, s.DB, k.Key),
		})
	}
	return out
}

// adminEmailsResponse — ответ GET/PUT /api/admin/emails. templatesActive:
// шаблоны работают только через Unisender Go (SMTP_UNISENDER_GO), без него
// заданный шаблон игнорируется — страница об этом предупреждает.
func (s *Server) adminEmailsResponse(ctx context.Context) map[string]any {
	return map[string]any{
		"kinds":           s.adminEmailKinds(ctx),
		"commonVars":      commonEmailVars,
		"templatesActive": s.Config.SMTPUnisenderGo,
	}
}

func (s *Server) handleAdminEmailsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.adminEmailsResponse(r.Context()))
}

// templateIDRe — id шаблона Unisender Go (UUID).
var templateIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// handleAdminEmailsPut меняет тумблер ({key, enabled}) и/или шаблон
// ({key, templateId}; пустая строка — снять шаблон). Шаблон можно задать и
// письму, которое нельзя выключить.
func (s *Server) handleAdminEmailsPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key        string  `json:"key"`
		Enabled    *bool   `json:"enabled"`
		TemplateID *string `json:"templateId"`
	}
	if err := readJSON(r, &body); err != nil || (body.Enabled == nil && body.TemplateID == nil) {
		writeJSON(w, 400, map[string]string{"error": "bad_request"})
		return
	}
	k, ok := emailKindByKey(body.Key)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "unknown_email"})
		return
	}
	if body.Enabled != nil && k.Locked {
		writeJSON(w, 400, map[string]string{"error": "email_locked"})
		return
	}
	var templateID string
	if body.TemplateID != nil {
		templateID = strings.ToLower(strings.TrimSpace(*body.TemplateID))
		if templateID != "" && !templateIDRe.MatchString(templateID) {
			writeJSON(w, 400, map[string]string{"error": "bad_template_id"})
			return
		}
	}
	if body.Enabled != nil {
		if err := store.SetEmailEnabled(r.Context(), s.DB, k.Key, *body.Enabled); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		log.Printf("[mail] admin: %s enabled=%v", k.Key, *body.Enabled)
	}
	if body.TemplateID != nil {
		if err := store.SetEmailTemplateID(r.Context(), s.DB, k.Key, templateID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
		log.Printf("[mail] admin: %s template=%q", k.Key, templateID)
	}
	writeJSON(w, 200, s.adminEmailsResponse(r.Context()))
}

const emailLogPerPage = 50

func (s *Server) handleAdminEmailLog(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	entries, total, err := store.ListEmailLog(r.Context(), s.DB, r.URL.Query().Get("q"), page, emailLogPerPage)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		title := e.Kind
		if k, ok := emailKindByKey(e.Kind); ok {
			title = k.Title
		}
		orderRef := ""
		if e.OrderID != "" {
			orderRef = publicOrderRef(s.Config.WebhookSecret, e.OrderID)
		}
		items = append(items, map[string]any{
			"id": e.ID, "createdAt": e.CreatedAt, "kind": e.Kind, "kindTitle": title,
			"recipient": e.Recipient, "subject": e.Subject, "status": e.Status, "error": e.Error,
			"orderId": e.OrderID, "orderRef": orderRef, "subscriptionId": e.SubscriptionID,
			"delivery": e.Delivery, "deliveredAt": e.DeliveredAt, "openedAt": e.OpenedAt, "openedHumanAt": e.OpenedHumanAt,
		})
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "perPage": emailLogPerPage})
}
