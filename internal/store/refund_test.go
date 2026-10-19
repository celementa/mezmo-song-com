package store

import (
	"context"
	"path/filepath"
	"testing"
)

// MarkCanceled применяется только к pending-заказу — переход в WHERE, а не в
// Go-ветке, поэтому оплаченный заказ вебхуком payment.canceled отменить нельзя.
func TestMarkCanceledOnlyFromPending(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_cancel_pending")
	if _, err := db.ExecContext(ctx, `UPDATE orders SET payment_status = ? WHERE id = ?`, PaymentPending, "ord_cancel_pending"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	changed, err := MarkCanceled(ctx, db, "ord_cancel_pending")
	if err != nil {
		t.Fatalf("MarkCanceled: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true (pending order must cancel)")
	}
	o, err := GetOrder(ctx, db, "ord_cancel_pending")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != PaymentCanceled {
		t.Fatalf("payment_status = %q, want %q", o.PaymentStatus, PaymentCanceled)
	}

	// Заказ, который уже оплачен, отменить нельзя.
	newTestOrder(t, ctx, db, "ord_cancel_paid") // newTestOrder создаёт заказ с payment_status = "paid"
	changed, err = MarkCanceled(ctx, db, "ord_cancel_paid")
	if err != nil {
		t.Fatalf("MarkCanceled paid: %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false (paid order must not cancel)")
	}
	o, err = GetOrder(ctx, db, "ord_cancel_paid")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (unchanged)", o.PaymentStatus)
	}
}

// MarkRefunded применяется только из paid — гард в WHERE. Неоплаченный заказ
// вебхуком возврата в refunded не переводится.
func TestMarkRefundedOnlyFromPaid(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_refund_paid") // payment_status = paid по умолчанию

	changed, err := MarkRefunded(ctx, db, "ord_refund_paid")
	if err != nil {
		t.Fatalf("MarkRefunded: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true (paid order must refund)")
	}
	o, err := GetOrder(ctx, db, "ord_refund_paid")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != PaymentRefunded {
		t.Fatalf("payment_status = %q, want %q", o.PaymentStatus, PaymentRefunded)
	}

	// Заказ, который не был оплачен, из pending в refunded не переходит.
	newTestOrder(t, ctx, db, "ord_refund_pending")
	if _, err := db.ExecContext(ctx, `UPDATE orders SET payment_status = ? WHERE id = ?`, PaymentPending, "ord_refund_pending"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	changed, err = MarkRefunded(ctx, db, "ord_refund_pending")
	if err != nil {
		t.Fatalf("MarkRefunded pending: %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false (pending order must not refund)")
	}
	o, err = GetOrder(ctx, db, "ord_refund_pending")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != PaymentPending {
		t.Fatalf("payment_status = %q, want pending (unchanged)", o.PaymentStatus)
	}

	// Повторный вызов на уже refunded-заказе — идемпотентность, changed=false.
	changed, err = MarkRefunded(ctx, db, "ord_refund_paid")
	if err != nil {
		t.Fatalf("MarkRefunded second call: %v", err)
	}
	if changed {
		t.Fatalf("changed = true on second call, want false (already refunded)")
	}
}

// SetRefundedKopecks перезаписывает значение, а не накапливает: вебхуки ЮKassa
// ретраятся, и сложение дало бы двойной учёт.
func TestSetRefundedKopecks(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_refund_amount")

	if err := SetRefundedKopecks(ctx, db, "ord_refund_amount", 49500); err != nil {
		t.Fatalf("set 49500: %v", err)
	}
	o, err := GetOrder(ctx, db, "ord_refund_amount")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.RefundedKopecks != 49500 {
		t.Fatalf("refunded_kopecks = %d, want 49500", o.RefundedKopecks)
	}

	// Второй вызов с тем же значением (пересчитано из API) — не складывается с прежним.
	if err := SetRefundedKopecks(ctx, db, "ord_refund_amount", 49500); err != nil {
		t.Fatalf("set 49500 again: %v", err)
	}
	o, err = GetOrder(ctx, db, "ord_refund_amount")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.RefundedKopecks != 49500 {
		t.Fatalf("refunded_kopecks after repeat = %d, want 49500 (not doubled)", o.RefundedKopecks)
	}

	// Полный возврат — перезапись новой суммой, не сложение.
	if err := SetRefundedKopecks(ctx, db, "ord_refund_amount", 99000); err != nil {
		t.Fatalf("set 99000: %v", err)
	}
	o, err = GetOrder(ctx, db, "ord_refund_amount")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.RefundedKopecks != 99000 {
		t.Fatalf("refunded_kopecks = %d, want 99000", o.RefundedKopecks)
	}
}

// Заказы, заведённые до появления этой миграции, читаются как refunded_kopecks = 0
// (возвратов не было), а не падают на отсутствующей колонке. Прогон миграции
// дважды на одной БД не должен ронять открытие.
func TestMigrateAddsRefundedKopecksColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy_refund.db")
	ctx := context.Background()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	newTestOrder(t, ctx, db, "ord_legacy_refund")
	_ = db.Close()

	// Повторное открытие той же БД — идемпотентность ALTER TABLE ADD COLUMN.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	o, err := GetOrder(ctx, db2, "ord_legacy_refund")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.RefundedKopecks != 0 {
		t.Fatalf("refunded_kopecks = %d, want 0 (default for pre-existing orders)", o.RefundedKopecks)
	}

	// Третье открытие — миграция должна оставаться идемпотентной.
	db3, err := Open(path)
	if err != nil {
		t.Fatalf("reopen again: %v", err)
	}
	defer db3.Close()
}
