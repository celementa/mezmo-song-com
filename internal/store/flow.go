// Плечи A/B-тестов флоу (см. CLAUDE.md, раздел «A/B-тесты флоу»): настройки,
// которыми плечи различаются, и данные заказа, которые эти настройки двигают.
//
// Все настройки правятся на /admin/settings без пересборки — это единственная
// причина, по которой они не константы: тест цены и краны расхода на Kie нужно
// уметь трогать на живом трафике. Конвенция проекта сохранена: 0 означает
// «выключено / без ограничений», а не «ноль».
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Ключи настроек флоу.
const (
	// SettingFlowArmsEnabled — какие плечи в ротации, CSV из id плеч.
	// Одно плечо в списке = сплита нет, весь трафик идёт по нему.
	SettingFlowArmsEnabled = "flow.arms_enabled"
	// SettingFlowArmWeights — веса сплита в виде `id:вес,id:вес`. Плечо без
	// веса получает 1. Формат с явными id, а не позиционный CSV: список
	// включённых плеч правится отдельным полем, и позиции разъехались бы.
	SettingFlowArmWeights = "flow.arm_weights"
	// SettingFlowPricePrefix + id плеча — цена разовой покупки этого плеча.
	// Значение перекрывает PAYMENT_AMOUNT_RUBLES; env остаётся фолбэком, чтобы
	// цена не имела двух источников истины.
	SettingFlowPricePrefix = "flow.price."

	// SettingSaveCardInFunnel — просить ли ЮKassa сохранить карту при оплате
	// песни в воронках. «1» — просим: апсейл «безлимит за 0 ₽ сегодня»
	// включается в один клик по уже привязанной карте. «0» (дефолт) — не
	// просим, и на форме оплаты появляются ВСЕ подключённые магазину способы.
	//
	// Это и есть суть настройки: сохранить способ оплаты умеет только карта,
	// поэтому просьба о сохранении убирает с формы СБП, SberPay и Mir Pay.
	// Выбор между «сколько людей вообще заплатит» и «сколько подпишется в один
	// клик» — продуктовый, чисел заранее нет, поэтому он краном в админке, а не
	// константой в коде. Дефолт «0»: доступность оплаты важнее апсейла.
	SettingSaveCardInFunnel = "payments.save_card_in_funnel"

	// SettingPreviewEnabled — «1» (дефолт) генерация до оплаты работает, «0» —
	// выключена целиком: шаги «пишем песню» и «слушайте» выпадают из воронок, а
	// заказ идёт прямо к оплате. Нужен как рубильник на случай, когда Kie.ai
	// отвечает долго и превью превращается в бесконечное ожидание: продавать
	// важнее, чем демонстрировать.
	SettingPreviewEnabled = "preview.enabled"
	// SettingPreviewDailyBudget — суточный потолок генераций до оплаты на весь
	// сайт. Исчерпан — превью выключается и флоу деградирует к «оплата до
	// генерации»: доступность оплаты важнее демонстрации.
	SettingPreviewDailyBudget = "preview.daily_budget"

	// SettingDeliveryDelayMinutes — на сколько придерживается обычная доставка.
	// Ускорение (шаг 8) продаёт снятие именно этой задержки: 0 — задержки нет,
	// и тогда ускорять нечего.
	SettingDeliveryDelayMinutes = "delivery.normal_delay_minutes"

	// SettingUpsellSpeedupPrice / SettingUpsellStemsPrice — цены апсейлов
	// шагов 8 и 12. 0 — шаг скрыт.
	SettingUpsellSpeedupPrice = "upsell.speedup_price"
	SettingUpsellStemsPrice   = "upsell.stems_price"
)

// Дефолты настроек флоу.
const (
	DefaultPreviewDailyBudget = 200
	// Задержка обычной доставки по умолчанию ВЫКЛЮЧЕНА. Включается в админке
	// вместе с ценой ускорения — это осознанное решение: пока её нет, песня
	// уходит клиенту сразу, как и до появления флоу, и ускорять нечего.
	DefaultDeliveryDelayMinutes = 0
	DefaultUpsellSpeedupPrice   = 290
	// Стемы по умолчанию выключены (0), хотя в документе цена 190 ₽: отдавать
	// инструментал и вокал отдельно пока нечем — разделение дорожек у Kie.ai не
	// подключено, и заказ-апсейл создаётся как задача на ручную выдачу. Цена
	// ставится в админке в тот момент, когда выдача появится.
	DefaultUpsellStemsPrice = 0
)

