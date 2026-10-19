package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func versionsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedVersionsOrder(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	o := Order{
		ID:               id,
		Status:           "completed",
		DeliveryStatus:   DeliveryReady,
		PaymentStatus:    PaymentPaid,
		InputPayloadJSON: `{"recipientName":"Ира"}`,
		CustomerJSON:     `{}`,
		BasePrice:        990,
		CreatedAt:        time.Now(),
	}
	if err := CreateOrder(context.Background(), db, o); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// Доработка перезатирает текст песни. Без истории удачный вариант терялся
// насовсем — именно поэтому итеративные правки превращались в рулетку.
func TestRegenerationKeepsPreviousLyrics(t *testing.T) {
	ctx := context.Background()
	db := versionsTestDB(t)
	seedVersionsOrder(t, db, "ord_lv1")

	if err := SaveLyrics(ctx, db, "ord_lv1", "первая версия"); err != nil {
		t.Fatalf("save lyrics: %v", err)
	}
	if _, err := IncrementRegeneration(ctx, db, "ord_lv1"); err != nil {
		t.Fatalf("increment: %v", err)
	}
	if err := SaveLyrics(ctx, db, "ord_lv1", "вторая версия"); err != nil {
		t.Fatalf("save lyrics: %v", err)
	}

	got, err := ListLyricsVersions(ctx, db, "ord_lv1")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("версий = %d, want 1 (заменённый текст)", len(got))
	}
	if got[0].Lyrics != "первая версия" {
		t.Fatalf("версия = %q, want «первая версия»", got[0].Lyrics)
	}
	if got[0].Version != 0 {
		t.Fatalf("номер версии = %d, want 0 (счётчик до доработки)", got[0].Version)
	}

	// Текущий текст остаётся в заказе и в историю не дублируется.
	o, err := GetOrder(ctx, db, "ord_lv1")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.LyricsText.String != "вторая версия" {
		t.Fatalf("текущий текст = %q, want «вторая версия»", o.LyricsText.String)
	}
}

// Каждая доработка добавляет ровно одну версию, свежие идут первыми.
func TestLyricsVersionsAccumulateNewestFirst(t *testing.T) {
	ctx := context.Background()
	db := versionsTestDB(t)
	seedVersionsOrder(t, db, "ord_lv2")

	// Лимит доработок по умолчанию — 2, поэтому берём внутренний заказ с безлимитом.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET discount_code = ? WHERE id = ?`, InternalDiscountCode, "ord_lv2"); err != nil {
		t.Fatalf("mark internal: %v", err)
	}

	texts := []string{"версия 1", "версия 2", "версия 3", "версия 4"}
	for i, text := range texts {
		if err := SaveLyrics(ctx, db, "ord_lv2", text); err != nil {
			t.Fatalf("save lyrics: %v", err)
		}
		if i == len(texts)-1 {
			break // последний текст — текущий, его заменять нечем
		}
		if _, err := IncrementRegeneration(ctx, db, "ord_lv2"); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}

	got, err := ListLyricsVersions(ctx, db, "ord_lv2")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("версий = %d, want 3", len(got))
	}
	if got[0].Lyrics != "версия 3" || got[2].Lyrics != "версия 1" {
		t.Fatalf("порядок версий неверный: %q … %q (ожидались свежие первыми)", got[0].Lyrics, got[2].Lyrics)
	}
}

// Отказ по лимиту доработок не должен плодить версии: текст не заменялся.
func TestRejectedRegenerationDoesNotAddVersion(t *testing.T) {
	ctx := context.Background()
	db := versionsTestDB(t)
	seedVersionsOrder(t, db, "ord_lv3")

	if err := SaveLyrics(ctx, db, "ord_lv3", "единственный текст"); err != nil {
		t.Fatalf("save lyrics: %v", err)
	}
	for i := range MaxRegenerations {
		if _, err := IncrementRegeneration(ctx, db, "ord_lv3"); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}
	// Лимит исчерпан — эта попытка обязана быть отклонена.
	if _, err := IncrementRegeneration(ctx, db, "ord_lv3"); err == nil {
		t.Fatal("доработка сверх лимита прошла, ожидалась ошибка")
	}

	got, err := ListLyricsVersions(ctx, db, "ord_lv3")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	// Версия одна: первая доработка сохранила текст, вторая нашла в истории тот же
	// самый (новый так и не пришёл) и дубль не записала, отклонённая не дошла до
	// записи вовсе.
	if len(got) != 1 {
		t.Fatalf("версий = %d, want 1", len(got))
	}
}
