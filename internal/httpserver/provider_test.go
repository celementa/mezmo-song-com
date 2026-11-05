package httpserver

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/store"
)

func providerTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "provider.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Server{DB: db, Config: config.Config{UnitPayCurrency: "USD", WebhookSecret: "test-secret"}}, db
}

// Провайдер выбирается веткой заказа: RU → ЮKassa, INT → Stripe. Глобальная
// настройка UnitPay не перебивает ветку, иначе международный покупатель снова
// попал бы в рублёвый шлюз.
func TestOrderProviderFollowsBranch(t *testing.T) {
	s, db := providerTestServer(t)
	ctx := context.Background()
	s.Config.UnitPayProjectID, s.Config.UnitPaySecretKey = "1", "sk"
	if err := store.SetSetting(ctx, db, store.SettingPaymentRUProvider, "yookassa"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(ctx, db, store.SettingPaymentIntlProvider, "stripe"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		branch string
		want   string
	}{
		{"ru", ProviderYooKassa},
		{"intl", ProviderStripe},
	}
	for _, c := range cases {
		o := store.Order{PaymentBranch: sql.NullString{String: c.branch, Valid: true}}
		if got := s.orderProvider(ctx, o); got != c.want {
			t.Fatalf("branch %s: provider = %q, want %q", c.branch, got, c.want)
		}
	}

	// Заказ без ветки сохраняет прежнее поведение.
	if got := s.orderProvider(ctx, store.Order{}); got != ProviderUnitPay {
		t.Fatalf("legacy provider = %q, want %q", got, ProviderUnitPay)
	}
}

// Сумма UnitPay — в валюте заказа. Тестовый UnitPay принимает только RUB и
// пересчитывает только долларовую сумму; рублёвую оставлять как есть.
func TestUnitPayAmountUsesOrderCurrency(t *testing.T) {
	s, db := providerTestServer(t)
	ctx := context.Background()
	seedOrder(t, db, "ord_cur_usd", store.PaymentPending)
	if _, err := db.Exec(`UPDATE orders SET currency = 'USD', base_price = 19 WHERE id = 'ord_cur_usd'`); err != nil {
		t.Fatal(err)
	}
	o, err := store.GetOrder(ctx, db, "ord_cur_usd")
	if err != nil {
		t.Fatal(err)
	}
	sum, cur := s.unitpayAmount(ctx, o)
	if sum != "19.00" || cur != "USD" {
		t.Fatalf("USD order: %s %s, want 19.00 USD", sum, cur)
	}

	if _, err := db.Exec(`UPDATE orders SET currency = 'RUB', base_price = 750 WHERE id = 'ord_cur_usd'`); err != nil {
		t.Fatal(err)
	}
	o, _ = store.GetOrder(ctx, db, "ord_cur_usd")
	sum, cur = s.unitpayAmount(ctx, o)
	if sum != "750.00" || cur != "RUB" {
		t.Fatalf("RUB order: %s %s, want 750.00 RUB", sum, cur)
	}

	// Тестовый режим: рубли не умножаются на курс, доллары — переводятся.
	s.Config.UnitPayTest = true
	s.Config.UnitPayTestRubRate = 85.5
	if _, err := db.Exec(`UPDATE orders SET currency = 'RUB', base_price = 750 WHERE id = 'ord_cur_usd'`); err != nil {
		t.Fatal(err)
	}
	o, _ = store.GetOrder(ctx, db, "ord_cur_usd")
	sum, cur = s.unitpayAmount(ctx, o)
	if sum != "750.00" || cur != "RUB" {
		t.Fatalf("test RUB: %s %s, want 750.00 RUB", sum, cur)
	}
}