// UpsellStems — значение orders.upsell_kind у дочернего заказа на стемы
// (инструментал и вокал отдельно, шаг 12).
//
// Апсейл после оплаты — отдельный заказ, а не второй платёж по тому же:
// refundExtraPayments считает два живых успешных платежа одного заказа дублем
// и возвращает лишний автоматически (T-020). Отдельный заказ обходит это без
// правок в логике возвратов.
const UpsellStems = "stems"

// FlowArmsEnabled — включённые плечи. Пустая или битая настройка отдаёт
// fallback: сайт без сплита работать обязан.
func FlowArmsEnabled(ctx context.Context, db *sql.DB, fallback []string) []string {
	raw := GetSetting(ctx, db, SettingFlowArmsEnabled, "")
	out := make([]string, 0, 4)
	for _, item := range strings.Split(raw, ",") {
		if id := strings.TrimSpace(item); id != "" {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

// FlowArmWeights — веса сплита по id плеча. Отсутствующий или нулевой вес
// читается как 1: плечо, включённое в ротацию, не должно молча получать 0%
// трафика из-за опечатки в другом поле.
func FlowArmWeights(ctx context.Context, db *sql.DB) map[string]int {
	weights := map[string]int{}
	for _, pair := range strings.Split(GetSetting(ctx, db, SettingFlowArmWeights, ""), ",") {
		id, value, ok := strings.Cut(pair, ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n <= 0 {
			continue
		}
		weights[strings.TrimSpace(id)] = n
	}
	return weights
}

// FlowArmPrice — цена плеча; fallback приходит из определения плеча в коде
// (в конечном счёте из PAYMENT_AMOUNT_RUBLES).
func FlowArmPrice(ctx context.Context, db *sql.DB, armID string, fallback int) int {
	price := settingInt(ctx, db, SettingFlowArmPriceKey(armID), fallback)
	if price <= 0 {
		return fallback
	}
	return price
}

// SettingFlowArmPriceKey — ключ настройки цены плеча.
func SettingFlowArmPriceKey(armID string) string { return SettingFlowPricePrefix + armID }

// PreviewEnabled — включена ли генерация до оплаты. Значение «0» выключает её
// для всех воронок сразу.
// SaveCardInFunnel — просить ли сохранение карты у платежа за песню.
// Дефолт false: на форме оплаты видны все способы, включая СБП.
func SaveCardInFunnel(ctx context.Context, db *sql.DB) bool {
	return GetSetting(ctx, db, SettingSaveCardInFunnel, "0") == "1"
}

func PreviewEnabled(ctx context.Context, db *sql.DB) bool {
	return GetSetting(ctx, db, SettingPreviewEnabled, "1") != "0"
}

// PreviewDailyBudget — суточный потолок генераций до оплаты на весь сайт.
// 0 — без ограничения.
//
// Лимиты «на человека» и «на адрес» живут отдельно (FreeGenerationLimits): это
// единый механизм для обеих воронок, а бюджет — общий предохранитель кошелька
// поверх них.
func PreviewDailyBudget(ctx context.Context, db *sql.DB) int {
	return settingInt(ctx, db, SettingPreviewDailyBudget, DefaultPreviewDailyBudget)
}

// DeliveryDelay — задержка обычной доставки.
func DeliveryDelay(ctx context.Context, db *sql.DB) time.Duration {
	return time.Duration(settingInt(ctx, db, SettingDeliveryDelayMinutes, DefaultDeliveryDelayMinutes)) * time.Minute
}

// UpsellPrices — цены апсейлов ускорения и стемов; 0 — апсейл выключен.
func UpsellPrices(ctx context.Context, db *sql.DB) (speedup, stems int) {
	return settingInt(ctx, db, SettingUpsellSpeedupPrice, DefaultUpsellSpeedupPrice),
		settingInt(ctx, db, SettingUpsellStemsPrice, DefaultUpsellStemsPrice)
}

// --- Схема ---

// migrateFlow добавляет колонки под A/B-флоу. Бэкфилла нет и не нужно:
// пустой flow_variant означает «заказ создан до сплита», пустой deliver_after —
// «доставку не задерживаем» (ровно так и работали все прошлые заказы).
func migrateFlow(db *sql.DB) error {
	for _, stmt := range []string{
		// Плечо A/B-теста, в котором создан заказ. Знаменатель того же теста
		// живёт в visits.flow_variant: конверсию надо считать от визита, иначе
		// плечо с более короткой формой выигрывает автоматически.
		`ALTER TABLE orders ADD COLUMN flow_variant TEXT`,
		`ALTER TABLE visits ADD COLUMN flow_variant TEXT`,
		// Генерация до оплаты: отметка старта плюс посетитель, которому она
		// досталась. Сессия и IP хранятся ради лимита, а не ради аналитики.
		`ALTER TABLE orders ADD COLUMN preview_started_at TEXT`,
		`ALTER TABLE orders ADD COLUMN preview_session TEXT`,
		`ALTER TABLE orders ADD COLUMN preview_ip TEXT`,
		// Ключ 15-секундного отрывка в MinIO. Отдельный объект, а не тот же
		// трек: остановить полный mp3 на 15-й секунде на фронте — не защита,
		// ссылка на весь файл видна в devtools.
		`ALTER TABLE orders ADD COLUMN preview_audio_url TEXT`,
		// Отложенная доставка: до этого момента песня не показывается и письмо
		// не уходит. Ускорение (шаг 8) обнуляет колонку.
		`ALTER TABLE orders ADD COLUMN deliver_after TEXT`,
		`ALTER TABLE orders ADD COLUMN speedup_purchased INTEGER NOT NULL DEFAULT 0`,
		// Апсейл после оплаты — дочерний заказ, а не второй платёж по родителю.
		`ALTER TABLE orders ADD COLUMN parent_order_id TEXT`,
		`ALTER TABLE orders ADD COLUMN upsell_kind TEXT`,
		// Отметки писем флоу. delivered_at появился здесь, а не рядом с
		// delivery_status: письмо «как всё прошло» отсчитывается от факта
		// доставки, а delivery_status времени не хранит.
		`ALTER TABLE orders ADD COLUMN welcome_sent_at TEXT`,
		`ALTER TABLE orders ADD COLUMN delivered_at TEXT`,
		`ALTER TABLE orders ADD COLUMN followup_sent_at TEXT`,
		// Момент старта генерации: от него считается окно демо-фолбэка. До
		// генерации до оплаты окно считалось от created_at, и заказ, созданный
		// на шаге email, мог получить демо-трек через секунды после «создать».
		`ALTER TABLE orders ADD COLUMN processing_started_at TEXT`,
		// Сохранённая карта с оплаты песни — под апсейл «подписка за 0 ₽ сегодня».
		`ALTER TABLE orders ADD COLUMN yoo_payment_method_id TEXT`,
	} {
		// Повторный ALTER на уже существующей колонке — ошибка, и это
		// нормальный путь на каждом рестарте: миграций у проекта нет.
		_, _ = db.Exec(stmt)
	}
	// Суточные лимиты превью считаются COUNT(*) по preview_started_at.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_preview_started
	                      ON orders(preview_started_at) WHERE preview_started_at IS NOT NULL`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_parent
	                      ON orders(parent_order_id) WHERE parent_order_id IS NOT NULL`); err != nil {
		return err
	}
	// Заходы по рекламной ссылке с плечом, которое не сработало: опечатка в
	// имени или выключенное плечо. Без этой таблицы такой заход неотличим от
	// обычного — человек молча попадает в ротацию, и кампания месяц льёт
	// трафик не в то плечо по не той цене.
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS arm_link_misses (
		day    TEXT NOT NULL,
		name   TEXT NOT NULL,
		reason TEXT NOT NULL,
		hits   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (day, name, reason)
	)`)
	return err
}

// Причины, по которым ссылка с плечом не сработала.
const (
	ArmMissUnknown  = "unknown"  // такого имени плеча нет — опечатка в ссылке
	ArmMissDisabled = "disabled" // плечо есть, но выключено в админке
)

// RecordArmLinkMiss считает заход по нерабочей ссылке с плечом. Имя пишется
// как пришло (обрезанное): именно по нему потом узнают, какая кампания
// настроена с опечаткой.
func RecordArmLinkMiss(ctx context.Context, db *sql.DB, name, reason string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" || reason == "" {
		return nil
	}
	if len(name) > 64 {
		name = name[:64]
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO arm_link_misses (day, name, reason, hits) VALUES (?, ?, ?, 1)
		ON CONFLICT(day, name, reason) DO UPDATE SET hits = hits + 1`,
		now.UTC().Format("2006-01-02"), name, reason)
	return err
}

// --- Плечо заказа ---

// SetOrderFlow записывает плечо и цену, назначенные заказу сервером. Цена
// приходит из плеча, а не из тела запроса: иначе покупатель выбирал бы себе
// цену сам, подставив чужой flowVariant.
func SetOrderFlow(ctx context.Context, db *sql.DB, id, arm string, basePrice int) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET flow_variant = ?, base_price = ? WHERE id = ?`, arm, basePrice, id)
	return err
}

// --- Генерация до оплаты ---

// ClaimPreview отмечает старт генерации до оплаты. Возвращает false, если
// превью по заказу уже стартовало: гард в WHERE делает двойной клик по кнопке
// «слушать» безопасным без блокировок на стороне приложения.
func ClaimPreview(ctx context.Context, db *sql.DB, id, session, ip string, now time.Time) (bool, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE orders SET preview_started_at = ?, preview_session = ?, preview_ip = ?
		WHERE id = ? AND preview_started_at IS NULL`,
		now.UTC().Format(time.RFC3339), session, ip, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CountPreviewsSince — сколько генераций до оплаты стартовало с момента since
// (суточный бюджетный кран на весь сайт).
func CountPreviewsSince(ctx context.Context, db *sql.DB, since time.Time) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE preview_started_at >= ?`,
		since.UTC().Format(time.RFC3339)).Scan(&n)
	return n, err
}

// SetPreviewAudio сохраняет ключ 15-секундного отрывка.
func SetPreviewAudio(ctx context.Context, db *sql.DB, id, key string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET preview_audio_url = ? WHERE id = ?`, key, id)
	return err
}

