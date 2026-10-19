package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func markInternal(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE orders SET discount_code = ?, base_price = 0 WHERE id = ?`,
		InternalDiscountCode, id); err != nil {
		t.Fatalf("mark internal: %v", err)
	}
}

// A customer order runs out of revisions after MaxRegenerations attempts.
func TestRegenerationLimitForCustomerOrder(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_client")

	for i := 1; i <= MaxRegenerations; i++ {
		count, err := IncrementRegeneration(ctx, db, "ord_client")
		if err != nil {
			t.Fatalf("regeneration %d: %v", i, err)
		}
		if count != i {
			t.Fatalf("regeneration %d: got count %d, want %d", i, count, i)
		}
	}

	if _, err := IncrementRegeneration(ctx, db, "ord_client"); err == nil ||
		err.Error() != "max_regenerations_reached" {
		t.Fatalf("past the limit: got err %v, want max_regenerations_reached", err)
	}
}

// Test orders from /admin/ads are exempt from the limit — bloggers iterate on lyrics there.
func TestRegenerationUnlimitedForInternalOrder(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_internal")
	markInternal(t, ctx, db, "ord_internal")

	for i := 1; i <= MaxRegenerations+3; i++ {
		count, err := IncrementRegeneration(ctx, db, "ord_internal")
		if err != nil {
			t.Fatalf("regeneration %d on internal order: %v", i, err)
		}
		if count != i {
			t.Fatalf("regeneration %d: got count %d, want %d", i, count, i)
		}
	}

	o, err := GetOrder(ctx, db, "ord_internal")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !IsInternalOrder(o) {
		t.Fatalf("IsInternalOrder = false for discount_code %q", o.DiscountCode.String)
	}
}

// Stress marks the model writes for its own meter check never reach the customer.
func TestSavedLyricsHaveNoStressMarks(t *testing.T) {
	ctx, db := testDB(t)
	marked := "Ната́ша, я жда́ла тебя́\nсего́дня"
	want := "Наташа, я ждала тебя\nсегодня"

	newTestOrder(t, ctx, db, "ord_marks")
	if err := SaveLyrics(ctx, db, "ord_marks", marked); err != nil {
		t.Fatalf("save lyrics: %v", err)
	}
	o, err := GetOrder(ctx, db, "ord_marks")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.LyricsText.String != want {
		t.Fatalf("SaveLyrics: got %q, want %q", o.LyricsText.String, want)
	}

	newTestOrder(t, ctx, db, "ord_marks_relay")
	if err := SaveRelayResult(ctx, db, "ord_marks_relay", []string{"orders/x/track-1.mp3"}, marked, "Ната́ша"); err != nil {
		t.Fatalf("save relay: %v", err)
	}
	o2, err := GetOrder(ctx, db, "ord_marks_relay")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.ContainsAny(o2.LyricsText.String, "\u0301\u0341") {
		t.Fatalf("SaveRelayResult kept stress marks: %q", o2.LyricsText.String)
	}
}
