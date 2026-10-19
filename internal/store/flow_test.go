package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func flowTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestConsumeFreeGeneration_LimitPerKey(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	// У куки и у адреса лимиты теперь разные: человеку 3, адресу — потолок
	// выше (в этом тесте специально одинаковые, чтобы проверить сам механизм).
	keys := []FreeGenKey{{Key: "v:abc", Limit: 3}, {Key: "ip:1.2.3.4", Limit: 3}}

	for i := 0; i < 3; i++ {
		ok, err := ConsumeFreeGeneration(ctx, db, keys, "2026-08-31")
		if err != nil || !ok {
			t.Fatalf("attempt %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, err := ConsumeFreeGeneration(ctx, db, keys, "2026-08-31")
	if err != nil {
		t.Fatalf("4th attempt err: %v", err)
	}
	if ok {
		t.Fatal("4th attempt must hit the daily limit")
	}

	// Другой день — счётчик с нуля.
	ok, err = ConsumeFreeGeneration(ctx, db, keys, "2026-09-01")
	if err != nil || !ok {
		t.Fatalf("next day: ok=%v err=%v", ok, err)
	}

	// Смена куки не спасает: IP-ключ уже упёрся в лимит.
	ok, err = ConsumeFreeGeneration(ctx, db, []FreeGenKey{{Key: "v:new", Limit: 3}, {Key: "ip:1.2.3.4", Limit: 3}}, "2026-08-31")
	if err != nil {
		t.Fatalf("ip limit err: %v", err)
	}
	if ok {
		t.Fatal("new cookie with exhausted IP must be refused")
	}
}

func TestConsumeFreeGeneration_RefusalDoesNotBurnAttempts(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()

	// IP исчерпан, кука свежая. Отказ не должен списать попытку с куки.
	for i := 0; i < 3; i++ {
		if ok, err := ConsumeFreeGeneration(ctx, db, []FreeGenKey{{Key: "ip:9.9.9.9", Limit: 3}}, "d"); err != nil || !ok {
			t.Fatalf("seed ip: ok=%v err=%v", ok, err)
		}
	}
	if ok, _ := ConsumeFreeGeneration(ctx, db, []FreeGenKey{{Key: "v:fresh", Limit: 3}, {Key: "ip:9.9.9.9", Limit: 3}}, "d"); ok {
		t.Fatal("must be refused by ip")
	}
	var count int
	if err := db.QueryRow(`SELECT COALESCE(SUM(count),0) FROM free_generations WHERE key='v:fresh'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("refusal burned %d attempts of the fresh cookie", count)
	}
}

// Лимит на человека и лимит на адрес — разные величины: за общим адресом
// (офис, мобильный оператор) должны пройти несколько человек, каждый со своей
// квотой, и только потом упереться в потолок адреса.
func TestConsumeFreeGeneration_SeparateLimitsPerKeyKind(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	const perVisitor, perIP = 1, 3

	// Трое разных людей за одним адресом: каждому по одной генерации.
	for i, visitor := range []string{"v:a", "v:b", "v:c"} {
		keys := []FreeGenKey{{Key: visitor, Limit: perVisitor}, {Key: "ip:5.5.5.5", Limit: perIP}}
		if ok, err := ConsumeFreeGeneration(ctx, db, keys, "d"); err != nil || !ok {
			t.Fatalf("человек %d за общим адресом получил отказ: ok=%v err=%v", i+1, ok, err)
		}
	}
	// Четвёртый упирается в потолок адреса — это уже осознанная страховка.
	if ok, _ := ConsumeFreeGeneration(ctx, db, []FreeGenKey{{Key: "v:d", Limit: perVisitor}, {Key: "ip:5.5.5.5", Limit: perIP}}, "d"); ok {
		t.Fatal("потолок адреса не сработал")
	}
	// А тот же человек второй раз — в свой лимит, независимо от адреса.
	if ok, _ := ConsumeFreeGeneration(ctx, db, []FreeGenKey{{Key: "v:a", Limit: perVisitor}, {Key: "ip:7.7.7.7", Limit: perIP}}, "d"); ok {
		t.Fatal("лимит на человека не сработал")
	}
}

// Ключ без лимита (0) пропускается: так выключается проверка по адресу, когда
// прокси не пробросил реальный IP.
func TestConsumeFreeGenerationSkipsKeysWithoutLimit(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		keys := []FreeGenKey{{Key: "v:x", Limit: 10}, {Key: "ip:0.0.0.0", Limit: 0}}
		if ok, err := ConsumeFreeGeneration(ctx, db, keys, "d"); err != nil || !ok {
			t.Fatalf("попытка %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COALESCE(SUM(count),0) FROM free_generations WHERE key='ip:0.0.0.0'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ключ без лимита израсходовал %d попыток", count)
	}
}

func TestExtras_SelectAndPay(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	o := Order{ID: "ord_x", Status: "created", DeliveryStatus: DeliveryPending, PaymentStatus: "pending",
		InputPayloadJSON: "{}", CustomerJSON: "{}", BasePrice: 990, CreatedAt: time.Now()}
	if err := CreateOrder(ctx, db, o); err != nil {
		t.Fatal(err)
	}

	// Выбор до оплаты идемпотентен.
	if err := SelectExtra(ctx, db, "ord_x", "speed"); err != nil {
		t.Fatal(err)
	}
	if err := SelectExtra(ctx, db, "ord_x", "speed"); err != nil {
		t.Fatal(err)
	}
	got, _ := GetOrder(ctx, db, "ord_x")
	if ids := ExtrasSelected(got); len(ids) != 1 || ids[0] != "speed" {
		t.Fatalf("selected=%v", ids)
	}

	// Выданное подтверждение → известный платёж → оплата (ретрай не задваивает).
	if err := SaveExtraPending(ctx, db, "ord_x", PendingExtra{ID: "stems", PaymentID: "pay1", URL: "u", IssuedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	got, _ = GetOrder(ctx, db, "ord_x")
	if !KnownExtraPaymentID(got, "pay1") || KnownExtraPaymentID(got, "other") {
		t.Fatal("known extra payment mismatch")
	}
	if err := MarkExtraPaid(ctx, db, "ord_x", "stems", "pay1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := MarkExtraPaid(ctx, db, "ord_x", "stems", "pay1", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ = GetOrder(ctx, db, "ord_x")
	paid := ExtrasPaid(got)
	if len(paid) != 1 || paid[0].ID != "stems" {
		t.Fatalf("paid=%v", paid)
	}
	if len(ExtrasPending(got)) != 0 {
		t.Fatal("pending confirmation must be consumed after payment")
	}
	if !KnownExtraPaymentID(got, "pay1") {
		t.Fatal("paid payment id must stay known")
	}
}

// Стадия воронки двигается только вперёд. Порядок шагов у воронок разный (в v2
// оплата раньше вопросов о песне), и без ранга «последняя достигнутая стадия»
// показывала оплаченный заказ как недооформленный.
func TestSetFunnelStageMovesForwardOnly(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	o := Order{ID: "ord_stage", Status: "created", DeliveryStatus: DeliveryPending, PaymentStatus: "pending",
		InputPayloadJSON: "{}", CustomerJSON: "{}", BasePrice: 990, CreatedAt: time.Now()}
	if err := CreateOrder(ctx, db, o); err != nil {
		t.Fatal(err)
	}

	stage := func() string {
		got, err := GetOrder(ctx, db, "ord_stage")
		if err != nil {
			t.Fatal(err)
		}
		return got.FunnelStage.String
	}

	for _, st := range []string{StageStarted, StageEmailCaptured, StagePaid} {
		if err := SetFunnelStage(ctx, db, "ord_stage", st); err != nil {
			t.Fatalf("stage %s: %v", st, err)
		}
		if stage() != st {
			t.Fatalf("стадия = %q, ожидалась %q", stage(), st)
		}
	}

	// Так ходит v2: вопросы о песне отвечают ПОСЛЕ оплаты. Стадия не должна
	// откатиться к config_filled.
	if err := SetFunnelStage(ctx, db, "ord_stage", StageConfigFilled); err != nil {
		t.Fatal(err)
	}
	if stage() != StagePaid {
		t.Fatalf("стадия = %q — уехала назад с paid", stage())
	}

	// Финальная стадия дальше оплаты и записывается.
	if err := SetFunnelStage(ctx, db, "ord_stage", StageDone); err != nil {
		t.Fatal(err)
	}
	if stage() != StageDone {
		t.Fatalf("стадия = %q, ожидалась %q", stage(), StageDone)
	}
}

// Готовая песня доводит стадию воронки до done. Раньше эту стадию не
// проставлял никто, и панель «последняя достигнутая стадия» никогда не
// показывала дошедших до конца.
func TestFinalizingContentReachesStageDone(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		id, flow, want string
	}{
		{"ord_flow_done", "v1", StageDone},
		{"ord_legacy_done", "", ""},
	} {
		o := Order{ID: tc.id, Status: "processing", DeliveryStatus: DeliveryPending, PaymentStatus: "paid",
			InputPayloadJSON: "{}", CustomerJSON: "{}", BasePrice: 990, CreatedAt: time.Now()}
		if tc.flow != "" {
			o.Flow = sql.NullString{String: tc.flow, Valid: true}
			o.FunnelStage = sql.NullString{String: StagePaid, Valid: true}
		}
		if err := CreateOrder(ctx, db, o); err != nil {
			t.Fatal(err)
		}
		if err := SaveRelayResult(ctx, db, tc.id, []string{"orders/" + tc.id + "/r0/track-1.mp3"}, "текст", ""); err != nil {
			t.Fatal(err)
		}
		got, err := GetOrder(ctx, db, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if got.FunnelStage.String != tc.want {
			t.Fatalf("%s: стадия = %q, ожидалась %q", tc.id, got.FunnelStage.String, tc.want)
		}
	}
}

// Планировщик досылки не должен слать письмо неоплаченному заказу, даже если у
// него по какой-то причине выставлен deliver_after.
func TestListOrdersDueForDeliverySkipsUnpaid(t *testing.T) {
	db := flowTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(id, paymentStatus string) {
		o := Order{ID: id, Status: "completed", DeliveryStatus: DeliveryReady, PaymentStatus: paymentStatus,
			InputPayloadJSON: "{}", CustomerJSON: `{"email":"x@y.ru"}`, BasePrice: 990, CreatedAt: now}
		if err := CreateOrder(ctx, db, o); err != nil {
			t.Fatal(err)
		}
		if err := SetDeliverAfter(ctx, db, id, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	mk("ord_paid", "paid")
	mk("ord_unpaid", "pending")

	due, err := ListOrdersDueForDelivery(ctx, db, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "ord_paid" {
		t.Fatalf("due=%+v, want only ord_paid", due)
	}
}
