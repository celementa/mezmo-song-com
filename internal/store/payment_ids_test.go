package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// Embedded-токен одноразовый и часовой: повторная попытка оплаты заводит новый
// платёж у ЮKassa, а вебхук обязан узнать любой из них — не только последний.
// AddPaymentID должен копить полную историю, а GetOrder и ListOrdersPage — два
// разных пути сканирования заказа — обязаны отдавать одну и ту же историю.
func TestAddPaymentIDKeepsHistory(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_multi")

	if err := AddPaymentID(ctx, db, "ord_multi", "pay_1", ""); err != nil {
		t.Fatalf("add pay_1: %v", err)
	}
	if err := AddPaymentID(ctx, db, "ord_multi", "pay_2", "https://yoomoney.ru/checkout/pay_2"); err != nil {
		t.Fatalf("add pay_2: %v", err)
	}

	o, err := GetOrder(ctx, db, "ord_multi")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	assertPaymentHistory(t, o, "pay_2", []string{"pay_1", "pay_2"})
	if o.XenditInvoiceURL.String != "https://yoomoney.ru/checkout/pay_2" {
		t.Fatalf("xendit_invoice_url = %q, want the redirect url from pay_2", o.XenditInvoiceURL.String)
	}

	orders, _, err := ListOrdersPage(ctx, db, OrderPage{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("list orders page: %v", err)
	}
	var found *Order
	for i := range orders {
		if orders[i].ID == "ord_multi" {
			found = &orders[i]
		}
	}
	if found == nil {
		t.Fatalf("ord_multi not found via ListOrdersPage")
	}
	assertPaymentHistory(t, *found, "pay_2", []string{"pay_1", "pay_2"})
}

// invoiceURL пустой (embedded) не должен затирать ранее сохранённую redirect-ссылку.
func TestAddPaymentIDEmbeddedKeepsRedirectURL(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_mix")

	if err := AddPaymentID(ctx, db, "ord_mix", "pay_redirect", "https://yoomoney.ru/checkout/pay_redirect"); err != nil {
		t.Fatalf("add redirect: %v", err)
	}
	if err := AddPaymentID(ctx, db, "ord_mix", "pay_embedded", ""); err != nil {
		t.Fatalf("add embedded: %v", err)
	}

	o, err := GetOrder(ctx, db, "ord_mix")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if o.XenditInvoiceURL.String != "https://yoomoney.ru/checkout/pay_redirect" {
		t.Fatalf("xendit_invoice_url = %q, want the earlier redirect url preserved", o.XenditInvoiceURL.String)
	}
	if !KnownPaymentID(o, "pay_redirect") || !KnownPaymentID(o, "pay_embedded") {
		t.Fatalf("both payments must be known")
	}
	if KnownPaymentID(o, "pay_other") {
		t.Fatalf("unrelated payment must not be known")
	}
}

func assertPaymentHistory(t *testing.T, o Order, wantCurrent string, wantHistory []string) {
	t.Helper()
	if o.YooPaymentID.String != wantCurrent {
		t.Fatalf("yoo_payment_id = %q, want %q", o.YooPaymentID.String, wantCurrent)
	}
	if !o.YooPaymentIDs.Valid {
		t.Fatalf("yoo_payment_ids is not set")
	}
	var ids []string
	if err := json.Unmarshal([]byte(o.YooPaymentIDs.String), &ids); err != nil {
		t.Fatalf("unmarshal yoo_payment_ids: %v", err)
	}
	if len(ids) != len(wantHistory) {
		t.Fatalf("yoo_payment_ids = %v, want %v", ids, wantHistory)
	}
	for i, want := range wantHistory {
		if ids[i] != want {
			t.Fatalf("yoo_payment_ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
	for _, id := range wantHistory {
		if !KnownPaymentID(o, id) {
			t.Fatalf("KnownPaymentID(%q) = false, want true", id)
		}
	}
}

// Двойной клик по «Оплатить» — два конкурентных initiate-payment на один заказ.
// AddPaymentID теперь один UPDATE без предварительного GetOrder, чтобы конкурентные
// вызовы не читали одну и ту же историю и не затирали друг друга: без этого второй
// писатель мог потерять id, добавленный первым, и вебхук по настоящей оплате
// отвечал бы payment_id_mismatch.
func TestAddPaymentIDConcurrentWritesKeepAllIDs(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_race")

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = AddPaymentID(ctx, db, "ord_race", fmt.Sprintf("pay_race_%d", i), "")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("AddPaymentID #%d: %v", i, err)
		}
	}

	o, err := GetOrder(ctx, db, "ord_race")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if !o.YooPaymentIDs.Valid {
		t.Fatalf("yoo_payment_ids is not set")
	}
	var ids []string
	if err := json.Unmarshal([]byte(o.YooPaymentIDs.String), &ids); err != nil {
		t.Fatalf("unmarshal yoo_payment_ids: %v", err)
	}
	if len(ids) != n {
		t.Fatalf("yoo_payment_ids has %d entries, want %d (lost some under concurrent writes): %v", len(ids), n, ids)
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("pay_race_%d", i)
		if !KnownPaymentID(o, want) {
			t.Fatalf("payment %q lost under concurrent writes", want)
		}
	}
}

// Заказы, заведённые до появления yoo_payment_ids, бэкфилятся при старте:
// одиночный yoo_payment_id становится историей из одного элемента.
func TestMigrateBackfillsPaymentIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy_payments.db")
	ctx := context.Background()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	newTestOrder(t, ctx, db, "ord_legacy_pay")
	// Симулируем состояние до этой миграции: yoo_payment_id заполнен старым кодом,
	// yoo_payment_ids ещё не существовал.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET yoo_payment_id = ? WHERE id = ?`, "pay_old", "ord_legacy_pay"); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	_ = db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	o, err := GetOrder(ctx, db2, "ord_legacy_pay")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	assertPaymentHistory(t, o, "pay_old", []string{"pay_old"})
}
