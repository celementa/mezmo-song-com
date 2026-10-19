import { useEffect, useState } from 'react'

import { apiGet } from '@/lib/http'
import { getSessionId } from '@/lib/session'

/**
 * Конфигурация плеча A/B-теста флоу, полученная с сервера.
 *
 * Плечо считает сервер по id сессии, а не фронт: цена приходит вместе с плечом
 * и в тело заказа не подставляется (сервер всё равно возьмёт свою). Состав
 * шагов тоже серверный — плечи варианта 2 отличаются только порядком, и фронт
 * не должен знать, какие плечи существуют.
 */
export type FlowConfig = {
  flowVariant: string
  /**
   * Имя плеча для адресной строки и отчётов — то же, что в рекламных ссылках
   * (`variant2-750s-990`). Технический id остаётся в `flowVariant`: он лежит в
   * БД за всю историю, но в адресе и в аналитике его быть не должно — реклама
   * не узнаёт свои кампании в `?arm=v1_price`.
   */
  armName?: string
  label: string
  // Путь на фронте, где живёт плечо. Пусто — рендерит FlowRoute на месте;
  // задано (плечи варианта 2) — FlowEntry уводит посетителя туда.
  route?: string
  // Предвыбор на экране «разовая vs подписка» ценовых плеч.
  compareDefault?: 'subscription' | 'onetime' | ''
  steps: string[]
  price: number
  originalPrice: number
  speedupPrice: number
  stemsPrice: number
  deliveryDelayHours: number
  subscriptionPrice: number
  previewReady: boolean
  previewReason: string
}

export type FlowState =
  | { status: 'loading' }
  | { status: 'ready'; config: FlowConfig; sessionId: string }
  | { status: 'unavailable' }

/**
 * useFlowConfig грузит конфигурацию плеча один раз за монтирование.
 *
 * Ошибка ручки — не повод показать пустую страницу: статус 'unavailable'
 * означает «работаем по прежнему флоу», и вызывающий рисует легаси-форму.
 * Так выкат нового флоу не может обрушить продажи целиком.
 */
export function useFlowConfig(options?: { trackAssignment?: boolean }): FlowState {
  const trackAssignment = options?.trackAssignment ?? true
  const [state, setState] = useState<FlowState>({ status: 'loading' })

  useEffect(() => {
    let alive = true
    const sessionId = getSessionId()
    // ?arm=<id> пробрасывается в ручку как принудительное плечо: сервер
    // закрепляет его за браузером кукой (см. pinForcedArm в flow.go), чтобы
    // рекламная ссылка с ценой доводила до заказа по этой же цене.
    const forcedArm = new URLSearchParams(window.location.search).get('arm')
    const armParam = forcedArm ? `&arm=${encodeURIComponent(forcedArm)}` : ''
    apiGet<FlowConfig>(`/api/public/flow?sid=${encodeURIComponent(sessionId)}${armParam}`)
      .then((config) => {
        if (!alive) return
        if (!config || !Array.isArray(config.steps) || config.steps.length === 0) {
          setState({ status: 'unavailable' })
          return
        }
        // Плечо в dataLayer один раз: дальше все события GTM могут разрезаться
        // по нему через переменную контейнера, без правок в каждом событии.
        // Страница заказа спрашивает конфиг ради цен апсейлов и события не
        // отбивает — иначе одно назначение плеча попало бы в аналитику дважды.
        if (trackAssignment) {
          const dataLayer = ((window as unknown as { dataLayer?: Record<string, unknown>[] }).dataLayer ??= [])
          dataLayer.push({
            event: 'flow_variant_assigned',
            flow_variant: config.flowVariant,
            // Имя из рекламной ссылки — по нему отчёт площадки сходится с GA и
            // Метрикой без сверки «id ↔ имя» в голове.
            flow_arm_name: config.armName || config.flowVariant,
          })
        }
        setState({ status: 'ready', config, sessionId })
      })
      .catch(() => {
        if (alive) setState({ status: 'unavailable' })
      })
    return () => {
      alive = false
    }
  }, [trackAssignment])

  return state
}
