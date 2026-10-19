// Журнал писем и тумблеры отправки.
//
// Каждое письмо, которое сервер пытался отправить, оставляет строку в
// email_log — включая выключенные в админке (status = disabled) и упавшие на
// SMTP. Без журнала вопрос «что ушло этому клиенту» решался только грепом по
// логам прода, а логи живут до пересоздания контейнера.
package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Исход попытки отправки.
const (
	EmailSent     = "sent"
	EmailFailed   = "failed"
	EmailDisabled = "disabled" // письмо выключено на /admin/emails
)

// EmailLogEntry — одна попытка отправки.
type EmailLogEntry struct {
	ID             int64
	CreatedAt      string
	Kind           string
	Recipient      string
	Subject        string
	OrderID        string
	SubscriptionID string
	Status         string
	Error          string
	// Ref — ключ письма в Unisender Go (metadata.ref), по нему вебхук Go
	// находит строку. Пусто — письмо ушло не через Go.
	Ref string
	// Судьба письма по вебхуку Go (см. ApplyEmailEvent).
	Delivery      string // "" | delivered | soft_bounced | hard_bounced | spam
	DeliveredAt   string
	OpenedAt      string // первое открытие любого рода
	OpenedHumanAt string // первое открытие, которое Go счёл человеческим
}

func migrateEmailLog(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS email_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at TEXT NOT NULL,
	kind TEXT NOT NULL,
	recipient TEXT NOT NULL,
	subject TEXT NOT NULL,
	order_id TEXT NOT NULL DEFAULT '',
	subscription_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT ''
);`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_email_log_created ON email_log(created_at)`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`ALTER TABLE email_log ADD COLUMN ref TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE email_log ADD COLUMN delivery TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE email_log ADD COLUMN delivered_at TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE email_log ADD COLUMN opened_at TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE email_log ADD COLUMN opened_human_at TEXT NOT NULL DEFAULT ''`)
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_email_log_ref ON email_log(ref) WHERE ref <> ''`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_email_log_order ON email_log(order_id) WHERE order_id <> ''`)
	return err
}

// emailSettingKey — тумблер письма в settings.
func emailSettingKey(kind string) string { return "emails." + kind + ".enabled" }

// EmailEnabled — включено ли письмо. Нет записи — включено: новые письма и
// письма до появления тумблеров отправляются как раньше.
func EmailEnabled(ctx context.Context, db *sql.DB, kind string) bool {
	return GetSetting(ctx, db, emailSettingKey(kind), "1") != "0"
}

func SetEmailEnabled(ctx context.Context, db *sql.DB, kind string, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	return SetSetting(ctx, db, emailSettingKey(kind), value)
}

// emailTemplateKey — id шаблона Unisender Go для письма в settings.
func emailTemplateKey(kind string) string { return "emails." + kind + ".template_id" }

// EmailTemplateID — шаблон Unisender Go, по которому уходит письмо. Пусто —
// письмо уходит нашим текстом через SMTP.
func EmailTemplateID(ctx context.Context, db *sql.DB, kind string) string {
	return GetSetting(ctx, db, emailTemplateKey(kind), "")
}

func SetEmailTemplateID(ctx context.Context, db *sql.DB, kind, templateID string) error {
	return SetSetting(ctx, db, emailTemplateKey(kind), templateID)
}

// LogEmail пишет попытку отправки. CreatedAt проставляется здесь.
func LogEmail(ctx context.Context, db *sql.DB, e EmailLogEntry) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO email_log (created_at, kind, recipient, subject, order_id, subscription_id, status, error, ref)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), e.Kind, e.Recipient, e.Subject, e.OrderID, e.SubscriptionID, e.Status, e.Error, e.Ref)
	return err
}

// Статусы письма из вебхука Unisender Go, которые мы храним.
const (
	DeliveryDelivered   = "delivered"
	DeliverySoftBounced = "soft_bounced"
	DeliveryHardBounced = "hard_bounced"
	DeliverySpam        = "spam"
)

// ApplyEmailEvent применяет событие Go к письму с ключом ref. Порядок вызовов
// Go не гарантирует и ретраит их, поэтому каждое событие только дополняет
// строку и ничего не откатывает:
//   - opened/clicked/unsubscribed/subscribed — письмо и доставлено, и прочитано;
//   - первая отметка времени выигрывает (COALESCE через NULLIF);
//   - soft_bounced пишется только поверх пустого статуса, hard_bounced — поверх
//     пустого или soft, spam — окончательный и перекрывает доставку.
//
// human — Go отнёс открытие к человеку (traffic_type=human), а не к
// предзагрузке почтовика (Apple Mail Privacy Protection, сканеры ссылок).
// Возвращает false, если письма с таким ref нет.
func ApplyEmailEvent(ctx context.Context, db *sql.DB, ref, status, at string, human bool) (bool, error) {
	var q string
	var args []any
	switch status {
	case DeliveryDelivered:
		q = `UPDATE email_log SET
			delivery = CASE WHEN delivery = 'spam' THEN delivery ELSE 'delivered' END,
			delivered_at = COALESCE(NULLIF(delivered_at, ''), ?)
			WHERE ref = ?`
		args = []any{at, ref}
	case "opened", "clicked", "unsubscribed", "subscribed":
		humanAt := ""
		if human {
			humanAt = at
		}
		q = `UPDATE email_log SET
			delivery = CASE WHEN delivery = 'spam' THEN delivery ELSE 'delivered' END,
			delivered_at = COALESCE(NULLIF(delivered_at, ''), ?),
			opened_at = COALESCE(NULLIF(opened_at, ''), ?),
			opened_human_at = COALESCE(NULLIF(opened_human_at, ''), NULLIF(?, ''), '')
			WHERE ref = ?`
		args = []any{at, at, humanAt, ref}
	case DeliverySoftBounced:
		q = `UPDATE email_log SET delivery = CASE WHEN delivery = '' THEN 'soft_bounced' ELSE delivery END WHERE ref = ?`
		args = []any{ref}
	case DeliveryHardBounced:
		q = `UPDATE email_log SET delivery = CASE WHEN delivery IN ('', 'soft_bounced') THEN 'hard_bounced' ELSE delivery END WHERE ref = ?`
		args = []any{ref}
	case DeliverySpam:
		q = `UPDATE email_log SET delivery = 'spam' WHERE ref = ?`
		args = []any{ref}
	default:
		// accepted / sent — промежуточные, нам ничего не дают.
		return true, nil
	}
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListEmailLog — страница журнала, новые сверху. q ищет подстроку в адресе,
// id заказа и id подписки (регистронезависимо только для ASCII — как и поиск
// заказов, но email и id и так латиница).
func ListEmailLog(ctx context.Context, db *sql.DB, q string, page, perPage int) ([]EmailLogEntry, int, error) {
	where, args := "", []any{}
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + q + "%"
		where = `WHERE recipient LIKE ? OR order_id LIKE ? OR subscription_id LIKE ?`
		args = append(args, like, like, like)
	}
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_log `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, created_at, kind, recipient, subject, order_id, subscription_id, status, error,
	delivery, delivered_at, opened_at, opened_human_at
FROM email_log `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []EmailLogEntry
	for rows.Next() {
		var e EmailLogEntry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Kind, &e.Recipient, &e.Subject, &e.OrderID, &e.SubscriptionID, &e.Status, &e.Error,
			&e.Delivery, &e.DeliveredAt, &e.OpenedAt, &e.OpenedHumanAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
