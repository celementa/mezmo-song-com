// Новые воронки заказа (v1 «оплата поздно» / v2 «оплата в начале»): миграции,
// квота бесплатных генераций, допники и письмо-догонка. Правила денег (цены
// допников, письма) живут в httpserver — здесь только состояние.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// Стадии воронки нового флоу (orders.funnel_stage) — рельса под email-маркетинг
// и панель «последняя достигнутая стадия» в Grafana.
const (
	StageStarted       = "started"        // заказ заведён (v1: генерация пошла, v2: email получен)
	StageEmailCaptured = "email_captured" // email оставлен
	StagePaid          = "paid"           // основной платёж прошёл
	StageConfigFilled  = "config_filled"  // v2: вопросы о песне отвечены, генерация пошла
	StageDone          = "done"           // песня готова и доступна
)

// stageRank — порядок стадий. Нужен потому, что у воронок он РАЗНЫЙ: в v1
// человек отвечает на вопросы и платит в конце, в v2 платит на втором шаге и
// отвечает после. Общая шкала берёт максимум: «последняя достигнутая» должна
// означать самую далёкую, а не последнюю записанную.
//
// Без ранга стадия ездила назад: у v2 после `paid` приходил `config_filled`, и
// панель воронки показывала оплаченный заказ как недооформленный.
var stageRank = map[string]int{
	StageStarted:       1,
	StageEmailCaptured: 2,
	StageConfigFilled:  3,
	StagePaid:          4,
	StageDone:          5,
}

func migrateCreateFlow(db *sql.DB) error {
	// Колонки новых воронок. Бэкфилл не нужен: пусто = заказ старого флоу.
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN flow TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN funnel_stage TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN email_consent INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN extras_selected TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN extras_paid TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN extras_pending TEXT`)
	_, _ = db.Exec(`ALTER TABLE orders ADD COLUMN followup_sent_at TEXT`)

	// Счётчик бесплатных генераций v1: по ключу (кука посетителя / IP) на день.
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS free_generations (
	key TEXT NOT NULL,
	day TEXT NOT NULL,
	count INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (key, day)
);`)
	return err
}

// Лимиты бесплатных генераций воронки v1. Их ДВА, и это принципиально:
// лимит на человека и лимит на адрес — разные величины.
//
// Кука посетителя (подписанная, HttpOnly) — это «человек»: удаляется в один
// клик, поэтому от скрипта не защищает, зато честно ограничивает живого
// покупателя. IP человеком не является: за одним адресом сидит офис, кафе или
// сотни абонентов мобильного оператора. Один общий лимит на оба ключа означал,
// что четвёртый человек за общим адресом не получит даже первую генерацию —
// за него потратили соседи.
const (
	SettingFreeGenerationsPerDay      = "flow.free_generations_per_day"
	SettingFreeGenerationsPerIPPerDay = "flow.free_generations_per_ip_per_day"
	DefaultFreeGenerationsPerDay      = 3
	// Потолок на адрес — страховка от скрипта в цикле, а не лимит на человека,
	// поэтому он на порядок выше. Настоящий предохранитель кошелька — суточный
	// бюджет на весь сайт (SettingPreviewDailyBudget).
	DefaultFreeGenerationsPerIPPerDay = 30
)

// FreeGenerationLimits возвращает дневные лимиты бесплатных генераций: на
// посетителя (кука) и на адрес. 0 в настройке трактуется как дефолт, чтобы
// опечатка в админке не открыла бесплатную генерацию всем желающим.
func FreeGenerationLimits(ctx context.Context, db *sql.DB) (perVisitor, perIP int) {
	perVisitor = settingInt(ctx, db, SettingFreeGenerationsPerDay, DefaultFreeGenerationsPerDay)
	if perVisitor <= 0 {
		perVisitor = DefaultFreeGenerationsPerDay
	}
	perIP = settingInt(ctx, db, SettingFreeGenerationsPerIPPerDay, DefaultFreeGenerationsPerIPPerDay)
	if perIP <= 0 {
		perIP = DefaultFreeGenerationsPerIPPerDay
	}
	return perVisitor, perIP
}

// FreeGenKey — ключ квоты со своим лимитом: у куки посетителя и у адреса они
// разные (см. FreeGenerationLimits).
type FreeGenKey struct {
	Key   string
	Limit int
}

