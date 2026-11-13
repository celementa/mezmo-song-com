// Вход в подписочную воронку из кабинета: новый заказ по подписке.
//
// Страница — тонкая обёртка над FlowEntry: анкету, шаги и фон рисует FlowRoute,
// а режим `subscriberMode` отключает рекламное плечо A/B-теста и редирект на
// движок /create — песня создаётся без оплаты (см. T-007).
import { FlowEntry } from '@/routes/FlowEntry'

export function AccountNewRoute() {
  return <FlowEntry subscriberMode />
}
