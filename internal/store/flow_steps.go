// Показы шагов воронок — то, из чего на дашборде строится «где отпадают люди».
//
// Считать это по заказам нельзя: заказ появляется в середине воронки (в
// варианте 1 — на шаге email, в /create v1 — после истории), поэтому первые
// шаги в БД не оставляют следа вообще. Браузерные события `flow_step` в
// dataLayer есть, но живут в GTM и в SQLite не попадают — а дашборд читает
// именно её.
package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// StepView — один показ шага одной сессией.
type StepView struct {
	SessionID string
	Flow      string // 'variant1' (/config) | 'v1' | 'v2' (/create)
	Arm       string // плечо A/B-теста; пусто — заход мимо ротации
	StepID    string
	StepIndex int
}

func migrateFlowSteps(db *sql.DB) error {
	// UNIQUE по сессии, дню и шагу: перезагрузка страницы и возврат назад не
	// должны раздувать счётчик — на воронке нужны люди, а не показы.
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS flow_step_views (
	session_id TEXT NOT NULL,
	day TEXT NOT NULL,
	flow TEXT NOT NULL,
	arm TEXT,
	step_id TEXT NOT NULL,
	step_index INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	UNIQUE(session_id, day, flow, step_id)
);`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_flow_steps_day ON flow_step_views(day)`)
	return err
}

// maxStepFieldLen — предел длины поля из публичной ручки: значения приходят из
// браузера и становятся ключами группировки в отчёте.
const maxStepFieldLen = 64

// RecordFlowStep сохраняет показ шага. Повтор в той же сессии за тот же день по
// тому же шагу игнорируется.
func RecordFlowStep(ctx context.Context, db *sql.DB, v StepView) error {
	clip := func(s string) string {
		s = strings.TrimSpace(s)
		if r := []rune(s); len(r) > maxStepFieldLen {
			return string(r[:maxStepFieldLen])
		}
		return s
	}
	session, flow, step := clip(v.SessionID), clip(v.Flow), clip(v.StepID)
	if session == "" || flow == "" || step == "" {
		return nil
	}
	arm := sql.NullString{String: clip(v.Arm), Valid: strings.TrimSpace(v.Arm) != ""}
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO flow_step_views (session_id, day, flow, arm, step_id, step_index, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session, now.Format("2006-01-02"), flow, arm, step, v.StepIndex, now.Format(time.RFC3339))
	return err
}
