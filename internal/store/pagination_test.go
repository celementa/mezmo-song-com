package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// seedMany создаёт n заказов с убывающим created_at, чтобы порядок страниц был
// предсказуем: ord_0 самый свежий.
func seedMany(t *testing.T, ctx context.Context, db *sql.DB, n int) {
	t.Helper()
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		o := Order{
			ID:               fmt.Sprintf("ord_%02d", i),
			Status:           "created",
			DeliveryStatus:   DeliveryPending,
			PaymentStatus:    "paid",
			InputPayloadJSON: `{"recipientName":"Ира"}`,
			CustomerJSON:     `{"email":"a@b.ru"}`,
			BasePrice:        990,
			CreatedAt:        base.Add(-time.Duration(i) * time.Minute),
		}
		if err := CreateOrder(ctx, db, o); err != nil {
			t.Fatalf("create %s: %v", o.ID, err)
		}
	}
}

func ids(orders []Order) []string {
	out := make([]string, 0, len(orders))
	for _, o := range orders {
		out = append(out, o.ID)
	}
	return out
}

// Страница отдаёт ровно свой срез, а total считает все подошедшие заказы —
// иначе админка не смогла бы показать «51–100 из 137».
func TestListOrdersPageSlicesAndCounts(t *testing.T) {
	ctx, db := testDB(t)
	seedMany(t, ctx, db, 12)

	first, total, err := ListOrdersPage(ctx, db, OrderPage{Limit: 5})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 12 {
		t.Fatalf("total = %d, want 12", total)
	}
	if got := ids(first); len(got) != 5 || got[0] != "ord_00" || got[4] != "ord_04" {
		t.Fatalf("page 1 = %v, want ord_00..ord_04", got)
	}

	third, _, err := ListOrdersPage(ctx, db, OrderPage{Limit: 5, Offset: 10})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if got := ids(third); len(got) != 2 || got[0] != "ord_10" {
		t.Fatalf("page 3 = %v, want ord_10, ord_11", got)
	}
}

// Поиск и фильтр применяются в SQL: с ними total — это число найденных, а не
// всех заказов, иначе пагинация нарисовала бы пустые страницы.
func TestListOrdersPageQueryAndFilter(t *testing.T) {
	ctx, db := testDB(t)
	seedMany(t, ctx, db, 4)
	markInternal(t, ctx, db, "ord_01")
	if _, err := db.ExecContext(ctx, `UPDATE orders SET payment_status = 'pending' WHERE id = ?`, "ord_02"); err != nil {
		t.Fatalf("mark unpaid: %v", err)
	}

	byID, total, err := ListOrdersPage(ctx, db, OrderPage{Query: "ord_03", Limit: 50})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(byID) != 1 || byID[0].ID != "ord_03" {
		t.Fatalf("search by id: got %v (total %d), want ord_03", ids(byID), total)
	}

	byEmail, total, err := ListOrdersPage(ctx, db, OrderPage{Query: "a@b.ru", Limit: 50})
	if err != nil {
		t.Fatalf("search by email: %v", err)
	}
	if total != 4 || len(byEmail) != 4 {
		t.Fatalf("search by email: got %d (total %d), want 4", len(byEmail), total)
	}

	internal, total, err := ListOrdersPage(ctx, db, OrderPage{Filter: FilterInternal, Limit: 50})
	if err != nil {
		t.Fatalf("filter internal: %v", err)
	}
	if total != 1 || len(internal) != 1 || internal[0].ID != "ord_01" {
		t.Fatalf("filter internal: got %v (total %d), want ord_01", ids(internal), total)
	}

	unpaid, total, err := ListOrdersPage(ctx, db, OrderPage{Filter: FilterUnpaid, Limit: 50})
	if err != nil {
		t.Fatalf("filter unpaid: %v", err)
	}
	if total != 1 || len(unpaid) != 1 || unpaid[0].ID != "ord_02" {
		t.Fatalf("filter unpaid: got %v (total %d), want ord_02", ids(unpaid), total)
	}

	if err := AddPaymentID(ctx, db, "ord_03", "31fa95da-000f-5000-b000-171e86816fbd", ""); err != nil {
		t.Fatalf("add payment id: %v", err)
	}
	byPayment, total, err := ListOrdersPage(ctx, db, OrderPage{Query: "31fa95da-000f-5000-b000-171e86816fbd", Limit: 50})
	if err != nil {
		t.Fatalf("search by payment id: %v", err)
	}
	if total != 1 || len(byPayment) != 1 || byPayment[0].ID != "ord_03" {
		t.Fatalf("search by payment id: got %v (total %d), want ord_03", ids(byPayment), total)
	}

	nothing, total, err := ListOrdersPage(ctx, db, OrderPage{Query: "не-найдётся", Limit: 50})
	if err != nil {
		t.Fatalf("empty search: %v", err)
	}
	if total != 0 || len(nothing) != 0 {
		t.Fatalf("empty search: got %v (total %d), want none", ids(nothing), total)
	}
}

// Аналитике нужен только запрошенный период, а не вся таблица.
func TestListOrdersSinceBoundsPeriod(t *testing.T) {
	ctx, db := testDB(t)
	seedMany(t, ctx, db, 2)
	if _, err := db.ExecContext(ctx, `UPDATE orders SET created_at = ? WHERE id = ?`,
		"2026-07-01T10:00:00Z", "ord_01"); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	recent, err := ListOrdersSince(ctx, db, "2026-08-01")
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if got := ids(recent); len(got) != 1 || got[0] != "ord_00" {
		t.Fatalf("since 2026-08-01 = %v, want only ord_00", got)
	}
}