// --- Отложенная доставка и ускорение ---

// SetDeliverAfter придерживает доставку до указанного момента.
func SetDeliverAfter(ctx context.Context, db *sql.DB, id string, at time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET deliver_after = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339), id)
	return err
}

// ClearDeliveryHold снимает задержку доставки: заказ отдаётся сразу. Так
// работает купленное ускорение — и апсейл варианта 1 (BuySpeedup), и допник
// воронки, который оплачивается отдельным платежом уже после основного.
func ClearDeliveryHold(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET deliver_after = NULL WHERE id = ?`, id)
	return err
}

// BuySpeedup поднимает цену заказа на price и снимает задержку доставки.
// Возвращает false, если ускорение уже куплено или заказ уже оплачен: цену
// оплаченного заказа менять нельзя — покупатель заплатил другую сумму.
func BuySpeedup(ctx context.Context, db *sql.DB, id string, price int) (bool, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE orders SET base_price = base_price + ?, speedup_purchased = 1, deliver_after = NULL
		WHERE id = ? AND speedup_purchased = 0 AND payment_status = ?`,
		price, id, PaymentPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeliveryHeld — доставка заказа ещё придержана (песня готова, но показывать
// её рано). Единственное место, где читается deliver_after, чтобы правило
// «придержано» не разъехалось между страницей заказа и письмом.
func DeliveryHeld(o Order, now time.Time) bool {
	if !o.DeliverAfter.Valid {
		return false
	}
	at, err := time.Parse(time.RFC3339, o.DeliverAfter.String)
	if err != nil {
		return false
	}
	return now.Before(at)
}

// ListOrdersDueForDelivery — заказы, у которых песня готова, задержка вышла, а
// письмо ещё не уходило. Планировщик досылает их: колбэк Kie.ai в момент
// задержки уже прошёл, и второго не будет.
func ListOrdersDueForDelivery(ctx context.Context, db *sql.DB, now time.Time) ([]Order, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+orderColumns+` FROM orders
		WHERE status = 'completed' AND payment_status = 'paid' AND delivery_status = ?
		  AND deliver_after IS NOT NULL AND deliver_after <= ?
		ORDER BY deliver_after LIMIT 200`,
		DeliveryReady, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

// --- Письма флоу ---

// ClaimWelcomeEmail — заявка на приветственное письмо (шаг 3). Гард
// welcome_sent_at IS NULL делает повторный вызов безобидным.
func ClaimWelcomeEmail(ctx context.Context, db *sql.DB, id string, now time.Time) (bool, error) {
	return claimEmailMark(ctx, db, id, "welcome_sent_at", now)
}

// claimEmailMark ставит отметку об отправленном письме, если её ещё не было.
// Тот же приём, что у MarkPurchaseTracked: атомарная заявка вместо чтения с
// последующей записью, иначе два тика планировщика отправят письмо дважды.
func claimEmailMark(ctx context.Context, db *sql.DB, id, column string, now time.Time) (bool, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE orders SET `+column+` = ? WHERE id = ? AND `+column+` IS NULL`,
		now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// --- Апсейл отдельным заказом ---

// SetUpsellParent привязывает дочерний заказ-апсейл к родителю.
func SetUpsellParent(ctx context.Context, db *sql.DB, id, parentID, kind string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET parent_order_id = ?, upsell_kind = ? WHERE id = ?`,
		parentID, kind, id)
	return err
}

// HasUpsell — по заказу уже создан апсейл этого вида (оплаченный или ждущий
// оплаты). Второй экран «добавить стемы» тому же покупателю не нужен.
func HasUpsell(ctx context.Context, db *sql.DB, parentID, kind string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE parent_order_id = ? AND upsell_kind = ?`, parentID, kind).Scan(&n)
	return n > 0, err
}

// MergeInputPayload дописывает поля в input_payload заказа (шаги брифа после
// того, как заказ уже создан ради сбора email). Правка разрешена только до
// старта генерации: после него бриф уже уехал в модель, и менять его молча —
// значит показывать клиенту одно, а петь другое.
func MergeInputPayload(ctx context.Context, db *sql.DB, id string, patch map[string]any) (bool, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT input_payload FROM orders WHERE id = ? AND status = 'created'`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		payload = map[string]any{}
	}
	for k, v := range patch {
		payload[k] = v
	}
	merged, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	res, err := db.ExecContext(ctx, `UPDATE orders SET input_payload = ? WHERE id = ? AND status = 'created'`, string(merged), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// --- Подписка из заказа ---

// SetOrderPaymentMethod запоминает способ оплаты, сохранённый ЮKassa при
// оплате песни. Пишется из вебхука после сверки платежа с API.
func SetOrderPaymentMethod(ctx context.Context, db *sql.DB, id, methodID string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET yoo_payment_method_id = ? WHERE id = ?`, methodID, id)
	return err
}

// AttachOrderToSubscription переводит заказ в «песню по подписке»: подписка
// оплатила её вместо разовой покупки (плечо v1_price, экран сравнения до
// оплаты). Заказ становится оплаченным с меткой SUBSCRIPTION и нулевой ценой —
// так же выглядят песни, созданные из кабинета подписчика. Возвращает false,
// если заказ уже оплачен: деньги за него взяты другим путём, переписывать
// сумму нельзя.
func AttachOrderToSubscription(ctx context.Context, db *sql.DB, id, subID string) (bool, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE orders SET subscription_id = ?, discount_code = ?, base_price = 0, discount_amount = NULL,
			payment_status = ?
		WHERE id = ? AND payment_status != ?`,
		subID, SubscriptionDiscountCode, PaymentPaid, id, PaymentPaid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LinkOrderSubscription помечает уже оплаченный заказ подпиской, оформленной
// после его оплаты (апсейл «0 ₽ сегодня»): цена и статус оплаты не меняются —
// песня куплена разово, подписка началась с неё.
func LinkOrderSubscription(ctx context.Context, db *sql.DB, id, subID string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET subscription_id = ? WHERE id = ?`, subID, id)
	return err
}
