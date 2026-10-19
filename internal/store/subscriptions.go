// Подписка «безлимит песен»: карта привязывается первым платежом ЮKassa
// (save_payment_method), дальше сервер сам списывает 2000 ₽ раз в календарный
// месяц. Здесь — состояние подписок, их платежи (дедуп вебхуков) и сессии
// личного кабинета. Правила денег (цена, ретраи) живут в httpserver.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Статусы подписки. Как и payment_status заказов, колонка TEXT — новые значения
// не требуют миграции.
const (
	SubPending  = "pending"  // создана, ждём первую оплату
	SubActive   = "active"   // оплачена, автосписания идут
	SubPastDue  = "past_due" // очередное списание не прошло, ретраим
	SubCanceled = "canceled" // отменена: списаний больше нет, доступ до access_until
)

// SubscriptionDiscountCode помечает заказы, созданные из кабинета подписчика:
// BasePrice=0, оплата не проходила — песня входит в подписку.
const SubscriptionDiscountCode = "SUBSCRIPTION"

// SettingSubscriptionDailyLimit — сколько песен подписчик может создать за
// сутки (защита безлимита от скриптов и перекупщиков). 0 — без ограничений.
const (
	SettingSubscriptionDailyLimit = "subscription.daily_limit"
	DefaultSubscriptionDailyLimit = 5
)

// SubscriptionDailyLimit возвращает действующий суточный лимит песен подписки.
func SubscriptionDailyLimit(ctx context.Context, db *sql.DB) int {
	return settingInt(ctx, db, SettingSubscriptionDailyLimit, DefaultSubscriptionDailyLimit)
}

// SettingSubscriptionTestEmails — CSV-список адресов, которым /subscribe
// активирует подписку без оплаты. Нужен, пока ЮKassa не включила рекуррентные
// платежи: тест всего флоу на проде. Пусто (дефолт) — тестовый вход выключен.
const SettingSubscriptionTestEmails = "subscription.test_emails"

// MockPaymentMethodID — заглушка вместо привязанной карты у подписок,
// активированных без оплаты (dev-режим и subscription.test_emails). Списывать
// по ней нечего, поэтому планировщик не шлёт по таким подпискам ни напоминаний,
// ни платежей: ЮKassa отвечает 400 «payment_method is not saved».
const MockPaymentMethodID = "mock-method"

// notMockMethodSQL — условие «у подписки настоящая привязка, а не заглушка».
// LIKE с префиксом: подписка из воронки в dev-режиме пишет "mock-method-<id заказа>".
const notMockMethodSQL = `COALESCE(yoo_payment_method_id, '') NOT LIKE '` + MockPaymentMethodID + `%'`

// IsMockPaymentMethod — заглушка ли это вместо привязанной карты.
func IsMockPaymentMethod(id string) bool {
	return strings.HasPrefix(id, MockPaymentMethodID)
}

