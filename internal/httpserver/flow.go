// Плечи A/B-тестов флоу заказа mezmo-song.com.
//
// Две воронки из .ru, перенесённые на .com (ТЗ «tech requirements Sep 2026»):
// variant3 — анкета, потом оплата (FlowRoute на /config), variant4 — оплата в
// начале (движок /create?v=2). Каждая в двух ценах: $25 разовая и
// «подписка $15 / разовая $19, предвыбрана разовая». Пока UnitPay не включил
// подписки, у плеч 500-750s экрана сравнения нет — продаётся разовая $19.
package httpserver

import (
	"context"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"mezmo-song-com/internal/store"
)

// Шаги флоу. Фронт рисует экраны в порядке, который отдал сервер, а не по
// зашитому в компонент номеру шага: порядок — единственное, чем плечи варианта 2
// отличаются от варианта 1.
const (
	StepRecipient    = "recipient"    // кому дарим + имя
	StepOccasion     = "occasion"     // повод
	StepEmail        = "email"        // email + телефон
	StepEmotion      = "emotion"      // эмоция песни (жанр — доп. экран внутри шага)
	StepStory        = "story"        // история
	StepGenerating   = "generating"   // «пишем вашу песню…»
	StepPreview      = "preview"      // 15 секунд под замком
	StepSpeedup      = "speedup"      // апсейл ускорения
	StepCompare      = "compare"      // v1_price: «разовая vs подписка» до оплаты
	StepPayment      = "payment"      // оплата — редирект на страницу ЮKassa
	StepSubscription = "subscription" // апсейл подписки
	StepStems        = "stems"        // апсейл инструментала и вокала
	StepDone         = "done"         // готово, скачать MP3
)

// flowArm — плечо теста: цена разовой покупки и порядок экранов.
type flowArm struct {
	ID           string
	Label        string
	DefaultPrice int // фолбэк, если цена плеча не задана в настройках; в валюте сайта
	Steps        []string
	// Route — путь на фронте, где живёт это плечо. Пусто — плечо рендерит
	// FlowRoute прямо на /config; задано — FlowEntry уводит посетителя туда
	// (prepay-плечи живут на движке /create, а не в frontend/src/flow).
	Route string
	// CompareDefault — что предвыбрано на экране «разовая vs подписка».
	CompareDefault string
	// SubscriptionPrice — цена подписки в этом плече за месяц. 0 — общая
	// (subscriptionPriceRub). Фиксируется у подписки при оформлении.
	SubscriptionPrice int
}

// Предвыбор экрана сравнения ценовых плеч.
const (
	CompareSubscription = "subscription"
	CompareOneTime      = "onetime"
)

// Плечи .com. Технический id совпадает с именем в рекламной ссылке: истории
// заказов под старыми id у .com нет, и второе имя только путало бы сверку.
// Правило имён: «подписка-разовая», суффикс s — что предвыбрано.
const (
	ArmCom990              = "variant3-990"      // анкета → оплата, $25
	ArmCom500750s          = "variant3-500-750s" // анкета → «подписка $15 / разовая $19» → оплата
	ArmComPrepay990        = "variant4-990"      // оплата в начале, $25
	ArmComPrepay500750s    = "variant4-500-750s" // оплата в начале, разовая $19 (подписка $15)
	routeCreatePrepay      = "/create?v=2"
	routeCreateLatePayment = "/create?v=1"
)

// Экрана сводки заказа (с промокодом) и отдельного экрана оплаты в воронках
// нет: после истории покупатель сразу уходит на платёжную форму — шаг payment
// это и есть редирект.
var variant1Steps = []string{
	StepRecipient, StepOccasion, StepEmail, StepEmotion, StepStory,
	StepGenerating, StepPreview, StepSpeedup, StepPayment,
	StepSubscription, StepStems, StepDone,
}

// Ценовое плечо: выбор «разовая vs подписка» показывается ДО оплаты, а апсейл
// подписки после оплаты не повторяется — человек уже ответил на этот вопрос.
var variant1PriceSteps = []string{
	StepRecipient, StepOccasion, StepEmail, StepEmotion, StepStory,
	StepGenerating, StepPreview, StepSpeedup, StepCompare, StepPayment,
	StepStems, StepDone,
}

