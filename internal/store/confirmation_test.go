package store

import (
	"path/filepath"
	"testing"
	"time"
)

// SaveConfirmation/LoadConfirmation переиспользуют один и тот же снимок заказа,
// но заказ читается тремя разными путями (GetOrder, ListOrdersPage/scanOrders,
// и напрямую поле Order) — пропуск любого молча возвращает пустое подтверждение.
func TestSaveConfirmationRoundTrip(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_conf")

	want := IssuedConfirmation{
		PaymentID: "pay_1",
		Mode:      "embedded",
		Token:     "ct-1",
		IssuedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := SaveConfirmation(ctx, db, "ord_conf", want); err != nil {
		t.Fatalf("save: %v", err)
	}

	o, err := GetOrder(ctx, db, "ord_conf")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	got, ok := LoadConfirmation(o)
	if !ok {
		t.Fatalf("GetOrder path: LoadConfirmation ok = false, want true")
	}
	if got.PaymentID != want.PaymentID || got.Mode != want.Mode || got.Token != want.Token || !got.IssuedAt.Equal(want.IssuedAt) {
		t.Fatalf("GetOrder path: got %+v, want %+v", got, want)
	}

	orders, total, err := ListOrdersPage(ctx, db, OrderPage{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(orders) != 1 {
		t.Fatalf("list: total=%d len=%d, want 1/1", total, len(orders))
	}
	got2, ok := LoadConfirmation(orders[0])
	if !ok {
		t.Fatalf("ListOrdersPage path: LoadConfirmation ok = false, want true")
	}
	if got2.PaymentID != want.PaymentID || got2.Mode != want.Mode || got2.Token != want.Token || !got2.IssuedAt.Equal(want.IssuedAt) {
		t.Fatalf("ListOrdersPage path: got %+v, want %+v", got2, want)
	}
}

func TestLoadConfirmationEmptyAndBroken(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_empty")

	o, err := GetOrder(ctx, db, "ord_empty")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if _, ok := LoadConfirmation(o); ok {
		t.Fatalf("empty column: ok = true, want false")
	}

	if _, err := db.ExecContext(ctx, `UPDATE orders SET yoo_confirmation = ? WHERE id = ?`, "not-json{{", "ord_empty"); err != nil {
		t.Fatalf("seed broken column: %v", err)
	}
	o2, err := GetOrder(ctx, db, "ord_empty")
	if err != nil {
		t.Fatalf("get order after broken write: %v", err)
	}
	if _, ok := LoadConfirmation(o2); ok {
		t.Fatalf("broken json: ok = true, want false")
	}
}

func TestMigrateAddsConfirmationColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy_conf.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Simulate a DB created before yoo_confirmation existed.
	if _, err := db.Exec(`ALTER TABLE orders DROP COLUMN yoo_confirmation`); err != nil {
		t.Fatalf("drop column to simulate legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// First migrate() on a DB missing the column must succeed (ADD COLUMN).
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after simulated legacy schema: %v", err)
	}
	// Second migrate() call must also succeed (idempotent ALTER TABLE, duplicate
	// column error swallowed) — Open() re-runs migrate() on an already-migrated DB.
	if err := migrate(db2); err != nil {
		t.Fatalf("second migrate call: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
