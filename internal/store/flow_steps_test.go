package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func stepsTestDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "steps.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background(), db
}

// Воронка считается по ЛЮДЯМ, а не по показам: перезагрузка страницы и возврат
// назад не должны раздувать шаг.
func TestRecordFlowStepCountsPeopleNotViews(t *testing.T) {
	ctx, db := stepsTestDB(t)

	for i := 0; i < 3; i++ {
		if err := RecordFlowStep(ctx, db, StepView{SessionID: "s1", Flow: "v1", Arm: "v2_base", StepID: "recipient"}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := RecordFlowStep(ctx, db, StepView{SessionID: "s2", Flow: "v1", StepID: "recipient"}); err != nil {
		t.Fatal(err)
	}
	if err := RecordFlowStep(ctx, db, StepView{SessionID: "s1", Flow: "v1", StepID: "occasion", StepIndex: 1}); err != nil {
		t.Fatal(err)
	}

	var people int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT session_id) FROM flow_step_views WHERE step_id='recipient'`).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if people != 2 {
		t.Fatalf("на первом шаге %d человек, ожидалось 2", people)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM flow_step_views`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Fatalf("строк %d, ожидалось 3 (три показа не задваиваются)", rows)
	}
}

// Пустые поля игнорируются молча: ручка публичная и фоновая, ронять воронку
// из-за мусора в теле нельзя.
func TestRecordFlowStepIgnoresEmptyFields(t *testing.T) {
	ctx, db := stepsTestDB(t)
	for _, v := range []StepView{
		{SessionID: "", Flow: "v1", StepID: "recipient"},
		{SessionID: "s", Flow: "", StepID: "recipient"},
		{SessionID: "s", Flow: "v1", StepID: ""},
	} {
		if err := RecordFlowStep(ctx, db, v); err != nil {
			t.Fatalf("record %+v: %v", v, err)
		}
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM flow_step_views`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("записано %d строк из мусора", rows)
	}
}