// Порядок шагов prepay — зеркало V2_STEPS из routes/create/flowConfig.ts.
// Источник истины для рендера — фронт; здесь список нужен только чтобы
// /api/public/flow честно отвечал про состав плеча.
var variant2PrepaySteps = []string{
	"email", "payment", "recipient", "occasion", "emotion", "story",
	"generating", "listen", "upsellSpeed", "upsellSub", "upsellStems", "done",
}

var flowArms = map[string]flowArm{
	ArmCom990:           {ID: ArmCom990, Label: "variant3-990 — анкета, потом оплата, $25", DefaultPrice: 25, Steps: variant1Steps},
	ArmCom500750s:       {ID: ArmCom500750s, Label: "variant3-500-750s — подписка $15 / разовая $19, предвыбрана разовая", DefaultPrice: 19, SubscriptionPrice: 15, Steps: variant1PriceSteps, CompareDefault: CompareOneTime},
	ArmComPrepay990:     {ID: ArmComPrepay990, Label: "variant4-990 — оплата в начале, $25", DefaultPrice: 25, Steps: variant2PrepaySteps, Route: routeCreatePrepay},
	ArmComPrepay500750s: {ID: ArmComPrepay500750s, Label: "variant4-500-750s — оплата в начале, разовая $19 / подписка $15", DefaultPrice: 19, SubscriptionPrice: 15, Steps: variant2PrepaySteps, Route: routeCreatePrepay, CompareDefault: CompareOneTime},
}

// KnownFlowArms — id всех реализованных плеч (порядок фиксирован для админки).
var KnownFlowArms = []string{ArmCom990, ArmCom500750s, ArmComPrepay990, ArmComPrepay500750s}

// defaultFlowArms — что крутится, если настройка не тронута. По умолчанию
// сплита нет: включение второго плеча — осознанное действие в админке.
var defaultFlowArms = []string{ArmCom990}

// enabledArms — включённые плечи из настроек. Неизвестные id игнорируются
// (плечо могли выключить в коде, а строка в настройках осталась); если после
// фильтрации не осталось ничего — работает дефолт, сайт без плеча жить не может.
func (s *Server) enabledArms(ctx context.Context) []flowArm {
	ids := store.FlowArmsEnabled(ctx, s.DB, defaultFlowArms)
	out := make([]flowArm, 0, len(ids))
	for _, id := range ids {
		if arm, ok := flowArms[id]; ok {
			out = append(out, arm)
		}
	}
	if len(out) == 0 {
		out = append(out, flowArms[defaultFlowArms[0]])
	}
	return out
}

// armForSession — плечо посетителя. Назначение детерминированное, от хеша
// сессии: одна и та же сессия всегда попадает в то же плечо без куки и без
// записи в БД, поэтому перезагрузка страницы не перекидывает человека между
// плечами (а с ним и цену в середине оформления).
func (s *Server) armForSession(ctx context.Context, sessionID string) (flowArm, int) {
	arms := s.enabledArms(ctx)
	weights := store.FlowArmWeights(ctx, s.DB)
	arm := pickArm(sessionID, arms, weights)
	return arm, s.armPrice(ctx, arm)
}

func (s *Server) armPrice(ctx context.Context, arm flowArm) int {
	return store.FlowArmPrice(ctx, s.DB, arm.ID, s.armFallbackPrice(arm))
}

// armAliases — второе имя плеча для ссылок (вход к тому же плечу). У .com
// id плеч и есть имена из ссылок, поэтому псевдонимов пока нет; карта остаётся
// точкой расширения, если имя в рекламе придётся сменить без смены id.
var armAliases = map[string]string{}

// armLinkNames — каноническое имя плеча для ссылки, если оно отличается от id.
var armLinkNames = map[string]string{}

// armAlias — имя плеча для ссылки; пусто, если плечу его не дали.
func armAlias(id string) string {
	return armLinkNames[id]
}