// IsSubscriptionTestEmail — входит ли адрес в тестовый allowlist подписки.
func IsSubscriptionTestEmail(ctx context.Context, db *sql.DB, email string) bool {
	raw := GetSetting(ctx, db, SettingSubscriptionTestEmails, "")
	if raw == "" {
		return false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	for _, item := range strings.Split(raw, ",") {
		if strings.ToLower(strings.TrimSpace(item)) == email && email != "" {
			return true
		}
	}
	return false
}

type Subscription struct {
	ID              string
	Email           string // всегда в нижнем регистре
	CustomerName    sql.NullString
	Status          string
	YooMethodID     sql.NullString // сохранённый способ оплаты для автосписаний
	AccessUntil     sql.NullString // RFC3339; доступ к кабинету и создание песен
	NextChargeAt    sql.NullString // RFC3339; NULL после отмены
	CancelToken     string
	ReminderSentFor sql.NullString // next_charge_at, для которого уже ушло напоминание
	LastAttemptAt   sql.NullString // последняя попытка списания (для ретраев раз в 7 дней)
	AttributionJSON sql.NullString // рекламные метки визита, с которого оформлена подписка
	GAClientID      sql.NullString // client_id GA4 из куки _ga — для серверной конверсии
	YMClientID      sql.NullString // ClientID Метрики из куки _ym_uid — для офлайн-конверсии
	// PendingPaymentID — последний заведённый, но ещё не подтверждённый платёж
	// (первый или ребилл). По нему фоновая сверка с ЮKassa подхватывает оплату,
	// если вебхук не дошёл. Сбрасывается, когда платёж записан в subscription_payments.
	PendingPaymentID sql.NullString
	// PriceRub — цена подписки, зафиксированная при оформлении (плечо теста
	// может продавать подписку дешевле общей). 0 — цена по умолчанию.
	PriceRub  int
	CreatedAt time.Time
}

// AccessUntilTime — access_until как время; нулевое время, если доступа не было.
func (s Subscription) AccessUntilTime() time.Time {
	if !s.AccessUntil.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s.AccessUntil.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// NextChargeTime — next_charge_at как время; нулевое, если списаний не будет.
func (s Subscription) NextChargeTime() time.Time {
	if !s.NextChargeAt.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s.NextChargeAt.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// HasAccess — оплаченный период ещё идёт. Именно этим, а не статусом,
// определяется доступ: у past_due и canceled кабинет открыт до конца периода.
func (s Subscription) HasAccess(now time.Time) bool {
	au := s.AccessUntilTime()
	return !au.IsZero() && now.Before(au)
}

func migrateSubscriptions(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS subscriptions (
	id TEXT PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	customer_name TEXT,
	status TEXT NOT NULL DEFAULT 'pending',
	yoo_payment_method_id TEXT,
	access_until TEXT,
	next_charge_at TEXT,
	cancel_token TEXT NOT NULL,
	reminder_sent_for TEXT,
	last_charge_attempt_at TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	// Дедуп платежей подписки: вебхук ЮKassa ретраится, INSERT OR IGNORE по
	// yoo_payment_id гарантирует, что продление и welcome-письмо случатся один раз.
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS subscription_payments (
	yoo_payment_id TEXT PRIMARY KEY,
	subscription_id TEXT NOT NULL,
	amount_value TEXT NOT NULL,
	flow TEXT NOT NULL,
	created_at TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	// Сессии кабинета: вход по email без пароля, cookie живёт до access_until.
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS subscription_sessions (
	token TEXT PRIMARY KEY,
	subscription_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	// attribution — рекламные метки визита, с которого оформлена подписка:
	// источник прихода для дашборда подписок в /admin/analytics. Бэкфилла нет —
	// у старых подписок источник неизвестен.
	_, _ = db.Exec(`ALTER TABLE subscriptions ADD COLUMN attribution TEXT`)
	// price_rub — цена подписки на момент оформления: ценовое плечо v20 продаёт
	// подписку за 750 ₽/мес, и продление обязано идти по той же цене.
	// NULL — общая цена (subscriptionPriceRub на сервере).
	_, _ = db.Exec(`ALTER TABLE subscriptions ADD COLUMN price_rub INTEGER`)
	// ga_client_id / ym_client_id — идентификаторы браузера для серверных
	// конверсий по платежам подписки (GA4 MP и офлайн-конверсии Метрики);
	// pending_payment_id — платёж, которого ждём: ключ фоновой сверки с ЮKassa.
	_, _ = db.Exec(`ALTER TABLE subscriptions ADD COLUMN ga_client_id TEXT`)
	_, _ = db.Exec(`ALTER TABLE subscriptions ADD COLUMN ym_client_id TEXT`)
	_, _ = db.Exec(`ALTER TABLE subscriptions ADD COLUMN pending_payment_id TEXT`)
	// Заказы подписчиков связываются с подпиской напрямую — список «мои песни»
	// в кабинете и суточный лимит считаются по этой колонке.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN subscription_id TEXT`)
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_subscription ON orders(subscription_id) WHERE subscription_id IS NOT NULL`)
	return err
}

func newSubToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// AddCalendarMonth прибавляет календарный месяц с прижимом дня: 31 января →
// 28/29 февраля, а не 2–3 марта, как сделал бы AddDate.
func AddCalendarMonth(t time.Time) time.Time {
	t = t.UTC()
	y, m, d := t.Date()
	firstOfNext := time.Date(y, m+1, 1, t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	lastDay := firstOfNext.AddDate(0, 1, -1).Day() // последний день целевого месяца
	if d > lastDay {
		d = lastDay
	}
	return time.Date(firstOfNext.Year(), firstOfNext.Month(), d, t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// UpsertPendingSubscription готовит подписку к первой оплате. Если по email уже
// есть подписка с живым доступом — возвращает ErrSubscriptionActive: второй раз
// продавать подписку тому же адресу нельзя. Просроченная/отменённая запись
// переиспользуется (email в таблице уникален).
var ErrSubscriptionActive = errors.New("subscription already active")

func UpsertPendingSubscription(ctx context.Context, db *sql.DB, email, name string) (Subscription, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return Subscription{}, fmt.Errorf("subscriptions: empty email")
	}
	now := time.Now().UTC()
	existing, err := GetSubscriptionByEmail(ctx, db, email)
	switch {
	case err == nil:
		if existing.HasAccess(now) && existing.Status != SubCanceled {
			return Subscription{}, ErrSubscriptionActive
		}
		_, err = db.ExecContext(ctx, `
			UPDATE subscriptions SET customer_name = COALESCE(NULLIF(?, ''), customer_name), updated_at = ?
			WHERE id = ?`, strings.TrimSpace(name), now.Format(time.RFC3339), existing.ID)
		if err != nil {
			return Subscription{}, err
		}
		return GetSubscription(ctx, db, existing.ID)
	case errors.Is(err, ErrNotFound):
		sub := Subscription{
			ID:          "sub_" + newSubToken(),
			Email:       email,
			Status:      SubPending,
			CancelToken: newSubToken(),
			CreatedAt:   now,
		}
		if n := strings.TrimSpace(name); n != "" {
			sub.CustomerName = sql.NullString{String: n, Valid: true}
		}
		_, err = db.ExecContext(ctx, `
			INSERT INTO subscriptions (id, email, customer_name, status, cancel_token, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			sub.ID, sub.Email, sub.CustomerName, sub.Status, sub.CancelToken,
			now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			return Subscription{}, err
		}
		return sub, nil
	default:
		return Subscription{}, err
	}
}

const subColumns = `id, email, customer_name, status, yoo_payment_method_id, access_until, next_charge_at,
	cancel_token, reminder_sent_for, last_charge_attempt_at, attribution, price_rub, created_at,
	ga_client_id, ym_client_id, pending_payment_id`

func scanSubscription(row interface{ Scan(...any) error }) (Subscription, error) {
	var s Subscription
	var created string
	var price sql.NullInt64
	err := row.Scan(&s.ID, &s.Email, &s.CustomerName, &s.Status, &s.YooMethodID, &s.AccessUntil,
		&s.NextChargeAt, &s.CancelToken, &s.ReminderSentFor, &s.LastAttemptAt, &s.AttributionJSON, &price, &created,
		&s.GAClientID, &s.YMClientID, &s.PendingPaymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.PriceRub = int(price.Int64)
	s.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return s, nil
}

// SetSubscriptionPrice фиксирует цену подписки при оформлении. Меняется только
// у неактивной (pending) подписки: у живой цена — часть уже данного согласия
// на списания.
func SetSubscriptionPrice(ctx context.Context, db *sql.DB, id string, priceRub int) error {
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET price_rub = ?, updated_at = ? WHERE id = ? AND status = ?`,
		priceRub, time.Now().UTC().Format(time.RFC3339), id, SubPending)
	return err
}

func GetSubscription(ctx context.Context, db *sql.DB, id string) (Subscription, error) {
	return scanSubscription(db.QueryRowContext(ctx, `SELECT `+subColumns+` FROM subscriptions WHERE id = ?`, id))
}

func GetSubscriptionByEmail(ctx context.Context, db *sql.DB, email string) (Subscription, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	return scanSubscription(db.QueryRowContext(ctx, `SELECT `+subColumns+` FROM subscriptions WHERE email = ?`, email))
}

func GetSubscriptionByCancelToken(ctx context.Context, db *sql.DB, token string) (Subscription, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Subscription{}, ErrNotFound
	}
	return scanSubscription(db.QueryRowContext(ctx, `SELECT `+subColumns+` FROM subscriptions WHERE cancel_token = ?`, token))
}

// SetSubscriptionAttribution записывает рекламные метки оформления подписки.
// Last click wins: повторное оформление той же подписки перезатирает метки —
// как и у заказов, источником считается последний рекламный клик.
func SetSubscriptionAttribution(ctx context.Context, db *sql.DB, subID, attributionJSON string) error {
	if strings.TrimSpace(attributionJSON) == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET attribution = ? WHERE id = ?`, attributionJSON, subID)
	return err
}

// SetSubscriptionClientIDs сохраняет идентификаторы браузера для серверных
// конверсий. Пустые значения существующие не затирают: подписка вместо разовой
// покупки наследует их от заказа, а прямое оформление — из куки страницы.
func SetSubscriptionClientIDs(ctx context.Context, db *sql.DB, subID, gaClientID, ymClientID string) error {
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET
		ga_client_id = COALESCE(NULLIF(?, ''), ga_client_id),
		ym_client_id = COALESCE(NULLIF(?, ''), ym_client_id)
		WHERE id = ?`, strings.TrimSpace(gaClientID), strings.TrimSpace(ymClientID), subID)
	return err
}

// SetSubscriptionPendingPayment запоминает заведённый платёж, исход которого
// ещё не известен. Фоновая сверка (reconcile) опросит его в ЮKassa, если
// вебхук не придёт.
func SetSubscriptionPendingPayment(ctx context.Context, db *sql.DB, subID, paymentID string) error {
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET pending_payment_id = ? WHERE id = ?`, paymentID, subID)
	return err
}

// ListSubscriptionsAwaitingPayment — подписки с незакрытым pending_payment_id,
// заведённым не позже cutoff (свежий платёж ещё может подтвердить вебхук).
// Платёж, уже записанный в subscription_payments, не возвращается: его
// pending_payment_id RecordSubscriptionPayment снимает сам, а этот фильтр —
// страховка на случай записи мимо него.
func ListSubscriptionsAwaitingPayment(ctx context.Context, db *sql.DB, limit int) ([]Subscription, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+subColumns+` FROM subscriptions
		WHERE pending_payment_id IS NOT NULL AND pending_payment_id != ''
		  AND pending_payment_id NOT IN (SELECT yoo_payment_id FROM subscription_payments)
		ORDER BY updated_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SubscriptionPayment — платёж подписки вместе с данными подписчика: строка
// для «Платежей подписок» в админке и покупок в /admin/analytics.
type SubscriptionPayment struct {
	PaymentID      string
	SubscriptionID string
	AmountValue    string // как отдала ЮKassa, "750.00"
	Flow           string // subscription_first / subscription_rebill
	CreatedAt      time.Time
	Email          string
	SubStatus      string
	Attribution    sql.NullString
	GAClientID     sql.NullString
	YMClientID     sql.NullString
}

// AmountRub — сумма платежа целыми рублями (для отчётов и конверсий).
func (p SubscriptionPayment) AmountRub() int {
	whole, _, _ := strings.Cut(p.AmountValue, ".")
	n, _ := strconv.Atoi(strings.TrimSpace(whole))
	return n
}

// PaidSubscriptionIDs — подписки, за которые прошёл хотя бы один настоящий
// платёж ЮKassa. Тестовые активации ("mock-…") не считаются: денег за ними нет.
func PaidSubscriptionIDs(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT subscription_id FROM subscription_payments
		WHERE yoo_payment_id NOT LIKE 'mock-%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ListSubscriptionPaymentsSince — платежи подписок c указанного дня (RFC3339
// или YYYY-MM-DD), свежие сверху. Тестовые активации без денег ("mock-…") не
// возвращаются: это не выручка.
func ListSubscriptionPaymentsSince(ctx context.Context, db *sql.DB, since string) ([]SubscriptionPayment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.yoo_payment_id, p.subscription_id, p.amount_value, p.flow, p.created_at,
		       s.email, s.status, s.attribution, s.ga_client_id, s.ym_client_id
		FROM subscription_payments p
		JOIN subscriptions s ON s.id = p.subscription_id
		WHERE p.created_at >= ? AND p.yoo_payment_id NOT LIKE 'mock-%'
		ORDER BY p.created_at DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubscriptionPayment
	for rows.Next() {
		var p SubscriptionPayment
		var created string
		if err := rows.Scan(&p.PaymentID, &p.SubscriptionID, &p.AmountValue, &p.Flow, &created,
			&p.Email, &p.SubStatus, &p.Attribution, &p.GAClientID, &p.YMClientID); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SubscriptionPaymentSummary — сколько реальных платежей было у подписки, когда
// прошёл первый и каким был последний (первый платёж или продление). Нужна
// списку заказов: песня подписчика идёт заказом на 0, и без сводки не понять,
// платил ли человек вообще.
type SubscriptionPaymentSummary struct {
	Count    int
	FirstAt  string // RFC3339; пусто — платежей не было
	LastFlow string // SubFlowFirst | SubFlowRebill
}

func GetSubscriptionPaymentSummary(ctx context.Context, db *sql.DB, subID string) (SubscriptionPaymentSummary, error) {
	var out SubscriptionPaymentSummary
	var first, lastFlow sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(created_at),
		       (SELECT flow FROM subscription_payments WHERE subscription_id = ? AND yoo_payment_id NOT LIKE 'mock-%' ORDER BY created_at DESC LIMIT 1)
		FROM subscription_payments WHERE subscription_id = ? AND yoo_payment_id NOT LIKE 'mock-%'`, subID, subID).
		Scan(&out.Count, &first, &lastFlow)
	out.FirstAt, out.LastFlow = first.String, lastFlow.String
	return out, err
}

// ListSubscriptions возвращает все подписки (для дашборда /admin/analytics —
// их на порядки меньше, чем заказов, пагинация не нужна).
func ListSubscriptions(ctx context.Context, db *sql.DB) ([]Subscription, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+subColumns+` FROM subscriptions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Флоу подписочных платежей в metadata ЮKassa и в subscription_payments.
const (
	SubFlowFirst  = "subscription_first"
	SubFlowRebill = "subscription_rebill"
)

// RecordSubscriptionPayment фиксирует успешный платёж подписки и продлевает
// период. Возвращает first=true, если платёж записан впервые — только тогда
// вызывающий шлёт письмо (ретраи вебхука сюда приходят повторно).
//
// Точка продления — next_charge_at (не «сейчас»): оплата за два дня до
// списания не должна дарить лишние дни, а ребилл, прошедший с третьего ретрая,
// продлевает от плановой даты. Если плановая дата уже позади (долгие ретраи),
// продлеваем от «сейчас» — иначе новый период родился бы сразу просроченным.
func RecordSubscriptionPayment(ctx context.Context, db *sql.DB, subID, paymentID, amountValue, flow, methodID string) (first bool, err error) {
	if paymentID == "" {
		return false, fmt.Errorf("subscriptions: empty payment id")
	}
	now := time.Now().UTC()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO subscription_payments (yoo_payment_id, subscription_id, amount_value, flow, created_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(yoo_payment_id) DO NOTHING`,
		paymentID, subID, amountValue, flow, now.Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, tx.Commit() // дубль вебхука: период уже продлён этим платежом
	}

	sub, err := scanSubscription(tx.QueryRowContext(ctx, `SELECT `+subColumns+` FROM subscriptions WHERE id = ?`, subID))
	if err != nil {
		return false, err
	}
	base := now
	if flow == SubFlowRebill {
		if next := sub.NextChargeTime(); !next.IsZero() && next.After(now.Add(-24*time.Hour)) {
			base = next
		}
	}
	until := AddCalendarMonth(base).Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `
		UPDATE subscriptions SET
			status = ?,
			yoo_payment_method_id = COALESCE(NULLIF(?, ''), yoo_payment_method_id),
			access_until = ?,
			next_charge_at = ?,
			reminder_sent_for = NULL,
			last_charge_attempt_at = NULL,
			pending_payment_id = CASE WHEN pending_payment_id = ? THEN NULL ELSE pending_payment_id END,
			updated_at = ?
		WHERE id = ?`,
		SubActive, methodID, until, until, paymentID, now.Format(time.RFC3339), subID)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ForceSubscriptionChargeDue переносит плановое списание на «сейчас» и снимает
// отметку прошлой попытки — чтобы ближайший прогон планировщика списал деньги
// не дожидаясь даты. Нужна для проверки рекуррентки на стенде и для ручного
// повторного списания из админки; ok=false — подписка без сохранённой карты
// (pending/canceled) или не найдена.
func ForceSubscriptionChargeDue(ctx context.Context, db *sql.DB, subID string, now time.Time) (ok bool, err error) {
	res, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET next_charge_at = ?, last_charge_attempt_at = NULL, updated_at = ?
		WHERE id = ? AND status IN (?, ?)
		  AND yoo_payment_method_id IS NOT NULL AND yoo_payment_method_id != ''`,
		now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339), subID, SubActive, SubPastDue)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ActivateSubscriptionWithMethod включает подписку по уже сохранённой карте
// без платежа сейчас: доступ и первое списание — через календарный месяц.
// Это апсейл «безлимит за 0 ₽ сегодня» после оплаты песни: карта сохранена
// платежом за песню, а сама песня и есть первый месяц. Возвращает false, если
// подписка уже активна — второй раз бесплатный месяц не выдаётся.
func ActivateSubscriptionWithMethod(ctx context.Context, db *sql.DB, subID, methodID string, now time.Time) (bool, error) {
	until := AddCalendarMonth(now.UTC()).Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET
			status = ?, yoo_payment_method_id = ?, access_until = ?, next_charge_at = ?,
			reminder_sent_for = NULL, last_charge_attempt_at = NULL, updated_at = ?
		WHERE id = ? AND status = ?`,
		SubActive, methodID, until, until, now.UTC().Format(time.RFC3339), subID, SubPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GiftSubscriptionAccess выдаёт месяц доступа без карты и без списаний: статус
// сразу canceled (как у отменённой подписки с оплаченным периодом), поэтому
// планировщик её не трогает, а /subscribe даёт оформить продление по priceRub.
// false — подписка не в pending (доступ уже есть или был).
func GiftSubscriptionAccess(ctx context.Context, db *sql.DB, subID string, priceRub int, now time.Time) (bool, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET
			status = ?, access_until = ?, next_charge_at = NULL, yoo_payment_method_id = NULL,
			price_rub = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		SubCanceled, AddCalendarMonth(now.UTC()).Format(time.RFC3339), priceRub,
		now.UTC().Format(time.RFC3339), subID, SubPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CancelSubscription останавливает автосписания: статус canceled, способ оплаты
// стирается (требование отмены рекуррентки), доступ остаётся до access_until.
// ok=false — подписка не найдена или уже отменена.
func CancelSubscription(ctx context.Context, db *sql.DB, subID string) (email string, accessUntil time.Time, ok bool, err error) {
	var au sql.NullString
	err = db.QueryRowContext(ctx, `
		UPDATE subscriptions SET
			status = ?, next_charge_at = NULL, yoo_payment_method_id = NULL, updated_at = ?
		WHERE id = ? AND status != ?
		RETURNING email, access_until`,
		SubCanceled, time.Now().UTC().Format(time.RFC3339), subID, SubCanceled).Scan(&email, &au)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	if au.Valid {
		accessUntil, _ = time.Parse(time.RFC3339, au.String)
	}
	return email, accessUntil, true, nil
}

// ListDueSubscriptionCharges — подписки, по которым пора списывать: срок
// наступил, способ оплаты привязан, и с прошлой неудачной попытки прошло не
// меньше retryCooldown. Подписки, просроченные дольше giveUpAfter, не
// возвращаются — их закрывает ExpireOverdueSubscriptions.
func ListDueSubscriptionCharges(ctx context.Context, db *sql.DB, now time.Time, retryCooldown, giveUpAfter time.Duration) ([]Subscription, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+subColumns+` FROM subscriptions
		WHERE status IN (?, ?)
		  AND yoo_payment_method_id IS NOT NULL AND yoo_payment_method_id != ''
		  AND `+notMockMethodSQL+`
		  AND next_charge_at IS NOT NULL AND next_charge_at <= ?
		  AND next_charge_at > ?
		  AND (last_charge_attempt_at IS NULL OR last_charge_attempt_at <= ?)`,
		SubActive, SubPastDue,
		now.UTC().Format(time.RFC3339),
		now.UTC().Add(-giveUpAfter).Format(time.RFC3339),
		now.UTC().Add(-retryCooldown).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkChargeAttemptFailed переводит подписку в past_due и запоминает время
// попытки — следующий ретрай не раньше, чем через retryCooldown.
func MarkChargeAttemptFailed(ctx context.Context, db *sql.DB, subID string, at time.Time) error {
	_, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET status = ?, last_charge_attempt_at = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		SubPastDue, at.UTC().Format(time.RFC3339), at.UTC().Format(time.RFC3339), subID, SubActive, SubPastDue)
	return err
}

// ExpireOverdueSubscriptions закрывает подписки, которые не удалось списать за
// giveUpAfter с плановой даты: статус canceled, привязка карты стирается.
// Возвращает закрытые подписки — по ним уходит прощальное письмо.
func ExpireOverdueSubscriptions(ctx context.Context, db *sql.DB, now time.Time, giveUpAfter time.Duration) ([]Subscription, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+subColumns+` FROM subscriptions
		WHERE status IN (?, ?)
		  AND next_charge_at IS NOT NULL AND next_charge_at <= ?`,
		SubActive, SubPastDue, now.UTC().Add(-giveUpAfter).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var expired []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		expired = append(expired, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, s := range expired {
		if _, _, _, err := CancelSubscription(ctx, db, s.ID); err != nil {
			return nil, err
		}
	}
	return expired, nil
}

// ListSubscriptionsNeedingReminder — активные подписки, у которых списание
// ближе, чем lead, и напоминание для этой даты ещё не уходило.
func ListSubscriptionsNeedingReminder(ctx context.Context, db *sql.DB, now time.Time, lead time.Duration) ([]Subscription, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+subColumns+` FROM subscriptions
		WHERE status = ?
		  AND `+notMockMethodSQL+`
		  AND next_charge_at IS NOT NULL
		  AND next_charge_at > ?
		  AND next_charge_at <= ?
		  AND (reminder_sent_for IS NULL OR reminder_sent_for != next_charge_at)`,
		SubActive, now.UTC().Format(time.RFC3339), now.UTC().Add(lead).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkReminderSent запоминает, что напоминание для этой даты списания ушло.
func MarkReminderSent(ctx context.Context, db *sql.DB, subID, forChargeAt string) error {
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET reminder_sent_for = ?, updated_at = ? WHERE id = ?`,
		forChargeAt, time.Now().UTC().Format(time.RFC3339), subID)
	return err
}

// --- Сессии личного кабинета ---

// CreateSubscriptionSession выдаёт cookie-токен для подписки с живым доступом.
// Сессия истекает вместе с access_until: продление подписки потребует нового
// входа, зато отменённый доступ не переживёт свой оплаченный период.
func CreateSubscriptionSession(ctx context.Context, db *sql.DB, subID string, expiresAt time.Time) (token string, err error) {
	token = newSubToken() + newSubToken()
	now := time.Now().UTC()
	_, err = db.ExecContext(ctx, `
		INSERT INTO subscription_sessions (token, subscription_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)`, token, subID, now.Format(time.RFC3339), expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	// Попутная уборка: протухшие сессии никому не нужны.
	_, _ = db.ExecContext(ctx, `DELETE FROM subscription_sessions WHERE expires_at < ?`, now.Format(time.RFC3339))
	return token, nil
}

// SubscriptionBySession возвращает подписку по живому cookie-токену.
func SubscriptionBySession(ctx context.Context, db *sql.DB, token string) (Subscription, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Subscription{}, ErrNotFound
	}
	var subID string
	err := db.QueryRowContext(ctx, `
		SELECT subscription_id FROM subscription_sessions WHERE token = ? AND expires_at > ?`,
		token, time.Now().UTC().Format(time.RFC3339)).Scan(&subID)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	return GetSubscription(ctx, db, subID)
}

// DeleteSubscriptionSession — выход из кабинета.
func DeleteSubscriptionSession(ctx context.Context, db *sql.DB, token string) {
	_, _ = db.ExecContext(ctx, `DELETE FROM subscription_sessions WHERE token = ?`, token)
}

// CountSubscriptionOrdersSince — сколько песен подписка создала после since
// (скользящее окно суточного лимита).
func CountSubscriptionOrdersSince(ctx context.Context, db *sql.DB, subID string, since time.Time) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE subscription_id = ? AND created_at >= ?`,
		subID, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n, err
}

// ListSubscriptionOrders — заказы подписки для кабинета, свежие сверху.
func ListSubscriptionOrders(ctx context.Context, db *sql.DB, subID string) ([]Order, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE subscription_id = ? ORDER BY created_at DESC LIMIT 200`, subID)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}
