package httpserver

import (
	"context"
	"database/sql"
	"encoding/csv"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/store"
)

func configForCSV() config.Config {
	return config.Config{PublicAppURL: "https://mezmo-song.com"}
}

// Выгрузка заказов: тип оплаты, провайдер и полный URL захода — те поля, ради
// которых она заведена (ТЗ п. 5); старая метка без домена дополняется им.
func TestAdminOrdersCSVHasPaymentKindProviderAndFullURL(t *testing.T) {
	s, id := newUnitPayTestServer(t, configForCSV())
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, `UPDATE orders SET attribution = ?, payment_status = 'paid' WHERE id = ?`,
		`{"landingUrl":"/?arm=variant3-990&utm_source=fb","utm_source":"fb"}`, id); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPaymentProvider(ctx, s.DB, id, ProviderUnitPay); err != nil {
		t.Fatal(err)
	}
	internal := store.Order{
		ID: "ord_int", Status: "created", DeliveryStatus: store.DeliveryPending, PaymentStatus: store.PaymentPaid,
		InputPayloadJSON: `{}`, CustomerJSON: `{}`, DiscountCode: sql.NullString{String: "INTERNAL", Valid: true}, CreatedAt: time.Now(),
	}
	if err := store.CreateOrder(ctx, s.DB, internal); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleAdminOrdersCSV(rec, httptest.NewRequest("GET", "/api/admin/orders.csv", nil))
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("строк = %d, ожидалось шапка + 2 заказа", len(rows))
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	byID := map[string][]string{}
	for _, r := range rows[1:] {
		byID[r[col["order_id"]]] = r
	}
	paid := byID[id]
	if paid[col["payment_kind"]] != PaymentKindOneTime || paid[col["payment_provider"]] != ProviderUnitPay || paid[col["currency"]] != "USD" {
		t.Fatalf("оплаченный заказ: kind=%q provider=%q currency=%q", paid[col["payment_kind"]], paid[col["payment_provider"]], paid[col["currency"]])
	}
	if got := paid[col["landing_url"]]; got != "https://mezmo-song.com/?arm=variant3-990&utm_source=fb" {
		t.Fatalf("landing_url = %q", got)
	}
	if byID["ord_int"][col["payment_kind"]] != PaymentKindInternal {
		t.Fatalf("тестовый заказ: kind=%q", byID["ord_int"][col["payment_kind"]])
	}
}