// armLinkName — имя плеча для ссылки и адресной строки; для плеча без
// отдельного имени это сам id (он и стоит в ссылках, например v2_prepay).
func armLinkName(id string) string {
	if name := armAlias(id); name != "" {
		return name
	}
	return id
}

// armLink — готовая ссылка «открыть это плечо», которую админка даёт скопировать
// рекламе. Собирается на сервере, потому что PUBLIC_APP_URL знает только он.
func (s *Server) armLink(id string) string {
	return strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/config?arm=" + armLinkName(id)
}

// armByName находит плечо по техническому id или по псевдониму.
func armByName(name string) (flowArm, bool) {
	if arm, ok := flowArms[name]; ok {
		return arm, true
	}
	if id, ok := armAliases[strings.ToLower(name)]; ok {
		arm, ok := flowArms[id]
		return arm, ok
	}
	return flowArm{}, false
}

// forcedArmCookie — плечо, закреплённое за браузером ссылкой ?arm=<id>.
//
// Ссылка с плечом — единственный способ дать рекламе (и девам на проверку)
// адрес с конкретной ценой: ротация от хеша сессии выдаёт плечо случайно, и
// ссылка «на v1_price» без форса открывала бы v2_base с другой ценой. Форс
// живёт в подписанной куке (та же HMAC-схема, что у публичного ref заказа),
// чтобы и заказ создался по тому же плечу, а не только витрина. Выключенное
// в админке плечо не форсится ни ссылкой, ни старой кукой: «снять с
// публикации» значит, что в плечо не попадёт никто.
const forcedArmCookie = "mzarm"

// forcedArm читает закреплённое плечо из куки; ok=false — форса нет, подпись
// не сошлась или плечо выключено.
func (s *Server) forcedArm(ctx context.Context, r *http.Request) (flowArm, bool) {
	c, err := r.Cookie(forcedArmCookie)
	if err != nil {
		return flowArm{}, false
	}
	id, ok := resolveOrderRef(s.Config.WebhookSecret, c.Value)
	if !ok {
		return flowArm{}, false
	}
	return s.enabledArm(ctx, id)
}

// enabledArm — плечо по id, если оно известно и в ротации.
func (s *Server) enabledArm(ctx context.Context, id string) (flowArm, bool) {
	for _, arm := range s.enabledArms(ctx) {
		if arm.ID == id {
			return arm, true
		}
	}
	return flowArm{}, false
}

