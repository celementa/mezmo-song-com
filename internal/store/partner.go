// Партнёрские заказы: песня продана в чужом магазине (Les Roses на InSales) и
// оплачена там же, к нам заказ приходит уже оплаченным и без анкеты. Здесь —
// только состояние: связь с заказом партнёра, отмена, напоминания про анкету и
// события показа блока в корзине партнёра (знаменатель A/B «корзина / попап»).
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// FlowLesroses — воронка заказов, проданных в корзине Les Roses.
const FlowLesroses = "lesroses"

// Плечи A/B в корзине Les Roses (orders.flow_variant). manual — заказ заведён
// руками в кабинете партнёра (вебхук не дошёл, продали по телефону).
const (
	ArmLesrosesCart   = "lesroses_cart"
	ArmLesrosesPopup  = "lesroses_popup"
	ArmLesrosesManual = "lesroses_manual"
)

func migratePartner(db *sql.DB) error {
	// partner_order_number — номер заказа партнёра «для людей» (его знают и
	// покупатель, и сотрудники Les Roses), он же ключ дедупа: повторный вебхук,
	// сверка и ручное создание сходятся на нём. partner_order_id — внутренний id
	// InSales, по нему заказ перечитывается через API (у ручных его нет).
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN partner_order_id TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN partner_order_number TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN partner_canceled_at TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN brief_reminders INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN brief_reminded_at TEXT`)
	// Ход анкеты (RecordBriefProgress): открыта ли ссылка и до какого шага дошли.
	// Ответы уходят на сервер одним запросом в конце, без этих отметок
	// «не открывал» и «бросил на последнем шаге» в кабинете неотличимы.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN brief_opened_at TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN brief_step INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN brief_step_at TEXT`)
	// Уникальность — на уровне БД, а не проверкой «есть ли уже»: вебхук и
	// фоновая сверка могут прийти одновременно, и проверка в коде пропустила
	// бы оба. Вторая песня по одному заказу — это и деньги, и второе письмо.
	_, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_partner_number
		ON orders(flow, partner_order_number) WHERE partner_order_number IS NOT NULL`)
	if err != nil {
		return err
	}

	// partner_events — показы и клики блока песни на сайте партнёра. Уники по
	// сессии за день: перезагрузка корзины не раздувает показы, а сравнивать
	// плечи нужно по людям, не по просмотрам.
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS partner_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	partner TEXT NOT NULL,
	day TEXT NOT NULL,
	session_id TEXT NOT NULL,
	arm TEXT NOT NULL,
	event TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE(partner, day, session_id, arm, event)
);`)
	return err
}

// PartnerOrderRef — связь нашего заказа с заказом партнёра.
type PartnerOrderRef struct {
	PartnerID string // внутренний id в системе партнёра; пусто у ручных
	Number    string // номер заказа партнёра для людей
}

// CreatePartnerOrder создаёт заказ, если по этому номеру партнёра его ещё нет.
// created=false — заказ уже был (повтор вебхука, сверка, ручной дубль), id —
// существующего. Серверные и браузерные конверсии помечаются отправленными
// сразу: деньги прошли не через нас и не с нашей рекламы, отчитываться о них
// в GA4/VK/Метрику нельзя — все пути отправки смотрят на эти отметки.
func CreatePartnerOrder(ctx context.Context, db *sql.DB, o Order, ref PartnerOrderRef) (created bool, id string, err error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO orders (id, status, delivery_status, payment_status, input_payload, customer, base_price, flow, funnel_stage, flow_variant,
	partner_order_id, partner_order_number, purchase_tracked_at, server_tracked_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.Status, o.DeliveryStatus, o.PaymentStatus, o.InputPayloadJSON, o.CustomerJSON, o.BasePrice, o.Flow, o.FunnelStage, o.FlowVariant,
		nullIfEmpty(ref.PartnerID), ref.Number, now, now, o.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, "", err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, o.ID, nil
	}
	id, err = PartnerOrderIDByNumber(ctx, db, o.Flow.String, ref.Number)
	return false, id, err
}

