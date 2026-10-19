package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background(), db
}

func newTestOrder(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	o := Order{
		ID:               id,
		Status:           "created",
		DeliveryStatus:   DeliveryPending,
		PaymentStatus:    "paid",
		InputPayloadJSON: `{"recipientName":"Ира"}`,
		CustomerJSON:     `{"email":"a@b.ru"}`,
		CreatedAt:        time.Now(),
	}
	if err := CreateOrder(ctx, db, o); err != nil {
		t.Fatalf("create: %v", err)
	}
}

func delivery(t *testing.T, ctx context.Context, db *sql.DB, id string) string {
	t.Helper()
	o, err := GetOrder(ctx, db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return o.DeliveryStatus
}

// A real song moves the order pending → ready, and the email moves it to delivered.
func TestDeliveryLifecycleRealSong(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_real")

	if got := delivery(t, ctx, db, "ord_real"); got != DeliveryPending {
		t.Fatalf("fresh order: got %q, want %q", got, DeliveryPending)
	}

	if err := SaveRelayResult(ctx, db, "ord_real", []string{"orders/ord_real/track-1.mp3"}, "текст", "Песня"); err != nil {
		t.Fatalf("save relay: %v", err)
	}
	if got := delivery(t, ctx, db, "ord_real"); got != DeliveryReady {
		t.Fatalf("after content saved: got %q, want %q", got, DeliveryReady)
	}

	if err := MarkDelivered(ctx, db, "ord_real"); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if got := delivery(t, ctx, db, "ord_real"); got != DeliveryDone {
		t.Fatalf("after email: got %q, want %q", got, DeliveryDone)
	}

	o, _ := GetOrder(ctx, db, "ord_real")
	if IsFallbackAudio(o.AudioURLsJSON) {
		t.Error("real song must not be flagged as fallback")
	}
}

// The demo fallback also reaches ready, and is distinguishable by its audio URL.
func TestDeliveryFallbackIsFlagged(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_demo")

	if err := CompleteWithDemoAudio(ctx, db, "ord_demo", ""); err != nil {
		t.Fatalf("complete demo: %v", err)
	}
	if got := delivery(t, ctx, db, "ord_demo"); got != DeliveryReady {
		t.Fatalf("after demo fallback: got %q, want %q", got, DeliveryReady)
	}

	o, _ := GetOrder(ctx, db, "ord_demo")
	if !IsFallbackAudio(o.AudioURLsJSON) {
		t.Error("demo fallback must be flagged")
	}
	if !o.DemoCompleted {
		t.Error("demo_completed must be set")
	}
}

// A late fallback must not undo an already-sent email.
func TestDeliveredIsNotDowngraded(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_late")

	if err := SaveRelayResult(ctx, db, "ord_late", []string{"orders/ord_late/track-1.mp3"}, "текст", ""); err != nil {
		t.Fatalf("save relay: %v", err)
	}
	if err := MarkDelivered(ctx, db, "ord_late"); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if err := CompleteWithDemoAudio(ctx, db, "ord_late", "текст"); err != nil {
		t.Fatalf("late fallback: %v", err)
	}

	if got := delivery(t, ctx, db, "ord_late"); got != DeliveryDone {
		t.Fatalf("late fallback downgraded delivery: got %q, want %q", got, DeliveryDone)
	}
}

// Regeneration starts a new delivery cycle from scratch.
func TestRegenerationResetsDelivery(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_regen")

	if err := SaveRelayResult(ctx, db, "ord_regen", []string{"orders/ord_regen/track-1.mp3"}, "текст", ""); err != nil {
		t.Fatalf("save relay: %v", err)
	}
	if err := MarkDelivered(ctx, db, "ord_regen"); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if _, err := IncrementRegeneration(ctx, db, "ord_regen"); err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	if got := delivery(t, ctx, db, "ord_regen"); got != DeliveryPending {
		t.Fatalf("after regeneration: got %q, want %q", got, DeliveryPending)
	}
}

// Orders finished before delivery_status was written must be backfilled on boot.
func TestMigrationBackfillsCompletedOrders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	ctx := context.Background()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	newTestOrder(t, ctx, db, "ord_legacy")
	// Simulate the pre-fix state: content finalized, delivery_status never touched.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET status = 'completed', delivery_status = ? WHERE id = ?`,
		DeliveryPending, "ord_legacy"); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	_ = db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	if got := delivery(t, ctx, db2, "ord_legacy"); got != DeliveryReady {
		t.Fatalf("legacy order after migration: got %q, want %q", got, DeliveryReady)
	}
}
