package store

import (
	"database/sql"
	"testing"
	"time"
)

// Календарный месяц с прижимом дня: 31 января не должно уезжать в март.
func TestAddCalendarMonthClampsDay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-01-31T12:00:00Z", "2026-02-28T12:00:00Z"},
		{"2024-01-31T12:00:00Z", "2024-02-29T12:00:00Z"}, // високосный
		{"2026-08-15T09:30:00Z", "2026-09-15T09:30:00Z"},
		{"2026-12-05T00:00:00Z", "2027-01-05T00:00:00Z"},
	}
	for _, c := range cases {
		in, _ := time.Parse(time.RFC3339, c.in)
		if got := AddCalendarMonth(in).Format(time.RFC3339); got != c.want {
			t.Errorf("AddCalendarMonth(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

// Первый платёж активирует подписку на месяц; дубль того же платежа (ретрай
// вебхука) не продлевает второй раз и не считается «первым» для писем.
func TestRecordSubscriptionPaymentIdempotent(t *testing.T) {
	ctx, db := testDB(t)
	sub, err := UpsertPendingSubscription(ctx, db, "User@Mail.ru ", "Анна")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if sub.Email != "user@mail.ru" {
		t.Fatalf("email = %q, want normalized lowercase", sub.Email)
	}

	first, err := RecordSubscriptionPayment(ctx, db, sub.ID, "pay_1", "990.00", SubFlowFirst, "pm_1")
	if err != nil || !first {
		t.Fatalf("first payment: first=%v err=%v", first, err)
	}
	got, _ := GetSubscription(ctx, db, sub.ID)
	if got.Status != SubActive || got.YooMethodID.String != "pm_1" {
		t.Fatalf("after first payment: status=%s method=%s", got.Status, got.YooMethodID.String)
	}
	until := got.AccessUntilTime()
	if until.Before(time.Now().Add(27*24*time.Hour)) || until.After(time.Now().Add(32*24*time.Hour)) {
		t.Fatalf("access_until = %s, want ~1 месяц от сейчас", until)
	}
	if !got.HasAccess(time.Now().UTC()) {
		t.Fatal("активная подписка обязана иметь доступ")
	}

	dup, err := RecordSubscriptionPayment(ctx, db, sub.ID, "pay_1", "990.00", SubFlowFirst, "pm_1")
	if err != nil || dup {
		t.Fatalf("duplicate payment: first=%v err=%v, want false/nil", dup, err)
	}
	got2, _ := GetSubscription(ctx, db, sub.ID)
	if got2.AccessUntil.String != got.AccessUntil.String {
		t.Fatalf("дубль продлил период: %s → %s", got.AccessUntil.String, got2.AccessUntil.String)
	}
}

// Ребилл продлевает от плановой даты (не дарит дни при раннем платеже), отмена
// стирает способ оплаты и дату списания, но сохраняет доступ до конца периода.
func TestRebillAndCancel(t *testing.T) {
	ctx, db := testDB(t)
	sub, _ := UpsertPendingSubscription(ctx, db, "a@b.ru", "")
	if _, err := RecordSubscriptionPayment(ctx, db, sub.ID, "pay_first", "990.00", SubFlowFirst, "pm_1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	before, _ := GetSubscription(ctx, db, sub.ID)

	if _, err := RecordSubscriptionPayment(ctx, db, sub.ID, "pay_rebill", "990.00", SubFlowRebill, ""); err != nil {
		t.Fatalf("rebill: %v", err)
	}
	after, _ := GetSubscription(ctx, db, sub.ID)
	want := AddCalendarMonth(before.NextChargeTime()).Format(time.RFC3339)
	if after.AccessUntil.String != want {
		t.Fatalf("rebill: access_until = %s, want %s (плановая дата + месяц)", after.AccessUntil.String, want)
	}
	if after.YooMethodID.String != "pm_1" {
		t.Fatalf("rebill без methodID стёр привязку: %q", after.YooMethodID.String)
	}

	email, accessUntil, ok, err := CancelSubscription(ctx, db, sub.ID)
	if err != nil || !ok || email != "a@b.ru" {
		t.Fatalf("cancel: ok=%v email=%q err=%v", ok, email, err)
	}
	if accessUntil.Format(time.RFC3339) != after.AccessUntil.String {
		t.Fatalf("cancel: access until %s, want unchanged %s", accessUntil, after.AccessUntil.String)
	}
	canceled, _ := GetSubscription(ctx, db, sub.ID)
	if canceled.Status != SubCanceled || canceled.NextChargeAt.Valid || canceled.YooMethodID.Valid {
		t.Fatalf("cancel: status=%s next=%v method=%v", canceled.Status, canceled.NextChargeAt, canceled.YooMethodID)
	}
	if !canceled.HasAccess(time.Now().UTC()) {
		t.Fatal("после отмены доступ до конца периода обязан сохраниться")
	}

	// Повторная отмена — no-op.
	_, _, ok, err = CancelSubscription(ctx, db, sub.ID)
	if err != nil || ok {
		t.Fatalf("second cancel: ok=%v err=%v, want false/nil", ok, err)
	}

	// Активную подписку нельзя продать повторно, отменённую — можно переоформить.
	if _, err := UpsertPendingSubscription(ctx, db, "a@b.ru", ""); err != nil {
		t.Fatalf("re-subscribe after cancel: %v", err)
	}
}

func TestUpsertPendingRejectsActive(t *testing.T) {
	ctx, db := testDB(t)
	sub, _ := UpsertPendingSubscription(ctx, db, "x@y.ru", "")
	if _, err := RecordSubscriptionPayment(ctx, db, sub.ID, "p1", "990.00", SubFlowFirst, "pm"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertPendingSubscription(ctx, db, "x@y.ru", ""); err != ErrSubscriptionActive {
		t.Fatalf("err = %v, want ErrSubscriptionActive", err)
	}
}

// Просроченное списание попадает в due-список, неудачная попытка уводит в
// past_due и откладывает ретрай, безнадёжная просрочка закрывает подписку.
func TestDueChargesAndExpiry(t *testing.T) {
	ctx, db := testDB(t)
	sub, _ := UpsertPendingSubscription(ctx, db, "due@t.ru", "")
	if _, err := RecordSubscriptionPayment(ctx, db, sub.ID, "p1", "990.00", SubFlowFirst, "pm"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cooldown, giveUp := 7*24*time.Hour, 30*24*time.Hour

	due, err := ListDueSubscriptionCharges(ctx, db, now, cooldown, giveUp)
	if err != nil || len(due) != 0 {
		t.Fatalf("свежая подписка не должна быть due: n=%d err=%v", len(due), err)
	}

	// Сдвигаем плановую дату в прошлое — подписка становится due.
	past := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE subscriptions SET next_charge_at = ? WHERE id = ?`, past, sub.ID); err != nil {
		t.Fatal(err)
	}
	due, _ = ListDueSubscriptionCharges(ctx, db, now, cooldown, giveUp)
	if len(due) != 1 || due[0].ID != sub.ID {
		t.Fatalf("due = %d, want 1", len(due))
	}

	// Неудачная попытка: до конца кулдауна подписка не due, после — снова due.
	if err := MarkChargeAttemptFailed(ctx, db, sub.ID, now); err != nil {
		t.Fatal(err)
	}
	if due, _ = ListDueSubscriptionCharges(ctx, db, now.Add(24*time.Hour), cooldown, giveUp); len(due) != 0 {
		t.Fatalf("в кулдауне ретраев быть не должно, got %d", len(due))
	}
	if due, _ = ListDueSubscriptionCharges(ctx, db, now.Add(8*24*time.Hour), cooldown, giveUp); len(due) != 1 {
		t.Fatalf("после кулдауна ретрай обязан вернуться, got %d", len(due))
	}

	// Просрочка дольше giveUp: не в due, закрывается ExpireOverdue.
	if due, _ = ListDueSubscriptionCharges(ctx, db, now.Add(40*24*time.Hour), cooldown, giveUp); len(due) != 0 {
		t.Fatalf("безнадёжная просрочка не должна ретраиться, got %d", len(due))
	}
	expired, err := ExpireOverdueSubscriptions(ctx, db, now.Add(40*24*time.Hour), giveUp)
	if err != nil || len(expired) != 1 {
		t.Fatalf("expire: n=%d err=%v", len(expired), err)
	}
	got, _ := GetSubscription(ctx, db, sub.ID)
	if got.Status != SubCanceled {
		t.Fatalf("expired status = %s, want canceled", got.Status)
	}
}

// Напоминание уходит один раз на каждую дату списания.
func TestReminderOncePerChargeDate(t *testing.T) {
	ctx, db := testDB(t)
	sub, _ := UpsertPendingSubscription(ctx, db, "r@t.ru", "")
	if _, err := RecordSubscriptionPayment(ctx, db, sub.ID, "p1", "990.00", SubFlowFirst, "pm"); err != nil {
		t.Fatal(err)
	}
	got, _ := GetSubscription(ctx, db, sub.ID)
	lead := 3 * 24 * time.Hour
	almostDue := got.NextChargeTime().Add(-24 * time.Hour)

	rows, _ := ListSubscriptionsNeedingReminder(ctx, db, time.Now().UTC(), lead)
	if len(rows) != 0 {
		t.Fatalf("за месяц до списания напоминание не нужно, got %d", len(rows))
	}
	rows, _ = ListSubscriptionsNeedingReminder(ctx, db, almostDue, lead)
	if len(rows) != 1 {
		t.Fatalf("за сутки до списания напоминание нужно, got %d", len(rows))
	}
	if err := MarkReminderSent(ctx, db, sub.ID, got.NextChargeAt.String); err != nil {
		t.Fatal(err)
	}
	if rows, _ = ListSubscriptionsNeedingReminder(ctx, db, almostDue, lead); len(rows) != 0 {
		t.Fatalf("повторное напоминание для той же даты, got %d", len(rows))
	}
}

// Сессия кабинета живёт до expires_at, заказы подписки видны и считаются в лимит.
func TestSessionsAndSubscriptionOrders(t *testing.T) {
	ctx, db := testDB(t)
	sub, _ := UpsertPendingSubscription(ctx, db, "s@t.ru", "")
	if _, err := RecordSubscriptionPayment(ctx, db, sub.ID, "p1", "990.00", SubFlowFirst, "pm"); err != nil {
		t.Fatal(err)
	}

	token, err := CreateSubscriptionSession(ctx, db, sub.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	bySess, err := SubscriptionBySession(ctx, db, token)
	if err != nil || bySess.ID != sub.ID {
		t.Fatalf("by session: %v %v", bySess.ID, err)
	}
	if _, err := SubscriptionBySession(ctx, db, "nope"); err != ErrNotFound {
		t.Fatalf("bad token err = %v, want ErrNotFound", err)
	}
	expired, err := CreateSubscriptionSession(ctx, db, sub.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SubscriptionBySession(ctx, db, expired); err != ErrNotFound {
		t.Fatalf("expired session err = %v, want ErrNotFound", err)
	}

	for i, id := range []string{"ord_s1", "ord_s2"} {
		o := Order{
			ID: id, Status: "created", DeliveryStatus: DeliveryPending, PaymentStatus: PaymentPaid,
			InputPayloadJSON: `{"recipientName":"Ира"}`, CustomerJSON: `{"email":"s@t.ru"}`,
			DiscountCode:   sql.NullString{String: SubscriptionDiscountCode, Valid: true},
			SubscriptionID: sql.NullString{String: sub.ID, Valid: true},
			CreatedAt:      time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := CreateOrder(ctx, db, o); err != nil {
			t.Fatal(err)
		}
	}
	orders, err := ListSubscriptionOrders(ctx, db, sub.ID)
	if err != nil || len(orders) != 2 {
		t.Fatalf("orders: n=%d err=%v", len(orders), err)
	}
	if orders[0].SubscriptionID.String != sub.ID {
		t.Fatalf("subscription_id не сохранился: %v", orders[0].SubscriptionID)
	}
	n, err := CountSubscriptionOrdersSince(ctx, db, sub.ID, time.Now().Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("count = %d err=%v, want 2", n, err)
	}
}

// Оплаченными считаются только подписки с настоящим платежом: pending без
// денег и тестовые активации "mock-…" в множество не попадают.
func TestPaidSubscriptionIDs(t *testing.T) {
	ctx, db := testDB(t)
	paid, _ := UpsertPendingSubscription(ctx, db, "paid@mail.ru", "")
	mock, _ := UpsertPendingSubscription(ctx, db, "mock@mail.ru", "")
	pending, _ := UpsertPendingSubscription(ctx, db, "pending@mail.ru", "")
	if _, err := RecordSubscriptionPayment(ctx, db, paid.ID, "pay_real", "750.00", SubFlowFirst, "pm"); err != nil {
		t.Fatalf("real payment: %v", err)
	}
	if _, err := RecordSubscriptionPayment(ctx, db, mock.ID, "mock-1", "0.00", SubFlowFirst, ""); err != nil {
		t.Fatalf("mock payment: %v", err)
	}
	ids, err := PaidSubscriptionIDs(ctx, db)
	if err != nil {
		t.Fatalf("PaidSubscriptionIDs: %v", err)
	}
	if !ids[paid.ID] || ids[mock.ID] || ids[pending.ID] || len(ids) != 1 {
		t.Fatalf("ids = %v, want только %s", ids, paid.ID)
	}
}
