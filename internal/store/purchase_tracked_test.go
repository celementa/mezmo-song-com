package store

import (
	"testing"
	"time"
)

// Отбивка покупки в браузере идёт из двух точек (/thanks и страница заказа),
// поэтому заявка обязана быть атомарной: второй вызов должен получить first=false
// и НЕ отправлять событие. Раньше UPDATE шёл без гарда и перезаписывал отметку —
// переход с /thanks на страницу заказа успевал задвоить покупку в отчётах.
func TestMarkPurchaseTrackedClaimsOnce(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_claim")

	first, err := MarkPurchaseTracked(ctx, db, "ord_claim", time.Now())
	if err != nil {
		t.Fatalf("первая заявка: %v", err)
	}
	if !first {
		t.Fatal("первая заявка должна вернуть first=true")
	}

	o, err := GetOrder(ctx, db, "ord_claim")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	stamp := o.PurchaseTrackedAt.String
	if stamp == "" {
		t.Fatal("purchase_tracked_at пуст после успешной заявки")
	}

	second, err := MarkPurchaseTracked(ctx, db, "ord_claim", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("вторая заявка: %v", err)
	}
	if second {
		t.Fatal("вторая заявка вернула first=true — покупка уйдёт в аналитику дважды")
	}

	o, err = GetOrder(ctx, db, "ord_claim")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PurchaseTrackedAt.String != stamp {
		t.Fatalf("вторая заявка перезаписала отметку: было %q, стало %q", stamp, o.PurchaseTrackedAt.String)
	}
}

// Заявка по несуществующему заказу — не первая и не ошибка: ручка ответит
// first=false, фронт промолчит.
func TestMarkPurchaseTrackedUnknownOrder(t *testing.T) {
	ctx, db := testDB(t)
	first, err := MarkPurchaseTracked(ctx, db, "ord_missing", time.Now())
	if err != nil {
		t.Fatalf("unknown order: %v", err)
	}
	if first {
		t.Fatal("неизвестный заказ не может быть первым")
	}
}
