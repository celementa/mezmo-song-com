package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Оплата НЕ должна возвращать заказ с готовой песней в начало производства.
//
// Раньше MarkPaid сбрасывал status в 'created', и вебхук оплаты успешно вызывал
// ConfirmOrder → генерация запускалась второй раз: двойной расход OpenRouter и
// Kie, а трек перезаписывался по тому же ключу, то есть покупатель получал не ту
// песню, которую слушал перед оплатой.
func TestMarkPaidDoesNotRestartFinishedOrder(t *testing.T) {
	ctx, db := testDB(t)

	for _, status := range []string{"processing", "completed", "failed"} {
		id := "ord_paid_" + status
		newUnpaidOrder(t, ctx, db, id)
		if _, err := db.ExecContext(ctx, `UPDATE orders SET status = ? WHERE id = ?`, status, id); err != nil {
			t.Fatalf("set status: %v", err)
		}
		if err := MarkPaid(ctx, db, id); err != nil {
			t.Fatalf("MarkPaid: %v", err)
		}
		o, err := GetOrder(ctx, db, id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if o.PaymentStatus != PaymentPaid {
			t.Fatalf("payment_status = %q, ожидалось paid", o.PaymentStatus)
		}
		if o.Status != status {
			t.Fatalf("status после оплаты = %q, ожидалось %q (оплата не двигает производство)", o.Status, status)
		}
		// Главное следствие: ConfirmOrder после оплаты не должен пройти —
		// именно он перезапускал генерацию.
		if transitioned, err := ConfirmOrder(ctx, db, id); err != nil || transitioned {
			t.Fatalf("ConfirmOrder после оплаты: transitioned=%v err=%v — генерация перезапустилась бы", transitioned, err)
		}
	}
}

// Заказ, ещё не начинавший генерацию, оплата оставляет готовым к запуску.
func TestMarkPaidKeepsCreatedOrderConfirmable(t *testing.T) {
	ctx, db := testDB(t)
	newUnpaidOrder(t, ctx, db, "ord_fresh")

	if err := MarkPaid(ctx, db, "ord_fresh"); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	transitioned, err := ConfirmOrder(ctx, db, "ord_fresh")
	if err != nil || !transitioned {
		t.Fatalf("ConfirmOrder: transitioned=%v err=%v — обычный флоу «оплатил → генерируем» сломан", transitioned, err)
	}
}

// Потерянный колбэк Kie.ai не должен оставлять НЕоплаченный заказ в processing
// навсегда: в воронках с генерацией до оплаты покупатель висел бы на экране
// «Пишем вашу песню…» и до оплаты не доходил вовсе.
func TestDemoFallbackWorksForUnpaidOrder(t *testing.T) {
	t.Setenv("DEMO_COMPLETE_AFTER_SECONDS", "1")
	ctx, db := testDB(t)
	newUnpaidOrder(t, ctx, db, "ord_unpaid")

	if _, err := ConfirmOrder(ctx, db, "ord_unpaid"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// Окно ещё не вышло — заказ трогать рано.
	if err := MaybeDemoComplete(ctx, db, "ord_unpaid"); err != nil {
		t.Fatalf("MaybeDemoComplete: %v", err)
	}
	if o, _ := GetOrder(ctx, db, "ord_unpaid"); o.Status != "processing" {
		t.Fatalf("status = %q, ожидалось processing (окно не вышло)", o.Status)
	}

	// Отматываем момент старта генерации назад — окно вышло.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET processing_started_at = ? WHERE id = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), "ord_unpaid"); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if err := MaybeDemoComplete(ctx, db, "ord_unpaid"); err != nil {
		t.Fatalf("MaybeDemoComplete: %v", err)
	}
	o, _ := GetOrder(ctx, db, "ord_unpaid")
	if o.Status != "completed" {
		t.Fatalf("status = %q, ожидалось completed: неоплаченный заказ остался бы висеть навсегда", o.Status)
	}
	if !IsFallbackAudio(o.AudioURLsJSON) {
		t.Fatalf("audio_urls = %q, ожидался демо-трек", o.AudioURLsJSON)
	}
	if o.PaymentStatus != PaymentPending {
		t.Fatalf("payment_status = %q — фолбэк не должен делать заказ оплаченным", o.PaymentStatus)
	}
}

// Окно фолбэка отсчитывается от старта генерации, а не от создания заказа.
//
// Заказ теперь создаётся раньше генерации (в варианте 1 — на шаге email), и
// покупатель может заполнять бриф сколько угодно. При отсчёте от created_at
// такой заказ получал бы демо-трек через секунды после нажатия «создать».
func TestDemoFallbackWindowCountsFromGenerationStart(t *testing.T) {
	t.Setenv("DEMO_COMPLETE_AFTER_SECONDS", "60")
	ctx, db := testDB(t)
	newUnpaidOrder(t, ctx, db, "ord_slow")
	// Заказ создан час назад, а генерация стартовала только что.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET created_at = ? WHERE id = ?`,
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), "ord_slow"); err != nil {
		t.Fatalf("age: %v", err)
	}
	if _, err := ConfirmOrder(ctx, db, "ord_slow"); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	if err := MaybeDemoComplete(ctx, db, "ord_slow"); err != nil {
		t.Fatalf("MaybeDemoComplete: %v", err)
	}
	if o, _ := GetOrder(ctx, db, "ord_slow"); o.Status != "processing" {
		t.Fatalf("status = %q: старый заказ со свежей генерацией подменили демо-треком", o.Status)
	}
}

// newUnpaidOrder — заказ в состоянии «создан, не оплачен»: так выглядят заказы
// воронок с генерацией до оплаты.
func newUnpaidOrder(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	o := Order{
		ID:               id,
		Status:           "created",
		DeliveryStatus:   DeliveryPending,
		PaymentStatus:    PaymentPending,
		InputPayloadJSON: `{"recipientName":"Ира"}`,
		CustomerJSON:     `{"email":"a@b.ru"}`,
		BasePrice:        990,
		CreatedAt:        time.Now(),
	}
	if err := CreateOrder(ctx, db, o); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// Демо-трек должен быть проигрываемым: прежняя внешняя ссылка отдаёт 404, и
// страховка выдавала клиенту молчащий плеер. Теперь файл раздаёт сам сервер.
func TestDemoAudioIsServedByUs(t *testing.T) {
	if strings.HasPrefix(DemoAudioURL, "http") {
		t.Fatalf("DemoAudioURL = %q — снова внешний хостинг, который мы не контролируем", DemoAudioURL)
	}
	if !strings.HasPrefix(DemoAudioURL, "/") {
		t.Fatalf("DemoAudioURL = %q — путь должен разрешаться от origin страницы заказа", DemoAudioURL)
	}
}

// Заказы, закрытые фолбэком до смены ссылки, обязаны по-прежнему опознаваться
// как демо — иначе колонка «Демо» в админке про них забудет.
func TestIsFallbackAudioRecognizesLegacyDemoURL(t *testing.T) {
	if !IsFallbackAudio(`["` + legacyDemoAudioURL + `"]`) {
		t.Fatal("старый демо-URL перестал опознаваться как фолбэк")
	}
	if !IsFallbackAudio(`["` + DemoAudioURL + `"]`) {
		t.Fatal("новый демо-URL не опознаётся как фолбэк")
	}
	if IsFallbackAudio(`["orders/ord_1/r0/track-1.mp3"]`) {
		t.Fatal("настоящая песня опознана как демо")
	}
}