// PartnerOrderIDByNumber — наш id заказа по номеру партнёра; ErrNotFound — нет.
func PartnerOrderIDByNumber(ctx context.Context, db *sql.DB, flow, number string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `SELECT id FROM orders WHERE flow = ? AND partner_order_number = ?`, flow, number).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// MarkPartnerCanceled фиксирует отмену/возврат на стороне партнёра. Доступ
// закрывается (refunded), только пока анкеты нет и генерация не шла: песню,
// которую уже начали делать, не отбираем — разбор остаётся людям, отметка
// видна в кабинете. closed — доступ закрыт этим вызовом.
func MarkPartnerCanceled(ctx context.Context, db *sql.DB, id string) (closed bool, err error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.ExecContext(ctx,
		`UPDATE orders SET partner_canceled_at = COALESCE(partner_canceled_at, ?) WHERE id = ?`, now, id); err != nil {
		return false, err
	}
	res, err := db.ExecContext(ctx,
		`UPDATE orders SET payment_status = ? WHERE id = ? AND status = 'created' AND payment_status = ?`,
		PaymentRefunded, id, PaymentPaid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PartnerOrderRow — строка списка в кабинете партнёра.
type PartnerOrderRow struct {
	ID             string
	CreatedAt      time.Time
	PartnerID      string
	Number         string
	Email          string
	Arm            string
	PaymentStatus  string
	Status         string
	DeliveryStatus string
	RecipientName  string
	CanceledAt     string
	BriefReminders int
	BriefOpenedAt  string
	BriefStep      int
	BriefStepAt    string
	// Письма воронки (ссылка на анкету и напоминания) по журналу email_log.
	MailSent          int
	MailTracked       bool // хоть одно ушло через Unisender Go — статусы ниже осмысленны
	MailDelivered     bool
	MailProblem       string // hard_bounced | spam, если было
	MailOpenedAt      string
	MailOpenedHumanAt string
}

// briefFilled — SQL-аналог hasSongConfig: анкета заполнена, когда есть имя получателя.
const briefFilled = `TRIM(COALESCE(json_extract(input_payload, '$.recipientName'), '')) <> ''`

// ListPartnerOrders — заказы воронки за период, новые сверху. q ищет по номеру
// партнёра, email и нашему id.
func ListPartnerOrders(ctx context.Context, db *sql.DB, flow string, since time.Time, q string, limit int) ([]PartnerOrderRow, error) {
	query := `SELECT id, created_at, COALESCE(partner_order_id, ''), COALESCE(partner_order_number, ''),
		COALESCE(json_extract(customer, '$.email'), ''), COALESCE(flow_variant, ''), payment_status, status, delivery_status,
		COALESCE(json_extract(input_payload, '$.recipientName'), ''), COALESCE(partner_canceled_at, ''), brief_reminders,
		COALESCE(brief_opened_at, ''), brief_step, COALESCE(brief_step_at, ''),
		COALESCE(m.sent, 0), COALESCE(m.tracked, 0), COALESCE(m.delivered, 0), COALESCE(m.problem, ''),
		COALESCE(m.opened, ''), COALESCE(m.opened_human, '')
		FROM orders
		LEFT JOIN (SELECT order_id, COUNT(*) AS sent, MAX(ref <> '') AS tracked,
				MAX(delivery = 'delivered') AS delivered,
				MAX(CASE WHEN delivery IN ('hard_bounced', 'spam') THEN delivery END) AS problem,
				MIN(NULLIF(opened_at, '')) AS opened, MIN(NULLIF(opened_human_at, '')) AS opened_human
			FROM email_log WHERE order_id <> '' AND status = 'sent' AND kind GLOB ? GROUP BY order_id) m ON m.order_id = orders.id
		WHERE flow = ? AND created_at >= ?`
	// Письма воронки — те, чей тип начинается с её имени (lesroses_link, lesroses_reminder).
	args := []any{flow + "_*", flow, since.UTC().Format(time.RFC3339Nano)}
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + q + "%"
		query += ` AND (COALESCE(partner_order_number, '') LIKE ? OR customer LIKE ? OR id LIKE ?)`
		args = append(args, like, like, like)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PartnerOrderRow
	for rows.Next() {
		var r PartnerOrderRow
		var created string
		if err := rows.Scan(&r.ID, &created, &r.PartnerID, &r.Number, &r.Email, &r.Arm, &r.PaymentStatus, &r.Status,
			&r.DeliveryStatus, &r.RecipientName, &r.CanceledAt, &r.BriefReminders,
			&r.BriefOpenedAt, &r.BriefStep, &r.BriefStepAt,
			&r.MailSent, &r.MailTracked, &r.MailDelivered, &r.MailProblem, &r.MailOpenedAt, &r.MailOpenedHumanAt); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordBriefProgress отмечает, что покупатель открыл анкету и дошёл до шага
// step (1..N). Шаг только растёт: «Назад» и повторное открытие ссылки прогресс
// не сбрасывают, время шага — когда он был достигнут впервые.
func RecordBriefProgress(ctx context.Context, db *sql.DB, id string, step int, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx, `UPDATE orders SET
		brief_opened_at = COALESCE(brief_opened_at, ?),
		brief_step_at = CASE WHEN ? > brief_step THEN ? ELSE brief_step_at END,
		brief_step = MAX(brief_step, ?)
		WHERE id = ?`, ts, step, ts, step, id)
	return err
}

// BriefReminderDue — заказ, которому пора напомнить про анкету; N — номер напоминания.
type BriefReminderDue struct {
	ID    string
	Email string
	N     int
}

// DueBriefReminders — оплаченные и не отменённые заказы воронки без анкеты,
// которым пора слать напоминание: первое через first после создания, второе
// через second. Каждое уходит один раз (см. ClaimBriefReminder).
func DueBriefReminders(ctx context.Context, db *sql.DB, flow string, now time.Time, first, second time.Duration) ([]BriefReminderDue, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, COALESCE(json_extract(customer, '$.email'), ''), brief_reminders + 1 FROM orders
WHERE flow = ? AND payment_status = ? AND status = 'created' AND partner_canceled_at IS NULL AND NOT (`+briefFilled+`)
  AND ((brief_reminders = 0 AND created_at <= ?) OR (brief_reminders = 1 AND created_at <= ?))
ORDER BY created_at LIMIT 100`,
		flow, PaymentPaid, now.Add(-first).UTC().Format(time.RFC3339Nano), now.Add(-second).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BriefReminderDue
	for rows.Next() {
		var d BriefReminderDue
		if err := rows.Scan(&d.ID, &d.Email, &d.N); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimBriefReminder атомарно занимает n-е напоминание: true — этот вызов его
// занял и письмо надо отправить; false — уже занято (параллельный тик).
func ClaimBriefReminder(ctx context.Context, db *sql.DB, id string, n int) (bool, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE orders SET brief_reminders = ?, brief_reminded_at = ? WHERE id = ? AND brief_reminders = ?`,
		n, time.Now().UTC().Format(time.RFC3339Nano), id, n-1)
	if err != nil {
		return false, err
	}
	k, _ := res.RowsAffected()
	return k > 0, nil
}

// События блока песни на сайте партнёра.
const (
	PartnerEventShown   = "shown"   // блок или попап показан
	PartnerEventAdd     = "add"     // нажали «Добавить песню»
	PartnerEventDecline = "decline" // «Нет, спасибо» в попапе
)

// RecordPartnerEvent пишет событие; повтор той же сессии за день — no-op.
func RecordPartnerEvent(ctx context.Context, db *sql.DB, partner, sessionID, arm, event string, now time.Time) error {
	_, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO partner_events (partner, day, session_id, arm, event, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		partner, now.UTC().Format("2006-01-02"), sessionID, arm, event, now.UTC().Format(time.RFC3339Nano))
	return err
}

// PartnerArmStats — воронка одного плеча: показы → добавления → оплаты →
// анкеты → песни. Показы и клики — люди (сессии), остальное — заказы.
type PartnerArmStats struct {
	Arm      string `json:"arm"`
	Shown    int    `json:"shown"`
	Added    int    `json:"added"`
	Declined int    `json:"declined"`
	Paid     int    `json:"paid"`
	Briefed  int    `json:"briefed"`
	Ready    int    `json:"ready"`
	Canceled int    `json:"canceled"`
	Revenue  int    `json:"revenue"`
}

// PartnerFunnel считает воронку по плечам с даты sinceDay (YYYY-MM-DD, UTC).
// Плечо события ("cart") и плечо заказа ("lesroses_cart") сводятся к короткому.
func PartnerFunnel(ctx context.Context, db *sql.DB, partner string, sinceDay string) ([]PartnerArmStats, error) {
	byArm := map[string]*PartnerArmStats{}
	get := func(arm string) *PartnerArmStats {
		arm = strings.TrimPrefix(arm, partner+"_")
		if byArm[arm] == nil {
			byArm[arm] = &PartnerArmStats{Arm: arm}
		}
		return byArm[arm]
	}

	rows, err := db.QueryContext(ctx, `
SELECT arm, event, COUNT(DISTINCT session_id) FROM partner_events
WHERE partner = ? AND day >= ? GROUP BY arm, event`, partner, sinceDay)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var arm, event string
		var n int
		if err := rows.Scan(&arm, &event, &n); err != nil {
			rows.Close()
			return nil, err
		}
		s := get(arm)
		switch event {
		case PartnerEventShown:
			s.Shown = n
		case PartnerEventAdd:
			s.Added = n
		case PartnerEventDecline:
			s.Declined = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = db.QueryContext(ctx, `
SELECT COALESCE(flow_variant, ''),
	COUNT(*),
	SUM(CASE WHEN `+briefFilled+` THEN 1 ELSE 0 END),
	SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
	SUM(CASE WHEN partner_canceled_at IS NOT NULL THEN 1 ELSE 0 END),
	SUM(CASE WHEN partner_canceled_at IS NULL THEN base_price ELSE 0 END)
FROM orders WHERE flow = ? AND created_at >= ? GROUP BY flow_variant`, partner, sinceDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var arm string
		var paid, briefed, ready, canceled, revenue int
		if err := rows.Scan(&arm, &paid, &briefed, &ready, &canceled, &revenue); err != nil {
			return nil, err
		}
		s := get(arm)
		s.Paid, s.Briefed, s.Ready, s.Canceled, s.Revenue = paid, briefed, ready, canceled, revenue
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]PartnerArmStats, 0, len(byArm))
	for _, arm := range []string{"cart", "popup", "manual"} {
		if s := byArm[arm]; s != nil {
			out = append(out, *s)
			delete(byArm, arm)
		}
	}
	for _, s := range byArm {
		out = append(out, *s)
	}
	return out, nil
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
