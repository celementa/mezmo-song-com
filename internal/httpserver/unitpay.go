// Оплата заказов через UnitPay (mezmo-song.com).
//
// Путь денег: handleInitiatePayment → initiateUnitPay (initPayment API, адрес
// формы) → покупатель платит на unitpay.money → UnitPay шлёт GET-уведомление
// check/pay/error на /api/webhooks/unitpay → settleUnitPayPayment → общий
// applyOrderPaid. Потерянное уведомление подбирает фоновая сверка
// (reconcileUnitPayOrders) по getPayment.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

// Провайдеры оплаты заказа (orders.payment_provider).
const (
	ProviderUnitPay  = "unitpay"
	ProviderYooKassa = "yookassa"
)

// paymentsConfigured — есть ли чем принимать деньги. Нет — mock-режим стенда:
// заказ создаётся сразу оплаченным.
func (s *Server) paymentsConfigured() bool {
	return s.unitpayConfigured() || s.yooConfigured()
}

func (s *Server) unitpayConfigured() bool {
	return s.Config.UnitPayProjectID != "" && s.Config.UnitPaySecretKey != ""
}

func (s *Server) unitpay() payment.UnitPay {
	return payment.UnitPay{
		ProjectID: s.Config.UnitPayProjectID,
		SecretKey: s.Config.UnitPaySecretKey,
		Test:      s.Config.UnitPayTest,
		Login:     s.Config.UnitPayLogin,
	}
}

// siteCurrency — валюта цен сайта: в ней считаются заказы и уходят конверсии.
func (s *Server) siteCurrency() string {
	if s.unitpayConfigured() {
		return strings.ToUpper(s.Config.UnitPayCurrency)
	}
	return "RUB"
}

// unitpayAmount — сумма и валюта платежа по заказу. Цены сайта целые, в валюте
// UNITPAY_CURRENCY. Тестовый режим UnitPay принимает только RUB, поэтому там
// сумма пересчитывается по UNITPAY_TEST_RUB_RATE — сверка уведомления идёт по
// этой же функции, и пересчёт не расходится с выставленным счётом.
func (s *Server) unitpayAmount(ctx context.Context, o store.Order) (sum, currency string) {
	amount := s.expectedPaymentRubles(ctx, o)
	if s.Config.UnitPayTest {
		return fmt.Sprintf("%d.00", int(math.Round(float64(amount)*s.Config.UnitPayTestRubRate))), "RUB"
	}
	return fmt.Sprintf("%d.00", amount), strings.ToUpper(s.Config.UnitPayCurrency)
}

// unitpayAmountMatches сравнивает сумму и валюту из UnitPay с ожидаемыми —
// в центах, без float.
func unitpayAmountMatches(gotSum, gotCurrency, wantSum, wantCurrency string) bool {
	if !strings.EqualFold(strings.TrimSpace(gotCurrency), wantCurrency) {
		return false
	}
	got, ok1 := rublesToKopecks(gotSum)
	want, ok2 := rublesToKopecks(wantSum)
	return ok1 && ok2 && got == want
}

