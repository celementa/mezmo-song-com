package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mezmo-song-com/internal/insales"
	"mezmo-song-com/internal/store"
)

// fakeInsales — API InSales в памяти: заказы по внутреннему id.
type fakeInsales struct {
	mu     sync.Mutex
	orders map[int64]insales.Order
}

func (f *fakeInsales) set(o insales.Order) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders[o.ID] = o
}

func newFakeInsales(t *testing.T) (*fakeInsales, *httptest.Server) {
	f := &fakeInsales{orders: map[int64]insales.Order{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "key" || p != "pass" {
			w.WriteHeader(401)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/admin/orders.json" {
			out := []insales.Order{}
			if r.URL.Query().Get("page") == "1" {
				for _, o := range f.orders {
					out = append(out, o)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		idStr := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/orders/"), ".json")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		o, ok := f.orders[id]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(o)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func lesrosesServer(t *testing.T) (*Server, *sql.DB, *fakeInsales) {
	t.Helper()
	s, db := testServer(t)
	f, srv := newFakeInsales(t)
	s.Config.InsalesShopURL = srv.URL
	s.Config.InsalesAPIID = "key"
	s.Config.InsalesAPIPassword = "pass"
	s.Config.LesrosesWebhookToken = "tok"
	s.Config.SMTPHost = "smtp.test"
	s.Config.WebhookSecret = "secret"
	s.Config.PublicAppURL = "https://mezmo.test"
	s.Config.LesrosesOrigins = []string{"https://lesrosesdechantal.ru"}
	stubMail(t, nil)
	return s, db, f
}

func songOrder(id, number int64, financial, fulfillment, sku string) insales.Order {
	return insales.Order{
		ID: id, Number: number, FinancialStatus: financial, FulfillmentStatus: fulfillment,
		Client: insales.OrderClient{Email: "Buyer@Mail.ru"},
		OrderLines: []insales.OrderLine{
			{VariantID: 1, Title: "Букет", Quantity: 1, SalePrice: 8601},
			{VariantID: 2, SKU: &sku, Title: "Персональная песня", Quantity: 1, SalePrice: 990},
		},
	}
}

func postLesrosesWebhook(t *testing.T, s *Server, token string, id int64) (int, string) {
	t.Helper()
	mux := http.NewServeMux()
	s.registerLesroses(mux)
	req := httptest.NewRequest("POST", "/api/partners/lesroses/webhook?token="+token,
		strings.NewReader(`{"id":`+strconv.FormatInt(id, 10)+`,"financial_status":"paid"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out["outcome"]
}

func lesrosesOrderCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE flow = 'lesroses'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitEmails ждёт, пока в журнале появится n писем вида kind (письмо-ссылка уходит в горутине).
func waitEmails(t *testing.T, db *sql.DB, kind string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var got int
		_ = db.QueryRow(`SELECT COUNT(*) FROM email_log WHERE kind = ? AND status = 'sent'`, kind).Scan(&got)
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("email_log %s: want %d, got %d", kind, n, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLesrosesWebhookCreatesPaidOrderOnce(t *testing.T) {
	s, db, f := lesrosesServer(t)

	if code, _ := postLesrosesWebhook(t, s, "wrong", 100); code != 401 {
		t.Fatalf("bad token: want 401, got %d", code)
	}

	// Не оплачен — заказа нет.
	f.set(songOrder(100, 4600, "pending", "new", "MEZMO-POPUP"))
	if code, outcome := postLesrosesWebhook(t, s, "tok", 100); code != 200 || outcome != lrUnpaid {
		t.Fatalf("unpaid: got %d %s", code, outcome)
	}
	// Заказ без песни — заказа нет.
	f.set(insales.Order{ID: 101, Number: 4601, FinancialStatus: "paid", OrderLines: []insales.OrderLine{{Title: "Букет"}}})
	if _, outcome := postLesrosesWebhook(t, s, "tok", 101); outcome != lrNoSong {
		t.Fatalf("no song: got %s", outcome)
	}
	if n := lesrosesOrderCount(t, db); n != 0 {
		t.Fatalf("orders before payment: %d", n)
	}

	// Оплачен: создаётся один заказ, повтор вебхука и сверка — не дублируют.
	f.set(songOrder(100, 4600, "paid", "accepted", "mezmo-popup"))
	if _, outcome := postLesrosesWebhook(t, s, "tok", 100); outcome != lrCreated {
		t.Fatalf("paid: got %s", outcome)
	}
	if _, outcome := postLesrosesWebhook(t, s, "tok", 100); outcome != lrExists {
		t.Fatalf("repeat: got %s", outcome)
	}
	s.reconcileLesroses(context.Background(), time.Now())
	if n := lesrosesOrderCount(t, db); n != 1 {
		t.Fatalf("want 1 order, got %d", n)
	}
	waitEmails(t, db, emailLesrosesLink, 1)

	id, err := store.PartnerOrderIDByNumber(context.Background(), db, store.FlowLesroses, "4600")
	if err != nil {
		t.Fatal(err)
	}
	o, _ := store.GetOrder(context.Background(), db, id)
	if o.PaymentStatus != store.PaymentPaid || o.Status != "created" || o.BasePrice != 990 ||
		o.FlowVariant.String != store.ArmLesrosesPopup || hasSongConfig(o) {
		t.Fatalf("order state: %+v", o)
	}
	// Конверсии в рекламные системы по партнёрскому заказу не уходят никогда.
	if !o.PurchaseTrackedAt.Valid || !o.ServerTrackedAt.Valid {
		t.Fatal("purchase/server tracking must be pre-marked for partner orders")
	}
	if email := extractCustomerEmail(id, db); email != "buyer@mail.ru" {
		t.Fatalf("email: %q", email)
	}
}

func TestLesrosesReconcilePicksUpMissedWebhook(t *testing.T) {
	s, db, f := lesrosesServer(t)
	f.set(songOrder(200, 4700, "paid", "approved", "MEZMO-CART"))
	f.set(insales.Order{ID: 201, Number: 4701, FinancialStatus: "paid"})
	s.reconcileLesroses(context.Background(), time.Now())
	if n := lesrosesOrderCount(t, db); n != 1 {
		t.Fatalf("reconcile: want 1 order, got %d", n)
	}
	waitEmails(t, db, emailLesrosesLink, 1)
}

func TestLesrosesCancelClosesOnlyBeforeBrief(t *testing.T) {
	s, db, f := lesrosesServer(t)
	ctx := context.Background()

	// Отмена до анкеты — доступ закрыт.
	f.set(songOrder(300, 4800, "paid", "accepted", "MEZMO-CART"))
	postLesrosesWebhook(t, s, "tok", 300)
	f.set(songOrder(300, 4800, "paid", "declined", "MEZMO-CART"))
	if _, outcome := postLesrosesWebhook(t, s, "tok", 300); outcome != lrCanceled {
		t.Fatalf("cancel before brief: %s", outcome)
	}
	id, _ := store.PartnerOrderIDByNumber(ctx, db, store.FlowLesroses, "4800")
	if o, _ := store.GetOrder(ctx, db, id); o.PaymentStatus != store.PaymentRefunded {
		t.Fatalf("want refunded, got %s", o.PaymentStatus)
	}

	// Отмена после анкеты — песню не отбираем, только отметка.
	f.set(songOrder(301, 4801, "paid", "accepted", "MEZMO-CART"))
	postLesrosesWebhook(t, s, "tok", 301)
	id2, _ := store.PartnerOrderIDByNumber(ctx, db, store.FlowLesroses, "4801")
	if _, err := db.Exec(`UPDATE orders SET status = 'processing', input_payload = '{"recipientName":"Ира"}' WHERE id = ?`, id2); err != nil {
		t.Fatal(err)
	}
	f.set(songOrder(301, 4801, "paid", "returned", "MEZMO-CART"))
	if _, outcome := postLesrosesWebhook(t, s, "tok", 301); outcome != lrCancelNoted {
		t.Fatalf("cancel after brief: %s", outcome)
	}
	if o, _ := store.GetOrder(ctx, db, id2); o.PaymentStatus != store.PaymentPaid {
		t.Fatalf("after brief must stay paid, got %s", o.PaymentStatus)
	}

	// Отменили раньше, чем заказ дошёл до нас — ничего не создаётся.
	f.set(songOrder(302, 4802, "paid", "declined", "MEZMO-CART"))
	if _, outcome := postLesrosesWebhook(t, s, "tok", 302); outcome != lrCancelNoOrder {
		t.Fatalf("cancel unknown: %s", outcome)
	}
}

func TestLesrosesStatusAsksForBriefAndConfigStartsGeneration(t *testing.T) {
	s, db, _ := lesrosesServer(t)
	ctx := context.Background()
	_, id, err := s.createLesrosesOrder(ctx, store.PartnerOrderRef{Number: "4900"}, "a@b.ru", store.ArmLesrosesManual, 990)
	if err != nil {
		t.Fatal(err)
	}
	ref := publicOrderRef(s.Config.WebhookSecret, id)
	mux := http.NewServeMux()
	s.Register(mux)

	status := func() map[string]any {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/public/order/"+ref+"/status", nil))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	st := status()
	if st["partner"] != "lesroses" || st["needsBrief"] != true {
		t.Fatalf("status before brief: partner=%v needsBrief=%v", st["partner"], st["needsBrief"])
	}

	// Страница заказа дёргает /confirm у оплаченного заказа в статусе created —
	// без анкеты это не должно запускать генерацию.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/orders/"+ref+"/confirm", nil))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "needs_config") {
		t.Fatalf("confirm before brief: %d %s", rec.Code, rec.Body.String())
	}
	if o, _ := store.GetOrder(ctx, db, id); o.Status != "created" {
		t.Fatalf("confirm before brief started generation: status=%s", o.Status)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/orders/"+ref+"/config",
		strings.NewReader(`{"recipientName":"Ира","relationship":"Жена","occasion":"Годовщина","story":"10 лет вместе"}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"generationStarted":true`) {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	if st := status(); st["needsBrief"] != false {
		t.Fatalf("needsBrief after config: %v", st["needsBrief"])
	}
	if o, _ := store.GetOrder(ctx, db, id); o.Status == "created" {
		t.Fatal("generation must start after brief")
	}
}

func TestLesrosesBriefReminders(t *testing.T) {
	s, db, _ := lesrosesServer(t)
	ctx := context.Background()
	_, id, _ := s.createLesrosesOrder(ctx, store.PartnerOrderRef{Number: "5000"}, "a@b.ru", store.ArmLesrosesCart, 990)
	waitEmails(t, db, emailLesrosesLink, 1)

	setAge := func(age time.Duration) {
		if _, err := db.Exec(`UPDATE orders SET created_at = ? WHERE id = ?`,
			time.Now().Add(-age).UTC().Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	s.sendBriefReminders(ctx, now)
	waitEmails(t, db, emailLesrosesReminder, 0)

	setAge(25 * time.Hour)
	s.sendBriefReminders(ctx, now)
	s.sendBriefReminders(ctx, now)
	waitEmails(t, db, emailLesrosesReminder, 1)

	setAge(73 * time.Hour)
	s.sendBriefReminders(ctx, now)
	s.sendBriefReminders(ctx, now)
	waitEmails(t, db, emailLesrosesReminder, 2)

	// Анкета заполнена — напоминаний больше нет (третьего и так не бывает).
	if _, err := db.Exec(`UPDATE orders SET brief_reminders = 0, input_payload = '{"recipientName":"Ира"}' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	s.sendBriefReminders(ctx, now)
	waitEmails(t, db, emailLesrosesReminder, 2)
}

func TestLesrosesManualOrderDedupesByNumber(t *testing.T) {
	s, db, f := lesrosesServer(t)
	mux := http.NewServeMux()
	s.registerLesroses(mux)
	post := func(body string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/admin/lesroses/orders", strings.NewReader(body)))
		return rec.Code
	}
	if code := post(`{"number":"abc","email":"a@b.ru"}`); code != 400 {
		t.Fatalf("bad number: %d", code)
	}
	if code := post(`{"number":"№5100","email":"a@b.ru"}`); code != 200 {
		t.Fatalf("manual: %d", code)
	}
	if code := post(`{"number":"5100","email":"a@b.ru"}`); code != 409 {
		t.Fatalf("manual dup: %d", code)
	}
	// Вебхук по тому же номеру позже — тоже не дубль.
	f.set(songOrder(510, 5100, "paid", "accepted", "MEZMO-CART"))
	if _, outcome := postLesrosesWebhook(t, s, "tok", 510); outcome != lrExists {
		t.Fatalf("webhook after manual: %s", outcome)
	}
	if n := lesrosesOrderCount(t, db); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
}

func TestLesrosesEventsAndFunnel(t *testing.T) {
	s, db, _ := lesrosesServer(t)
	ctx := context.Background()
	mux := http.NewServeMux()
	s.registerLesroses(mux)
	send := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/public/partners/lesroses/event", strings.NewReader(body))
		req.Header.Set("Origin", "https://lesrosesdechantal.ru")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := send(`{"sid":"sess-0001","arm":"popup","event":"shown"}`)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "https://lesrosesdechantal.ru" {
		t.Fatalf("event: %d acao=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	send(`{"sid":"sess-0001","arm":"popup","event":"shown"}`) // повтор той же сессии
	send(`{"sid":"sess-0002","arm":"popup","event":"shown"}`)
	send(`{"sid":"sess-0002","arm":"popup","event":"add"}`)
	send(`{"sid":"sess-0003","arm":"cart","event":"shown"}`)
	send(`{"sid":"sess-0004","arm":"evil","event":"shown"}`) // чужое плечо игнорируется

	_, _, _ = s.createLesrosesOrder(ctx, store.PartnerOrderRef{Number: "5200"}, "a@b.ru", store.ArmLesrosesPopup, 990)

	funnel, err := store.PartnerFunnel(ctx, db, store.FlowLesroses, time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]store.PartnerArmStats{}
	for _, a := range funnel {
		got[a.Arm] = a
	}
	if p := got["popup"]; p.Shown != 2 || p.Added != 1 || p.Paid != 1 || p.Revenue != 990 {
		t.Fatalf("popup funnel: %+v", p)
	}
	if c := got["cart"]; c.Shown != 1 || c.Paid != 0 {
		t.Fatalf("cart funnel: %+v", c)
	}
	if _, ok := got["evil"]; ok {
		t.Fatal("unknown arm recorded")
	}
}
