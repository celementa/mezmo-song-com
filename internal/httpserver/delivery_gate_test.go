package httpserver

import (
	"database/sql"
	"testing"
	"time"

	"mezmo-song-com/internal/store"
)

// Письмо «Ваша песня готова» уходит только оплаченному заказу: в воронках с
// генерацией ДО оплаты песня готова раньше денег, и письмо неоплаченному вело
// на страницу без песни и без кнопки оплаты.
func TestReadyForDelivery(t *testing.T) {
	now := time.Now()
	held := sql.NullString{Valid: true, String: now.Add(time.Hour).UTC().Format(time.RFC3339)}
	expired := sql.NullString{Valid: true, String: now.Add(-time.Hour).UTC().Format(time.RFC3339)}

	cases := []struct {
		name string
		o    store.Order
		want bool
	}{
		{"generated before payment, unpaid", store.Order{PaymentStatus: store.PaymentPending, Status: "completed", DeliveryStatus: store.DeliveryReady}, false},
		{"paid, song not ready yet", store.Order{PaymentStatus: store.PaymentPaid, Status: "processing", DeliveryStatus: store.DeliveryPending}, false},
		{"paid and ready", store.Order{PaymentStatus: store.PaymentPaid, Status: "completed", DeliveryStatus: store.DeliveryReady}, true},
		{"paid, ready, held (speedup not bought)", store.Order{PaymentStatus: store.PaymentPaid, Status: "completed", DeliveryStatus: store.DeliveryReady, DeliverAfter: held}, false},
		{"paid, ready, hold expired", store.Order{PaymentStatus: store.PaymentPaid, Status: "completed", DeliveryStatus: store.DeliveryReady, DeliverAfter: expired}, true},
		{"already delivered", store.Order{PaymentStatus: store.PaymentPaid, Status: "completed", DeliveryStatus: store.DeliveryDone}, false},
		{"refunded", store.Order{PaymentStatus: store.PaymentRefunded, Status: "completed", DeliveryStatus: store.DeliveryReady}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := readyForDelivery(tc.o, now)
			if got != tc.want {
				t.Fatalf("readyForDelivery = %v (%q), want %v", got, reason, tc.want)
			}
		})
	}
}