// initiateUnitPay выставляет счёт и отдаёт адрес формы ({"confirmationUrl"} —
// тот же ответ, что у redirect-платежа ЮKassa, фронт их не различает).
//
// Повторный заход на оплату в течение confirmationTTL получает ТОТ ЖЕ счёт,
// если сумма не изменилась и он ещё не оплачен: две открытые формы — это две
// возможные оплаты одной песни, а автовозврата дублей для UnitPay нет.
func (s *Server) initiateUnitPay(w http.ResponseWriter, r *http.Request, o store.Order, returnURL string) {
	ctx := r.Context()
	up := s.unitpay()
	sum, currency := s.unitpayAmount(ctx, o)

	for _, pid := range paymentHistoryIDs(o, maxPaymentHistory) {
		p, err := up.GetPayment(pid)
		if err != nil {
			log.Printf("[pay] unitpay gate order=%s payment=%s get err=%v", o.ID, pid, err)
			continue
		}
		if p.Status == payment.UnitPayStatusSuccess {
			log.Printf("[pay] unitpay initiate order=%s REFUSED: платёж %s уже оплачен, ждём уведомление", o.ID, pid)
			writeJSON(w, 409, map[string]string{"error": "already_paid"})
			return
		}
		if c, ok := store.LoadConfirmation(o); ok && c.PaymentID == pid && c.URL != "" &&
			time.Since(c.IssuedAt) < confirmationTTL && p.Status == payment.UnitPayStatusWait &&
			unitpayAmountMatches(p.OrderSum, p.OrderCurrency, sum, currency) {
			log.Printf("[pay] unitpay initiate order=%s payment=%s reused (age=%s)", o.ID, pid, time.Since(c.IssuedAt).Round(time.Second))
			writeJSON(w, 200, map[string]any{"confirmationUrl": c.URL})
			return
		}
	}

	backURL := strings.TrimSuffix(s.Config.PublicAppURL, "/") + "/"
	res, err := up.InitPayment(payment.UnitPayInitRequest{
		Account:   o.ID,
		Sum:       sum,
		Currency:  currency,
		Desc:      "Personal song / Персональная песня",
		ResultURL: returnURL,
		BackURL:   backURL,
		Locale:    "ru",
		IP:        publicIPOrEmpty(clientIP(r)),
	})
	if err != nil {
		log.Printf("[pay] unitpay initiate order=%s initPayment err=%v", o.ID, err)
		writeJSON(w, 502, map[string]string{"error": "payment_gateway"})
		return
	}
	if err := store.AddPaymentID(ctx, s.DB, o.ID, res.PaymentID, res.RedirectURL); err != nil {
		log.Printf("[pay] unitpay initiate order=%s add payment id err=%v", o.ID, err)
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if err := store.SetPaymentProvider(ctx, s.DB, o.ID, ProviderUnitPay); err != nil {
		log.Printf("[pay] unitpay initiate order=%s set provider err=%v", o.ID, err)
	}
	if err := store.SaveConfirmation(ctx, s.DB, o.ID, store.IssuedConfirmation{
		PaymentID: res.PaymentID,
		Mode:      payment.ModeRedirect,
		URL:       res.RedirectURL,
		IssuedAt:  time.Now().UTC(),
	}); err != nil {
		log.Printf("[pay] unitpay initiate order=%s save confirmation err=%v", o.ID, err)
	}
	log.Printf("[pay] unitpay initiate order=%s payment=%s amount=%s %s test=%v created", o.ID, res.PaymentID, sum, currency, s.Config.UnitPayTest)
	writeJSON(w, 200, map[string]any{"confirmationUrl": res.RedirectURL})
}

func publicIPOrEmpty(ip string) string {
	if isPublicClientIP(ip) {
		return ip
	}
	return ""
}

// handleUnitPayWebhook — HTTP-уведомления UnitPay (GET, параметры params[...]).
// Ответ — JSON {"result":{"message"}} или {"error":{"message"}}: ошибка на check
// отклоняет платёж до списания, на pay — помечает его «Не завершен» (деньги при
// этом уже у нас, повтор доступен из кабинета UnitPay).
func (s *Server) handleUnitPayWebhook(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	method := q.Get("method")
	params := map[string]string{}
	for k, v := range q {
		if strings.HasPrefix(k, "params[") && strings.HasSuffix(k, "]") && len(v) > 0 {
			params[k[len("params["):len(k)-1]] = v[0]
		}
	}
	orderID := params["account"]
	paymentID := params["unitpayId"]
	ip := clientIP(r)
	log.Printf("[pay] unitpay webhook received: method=%s order=%s payment=%s sum=%s %s test=%s ip=%s",
		method, orderID, paymentID, params["orderSum"], params["orderCurrency"], params["test"], ip)

	reply := func(errMsg string) {
		if errMsg != "" {
			writeJSON(w, 200, map[string]any{"error": map[string]string{"message": errMsg}})
			return
		}
		writeJSON(w, 200, map[string]any{"result": map[string]string{"message": "Запрос успешно обработан"}})
	}

	if !s.unitpayConfigured() {
		log.Printf("[pay] unitpay webhook: UnitPay не настроен — отклонено")
		reply("Магазин не принимает оплату")
		return
	}
	if !s.Config.UnitPaySkipIPCheck && !slices.Contains(payment.UnitPayIPs, ip) {
		log.Printf("[pay] unitpay webhook REJECTED: адрес %s не из списка UnitPay", ip)
		reply("Forbidden")
		return
	}
	notifyKey := s.Config.UnitPayNotifySecretKey
	if notifyKey == "" {
		notifyKey = s.Config.UnitPaySecretKey
	}
	if !payment.UnitPayVerify(method, params, notifyKey) {
		log.Printf("[pay] unitpay webhook REJECTED: неверная подпись (method=%s order=%s)", method, orderID)
		reply("Неверная подпись запроса")
		return
	}

	ctx := r.Context()
	switch method {
	case "check":
		o, err := store.GetOrder(ctx, s.DB, orderID)
		if err != nil {
			reply("Заказ не найден")
			return
		}
		if o.PaymentStatus == store.PaymentPaid {
			reply("Заказ уже оплачен")
			return
		}
		sum, currency := s.unitpayAmount(ctx, o)
		if !unitpayAmountMatches(params["orderSum"], params["orderCurrency"], sum, currency) {
			log.Printf("[pay] unitpay check order=%s REJECTED: сумма %s %s, ожидалось %s %s",
				orderID, params["orderSum"], params["orderCurrency"], sum, currency)
			reply("Сумма заказа не совпадает")
			return
		}
		reply("")
	case "pay":
		switch s.settleUnitPayPayment(ctx, orderID, paymentID, params["orderSum"], params["orderCurrency"], "unitpay webhook") {
		case settleOK, settleConflict:
			reply("")
		case settleNotFound:
			reply("Заказ не найден")
		case settleRejected:
			reply("Платёж не прошёл сверку с заказом")
		default:
			reply("Временная ошибка, повторите позже")
		}
	case "error":
		log.Printf("[pay] unitpay error order=%s payment=%s: %s", orderID, paymentID, params["errorMessage"])
		reply("")
	default:
		// preauth и будущие методы: двухстадийных платежей мы не заводим.
		log.Printf("[pay] unitpay webhook order=%s: unhandled method=%s", orderID, method)
		reply("")
	}
}

// settleUnitPayPayment зачитывает оплату заказа. Сумма и валюта — из
// уведомления (его подлинность уже доказана подписью и IP) или из getPayment
// (фоновая сверка). Чужой для заказа id платежа не отклоняется: уведомление
// приходит и по платежу, выставленному формой UnitPay напрямую, а в тестовом
// режиме id после оплаты меняется. Такой id дописывается в историю заказа.
func (s *Server) settleUnitPayPayment(ctx context.Context, orderID, paymentID, orderSum, orderCurrency, src string) settleOutcome {
	prev, err := store.GetOrder(ctx, s.DB, orderID)
	if errors.Is(err, store.ErrNotFound) {
		log.Printf("[pay] %s order=%s: заказа нет", src, orderID)
		return settleNotFound
	}
	if err != nil {
		log.Printf("[pay] %s order=%s: get order err=%v", src, orderID, err)
		return settleRetry
	}
	sum, currency := s.unitpayAmount(ctx, prev)
	if !unitpayAmountMatches(orderSum, orderCurrency, sum, currency) {
		log.Printf("[pay] %s order=%s payment=%s REJECTED: сумма %s %s, ожидалось %s %s",
			src, orderID, paymentID, orderSum, orderCurrency, sum, currency)
		return settleRejected
	}
	known := store.KnownPaymentID(prev, paymentID)
	if prev.PaymentStatus == store.PaymentPaid && !known {
		// Вторая оплата той же песни: например, две открытые формы. Автовозврата
		// у UnitPay нет — возвращается вручную в кабинете.
		log.Printf("[pay] unitpay dup order=%s payment=%s: ВТОРАЯ ОПЛАТА уже оплаченного заказа — вернуть вручную в кабинете UnitPay", orderID, paymentID)
		return settleOK
	}
	if !known && prev.PaymentStatus == store.PaymentPending && paymentID != "" {
		if err := store.AddPaymentID(ctx, s.DB, orderID, paymentID, ""); err != nil {
			log.Printf("[pay] %s order=%s add payment id err=%v", src, orderID, err)
		}
	}
	log.Printf("[pay] %s order=%s payment=%s: verified (amount=%s %s)", src, orderID, paymentID, orderSum, orderCurrency)
	return s.applyOrderPaid(ctx, prev, orderID, paymentID, src)
}

// reconcileUnitPayOrders — фоновая сверка для UnitPay: pending-заказы со
// выставленным счётом перечитываются через getPayment, успешные зачитываются
// тем же кодом, что уведомление.
func (s *Server) reconcileUnitPayOrders(ctx context.Context, now time.Time) int {
	orders, err := store.ListPendingOrdersWithPayments(ctx, s.DB, now.Add(-reconcileMaxAge), now.Add(-reconcileMinAge), reconcileBatch)
	if err != nil {
		log.Printf("[reconcile] list pending orders: %v", err)
		return 0
	}
	up := s.unitpay()
	settled := 0
	for _, o := range orders {
		if ctx.Err() != nil {
			return settled
		}
		for _, id := range paymentHistoryIDs(o, maxPaymentHistory) {
			p, err := up.GetPayment(id)
			if err != nil {
				log.Printf("[reconcile] order=%s payment=%s: get err=%v", o.ID, id, err)
				continue
			}
			if p.Status != payment.UnitPayStatusSuccess || p.Account != o.ID {
				continue
			}
			log.Printf("[reconcile] order=%s payment=%s: оплачен в UnitPay, а заказ pending — зачитываем", o.ID, id)
			if s.settleUnitPayPayment(ctx, o.ID, id, p.OrderSum, p.OrderCurrency, "reconcile") == settleOK {
				settled++
			}
			break
		}
	}
	return settled
}