// FreeGenerationsAvailable — read-only проверка, осталась ли квота хотя бы по
// одному ключу. Нужна GET-ручке конфигурации флоу: она отвечает, показывать ли
// шаги генерации, и расходовать квоту при этом не должна.
func FreeGenerationsAvailable(ctx context.Context, db *sql.DB, keys []FreeGenKey, day string) (bool, error) {
	for _, k := range keys {
		if k.Key == "" || k.Limit <= 0 {
			continue
		}
		var used int
		err := db.QueryRowContext(ctx,
			`SELECT COALESCE(count, 0) FROM free_generations WHERE key = ? AND day = ?`, k.Key, day).Scan(&used)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if used >= k.Limit {
			return false, nil
		}
	}
	return true, nil
}

// ConsumeFreeGeneration атомарно расходует одну бесплатную генерацию дня по
// каждому ключу — у каждого свой лимит. false — хотя бы один ключ упёрся в
// свой лимит; счётчики при этом не меняются (транзакция откатывается), чтобы
// отказ не съедал попытки.
func ConsumeFreeGeneration(ctx context.Context, db *sql.DB, keys []FreeGenKey, day string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, k := range keys {
		if k.Key == "" || k.Limit <= 0 {
			continue
		}
		// Одна атомарная запись на ключ (без SELECT-затем-INSERT): апгрейд
		// shared→write лока внутри транзакции при конкурентной записи в SQLite
		// падает мгновенным SQLITE_BUSY, а запись первой же операцией честно
		// ждёт busy_timeout. WHERE в DO UPDATE не даёт превысить лимит:
		// 0 затронутых строк = ключ упёрся.
		res, err := tx.ExecContext(ctx, `
INSERT INTO free_generations (key, day, count) VALUES (?, ?, 1)
ON CONFLICT(key, day) DO UPDATE SET count = count + 1 WHERE count < ?`, k.Key, day, k.Limit)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if n == 0 {
			return false, nil
		}
	}
	return true, tx.Commit()
}

// SetOrderEmail записывает email (и телефон) в customer заказа и фиксирует
// согласие на письма. Пустой телефон не затирает ранее сохранённый.
func SetOrderEmail(ctx context.Context, db *sql.DB, id, email, phone string, consent bool) error {
	o, err := GetOrder(ctx, db, id)
	if err != nil {
		return err
	}
	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	if cust == nil {
		cust = map[string]any{}
	}
	cust["email"] = email
	if phone != "" {
		cust["whatsappNumber"] = phone
	}
	raw, err := json.Marshal(cust)
	if err != nil {
		return err
	}
	consentInt := 0
	if consent {
		consentInt = 1
	}
	_, err = db.ExecContext(ctx, `UPDATE orders SET customer = ?, email_consent = ? WHERE id = ?`, string(raw), consentInt, id)
	return err
}

// SetOrderPayload заменяет input_payload заказа — v2 присылает вопросы о песне
// уже после оплаты.
func SetOrderPayload(ctx context.Context, db *sql.DB, id, payloadJSON string) error {
	_, err := db.ExecContext(ctx, `UPDATE orders SET input_payload = ? WHERE id = ?`, payloadJSON, id)
	return err
}

