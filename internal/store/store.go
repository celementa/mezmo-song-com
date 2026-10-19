package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Order struct {
	ID                string
	Status            string
	DeliveryStatus    string
	PaymentStatus     string
	InputPayloadJSON  string
	CustomerJSON      string
	ThemeSlug         sql.NullString
	XenditInvoiceURL  sql.NullString
	YooPaymentID      sql.NullString
	YooPaymentIDs     sql.NullString // JSON-массив всех платежей, заведённых по заказу
	BasePrice         int
	DiscountCode      sql.NullString
	DiscountAmount    sql.NullInt64
	PurchaseTrackedAt sql.NullString
	ServerTrackedAt   sql.NullString // серверные конверсии (GA4/Метрика/VK) с вебхука отправлены
	GAClientID        sql.NullString
	YMClientID        sql.NullString // ClientID Метрики из куки _ym_uid — для офлайн-конверсий
	AttributionJSON   sql.NullString // рекламные метки посадочной страницы (utm_*, yclid, rb_clickid)
	CreatedAt         time.Time
	AudioURLsJSON     string // when relay completes
	LyricsText        sql.NullString
	DemoCompleted     bool
	RegenerationCount int
	KieTaskID         sql.NullString // задача Kie.ai последнего прогона генерации
	KieAudioIDs       sql.NullString // JSON-массив id треков этого прогона
	YooConfirmation   sql.NullString // JSON IssuedConfirmation; пусто — подтверждение не выдавалось
	RefundedKopecks   int64          // суммарно возвращено по заказу; 0 — возвратов не было
	SubscriptionID    sql.NullString // подписка, в рамках которой создана песня (кабинет подписчика)
	// Поля воронок /create (см. create_flow.go). Пустые у заказов старого флоу.
	Flow               sql.NullString // воронка заказа: '' — старый флоу, 'v1' (оплата поздно) / 'v2' (оплата в начале)
	FunnelStage        sql.NullString // последняя достигнутая стадия воронки — рельса под email-маркетинг
	EmailConsent       bool           // согласие на письма, зафиксированное при вводе email
	ExtrasSelectedJSON sql.NullString // JSON-массив id допников, выбранных ДО оплаты (входят в основной платёж)
	ExtrasPaidJSON     sql.NullString // JSON-массив оплаченных ПОСЛЕ оплаты допников: [{id, paymentId, paidAt}]
	ExtrasPendingJSON  sql.NullString // JSON-массив выданных подтверждений оплаты допников: [{id, paymentId, url, issuedAt}]
	FollowupSentAt     sql.NullString // письмо «как всё прошло + промокод» отправлено

	// Поля A/B-флоу (см. flow.go). Пустые у заказов, созданных до сплита.
	FlowVariant      sql.NullString // плечо теста, в котором создан заказ
	PreviewStartedAt sql.NullString // RFC3339; генерация до оплаты стартовала
	PreviewAudioURL  sql.NullString // ключ 15-секундного отрывка в MinIO
	DeliverAfter     sql.NullString // RFC3339; до этого момента песню не показываем
	SpeedupPurchased bool           // куплено ускорение (шаг 8) — задержки нет
	ParentOrderID    sql.NullString // родительский заказ, если это апсейл
	UpsellKind       sql.NullString // вид апсейла (UpsellStems)
	// ProcessingStartedAt — когда пошла генерация (RFC3339). Пусто у заказов,
	// созданных до появления колонки; от неё считается окно демо-фолбэка.
	ProcessingStartedAt sql.NullString
	// DeliveredAt — когда впервые ушло письмо «Ваша песня готова» (RFC3339).
	// Не сбрасывается доработкой: по нему письмо на заказ уходит один раз.
	DeliveredAt sql.NullString
	// PaymentProvider — через кого выставлен счёт: unitpay | yookassa; пусто —
	// счёта не было (бесплатный, тестовый, песня по подписке).
	PaymentProvider sql.NullString
	// YooMethodID — способ оплаты, сохранённый ЮKassa при оплате песни
	// (save_payment_method). По нему апсейл «безлимит за 0 ₽ сегодня»
	// активирует подписку без второго ввода карты.
	YooMethodID sql.NullString
}

// Значения orders.payment_status, которые ставит вебхук ЮKassa. Схема их не
// ограничивает (колонка TEXT), миграция для новых значений не нужна.
const (
	PaymentPending  = "pending"
	PaymentPaid     = "paid"
	PaymentCanceled = "canceled"
	PaymentRefunded = "refunded"
)

// MaxRegenerations — лимит бесплатных доработок для клиентских заказов
// по умолчанию. Фактическое значение правится в /admin/settings.
const MaxRegenerations = 2

// Ключи настроек лимита доработок. Оба редактируются на /admin/settings без
// пересборки; 0 означает «без ограничений».
//
// Тестовым заказам из /admin/ads по умолчанию выдан безлимит: на них блогеры
// подбирают текст, и упереться в лимит означало бы выбросить заказ.
const (
	SettingRegenerationsClient   = "orders.regenerations_client"
	SettingRegenerationsInternal = "orders.regenerations_internal"

	defaultRegenerationsInternal = 0
)

// RegenerationLimits возвращает лимиты доработок для клиентских и для тестовых
// заказов. 0 — без ограничений. Битое или отрицательное значение в настройках
// трактуется как дефолт, чтобы опечатка в админке не открывала безлимит всем.
func RegenerationLimits(ctx context.Context, db *sql.DB) (client, internal int) {
	return settingInt(ctx, db, SettingRegenerationsClient, MaxRegenerations),
		settingInt(ctx, db, SettingRegenerationsInternal, defaultRegenerationsInternal)
}

func settingInt(ctx context.Context, db *sql.DB, key string, defaultValue int) int {
	n, err := strconv.Atoi(GetSetting(ctx, db, key, ""))
	if err != nil || n < 0 {
		return defaultValue
	}
	return n
}

