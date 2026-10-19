/** Общие типы воронки /create: состояние формы и контракт шага. */

import type { StepTracker } from './analytics'
import type { StepId } from './flowConfig'
import type { FlowVariant } from './variant'

export type FlowFormState = {
  relationship: string
  recipientName: string
  occasion: string
  emotion: string
  genre: string
  customStyle: string
  story: string
  email: string
  phone: string
}

export const EMPTY_FORM: FlowFormState = {
  relationship: '',
  recipientName: '',
  occasion: '',
  emotion: '',
  genre: '',
  customStyle: '',
  story: '',
  email: '',
  phone: '',
}

/** Состояние заказа на сервере, насколько его знает воронка. */
export type FlowOrderState = {
  /** Публичный ref заказа; пусто — заказ ещё не создан. */
  orderId: string
  paymentStatus: string | null
  /** Допники, уже оплаченные или выбранные до оплаты. */
  extras: string[]
  /** Цены допников с сервера — источник истины, не хардкод в вёрстке. */
  extraPrices: Record<string, number>
  /**
   * Сумма к оплате по мнению сервера: цена плеча минус скидка плюс допники.
   * null — заказа ещё нет. Экраны сводки и оплаты показывают именно её, а не
   * считают сами: иначе применённый промокод или цена плеча из админки на
   * витрине не видны, и показ расходится со списанием.
   */
  paymentAmount: number | null
  /** Применённый промокод (с сервера) — пусто, если скидки нет. */
  discountCode: string | null
  /**
   * Доставка придержана: песня готова, но обычный срок ещё не вышел. Именно
   * снятие этого ожидания продаёт допник «ускорение», поэтому финальный экран
   * в таком состоянии не предлагает скачивание — оно вернуло бы 404.
   */
  deliveryHeld: boolean
  /** Когда песня станет доступна (RFC3339) — если доставка придержана. */
  deliverAt: string | null
  /** Подписка уже оформлена с этого заказа — апсейл подписки не повторяется. */
  subscribed: boolean
}

export type StepProps = {
  variant: FlowVariant
  stepId: StepId
  stepIndex: number
  stepTotal: number
  form: FlowFormState
  setForm: (patch: Partial<FlowFormState>) => void
  order: FlowOrderState
  /** Цена песни с сервера (paymentAmount), рубли. */
  price: number
  originalPrice: number
  /** Переход вперёд; шаг сам решает, когда он завершён. */
  onNext: () => void
  onBack: () => void
  canGoBack: boolean
  track: StepTracker
  /** Ошибка последнего серверного действия — рисуется шагом. */
  error: string | null
  busy: boolean
}

/**
 * Пропсы оболочки шага, общие для всех экранов, — чтобы каждый шаг не
 * перечислял их заново и не разъезжался с остальными.
 */
export function shellProps(p: StepProps) {
  return {
    stepIndex: p.stepIndex,
    stepTotal: p.stepTotal,
    onBack: p.onBack,
    canGoBack: p.canGoBack,
    busy: p.busy,
    error: p.error,
  }
}