// SetFunnelStage записывает стадию воронки.
// SetFunnelStage двигает стадию воронки ТОЛЬКО вперёд: назад её не откатит ни
// порядок шагов другой воронки, ни повторный вебхук. Неизвестная стадия
// (ранг 0) записывается как есть — иначе опечатка молча потерялась бы.
func SetFunnelStage(ctx context.Context, db *sql.DB, id, stage string) error {
	rank, known := stageRank[stage]
	if !known {
		_, err := db.ExecContext(ctx, `UPDATE orders SET funnel_stage = ? WHERE id = ?`, stage, id)
		return err
	}
	// CASE в WHERE вместо чтения-и-записи: два конкурентных перехода (вебхук
	// оплаты и ответ фронта) не должны затирать друг друга.
	// Порядок аргументов повторяет порядок «?» в запросе: значение, id, пары
	// CASE и только потом ранг — иначе сравнение уедет на чужой аргумент.
	args := []any{stage, id}
	cases := make([]string, 0, len(stageRank))
	for st, r := range stageRank {
		cases = append(cases, "WHEN ? THEN ?")
		args = append(args, st, r)
	}
	args = append(args, rank)
	query := `UPDATE orders SET funnel_stage = ? WHERE id = ?
	          AND COALESCE(CASE funnel_stage ` + strings.Join(cases, " ") + ` ELSE 0 END, 0) < ?`
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

// ExtrasSelected возвращает id допников, выбранных до оплаты.
func ExtrasSelected(o Order) []string {
	var ids []string
	if o.ExtrasSelectedJSON.Valid {
		_ = json.Unmarshal([]byte(o.ExtrasSelectedJSON.String), &ids)
	}
	return ids
}

// SelectExtra добавляет допник в выбранные до оплаты (идемпотентно).
func SelectExtra(ctx context.Context, db *sql.DB, id, extraID string) error {
	o, err := GetOrder(ctx, db, id)
	if err != nil {
		return err
	}
	ids := ExtrasSelected(o)
	if slices.Contains(ids, extraID) {
		return nil
	}
	ids = append(ids, extraID)
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE orders SET extras_selected = ? WHERE id = ?`, string(raw), id)
	return err
}

// PendingExtra — выданное, но ещё не оплаченное подтверждение платежа допника.
// Пока оно живо, повторное «Добавить» отдаёт ту же ссылку на оплату, а не
// заводит второй платёж (та же логика, что yoo_confirmation основного платежа).
type PendingExtra struct {
	ID        string `json:"id"`
	PaymentID string `json:"paymentId"`
	URL       string `json:"url"`
	IssuedAt  string `json:"issuedAt"`
}

// ExtrasPending возвращает выданные подтверждения оплаты допников.
func ExtrasPending(o Order) []PendingExtra {
	var out []PendingExtra
	if o.ExtrasPendingJSON.Valid {
		_ = json.Unmarshal([]byte(o.ExtrasPendingJSON.String), &out)
	}
	return out
}

// SaveExtraPending запоминает выданное подтверждение платежа допника,
// заменяя прежнее подтверждение того же допника.
func SaveExtraPending(ctx context.Context, db *sql.DB, id string, p PendingExtra) error {
	o, err := GetOrder(ctx, db, id)
	if err != nil {
		return err
	}
	pending := ExtrasPending(o)
	out := pending[:0]
	for _, e := range pending {
		if e.ID != p.ID {
			out = append(out, e)
		}
	}
	out = append(out, p)
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE orders SET extras_pending = ? WHERE id = ?`, string(raw), id)
	return err
}

// KnownExtraPaymentID — заводили ли мы такой платёж допника по этому заказу.
func KnownExtraPaymentID(o Order, paymentID string) bool {
	for _, e := range ExtrasPending(o) {
		if e.PaymentID == paymentID {
			return true
		}
	}
	for _, e := range ExtrasPaid(o) {
		if e.PaymentID == paymentID {
			return true
		}
	}
	return false
}

// PaidExtra — допник, оплаченный отдельным платежом после основной оплаты.
type PaidExtra struct {
	ID        string `json:"id"`
	PaymentID string `json:"paymentId"`
	PaidAt    string `json:"paidAt"`
}

// ExtrasPaid возвращает оплаченные допники заказа.
func ExtrasPaid(o Order) []PaidExtra {
	var out []PaidExtra
	if o.ExtrasPaidJSON.Valid {
		_ = json.Unmarshal([]byte(o.ExtrasPaidJSON.String), &out)
	}
	return out
}

// MarkExtraPaid записывает оплату допника. Идемпотентно по paymentId — ретрай
// вебхука ЮKassa не задвоит допник.
func MarkExtraPaid(ctx context.Context, db *sql.DB, id, extraID, paymentID string, at time.Time) error {
	o, err := GetOrder(ctx, db, id)
	if err != nil {
		return err
	}
	extras := ExtrasPaid(o)
	for _, e := range extras {
		if e.PaymentID == paymentID {
			return nil
		}
	}
	extras = append(extras, PaidExtra{ID: extraID, PaymentID: paymentID, PaidAt: at.UTC().Format(time.RFC3339)})
	raw, err := json.Marshal(extras)
	if err != nil {
		return err
	}
	// Оплаченное подтверждение убирается из pending — оно больше не переиспользуется.
	pending := ExtrasPending(o)
	left := pending[:0]
	for _, e := range pending {
		if e.ID != extraID {
			left = append(left, e)
		}
	}
	pendingRaw, err := json.Marshal(left)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE orders SET extras_paid = ?, extras_pending = ? WHERE id = ?`, string(raw), string(pendingRaw), id)
	return err
}