// InternalDiscountCode помечает заказы, созданные из /admin/ads: у них BasePrice=0,
// оплата не проходила. На таких заказах блогеры и модераторы подбирают текст, поэтому
// лимит доработок к ним не применяется (см. IncrementRegeneration): упереться в две
// попытки означало бы выкинуть заказ и создавать его заново.
const InternalDiscountCode = "INTERNAL"

// IsInternalOrder reports whether the order came from /admin/ads (unlimited revisions).
func IsInternalOrder(o Order) bool {
	return o.DiscountCode.Valid && o.DiscountCode.String == InternalDiscountCode
}

// Delivery statuses. delivery_status tracks how far the finished song got to the
// customer and moves strictly forward within one generation cycle:
//
//	DeliveryPending — песни ещё нет (заказ создан / в работе)
//	DeliveryReady   — контент сохранён, песня доступна на /order/{id}
//	DeliveryDone    — вдобавок ушло письмо "Ваша песня готова"
//
// Regeneration resets it to DeliveryPending, since the cycle starts over.
const (
	DeliveryPending = "delivery_pending"
	DeliveryReady   = "delivery_ready"
	DeliveryDone    = "delivered"
)

// DemoAudioURL — трек-заглушка, которую получает заказ, если Kie.ai так и не
// прислал аудио. Её наличие в audio_urls — единственный надёжный признак
// фолбэка: demo_completed ставят все три финализатора, и демо от настоящей
// песни он не отличает.
//
// Путь СВОЙ, а не внешний: прежняя ссылка на files.freemusicarchive.org с
// некоторых пор отдаёт 404 (проверено 2026-09-02), то есть страховка выдавала
// клиенту непроигрываемый плеер. Файл лежит в frontend/public/audio, попадает
// в dist и вшивается в бинарь — его раздаёт сам сервер, и чужой хостинг больше
// ни на что не влияет. Относительный путь браузер разрешает от origin страницы
// заказа.
const DemoAudioURL = "/audio/sample-maxim.mp3"

// legacyDemoAudioURL — прежняя внешняя ссылка, оставленная только для
// РАСПОЗНАВАНИЯ: у заказов, закрытых фолбэком до 2026-09-02, в audio_urls лежит
// именно она, и колонка «Демо» в админке не должна про них забыть.
const legacyDemoAudioURL = "https://files.freemusicarchive.org/storage-freemusicarchive-org/music/no_curator/Komiku/Tale_on_the_Lake/Komiku_-_02_-_Home.mp3"

const demoLyrics = "[Куплет 1]\nДемо-текст вашей истории…\n\n[Припев]\nПерсональная песня готова.\n"

// stressMarkStripper убирает знаки ударения из текста песни.
//
// Модель по промпту (data/prompts/lyrics.md) ставит ударения над гласными — так она
// сама проверяет, что ударные слоги попадают в размер, и в таком виде текст уходит в
// Kie.ai подсказкой по произношению. Клиенту же нужен чистый текст, поэтому в
// lyrics_text значки не попадают: пишем в БД уже зачищенную версию.
//
// U+0301 — combining acute accent, «как в Википедии» (Ната́ша); U+0341 — его редкий
// дубль (combining acute tone mark), который модели иногда подставляют вместо первого.
var stressMarkStripper = strings.NewReplacer("\u0301", "", "\u0341", "")

// StripStressMarks возвращает текст песни без знаков ударения — вид для клиента.
func StripStressMarks(lyrics string) string {
	return stressMarkStripper.Replace(lyrics)
}

// IsFallbackAudio reports whether the order was closed with the demo track
// instead of a real generated song. Узнаёт обе заглушки — текущую и прежнюю
// внешнюю, чтобы старые заказы остались помеченными демо.
func IsFallbackAudio(audioURLsJSON string) bool {
	var urls []string
	_ = json.Unmarshal([]byte(audioURLsJSON), &urls)
	return slices.Contains(urls, DemoAudioURL) || slices.Contains(urls, legacyDemoAudioURL)
}

// markReadyExpr advances delivery_status to DeliveryReady without downgrading an
// order that is already DeliveryDone (a late fallback must not undo a sent email).
//
// Заодно доводится стадия воронки: `done` — единственная стадия, которую не
// проставлял никто, из-за чего панель «последняя достигнутая стадия» никогда не
// показывала дошедших до готовой песни. Ранг стадий (SetFunnelStage) вперёд
// пускает, назад нет, поэтому здесь допустима прямая запись только для заказов
// воронок: у остальных колонка вообще не используется.
const markReadyExpr = `delivery_status = CASE WHEN delivery_status = '` + DeliveryDone +
	`' THEN delivery_status ELSE '` + DeliveryReady + `' END,
	funnel_stage = CASE WHEN flow IS NULL OR flow = '' THEN funnel_stage ELSE '` + StageDone + `' END`

