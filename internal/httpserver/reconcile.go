package httpserver

import (
	"context"
	"log"
	"time"

	"mezmo-song-com/internal/store"
)

// Фоновая сверка платежей с ЮKassa.
//
// Единственный путь заказа в paid — HTTP-уведомление ЮKassa (payment.succeeded).
// Если оно не дошло (контейнер перезапускался деплоем в этот момент, ЮKassa
// исчерпала ретраи, сеть), деньги в кабинете ЮKassa есть, а заказ навсегда
// висит pending: без песни, без письма, без конверсий. Сверка раз в
// reconcileInterval опрашивает API ЮKassa по всем заказам и подпискам, у которых
// есть заведённый, но не подтверждённый платёж, и проводит оплату тем же кодом,
// что и вебхук (settleSucceededPayment / settleSubscriptionPayment) — с полной
// сверкой суммы, заказа и id платежа. Дедуп там же, поэтому вебхук, пришедший
// параллельно, дубля не даст.
const (
	reconcileInterval = 5 * time.Minute
	// Свежему платежу дают время дойти штатным вебхуком — иначе сверка будет
	// гонять API на каждый заказ, который ещё оплачивают.
	reconcileMinAge = 3 * time.Minute
	// Токен виджета живёт час, платёж без оплаты ЮKassa отменяет сама; неделя —
	// запас на ретраи и на ручные разборы.
	reconcileMaxAge = 7 * 24 * time.Hour
	reconcileBatch  = 200
)

// StartPaymentReconciliation запускает цикл сверки. Без ключей ЮKassa (мок-режим
// стенда) сверять нечего — цикл не стартует.
func (s *Server) StartPaymentReconciliation(ctx context.Context) {
	if !s.paymentsConfigured() {
		log.Printf("[reconcile] disabled: no payment provider credentials")
		return
	}
	go func() {
		// Первый прогон — сразу после старта: если контейнер перезапускался,
		// именно в это окно вебхуки и терялись.
		s.reconcilePayments(ctx, time.Now())
		t := time.NewTicker(reconcileInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.reconcilePayments(ctx, now)
			}
		}
	}()
}

// reconcilePayments — один прогон сверки. Возвращает число зачтённых оплат
// (для тестов и логов).
func (s *Server) reconcilePayments(ctx context.Context, now time.Time) int {
	if s.unitpayConfigured() {
		settled := s.reconcileUnitPayOrders(ctx, now)
		if settled > 0 {
			log.Printf("[reconcile] settled %d payment(s) missed by webhook", settled)
		}
		return settled
	}
	settled := s.reconcileOrders(ctx, now)
	settled += s.reconcileSubscriptions(ctx)
	if settled > 0 {
		log.Printf("[reconcile] settled %d payment(s) missed by webhook", settled)
	}
	return settled
}

func (s *Server) reconcileOrders(ctx context.Context, now time.Time) int {
	orders, err := store.ListPendingOrdersWithPayments(ctx, s.DB, now.Add(-reconcileMaxAge), now.Add(-reconcileMinAge), reconcileBatch)
	if err != nil {
		log.Printf("[reconcile] list pending orders: %v", err)
		return 0
	}
	settled := 0
	for _, o := range orders {
		if ctx.Err() != nil {
			return settled
		}
		ids := paymentHistoryIDs(o, maxPaymentHistory)
		allCanceled := len(ids) > 0
		for _, id := range ids {
			p, err := s.yoo().GetPayment(id)
			if err != nil {
				log.Printf("[reconcile] order=%s payment=%s: get err=%v", o.ID, id, err)
				allCanceled = false
				continue
			}
			if p.Status == "succeeded" && p.Paid {
				log.Printf("[reconcile] order=%s payment=%s: succeeded in YooKassa but order still pending — settling", o.ID, id)
				if s.settleSucceededPayment(ctx, o.ID, id, "reconcile") == settleOK {
					settled++
				}
				allCanceled = false
				break
			}
			if p.Status != "canceled" {
				allCanceled = false
			}
		}
		// Все заведённые платежи ЮKassa отменила (истёк час виджета, отказ банка),
		// а payment.canceled тоже не дошёл: закрываем заказ, как сделал бы вебхук,
		// иначе сверка будет опрашивать его неделю.
		if allCanceled {
			if changed, err := store.MarkCanceled(ctx, s.DB, o.ID); err != nil {
				log.Printf("[reconcile] order=%s MarkCanceled err=%v", o.ID, err)
			} else if changed {
				log.Printf("[reconcile] order=%s: all %d payment(s) canceled in YooKassa — order canceled", o.ID, len(ids))
			}
		}
	}
	return settled
}

func (s *Server) reconcileSubscriptions(ctx context.Context) int {
	subs, err := store.ListSubscriptionsAwaitingPayment(ctx, s.DB, reconcileBatch)
	if err != nil {
		log.Printf("[reconcile] list subscriptions awaiting payment: %v", err)
		return 0
	}
	settled := 0
	for _, sub := range subs {
		if ctx.Err() != nil {
			return settled
		}
		id := sub.PendingPaymentID.String
		p, err := s.yoo().GetPayment(id)
		if err != nil {
			log.Printf("[reconcile] sub=%s payment=%s: get err=%v", sub.ID, id, err)
			continue
		}
		switch {
		case p.Status == "succeeded" && p.Paid:
			log.Printf("[reconcile] sub=%s payment=%s: succeeded in YooKassa but not recorded — settling", sub.ID, id)
			if s.settleSubscriptionPayment(ctx, sub.ID, id, "reconcile") == settleOK {
				settled++
			}
		case p.Status == "canceled":
			// Платёж не состоялся: ждать больше нечего. Исход для подписки решает
			// её планировщик (pending так и не активируется, ребилл уйдёт в ретрай).
			if err := store.SetSubscriptionPendingPayment(ctx, s.DB, sub.ID, ""); err != nil {
				log.Printf("[reconcile] sub=%s payment=%s: clear pending err=%v", sub.ID, id, err)
			} else {
				log.Printf("[reconcile] sub=%s payment=%s: canceled in YooKassa — no longer awaited", sub.ID, id)
			}
		}
	}
	return settled
}