// pinForcedArm закрепляет плечо из ?arm= за браузером и возвращает его для
// текущего ответа (кука доедет только со следующим запросом). Неизвестный или
// выключенный id молча игнорируется — ротация работает как обычно.
//
// Плечо, которое ротация и так назначила этой сессии, куку не получает: фронт
// показывает назначенное плечо в адресе (/config?arm=…), и перезагрузка такой
// страницы не должна прибивать человека к плечу на 30 дней — иначе смена
// весов в админке не доходила бы до вернувшихся посетителей.
func (s *Server) pinForcedArm(ctx context.Context, w http.ResponseWriter, r *http.Request, sessionID string) (flowArm, bool) {
	id := strings.TrimSpace(r.URL.Query().Get("arm"))
	if id == "" {
		return flowArm{}, false
	}
	named, known := armByName(id)
	if !known {
		// Опечатка в рекламной ссылке. Молчать нельзя: человек попадёт в
		// ротацию по случайной цене, и кампания будет считать, что льёт в
		// заданное плечо. Счётчик читает панель дашборда «Ссылки с нерабочим
		// плечом» — лог сам по себе никто не смотрит.
		log.Printf("[flow] arm=%q из ссылки не опознан — работает ротация", id)
		if err := store.RecordArmLinkMiss(ctx, s.DB, id, store.ArmMissUnknown, time.Now()); err != nil {
			log.Printf("[flow] счётчик нерабочих ссылок: %v", err)
		}
		return flowArm{}, false
	}
	arm, ok := s.enabledArm(ctx, named.ID)
	if !ok {
		// Плечо снято с публикации, а ссылка на него всё ещё в объявлении.
		log.Printf("[flow] arm=%s из ссылки выключен в админке — работает ротация", named.ID)
		if err := store.RecordArmLinkMiss(ctx, s.DB, id, store.ArmMissDisabled, time.Now()); err != nil {
			log.Printf("[flow] счётчик нерабочих ссылок: %v", err)
		}
		return flowArm{}, false
	}
	if _, pinned := s.forcedArm(ctx, r); !pinned {
		if rotated, _ := s.armForSession(ctx, sessionID); rotated.ID == arm.ID {
			return arm, true
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     forcedArmCookie,
		Value:    publicOrderRef(s.Config.WebhookSecret, arm.ID),
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   s.subCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	log.Printf("[flow] arm=%s закреплено ссылкой ?arm=%s", arm.ID, id)
	return arm, true
}

// armForRequest — плечо посетителя с учётом форса из куки: сначала кука, потом
// ротация по сессии. Через него идут все места, где плечо решает цену заказа.
func (s *Server) armForRequest(ctx context.Context, r *http.Request, sessionID string) (flowArm, int) {
	if arm, ok := s.forcedArm(ctx, r); ok {
		return arm, s.armPrice(ctx, arm)
	}
	return s.armForSession(ctx, sessionID)
}

// armFallbackPrice — цена плеча, когда настройки цены нет: цена из определения
// плеча (переопределяется на /admin/settings).
func (s *Server) armFallbackPrice(arm flowArm) int {
	return arm.DefaultPrice
}

// armSubscriptionPrice — цена подписки плеча, ₽/мес; без своей — общая.
func armSubscriptionPrice(arm flowArm) int {
	if arm.SubscriptionPrice > 0 {
		return arm.SubscriptionPrice
	}
	return subscriptionPriceRub
}

// pickArm распределяет сессию по плечам с учётом весов. Плечо без веса весит 1.
func pickArm(sessionID string, arms []flowArm, weights map[string]int) flowArm {
	if len(arms) == 1 {
		return arms[0]
	}
	total := 0
	for _, arm := range arms {
		total += armWeight(arm.ID, weights)
	}
	if total <= 0 {
		return arms[0]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(sessionID))
	point := int(h.Sum32() % uint32(total))
	for _, arm := range arms {
		point -= armWeight(arm.ID, weights)
		if point < 0 {
			return arm
		}
	}
	return arms[len(arms)-1]
}

func armWeight(id string, weights map[string]int) int {
	if w, ok := weights[id]; ok && w > 0 {
		return w
	}
	return 1
}

// previewWindow — окно, за которое считается суточный бюджет генераций.
const previewWindow = 24 * time.Hour

// Квота бесплатных генераций — ОДИН механизм на обе воронки. Три
// предохранителя, и они измеряют разное:
//
//  1. подписанная кука посетителя — лимит на ЧЕЛОВЕКА. Удаляется в один клик,
//     поэтому от скрипта не защищает, зато честно ограничивает покупателя;
//  2. IP — страховка от скрипта в цикле, потолок на порядок выше. Человеком
//     адрес не является: за одним сидит офис или сотни абонентов оператора;
//  3. суточный бюджет на весь сайт — единственное, что реально ограничивает
//     счёт за OpenRouter/Kie.
//
// Раньше у воронок были разные механизмы: вариант 1 считал по сессии+IP в
// таблице заказов, /create — по куке+IP в free_generations. Одно и то же
// правило в двух реализациях означало два разных ответа на один вопрос и два
// набора полей в админке.

// freeGenerationKeys собирает ключи квоты со своими лимитами.
//
// Приватный адрес в ключи НЕ попадает: он означает, что прокси не пробросил
// X-Forwarded-For и реального адреса посетителя мы не знаем — он одинаковый у
// всех. Такой ключ схлопнул бы весь сайт в один счётчик, поэтому проверка по IP
// пропускается, а в лог уходит предупреждение: это и есть способ заметить, что
// прокси настроен не так (nginx mezmo.ru живёт в соседнем репозитории).
func (s *Server) freeGenerationKeys(ctx context.Context, visitor, ip string) []store.FreeGenKey {
	perVisitor, perIP := store.FreeGenerationLimits(ctx, s.DB)
	keys := []store.FreeGenKey{{Key: "v:" + visitor, Limit: perVisitor}}
	if isPublicClientIP(ip) {
		keys = append(keys, store.FreeGenKey{Key: "ip:" + ip, Limit: perIP})
	} else {
		log.Printf("[flow] квота: адрес %q приватный — X-Forwarded-For не проброшен, лимит по IP пропущен", ip)
	}
	return keys
}

// previewBudgetLeft — остался ли суточный бюджет генераций до оплаты.
// Ошибка чтения счётчика закрывает генерацию, а не открывает: цена ошибки в
// одну сторону — потерянная демонстрация, в другую — неограниченный счёт.
func (s *Server) previewBudgetLeft(ctx context.Context) (bool, string) {
	// Рубильник: превью выключено целиком — воронки идут сразу к оплате.
	if !store.PreviewEnabled(ctx, s.DB) {
		return false, "preview_disabled"
	}
	budget := store.PreviewDailyBudget(ctx, s.DB)
	if budget <= 0 {
		return true, ""
	}
	used, err := store.CountPreviewsSince(ctx, s.DB, time.Now().UTC().Add(-previewWindow))
	if err != nil {
		return false, "budget_unknown"
	}
	if used >= budget {
		return false, "daily_budget_spent"
	}
	return true, ""
}

// freeGenerationAvailable — read-only ответ на вопрос «покажем ли шаги
// генерации этому посетителю». Квоту не расходует: её тратит сама генерация.
func (s *Server) freeGenerationAvailable(ctx context.Context, visitor, ip string) (bool, string) {
	if ok, reason := s.previewBudgetLeft(ctx); !ok {
		return false, reason
	}
	ok, err := store.FreeGenerationsAvailable(ctx, s.DB, s.freeGenerationKeys(ctx, visitor, ip), quotaDay())
	if err != nil {
		log.Printf("[flow] квота: чтение счётчиков err=%v", err)
		return false, "quota_unknown"
	}
	if !ok {
		return false, "daily_limit_reached"
	}
	return true, ""
}

// claimFreeGeneration расходует одну бесплатную генерацию. false — квота или
// бюджет исчерпаны; вызывающий не отказывает покупателю, а ведёт его к оплате
// («сначала заплати, потом песня»).
func (s *Server) claimFreeGeneration(ctx context.Context, visitor, ip string) (bool, string) {
	if ok, reason := s.previewBudgetLeft(ctx); !ok {
		return false, reason
	}
	ok, err := store.ConsumeFreeGeneration(ctx, s.DB, s.freeGenerationKeys(ctx, visitor, ip), quotaDay())
	if err != nil {
		log.Printf("[flow] квота: расход err=%v", err)
		return false, "quota_unknown"
	}
	if !ok {
		return false, "daily_limit_reached"
	}
	return true, ""
}

// quotaDay — сутки квоты в UTC: тот же ключ, что в таблице free_generations.
func quotaDay() string { return time.Now().UTC().Format("2006-01-02") }

// isPublicClientIP — можно ли считать этот адрес адресом посетителя.
func isPublicClientIP(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	return !parsed.IsLoopback() && !parsed.IsPrivate() && !parsed.IsLinkLocalUnicast() &&
		!parsed.IsLinkLocalMulticast() && !parsed.IsUnspecified()
}

// clientIP — адрес посетителя для лимита превью. За nginx соседнего проекта
// (см. deploy.md) реальный адрес приходит в X-Forwarded-For, поэтому первый
// адрес списка приоритетнее RemoteAddr.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, found := strings.Cut(fwd, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// handlePublicFlow отдаёт фронту конфигурацию плеча: порядок экранов, цену,
// цены апсейлов и доступность превью. Плечо считается от sid (той же сессии,
// что уходит в /api/public/track), поэтому ответ стабилен между перезагрузками.
func (s *Server) handlePublicFlow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := strings.TrimSpace(r.URL.Query().Get("sid"))
	var arm flowArm
	var price int
	if pinned, ok := s.pinForcedArm(ctx, w, r, session); ok {
		arm, price = pinned, s.armPrice(ctx, pinned)
	} else {
		arm, price = s.armForRequest(ctx, r, session)
	}
	speedup, stems := store.UpsellPrices(ctx, s.DB)
	delay := store.DeliveryDelay(ctx, s.DB)
	previewOK, reason := s.freeGenerationAvailable(ctx, s.visitorID(w, r), clientIP(r))

	steps := arm.Steps
	// Резать шаги есть смысл только у плеч, которые рендерит FlowRoute:
	// у плеч с Route (вариант 2) свой движок на /create со своей логикой.
	if arm.Route == "" {
		// Кран закрыт — шаги генерации и превью выпадают, и флоу становится
		// «оплата до генерации». Это единственная штатная деградация плеча:
		// возможность купить важнее возможности послушать заранее.
		if !previewOK {
			steps = withoutSteps(steps, StepGenerating, StepPreview)
		}
		// Ускорение продаёт снятие задержки обычной доставки. Задержки нет —
		// продавать нечего, и шаг 8 не показывается ни за какую цену.
		if speedup <= 0 || delay <= 0 {
			steps = withoutSteps(steps, StepSpeedup)
		}
		if stems <= 0 {
			steps = withoutSteps(steps, StepStems)
		}
		// Подписку продать нечем — ни экрана сравнения до оплаты, ни апсейла
		// после: ценовое плечо продаёт свою разовую цену.
		if !s.subscriptionsAvailable() {
			steps = withoutSteps(steps, StepCompare, StepSubscription)
		}
	}

	// armName — имя плеча для адресной строки и отчётов: то же, что в рекламных
	// ссылках. Технический id (flowVariant) туда писать нельзя — в аналитике
	// всплывали адреса вида ?arm=v1_price, которых нет ни в одной кампании.
	writeJSON(w, 200, map[string]any{
		"flowVariant":    arm.ID,
		"armName":        armLinkName(arm.ID),
		"route":          arm.Route,
		"label":          arm.Label,
		"compareDefault": arm.CompareDefault,
		"steps":          steps,
		"price":          price,
		"originalPrice":  originalPriceFromConfig(s.Config),
		"speedupPrice":   speedup,
		"stemsPrice":     stems,
		// Срок обычной доставки — тот же источник, что у задержки: экран
		// ускорения обещает «вместо N часов», и второй раз спрашивать сервер
		// об этом же значении незачем.
		"deliveryDelayHours": int(delay.Hours()),
		// Цена подписки — для экранов сравнения и апсейла; у ценовых плеч своя.
		"subscriptionPrice": armSubscriptionPrice(arm),
		// false — подписку не продаём (см. subscriptionsAvailable): движок /create
		// по нему пропускает апсейл подписки.
		"subscriptionsEnabled": s.subscriptionsAvailable(),
		"previewReady":         previewOK,
		"previewReason":        reason,
	})
}

// handleFlowStep принимает показ шага воронки. Пинг фоновый: на любых
// проблемах отвечаем 200 и просто ничего не пишем — аналитика не должна
// ломать воронку.
//
// Зачем сервер, если события шагов уже уходят в dataLayer: отчёт «где отпадают
// люди» читается с дашборда, а он смотрит в SQLite приложения. Считать воронку
// по заказам нельзя — заказ появляется только в её середине.
func (s *Server) handleFlowStep(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"sessionId"`
		Flow      string `json:"flow"`
		Arm       string `json:"arm"`
		StepID    string `json:"stepId"`
		StepIndex int    `json:"stepIndex"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if err := store.RecordFlowStep(r.Context(), s.DB, store.StepView{
		SessionID: body.SessionID,
		Flow:      body.Flow,
		Arm:       body.Arm,
		StepID:    body.StepID,
		StepIndex: body.StepIndex,
	}); err != nil {
		log.Printf("[flow] шаг %q сессии %q не записан: %v", body.StepID, body.SessionID, err)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func withoutSteps(steps []string, drop ...string) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		if !slices.Contains(drop, step) {
			out = append(out, step)
		}
	}
	return out
}