func Open(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS orders (
	id TEXT PRIMARY KEY,
	status TEXT NOT NULL DEFAULT 'created',
	delivery_status TEXT NOT NULL DEFAULT 'delivery_pending',
	payment_status TEXT NOT NULL DEFAULT 'pending',
	input_payload TEXT NOT NULL DEFAULT '{}',
	customer TEXT NOT NULL DEFAULT '{}',
	theme_slug TEXT,
	xendit_invoice_url TEXT,
	yoo_payment_id TEXT,
	base_price INTEGER NOT NULL DEFAULT 2990,
	discount_code TEXT,
	discount_amount INTEGER,
	purchase_tracked_at TEXT,
	server_tracked_at TEXT,
	ga_client_id TEXT,
	ym_client_id TEXT,
	attribution TEXT,
	audio_urls TEXT DEFAULT '[]',
	lyrics_text TEXT,
	demo_completed INTEGER NOT NULL DEFAULT 0,
	regeneration_count INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	// migration for existing DBs without regeneration_count
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN regeneration_count INTEGER NOT NULL DEFAULT 0`)
	// migration for existing DBs without ga_client_id
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN ga_client_id TEXT`)
	// migration for existing DBs without attribution
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN attribution TEXT`)
	// migrations for server-side conversion tracking
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN server_tracked_at TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN ym_client_id TEXT`)

	// yoo_payment_ids — все платежи, которые мы заводили по этому заказу (JSON-массив).
	// Нужен потому, что embedded-токен одноразовый и часовой: повторная попытка оплаты
	// создаёт новый платёж, а человек может оплатить любой из открытых. Вебхук по
	// «не последнему» платежу обязан зачесть деньги, а не отклонить их.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN yoo_payment_ids TEXT`)
	_, err = db.Exec(`UPDATE orders SET yoo_payment_ids = json_array(yoo_payment_id)
	                  WHERE yoo_payment_ids IS NULL AND yoo_payment_id IS NOT NULL AND yoo_payment_id != ''`)
	if err != nil {
		return err
	}

	// yoo_confirmation — последнее подтверждение оплаты, выданное покупателю (JSON:
	// paymentId, mode, token/url, issuedAt). Пока оно живо и платёж всё ещё pending,
	// повторное «Оплатить» отдаёт то же подтверждение, а не заводит второй
	// оплачиваемый платёж. Бэкфилл не нужен: пусто = не выдавали.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN yoo_confirmation TEXT`)

	// refunded_kopecks — суммарно возвращено по заказу, перечитывается из API
	// ЮKassa при каждом refund.succeeded (не накапливается, чтобы ретраи вебхука
	// не задвоили сумму). Бэкфилл не нужен: 0 = возвратов не было.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN refunded_kopecks INTEGER NOT NULL DEFAULT 0`)

	// payment_provider — провайдер, выставивший последний счёт по заказу. Нужен
	// дашборду: оплаты .com (UnitPay) отделяются от ЮKassa.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN payment_provider TEXT`)

	// kie_task_id / kie_audio_ids — идентификаторы последнего прогона генерации у
	// Kie.ai. Нужны для точечной правки отрезка аудио (replace-section) и Persona:
	// оба требуют taskId + audioId, а восстановить их постфактум нечем. Бэкфилла
	// нет и быть не может — у заказов, созданных до этой миграции, они остаются
	// пустыми навсегда.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN kie_task_id TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN kie_audio_ids TEXT`)

	// Backfill: delivery_status was never written before, so every finished order
	// is stuck at delivery_pending. Whether their email went out is unknowable in
	// hindsight, so they settle at DeliveryReady — the honest lower bound.
	_, err = db.Exec(`UPDATE orders SET delivery_status = ? WHERE status = 'completed' AND delivery_status = ?`,
		DeliveryReady, DeliveryPending)
	if err != nil {
		return err
	}

	// visits — знаменатель конверсии в /admin/analytics. Уники считаются по
	// session_id, поэтому повторные загрузки лендинга в одной сессии за день
	// схлопываются UNIQUE-констрейнтом.
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS visits (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	day TEXT NOT NULL,
	path TEXT NOT NULL DEFAULT '/',
	theme_slug TEXT,
	source TEXT NOT NULL DEFAULT 'прямой',
	campaign TEXT,
	attribution TEXT,
	created_at TEXT NOT NULL,
	UNIQUE(session_id, day, path)
);`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_visits_day ON visits(day)`)
	if err != nil {
		return err
	}

	// lyrics_versions — тексты, заменённые доработками. Текущий текст остаётся в
	// orders.lyrics_text и здесь не дублируется. Версия — regeneration_count, при
	// котором текст был заменён, поэтому первый текст заказа лежит как версия 0;
	// UNIQUE делает повторную запись того же прогона безобидной.
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS lyrics_versions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id TEXT NOT NULL,
	version INTEGER NOT NULL,
	lyrics TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE(order_id, version)
);`)
	if err != nil {
		return err
	}

	// settings — key/value настройки, редактируемые из админки без редеплоя
	// (например analytics.track_test_orders).
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`)
	if err != nil {
		return err
	}

	// Админка листает заказы страницами с ORDER BY created_at DESC — без индекса
	// каждая страница заставляла бы SQLite сортировать всю таблицу заново.
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at DESC)`)
	if err != nil {
		return err
	}

	if err := migrateFlow(db); err != nil {
		return err
	}

	if err := migrateFlowSteps(db); err != nil {
		return err
	}

	if err := migrateCreateFlow(db); err != nil {
		return err
	}

	if err := migrateEmailLog(db); err != nil {
		return err
	}

	if err := migrateSubscriptions(db); err != nil {
		return err
	}

	return migratePartner(db)
}

func CreateOrder(ctx context.Context, db *sql.DB, o Order) error {
	consent := 0
	if o.EmailConsent {
		consent = 1
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO orders (id, status, delivery_status, payment_status, input_payload, customer, theme_slug, base_price, discount_code, ga_client_id, ym_client_id, attribution, subscription_id, flow, funnel_stage, email_consent, flow_variant, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.Status, o.DeliveryStatus, o.PaymentStatus, o.InputPayloadJSON, o.CustomerJSON, o.ThemeSlug, o.BasePrice, o.DiscountCode, o.GAClientID, o.YMClientID, o.AttributionJSON, o.SubscriptionID, o.Flow, o.FunnelStage, consent, o.FlowVariant, o.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// ErrNotFound when no row matches.
var ErrNotFound = errors.New("not found")

func GetOrder(ctx context.Context, db *sql.DB, id string) (Order, error) {
	var o Order
	var created string
	var demoFlag, consentFlag, speedupFlag int64
	err := db.QueryRowContext(ctx, `
