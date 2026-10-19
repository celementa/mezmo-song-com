package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mezmo-song-com/internal/storage"
	"mezmo-song-com/internal/store"
)

// T-020: автовозврат дублирующей оплаты (вебхук payment.succeeded). Отдельный
// файл, чтобы не конфликтовать с refund_test.go из T-002; харнессы
// testServer/seedOrder и заглушку yooRoutedServer переиспользуем без изменений.

// refundCall — что реально ушло в POST /v3/refunds.
type refundCall struct {
	PaymentID string
	Amount    string
}

// refundStub принимает возвраты в calls и отвечает succeeded — как будто ЮKassa
// провела возврат сразу.
func refundStub(calls *[]refundCall) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PaymentID string `json:"payment_id"`
			Amount    struct {
				Value string `json:"value"`
			} `json:"amount"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*calls = append(*calls, refundCall{PaymentID: body.PaymentID, Amount: body.Amount.Value})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ref_" + body.PaymentID, "status": "succeeded"})
	}
}

// getFromMap — GET /v3/payments/{id} по карте id → тело ответа; карта передаётся
// по указателю, чтобы тест мог поменять состояние платежа между двумя вебхуками
// одного сценария (напр. до и после реального возврата).
func getFromMap(byID *map[string]map[string]any) func(w http.ResponseWriter, paymentID string) {
	return func(w http.ResponseWriter, paymentID string) {
		resp, ok := (*byID)[paymentID]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// waitForOrderStatus ждёт, пока заказ дойдёт до нужного status, вместо того
// чтобы проверять его сразу после postWebhook. Первая успешная оплата в
// handleYooWebhook запускает s.triggerRelay асинхронной горутиной
// (server.go: `go s.triggerRelay(...)`); без ключей OpenRouter/Kie.ai она
// сама, без участия теста, доводит заказ до status="completed" демо-фолбэком
// (store.CompleteWithDemoAudio). Момент её завершения не детерминирован
// относительно возврата из postWebhook, поэтому тест, которому нужно состояние
// ПОСЛЕ этой горутины (а не гонку с ней), обязан дождаться его явно, а не
// читать заказ немедленно.
func waitForOrderStatus(t *testing.T, ctx context.Context, db *sql.DB, id, want string) store.Order {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		o, err := store.GetOrder(ctx, db, id)
		if err != nil {
			t.Fatalf("waitForOrderStatus: get order %s: %v", id, err)
		}
		if o.Status == want {
			return o
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitForOrderStatus: order %s did not reach status %q in time, last status=%q", id, want, o.Status)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// История [p1, p2], оба живые успешные, оба заведены заказом ord_d1: вебхук по
// p2 должен вернуть ровно p2 (самый ранний, p1, остаётся).
func TestWebhookRefundsExtraSuccessfulPayment(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d1"),
		"p2": paymentJSON("p2", "succeeded", true, "990.00", "", "ord_d1"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d1", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d1", "p1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_d1", "p2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d1"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p2","metadata":{"order_id":"ord_d1"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 1 {
		t.Fatalf("refund calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].PaymentID != "p2" {
		t.Fatalf("refunded payment = %q, want p2 (самый ранний p1 остаётся)", calls[0].PaymentID)
	}
}

// Тот же расклад, но вебхук приходит по p1 (не по дублю) — решение всё равно
// оставляет самый ранний (p1) и возвращает p2: оно не зависит от того, по какому
// платежу пришёл вебхук.
func TestWebhookKeepsEarliestPaymentRegardlessOfWebhookOrder(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d2"),
		"p2": paymentJSON("p2", "succeeded", true, "990.00", "", "ord_d2"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d2", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d2", "p1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_d2", "p2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d2"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d2"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 1 {
		t.Fatalf("refund calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].PaymentID != "p2" {
		t.Fatalf("refunded payment = %q, want p2 regardless of which payment the webhook was about", calls[0].PaymentID)
	}
}

// Один живой успешный платёж — дубля нет, возвратов не бывает.
func TestWebhookDoesNotRefundSinglePayment(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d3"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d3", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d3", "p1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d3"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d3"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0: %+v", len(calls), calls)
	}
}

// p2 уже полностью возвращён (refunded_amount == amount) — он не живой, дубля не
// нашлось, повторной доставкой вебхука второй возврат не заводится.
func TestWebhookDoesNotRefundAlreadyRefundedDuplicate(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d4"),
		"p2": paymentJSON("p2", "succeeded", true, "990.00", "990.00", "ord_d4"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d4", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d4", "p1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_d4", "p2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d4"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d4"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0 (p2 уже возвращён): %+v", len(calls), calls)
	}
}

// p2 возвращён частично (refunded_amount < amount) — он всё ещё живой, поэтому
// это дубль, и возвращается ОСТАТОК, а не полная сумма.
func TestWebhookRefundsRemainderOfPartiallyRefundedDuplicate(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d5"),
		"p2": paymentJSON("p2", "succeeded", true, "990.00", "200.00", "ord_d5"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d5", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d5", "p1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_d5", "p2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d5"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d5"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 1 {
		t.Fatalf("refund calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].PaymentID != "p2" {
		t.Fatalf("refunded payment = %q, want p2", calls[0].PaymentID)
	}
	if calls[0].Amount != "790.00" {
		t.Fatalf("refunded amount = %q, want 790.00 (остаток 990.00 - 200.00)", calls[0].Amount)
	}
}

// Платёж, которого нет в истории заказа: verifyYooPayment отдаёт
// payment_id_mismatch до того, как дело доходит до автовозврата.
func TestWebhookDoesNotRefundForeignPayment(t *testing.T) {
	byID := map[string]map[string]any{
		"p1":          paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d6"),
		"pay_unknown": paymentJSON("pay_unknown", "succeeded", true, "990.00", "", "ord_d6"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d6", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d6", "p1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"pay_unknown","metadata":{"order_id":"ord_d6"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0 (foreign payment must not trigger refund): %+v", len(calls), calls)
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_d6")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPending {
		t.Fatalf("payment_status = %q, want pending (unchanged)", o.PaymentStatus)
	}
}

// Заказ уже paid, тело вебхука утверждает payment.succeeded, но свежий GetPayment
// отдаёт pending: сверка с API главнее тела вебхука, ни возвратов, ни изменений.
func TestWebhookDuplicateStillVerifiesAgainstAPI(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "pending", false, "990.00", "", "ord_d7"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d7", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d7", "p1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d7"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d7"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0: %+v", len(calls), calls)
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_d7")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (unchanged)", o.PaymentStatus)
	}
	if o.RefundedKopecks != 0 {
		t.Fatalf("refunded_kopecks = %d, want 0", o.RefundedKopecks)
	}
}

// Регресс: на дубле не отправляется второй чек, не сбрасывается status и не
// перезапускается генерация. Первая доставка проходит полный цикл, вторая
// (тот же платёж, повтор вебхука) не должна ничего трогать повторно.
func TestWebhookDuplicateDoesNotResendReceiptOrRetrigger(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d8"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d8", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d8", "p1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	body := `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d8"}}}`
	if rec := postWebhook(s, body); rec.Code != 200 {
		t.Fatalf("first delivery: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	// Первая доставка запускает асинхронный s.triggerRelay; без ключей
	// OpenRouter/Kie.ai он сам доводит заказ до status="completed" демо-
	// фолбэком за неопределённое время. Дожидаемся его, чтобы дальше сравнивать
	// с устойчивым состоянием, а не гонять с этой горутиной.
	first := waitForOrderStatus(t, ctx, s.DB, "ord_d8", "completed")
	if !first.ServerTrackedAt.Valid {
		t.Fatalf("server_tracked_at not set after first delivery")
	}

	if rec := postWebhook(s, body); rec.Code != 200 {
		t.Fatalf("second delivery (retry): status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	second, err := store.GetOrder(ctx, s.DB, "ord_d8")
	if err != nil {
		t.Fatalf("get after second delivery: %v", err)
	}
	if second.Status != first.Status {
		t.Fatalf("status changed on retry: %q -> %q (must not reset/re-trigger generation)", first.Status, second.Status)
	}
	if second.ServerTrackedAt.String != first.ServerTrackedAt.String {
		t.Fatalf("server_tracked_at changed on retry: %v -> %v", first.ServerTrackedAt, second.ServerTrackedAt)
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0 (single payment, no dup)", len(calls))
	}
}

// Заказ уже refunded, приходит поздняя успешная оплата: MarkPaid переводил бы
// заказ в paid из любого нестатуса кроме paid — это надо перехватить, а не
// решать автоматически. Заказ остаётся refunded, возвратов не заводится.
func TestWebhookLateSuccessDoesNotResurrectRefundedOrder(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "990.00", "ord_d9"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d9", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d9", "p1", ""); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if err := store.MarkPaid(ctx, s.DB, "ord_d9"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if changed, err := store.MarkRefunded(ctx, s.DB, "ord_d9"); err != nil || !changed {
		t.Fatalf("mark refunded: changed=%v err=%v", changed, err)
	}

	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d9"}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0: %+v", len(calls), calls)
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_d9")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentRefunded {
		t.Fatalf("payment_status = %q, want %q (late success must not resurrect a refunded order)", o.PaymentStatus, store.PaymentRefunded)
	}
}

// Главный сквозной сценарий задачи: два платежа, вебхук по одному из них
// автоматически возвращает второй; когда по возвращённому платежу приходит
// refund.succeeded (обработчик T-002), заказ остаётся paid (жив первый платёж),
// refunded_kopecks фиксирует сумму, аудио остаётся доступным.
func TestAutoRefundKeepsOrderPaidAfterRefundWebhook(t *testing.T) {
	byID := map[string]map[string]any{
		"p1": paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d10"),
		"p2": paymentJSON("p2", "succeeded", true, "990.00", "", "ord_d10"),
	}
	var calls []refundCall
	s := yooRoutedServer(t, getFromMap(&byID), nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d10", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d10", "p1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_d10", "p2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}
	sc, err := storage.New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	s.Storage = sc

	// Первая доставка (по p1): заказ становится paid, обнаруживает второй живой
	// платёж (p2) и автоматически его возвращает.
	rec := postWebhook(s, `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d10"}}}`)
	if rec.Code != 200 {
		t.Fatalf("payment.succeeded p1: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(calls) != 1 || calls[0].PaymentID != "p2" {
		t.Fatalf("refund calls = %+v, want exactly one refund of p2", calls)
	}
	if calls[0].Amount != "990.00" {
		t.Fatalf("refunded amount = %q, want 990.00", calls[0].Amount)
	}

	// Та же первая доставка запускает асинхронный s.triggerRelay; без ключей
	// OpenRouter/Kie.ai он сам доводит заказ до status="completed" демо-
	// фолбэком (store.CompleteWithDemoAudio), перезаписывая audio_urls.
	// Дожидаемся его до конца и только потом сажаем контент теста — иначе
	// наша запись и запись из этой горутины гонятся за тем, кто последний
	// напишет в audio_urls (см. T-020-test-1.md, разбор причины флака).
	waitForOrderStatus(t, ctx, s.DB, "ord_d10", "completed")
	if err := store.SaveRelayResult(ctx, s.DB, "ord_d10", []string{"orders/ord_d10/track-1.mp3"}, "текст", "Песня"); err != nil {
		t.Fatalf("save relay result: %v", err)
	}

	// Возврат реально прошёл — GET p2 теперь отдаёт refunded_amount == amount.
	byID["p2"] = paymentJSON("p2", "succeeded", true, "990.00", "990.00", "ord_d10")

	// ЮKassa присылает refund.succeeded по p2 — обработчик T-002 пересчитывает
	// живые платежи, находит p1 живым и оставляет заказ paid.
	rec = postWebhook(s, `{"event":"refund.succeeded","object":{"id":"ref_1","payment_id":"p2","status":"succeeded","amount":{"value":"990.00","currency":"RUB"},"metadata":{"order_id":"ord_d10"}}}`)
	if rec.Code != 200 {
		t.Fatalf("refund.succeeded: status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(ctx, s.DB, "ord_d10")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid (p1 остаётся живым)", o.PaymentStatus)
	}
	if o.RefundedKopecks != 99000 {
		t.Fatalf("refunded_kopecks = %d, want 99000", o.RefundedKopecks)
	}

	req := httptest.NewRequest("GET", "/api/public/order/ord_d10/audio/0", nil)
	req.SetPathValue("id", "ord_d10")
	req.SetPathValue("index", "0")
	audioRec := httptest.NewRecorder()
	s.handleAudioProxy(audioRec, req)
	if audioRec.Code == http.StatusNotFound {
		t.Fatalf("audio proxy status = 404, want access to pass the payment gate (order must stay paid after auto-refund)")
	}
}

// Ошибка чтения истории платежей на первой (не дублирующей) оплате — ответ 200,
// работа уже сделана; на дубле — 500, единственная работа этого вебхука — возврат,
// имеет смысл ретраить.
func TestWebhookRefundScanErrorRetriesOnlyForDuplicate(t *testing.T) {
	get := func(w http.ResponseWriter, paymentID string) {
		switch paymentID {
		case "p1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paymentJSON("p1", "succeeded", true, "990.00", "", "ord_d11"))
		case "p2":
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}
	var calls []refundCall
	s := yooRoutedServer(t, get, nil, refundStub(&calls))
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_d11", "pending")
	if err := store.AddPaymentID(ctx, s.DB, "ord_d11", "p1", ""); err != nil {
		t.Fatalf("seed payment 1: %v", err)
	}
	if err := store.AddPaymentID(ctx, s.DB, "ord_d11", "p2", ""); err != nil {
		t.Fatalf("seed payment 2: %v", err)
	}

	body := `{"event":"payment.succeeded","object":{"id":"p1","metadata":{"order_id":"ord_d11"}}}`

	// Первая (не дублирующая) оплата: verify по p1 проходит, но сканирование
	// истории (внутри refundExtraPayments натыкается на p2) падает — работа
	// (MarkPaid и т.д.) уже сделана, поэтому ответ 200.
	rec := postWebhook(s, body)
	if rec.Code != 200 {
		t.Fatalf("first (not duplicate): status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	o, err := store.GetOrder(ctx, s.DB, "ord_d11")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid", o.PaymentStatus)
	}
	if len(calls) != 0 {
		t.Fatalf("refund calls = %d, want 0 (scan failed, nothing to compare)", len(calls))
	}

	// Повторная доставка того же вебхука (дубль): заказ уже paid, единственная
	// работа этого вызова — автовозврат, а сканирование истории снова падает —
	// ЮKassa должна повторить доставку, поэтому 500.
	rec = postWebhook(s, body)
	if rec.Code != 500 {
		t.Fatalf("duplicate delivery: status = %d, want 500 (retry), body=%s", rec.Code, rec.Body.String())
	}
}