SELECT `+orderColumns+`
FROM orders WHERE id = ?`, id,
	).Scan(&o.ID, &o.Status, &o.DeliveryStatus, &o.PaymentStatus, &o.InputPayloadJSON, &o.CustomerJSON,
		&o.ThemeSlug, &o.XenditInvoiceURL, &o.YooPaymentID, &o.BasePrice, &o.DiscountCode, &o.DiscountAmount,
		&o.PurchaseTrackedAt, &o.ServerTrackedAt, &o.GAClientID, &o.YMClientID, &o.AttributionJSON, &o.AudioURLsJSON, &o.LyricsText, &demoFlag, &o.RegenerationCount, &o.KieTaskID, &o.KieAudioIDs, &o.YooPaymentIDs, &o.YooConfirmation, &o.RefundedKopecks, &o.SubscriptionID, &o.Flow, &o.FunnelStage, &consentFlag, &o.ExtrasSelectedJSON, &o.ExtrasPaidJSON, &o.ExtrasPendingJSON, &o.FollowupSentAt, &created,
		&o.FlowVariant, &o.PreviewStartedAt, &o.PreviewAudioURL, &o.DeliverAfter, &speedupFlag, &o.ParentOrderID, &o.UpsellKind, &o.ProcessingStartedAt, &o.YooMethodID, &o.DeliveredAt, &o.PaymentProvider)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	o.DemoCompleted = demoFlag != 0
	o.EmailConsent = consentFlag != 0
	o.SpeedupPurchased = speedupFlag != 0
	o.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return o, nil
}

const orderColumns = `id, status, delivery_status, payment_status, input_payload, customer, theme_slug, xendit_invoice_url, yoo_payment_id,
       base_price, discount_code, discount_amount, purchase_tracked_at, server_tracked_at, ga_client_id, ym_client_id, attribution, audio_urls, lyrics_text, demo_completed, regeneration_count, kie_task_id, kie_audio_ids, yoo_payment_ids, yoo_confirmation, refunded_kopecks, subscription_id, flow, funnel_stage, email_consent, extras_selected, extras_paid, extras_pending, followup_sent_at, created_at,
       flow_variant, preview_started_at, preview_audio_url, deliver_after, speedup_purchased, parent_order_id, upsell_kind, processing_started_at, yoo_payment_method_id, delivered_at, payment_provider`

func scanOrders(rows *sql.Rows) ([]Order, error) {
	defer rows.Close()

	var out []Order
	for rows.Next() {
		var o Order
		var created string
		var demoFlag, consentFlag, speedupFlag int64
		if err := rows.Scan(&o.ID, &o.Status, &o.DeliveryStatus, &o.PaymentStatus, &o.InputPayloadJSON, &o.CustomerJSON,
			&o.ThemeSlug, &o.XenditInvoiceURL, &o.YooPaymentID, &o.BasePrice, &o.DiscountCode, &o.DiscountAmount,
			&o.PurchaseTrackedAt, &o.ServerTrackedAt, &o.GAClientID, &o.YMClientID, &o.AttributionJSON, &o.AudioURLsJSON, &o.LyricsText, &demoFlag, &o.RegenerationCount, &o.KieTaskID, &o.KieAudioIDs, &o.YooPaymentIDs, &o.YooConfirmation, &o.RefundedKopecks, &o.SubscriptionID, &o.Flow, &o.FunnelStage, &consentFlag, &o.ExtrasSelectedJSON, &o.ExtrasPaidJSON, &o.ExtrasPendingJSON, &o.FollowupSentAt, &created,
			&o.FlowVariant, &o.PreviewStartedAt, &o.PreviewAudioURL, &o.DeliverAfter, &speedupFlag, &o.ParentOrderID, &o.UpsellKind, &o.ProcessingStartedAt, &o.YooMethodID, &o.DeliveredAt, &o.PaymentProvider); err != nil {
			return nil, err
		}
		o.DemoCompleted = demoFlag != 0
		o.EmailConsent = consentFlag != 0
		o.SpeedupPurchased = speedupFlag != 0
		o.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, o)
	}
	return out, rows.Err()
}

// OrderFilter — именованные фильтры списка заказов в админке. Значения приходят
// из селекта на /admin/orders; неизвестное трактуется как «все».
const (
	FilterAll          = ""
	FilterProcessing   = "processing"
	FilterCompleted    = "completed"
	FilterUnpaid       = "unpaid"
	FilterInternal     = "internal"
	FilterSubscription = "subscription"
)

// OrderPage — запрос страницы списка заказов.
//
// Поиск идёт LIKE по сырым колонкам: id, customer (email и телефон), input_payload
// (имя получателя), attribution (источник, кампания) и yoo_payment_ids (ID платежа
// из ЮKassa — чтобы находить заказ по письму о возврате) — те же поля, по которым
// админка искала на клиенте, пока список грузился целиком. Регистронезависимость
// LIKE в SQLite работает только для ASCII, поэтому id и email находятся в любом
// регистре, а русское имя — как набрано.
type OrderPage struct {
	Query  string
	Filter string
	Limit  int
	Offset int
}

// where собирает условие и аргументы, общие для выборки страницы и подсчёта total.
func (p OrderPage) where() (string, []any) {
	var conds []string
	var args []any

	switch p.Filter {
	case FilterProcessing:
		conds = append(conds, `status = 'processing'`)
	case FilterCompleted:
		conds = append(conds, `status = 'completed'`)
	case FilterUnpaid:
		conds = append(conds, `payment_status <> 'paid'`)
	case FilterInternal:
		conds = append(conds, `discount_code = ?`)
		args = append(args, InternalDiscountCode)
	case FilterSubscription:
		conds = append(conds, `discount_code = ?`)
		args = append(args, SubscriptionDiscountCode)
	}

	if q := strings.TrimSpace(p.Query); q != "" {
		like := "%" + q + "%"
		conds = append(conds, `(id LIKE ? OR customer LIKE ? OR input_payload LIKE ? OR COALESCE(attribution, '') LIKE ? OR COALESCE(yoo_payment_ids, '') LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListOrdersPage возвращает одну страницу заказов и общее число подошедших под
// фильтр — админка листает по 50, а таблица целиком в память больше не грузится.
func ListOrdersPage(ctx context.Context, db *sql.DB, p OrderPage) (orders []Order, total int, err error) {
	cond, args := p.where()

	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders`+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders`+cond+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	orders, err = scanOrders(rows)
	return orders, total, err
}

// ListOrdersSince отдаёт заказы за период — выборка для /admin/analytics, которой
// нужны только последние N дней, а не вся таблица.
func ListOrdersSince(ctx context.Context, db *sql.DB, sinceDay string) ([]Order, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE created_at >= ? ORDER BY created_at DESC`, sinceDay)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

// ListPendingOrdersWithPayments — заказы, по которым мы заводили платёж, но
// оплата так и не подтвердилась: payment_status = pending и непустая история
// yoo_payment_ids. Окно [since, until]: слишком старые заказы опрашивать
// незачем (их платежи давно протухли), слишком свежие — ещё не успел вебхук.
// Это вход фоновой сверки с ЮKassa (reconcile), страхующей от потерянного
// вебхука.
func ListPendingOrdersWithPayments(ctx context.Context, db *sql.DB, since, until time.Time, limit int) ([]Order, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders
		 WHERE payment_status = 'pending'
		   AND yoo_payment_ids IS NOT NULL AND yoo_payment_ids != '' AND yoo_payment_ids != '[]'
		   AND created_at >= ? AND created_at <= ?
		 ORDER BY created_at ASC LIMIT ?`,
		since.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

// ConfirmOrder transitions an order from 'created' to 'processing'. It returns
// transitioned=true only when this call performed the transition, so callers can
// trigger song generation exactly once even if invoked concurrently (YooKassa
// webhook and the client-side /confirm can both fire for the same order).
func ConfirmOrder(ctx context.Context, db *sql.DB, id string) (transitioned bool, err error) {
	// processing_started_at — от него, а не от created_at, отсчитывается окно
	// демо-фолбэка (см. MaybeDemoComplete): заказ теперь создаётся раньше старта
	// генерации (в варианте 1 — на шаге email), и отсчёт от создания подменял бы
	// песню демо-треком через секунды после нажатия «создать».
	res, err := db.ExecContext(ctx, `UPDATE orders SET status = 'processing', processing_started_at = ? WHERE id = ? AND status = 'created'`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkPurchaseTracked ставит отметку о браузерной отбивке покупки и сообщает,
// поставила ли её именно эта вызывающая сторона. Гард `purchase_tracked_at IS NULL`
// делает заявку атомарной — тот же приём, что у MarkServerTracked для вебхуков.
//
// Это не оптимизация, а условие корректности: браузерных точек трекинга две
// (/thanks и страница заказа), и без гарда переход между ними успевал прочитать
// ещё пустой purchase_tracked_at и отправить покупку второй раз.
func MarkPurchaseTracked(ctx context.Context, db *sql.DB, id string, ts time.Time) (first bool, err error) {
	res, err := db.ExecContext(ctx, `UPDATE orders SET purchase_tracked_at = ? WHERE id = ? AND purchase_tracked_at IS NULL`,
		ts.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkServerTracked ставит отметку об отправке серверных конверсий и сообщает,
// поставила ли её именно эта вызывающая сторона. Гард от повторных вебхуков
// ЮKassa: события в GA4/Метрику/VK уходят ровно один раз на заказ.
func MarkServerTracked(ctx context.Context, db *sql.DB, id string, ts time.Time) (first bool, err error) {
	res, err := db.ExecContext(ctx, `UPDATE orders SET server_tracked_at = ? WHERE id = ? AND server_tracked_at IS NULL`,
		ts.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetSetting возвращает значение настройки или defaultValue, если ключа нет.
func GetSetting(ctx context.Context, db *sql.DB, key, defaultValue string) string {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return defaultValue
	}
	return v
}

func SetSetting(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// AddPaymentID запоминает заведённый платёж как актуальный и добавляет его в историю.
// invoiceURL пустой (embedded) не затирает ранее сохранённую redirect-ссылку.
//
// Один UPDATE без предварительного чтения — нарочно. Двойной клик по «Оплатить»
// шлёт два конкурентных вызова; версия через GetOrder+UPDATE читала одну и ту же
// историю в обоих и второй UPDATE затирал id, добавленный первым — вебхук по
// потерянному платежу отвечал бы payment_id_mismatch на настоящую оплату. Здесь
// добавление в JSON-массив выражено прямо в SET (тот же приём, что в
// IncrementRegeneration), и SQLite сериализует конкурентные писатели сама
// (busy_timeout выставлен в Open). Идемпотентно: повторное добавление уже
// известного id не дублирует его в истории.
func AddPaymentID(ctx context.Context, db *sql.DB, id, paymentID, invoiceURL string) error {
	_, err := db.ExecContext(ctx, `
UPDATE orders
SET payment_status = 'pending',
    yoo_payment_id = ?,
    xendit_invoice_url = CASE WHEN ? <> '' THEN ? ELSE xendit_invoice_url END,
    yoo_payment_ids = CASE
        WHEN yoo_payment_ids IS NULL OR yoo_payment_ids = '' THEN json_array(?)
        WHEN (SELECT COUNT(*) FROM json_each(yoo_payment_ids) WHERE value = ?) > 0 THEN yoo_payment_ids
        ELSE json_insert(yoo_payment_ids, '$[#]', ?)
    END
WHERE id = ?`,
		paymentID, invoiceURL, invoiceURL, paymentID, paymentID, paymentID, id)
	return err
}

// SetPaymentProvider запоминает, через кого выставлен счёт по заказу.
func SetPaymentProvider(ctx context.Context, db *sql.DB, id, provider string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET payment_provider = ? WHERE id = ?`, provider, id)
	return err
}

// KnownPaymentID — принадлежит ли платёж этому заказу (по истории, не только по
// последнему). Если по заказу ещё не заведено ни одного платежа, ограничения нет —
// это сохраняет прежнее поведение до первого initiate-payment.
func KnownPaymentID(o Order, paymentID string) bool {
	hasAny := false
	if o.YooPaymentIDs.Valid && o.YooPaymentIDs.String != "" {
		var ids []string
		if err := json.Unmarshal([]byte(o.YooPaymentIDs.String), &ids); err == nil && len(ids) > 0 {
			hasAny = true
			if slices.Contains(ids, paymentID) {
				return true
			}
		}
	}
	if o.YooPaymentID.Valid && o.YooPaymentID.String != "" {
		hasAny = true
		if o.YooPaymentID.String == paymentID {
			return true
		}
	}
	return !hasAny
}

// IssuedConfirmation — подтверждение оплаты, которое мы уже выдали покупателю по
// этому заказу. Храним, потому что токен виджета живёт час и одноразов: пока он
// жив, новый платёж заводить нельзя (старый остаётся оплачиваемым — capture: true,
// деньги списываются сразу). T-011 сознательно токен не хранила, рассчитывая гасить
// прежние платежи отменой; ЮKassa отменять pending не даёт, поэтому решение отменено.
//
// Имя с префиксом Issued — чтобы не путать с payment.Confirmation, который появится
// в T-015 (там это «подтверждение, как его отдала ЮKassa»).
type IssuedConfirmation struct {
	PaymentID string    `json:"paymentId"`
	Mode      string    `json:"mode"`            // embedded | redirect — часть ключа переиспользования
	Token     string    `json:"token,omitempty"` // embedded
	URL       string    `json:"url,omitempty"`   // redirect
	IssuedAt  time.Time `json:"issuedAt"`
}

// SaveConfirmation запоминает выданное подтверждение (перезапись: актуально
// последнее). Отдельный UPDATE после AddPaymentID, а не внутри него: AddPaymentID
// вызывается и там, где подтверждения нет.
func SaveConfirmation(ctx context.Context, db *sql.DB, id string, c IssuedConfirmation) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE orders SET yoo_confirmation = ? WHERE id = ?`, string(raw), id)
	return err
}

// LoadConfirmation разбирает сохранённое подтверждение. ok == false — подтверждения
// нет либо оно битое; вызывающая сторона в этом случае просто создаёт новый платёж.
func LoadConfirmation(o Order) (IssuedConfirmation, bool) {
	var c IssuedConfirmation
	if !o.YooConfirmation.Valid || o.YooConfirmation.String == "" {
		return c, false
	}
	if err := json.Unmarshal([]byte(o.YooConfirmation.String), &c); err != nil {
		return c, false
	}
	return c, true
}

// MarkPaid flips a pending order to paid. It is a no-op once the order is already
// paid, so a duplicate YooKassa webhook (they retry) won't reset an in-flight or
// completed order back to 'created' and re-trigger generation.
// MarkPaid помечает заказ оплаченным. Производственный статус (status) при этом
// НЕ трогается.
//
// Раньше здесь стоял `status = CASE WHEN status = 'failed' THEN status ELSE
// 'created' END` — из времён, когда генерация начиналась только после оплаты, и
// «оплачен» означало «готов к запуску». С генерацией ДО оплаты (превью варианта 1
// и воронка /create v1) этот сброс стал ошибкой: заказ, у которого песня уже
// готова, возвращался в created, вебхук оплаты успешно вызывал ConfirmOrder и
// запускал генерацию ВТОРОЙ раз — двойной расход OpenRouter/Kie, а трек
// перезаписывался по тому же ключу, то есть покупатель получал не ту песню,
// которую слушал перед оплатой.
//
// Сброс и не был нужен: у заказа, ещё не начинавшего генерацию, status и так
// 'created', а остальные значения (processing / completed / failed) сбрасывать
// нельзя. Гард `payment_status != 'paid'` оставлен — он даёт идемпотентность
// повторным вебхукам ЮKassa.
func MarkPaid(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET payment_status = 'paid' WHERE id = ? AND payment_status != 'paid'`, id)
	return err
}

// MarkCanceled помечает несостоявшийся платёж. Применяется только к заказу, который
// ещё не оплачен: оплаченный заказ отменить вебхуком нельзя — гард в WHERE, как в
// MarkPaid, даёт идемпотентность бесплатно.
// changed=false — переход не применился (заказ уже оплачен / нет такого заказа).
func MarkCanceled(ctx context.Context, db *sql.DB, id string) (bool, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE orders SET payment_status = ? WHERE id = ? AND payment_status = ?`,
		PaymentCanceled, id, PaymentPending)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MarkRefunded фиксирует, что за заказ денег не осталось: только из paid. Доступ к
// песне после этого пропадает сам — handleAudioProxy и handleRegenerate требуют
// payment_status == paid. Вызывать ТОЛЬКО когда по заказу не осталось живого
// успешного платежа (см. Server.livePaymentsLeft в httpserver): при возврате
// лишнего платежа заказ обязан остаться paid.
// changed=false — переход не применился (заказ не был paid / нет такого заказа).
func MarkRefunded(ctx context.Context, db *sql.DB, id string) (bool, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE orders SET payment_status = ? WHERE id = ? AND payment_status = ?`,
		PaymentRefunded, id, PaymentPaid)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetRefundedKopecks записывает суммарно возвращённую по заказу сумму в копейках.
// Перезапись, а не накопление: значение каждый раз пересчитывается из API ЮKassa,
// потому что вебхуки ретраятся и сложение дало бы двойной учёт.
func SetRefundedKopecks(ctx context.Context, db *sql.DB, id string, kopecks int64) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET refunded_kopecks = ? WHERE id = ?`, kopecks, id)
	return err
}

func ApplyDiscount(ctx context.Context, db *sql.DB, id string, code string, amount int) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET discount_code = ?, discount_amount = ? WHERE id = ?`, code, amount, id)
	return err
}

// ProcessingStartedAtTime — момент старта генерации; у заказов, созданных до
// появления колонки, откатывается к CreatedAt (честная нижняя граница).
func (o Order) ProcessingStartedAtTime() time.Time {
	if o.ProcessingStartedAt.Valid {
		if t, err := time.Parse(time.RFC3339, o.ProcessingStartedAt.String); err == nil {
			return t
		}
	}
	return o.CreatedAt
}

// demoCompleteAfter — сколько заказ висит в processing до демо-фолбэка.
// DEMO_COMPLETE_AFTER_SECONDS переопределяет порог ТОЛЬКО для дев-стенда
// (scripts/stand.sh start go ставит секунды, чтобы прогоны флоу не ждали
// 15 минут без ключей генерации); на проде переменная не задаётся.
func demoCompleteAfter() time.Duration {
	if s := os.Getenv("DEMO_COMPLETE_AFTER_SECONDS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 15 * time.Minute
}

func MaybeDemoComplete(ctx context.Context, db *sql.DB, id string) error {
	o, err := GetOrder(ctx, db, id)
	if err != nil {
		return err
	}
	if o.DemoCompleted {
		return nil
	}
	// Только заказы, застрявшие в processing: генерация пошла, а колбэк Kie.ai не
	// пришёл. Заказы в created не трогаем — там генерация ещё не начиналась.
	//
	// Оплата больше НЕ требуется. Требование `payment_status = paid` осталось от
	// модели «сначала оплата, потом генерация»; с генерацией до оплаты оно
	// означало, что потерянный колбэк Kie.ai оставляет неоплаченный заказ в
	// processing навсегда — покупатель висит на экране «Пишем вашу песню…» и до
	// оплаты не доходит вообще. Фолбэк отдаёт демо-трек, то есть бесплатно
	// раздать настоящую песню он не может.
	if o.Status != "processing" {
		return nil
	}
	if time.Since(o.ProcessingStartedAtTime()) < demoCompleteAfter() {
		return nil
	}
	audio, _ := json.Marshal([]string{DemoAudioURL})
	_, err = db.ExecContext(ctx, `UPDATE orders SET demo_completed = 1, audio_urls = ?, lyrics_text = ?, status = 'completed', `+markReadyExpr+` WHERE id = ?`,
		string(audio), demoLyrics, id)
	return err
}

func SaveLyrics(ctx context.Context, db *sql.DB, id string, lyrics string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET lyrics_text = ? WHERE id = ?`, StripStressMarks(lyrics), id)
	return err
}

// SetVoiceStyle записывает выбранный пол вокала в input_payload.musicPreferences.voiceStyle.
// Без этого выбор голоса живёт одну доработку: следующая без явного выбора
// откатывается к значению из исходного заказа (например «Surprise me» → female),
// и человек снова получает не тот голос, который уже попросил.
func SetVoiceStyle(ctx context.Context, db *sql.DB, id, voiceStyle string) error {
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT input_payload FROM orders WHERE id = ?`, id).Scan(&raw); err != nil {
		return err
	}
	payload := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &payload)
	mp, _ := payload["musicPreferences"].(map[string]any)
	if mp == nil {
		mp = map[string]any{}
		payload["musicPreferences"] = mp
	}
	mp["voiceStyle"] = voiceStyle
	updated, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE orders SET input_payload = ? WHERE id = ?`, string(updated), id)
	return err
}

// CompleteWithDemoAudio marks order done with demo audio track and given lyrics.
// Used when Kie.ai is unavailable but real lyrics were generated.
func CompleteWithDemoAudio(ctx context.Context, db *sql.DB, id string, lyrics string) error {
	audio, _ := json.Marshal([]string{DemoAudioURL})
	if lyrics == "" {
		lyrics = demoLyrics
	}
	_, err := db.ExecContext(ctx, `UPDATE orders SET demo_completed = 1, audio_urls = ?, lyrics_text = ?, status = 'completed', `+markReadyExpr+` WHERE id = ?`,
		string(audio), StripStressMarks(lyrics), id)
	return err
}

func SaveRelayResult(ctx context.Context, db *sql.DB, id string, audioURLs []string, lyrics string, title string) error {
	b, _ := json.Marshal(audioURLs)
	_, err := db.ExecContext(ctx, `UPDATE orders SET audio_urls = ?, lyrics_text = ?, status = 'completed', demo_completed = 1, `+markReadyExpr+` WHERE id = ?`,
		string(b), StripStressMarks(lyrics+"\n# "+title), id)
	return err
}

// SaveKieIDs сохраняет идентификаторы последнего прогона генерации у Kie.ai:
// задачу (task_id) и треки, которые она вернула (audio id каждого варианта).
//
// Сами по себе они ничего не меняют — это данные, без которых недоступны
// точечная правка отрезка аудио (replace-section) и Persona (единый голос между
// доработками): оба требуют taskId + audioId, а восстановить их постфактум
// нечем. Значения перезаписываются целиком: актуален всегда последний прогон,
// доработка начинает новый.
func SaveKieIDs(ctx context.Context, db *sql.DB, id, taskID string, audioIDs []string) error {
	var task any
	if taskID != "" {
		task = taskID
	}
	var ids any
	if len(audioIDs) > 0 {
		b, err := json.Marshal(audioIDs)
		if err != nil {
			return err
		}
		ids = string(b)
	}
	_, err := db.ExecContext(ctx, `UPDATE orders SET kie_task_id = ?, kie_audio_ids = ? WHERE id = ?`, task, ids, id)
	return err
}

// MarkDelivered records that the "Ваша песня готова" email actually reached the
// customer. Called only after a successful send — a failed or skipped email
// leaves the order at DeliveryReady, so the admin list shows the difference.
// MarkDelivered фиксирует, что письмо «Ваша песня готова» ушло. delivered_at
// пишется тем же запросом: от него отсчитывается письмо «как всё прошло», а
// delivery_status времени не хранит. COALESCE сохраняет первую отметку — повторная
// доставка после доработки не сдвигает отсчёт.
func MarkDelivered(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE orders SET delivery_status = ?, delivered_at = COALESCE(delivered_at, ?) WHERE id = ?`,
		DeliveryDone, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// IncrementRegeneration atomically increments regeneration_count and resets order to processing.
// Returns the new count, or error if max reached.
//
// Лимиты берутся из настроек (RegenerationLimits): у клиентских заказов свой,
// у тестовых из /admin/ads — свой, и 0 в любом из них означает «без ограничений».
// Проверка остаётся внутри одного UPDATE, иначе два одновременных запроса на
// доработку могли бы вместе перескочить лимит.
func IncrementRegeneration(ctx context.Context, db *sql.DB, id string) (int, error) {
	client, internal := RegenerationLimits(ctx, db)
	// Текущий текст уезжает в историю до того, как доработка его перезатрёт: без
	// этого удачный вариант терялся навсегда, а доработки превращались в рулетку —
	// «попробовала раз 15, и всё хуже». Снимок делается до UPDATE, но записывается
	// после успешного — иначе отказ по лимиту плодил бы версии на пустом месте.
	snapshot := currentLyrics(ctx, db, id)
	res, err := db.ExecContext(ctx,
		`UPDATE orders SET regeneration_count = regeneration_count + 1, status = 'processing', processing_started_at = ?, audio_urls = '[]',
		        delivery_status = ?
		 WHERE id = ? AND CASE WHEN discount_code IN (?, ?)
		            THEN (? = 0 OR regeneration_count < ?)
		            ELSE (? = 0 OR regeneration_count < ?) END`,
		time.Now().UTC().Format(time.RFC3339), DeliveryPending, id, InternalDiscountCode, SubscriptionDiscountCode, internal, internal, client, client)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var count int
		err := db.QueryRowContext(ctx, `SELECT regeneration_count FROM orders WHERE id = ?`, id).Scan(&count)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, err
		}
		return count, errors.New("max_regenerations_reached")
	}
	var newCount int
	_ = db.QueryRowContext(ctx, `SELECT regeneration_count FROM orders WHERE id = ?`, id).Scan(&newCount)
	// Номер версии — счётчик до инкремента: первый текст заказа сохраняется как
	// версия 0. Ошибка записи истории не отменяет доработку: потерять историю
	// неприятно, а не начать доработку — прямой отказ клиенту.
	if snapshot != "" {
		if err := appendLyricsVersion(ctx, db, id, newCount-1, snapshot); err != nil {
			log.Printf("[store] order=%s append lyrics version err=%v", id, err)
		}
	}
	return newCount, nil
}

// LyricsVersion — текст песни, заменённый доработкой. Текущий текст лежит в
// orders.lyrics_text и в историю не дублируется.
type LyricsVersion struct {
	Version   int
	Lyrics    string
	CreatedAt time.Time
}

func currentLyrics(ctx context.Context, db *sql.DB, id string) string {
	var lyrics sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT lyrics_text FROM orders WHERE id = ?`, id).Scan(&lyrics); err != nil {
		return ""
	}
	return strings.TrimSpace(lyrics.String)
}

// appendLyricsVersion сохраняет заменяемый текст. UNIQUE(order_id, version)
// делает повторный вызов для того же прогона безобидным: одна доработка — одна
// версия, даже если её запуск отретраили.
//
// Совпадающий с последней версией текст не записывается: две доработки подряд, из
// которых первая не принесла нового текста (Kie.ai не ответил, генерация упала),
// иначе положили бы в историю два одинаковых варианта, между которыми нечего
// выбирать.
func appendLyricsVersion(ctx context.Context, db *sql.DB, id string, version int, lyrics string) error {
	var last string
	err := db.QueryRowContext(ctx,
		`SELECT lyrics FROM lyrics_versions WHERE order_id = ? ORDER BY version DESC LIMIT 1`, id).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if last == lyrics {
		return nil
	}
	_, err = db.ExecContext(ctx,
		`INSERT OR IGNORE INTO lyrics_versions (order_id, version, lyrics, created_at) VALUES (?, ?, ?, ?)`,
		id, version, lyrics, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ListLyricsVersions возвращает прошлые версии текста заказа, свежие первыми.
func ListLyricsVersions(ctx context.Context, db *sql.DB, id string) ([]LyricsVersion, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT version, lyrics, created_at FROM lyrics_versions WHERE order_id = ? ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LyricsVersion
	for rows.Next() {
		var v LyricsVersion
		var created string
		if err := rows.Scan(&v.Version, &v.Lyrics, &created); err != nil {
			return nil, err
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Visit — визит на лендинг с источником трафика, снятым с посадочной страницы.
type Visit struct {
	SessionID       string
	Day             string // YYYY-MM-DD, ключ агрегации по дням
	Path            string
	ThemeSlug       sql.NullString
	Source          string
	Campaign        sql.NullString
	AttributionJSON sql.NullString
	FlowVariant     sql.NullString // плечо A/B-теста флоу, назначенное этой сессии
	CreatedAt       time.Time
}

// RecordVisit сохраняет визит. Повтор в той же сессии за тот же день по тому же
// пути игнорируется — таблица не растёт от перезагрузок страницы.
func RecordVisit(ctx context.Context, db *sql.DB, v Visit) error {
	_, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO visits (session_id, day, path, theme_slug, source, campaign, attribution, flow_variant, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.SessionID, v.Day, v.Path, v.ThemeSlug, v.Source, v.Campaign, v.AttributionJSON, v.FlowVariant,
		v.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// VisitStat — уники за день по источнику.
type VisitStat struct {
	Day      string
	Source   string
	Visitors int
}

// VisitStats считает уникальные сессии по дням и источникам, начиная с sinceDay.
// ArmVisitStat — визиты в разрезе плеча A/B за период. Считается по visits, а
// не по orders: конверсию плеча надо мерить от визита, иначе плечо с более
// коротким путём до заказа выигрывает автоматически.
type ArmVisitStat struct {
	Arm      string
	Visitors int
}

// VisitArmStats — уникальные посетители по плечам с указанного дня. Визиты без
// плеча (заходы до сплита, прямые ссылки мимо ротации) в выборку не попадают:
// делить их между плечами было бы вымыслом.
func VisitArmStats(ctx context.Context, db *sql.DB, sinceDay string) ([]ArmVisitStat, error) {
	rows, err := db.QueryContext(ctx, `
SELECT flow_variant, COUNT(DISTINCT session_id)
FROM visits WHERE day >= ? AND COALESCE(flow_variant,'') <> ''
GROUP BY flow_variant`, sinceDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArmVisitStat
	for rows.Next() {
		var s ArmVisitStat
		if err := rows.Scan(&s.Arm, &s.Visitors); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func VisitStats(ctx context.Context, db *sql.DB, sinceDay string) ([]VisitStat, error) {
	rows, err := db.QueryContext(ctx, `
SELECT day, source, COUNT(DISTINCT session_id)
FROM visits WHERE day >= ?
GROUP BY day, source`, sinceDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VisitStat
	for rows.Next() {
		var s VisitStat
		if err := rows.Scan(&s.Day, &s.Source, &s.Visitors); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
