import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, Loader2, PartyPopper, Sparkles } from 'lucide-react'

import { apiPost } from '@/lib/http'
import { CREAM, GOLD } from '@/routes/create/StepShell'
import { useFlowConfig } from './useFlowConfig'
import { markUpsellSeen } from './upsellSeen'
import { fmtCurrency } from '@/routes/config/utils'

/**
 * Шаги 11–12 варианта 1: апсейл подписки и апсейл стемов сразу после оплаты.
 *
 * Живут на странице заказа, а не в форме: при redirect-оплате покупатель
 * возвращается именно сюда, и показывать апсейл в форме означало бы потерять его
 * для всех, кто платил через СБП (банковское приложение назад не возвращает).
 *
 * Отметка «показано» живёт в upsellSeen.ts. Дизайн — карточки движка /create
 * (капс-заголовок золотом, сериф, градиентная кнопка).
 */

const eyebrowClass = 'text-[11px] font-bold uppercase tracking-[0.16em] text-[#8a6621]'
const primaryButtonClass =
  'inline-flex w-full items-center justify-center gap-2 rounded-xl px-5 py-3 text-sm font-bold text-[#151210] shadow-[0_10px_24px_-14px_rgba(185,133,46,0.9)] transition active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-45'
const primaryButtonStyle = { background: `linear-gradient(135deg,#f6dfa3 0%, ${GOLD} 40%, #B9852E 100%)` }

/** Страница-обёртка и белая карточка — как StepShell, но без прогресс-бара. */
function UpsellCard({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen px-4 py-6 sm:py-10" style={{ background: CREAM }}>
      <div className="mx-auto w-full max-w-xl">
        <div className="rounded-3xl border border-[#EFE4D2] bg-white p-5 shadow-[0_18px_50px_-32px_rgba(21,18,16,0.55)] sm:p-7">
          {children}
        </div>
      </div>
    </div>
  )
}

export function PostPaymentUpsell({
  orderRef,
  paidAmount,
  onDone,
}: {
  orderRef: string
  paidAmount: number
  onDone: () => void
}) {
  const navigate = useNavigate()
  const flow = useFlowConfig({ trackAssignment: false })
  const [screen, setScreen] = useState<'subscription' | 'stems'>('subscription')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const finish = () => {
    markUpsellSeen(orderRef)
    onDone()
  }

  if (flow.status === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center" style={{ background: CREAM }}>
        <Loader2 className="h-8 w-8 animate-spin text-[#B9852E]" />
      </div>
    )
  }
  // Конфигурация плеча недоступна — апсейл не показываем вовсе: лучше сразу
  // отдать песню, чем держать оплатившего человека на экране без цен.
  if (flow.status === 'unavailable') {
    finish()
    return null
  }

  const subscriptionPrice = flow.config.subscriptionPrice
  const stemsPrice = flow.config.stemsPrice
  // Ценовое плечо: выбор «разовая vs подписка» уже был ДО оплаты (compare), и
  // повторять его после — значит переспрашивать. Плечо без шага подписки
  // сразу идёт к стемам или к песне.
  const offerSubscription = flow.config.steps.includes('subscription') && screen === 'subscription'

  /**
   * «Безлимит за 0 ₽ сегодня»: подписка включается по карте, сохранённой
   * платежом за эту песню; списание — через месяц. Карта не сохранилась —
   * обычное оформление на /subscribe.
   */
  const subscribeFromOrder = async () => {
    setBusy(true)
    setError(null)
    try {
      await apiPost(`/api/orders/${encodeURIComponent(orderRef)}/subscribe`, {})
      // Отдельного экрана «Подписка оформлена» нет: о подписке расскажет письмо,
      // а человека сразу ведём дальше — к песне.
      goToStemsOrFinish()
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      if (msg === 'no_saved_method') {
        markUpsellSeen(orderRef)
        navigate('/subscribe')
        return
      }
      setError(
        msg === 'already_active'
          ? 'У этого email уже есть подписка — сегодняшняя песня и так ваша.'
          : 'Не получилось оформить подписку. Песня уже ваша — попробуйте позже.',
      )
    } finally {
      setBusy(false)
    }
  }

  const goToStemsOrFinish = () => {
    if (stemsPrice > 0) {
      setScreen('stems')
      window.scrollTo(0, 0)
      return
    }
    finish()
  }

  const buyStems = async () => {
    setBusy(true)
    setError(null)
    try {
      const res = await apiPost<{ orderId: string }>(`/api/orders/${encodeURIComponent(orderRef)}/upsell/stems`, {})
      markUpsellSeen(orderRef)
      navigate(`/checkout?orderId=${encodeURIComponent(res.orderId)}`)
    } catch {
      setError('Не получилось оформить. Песня уже ваша — попробуйте позже.')
      setBusy(false)
    }
  }

  if (!offerSubscription && screen === 'subscription') {
    goToStemsOrFinish()
    return null
  }

  if (screen === 'subscription') {
    return (
      <UpsellCard>
        <div className="flex items-center gap-2">
          <PartyPopper className="h-4 w-4 text-[#B9852E]" />
          <span className={eyebrowClass}>Песня оплачена</span>
        </div>
        <h1 className="mt-2 font-[Prata,serif] text-[22px] leading-tight text-[#151210] sm:text-[26px]">
          Но у нас есть идея получше
        </h1>
        <p className="mt-2 text-sm leading-relaxed text-[#6b6259]">
          За те же деньги вы можете получать неограниченное число песен каждый месяц, а не одну разово.
        </p>

        <div className="mt-5 grid gap-3 sm:grid-cols-2">
          <div className="rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4">
            <p className={eyebrowClass}>То, что вы купили</p>
            <p className="mt-1 text-lg font-bold text-[#151210]">{fmtCurrency(paidAmount)}</p>
            <p className="text-[11px] text-[#9c9186]">разово, 1 песня</p>
            <ul className="mt-3 space-y-1 text-[11px] text-[#6b6259]">
              <li className="flex gap-1.5"><Check className="h-3 w-3 shrink-0 text-[#b4a893]" /> 1 готовая песня</li>
              <li className="flex gap-1.5"><Check className="h-3 w-3 shrink-0 text-[#b4a893]" /> Доступ навсегда</li>
            </ul>
          </div>
          <div className="rounded-2xl border border-[#d9b978] bg-[#FCF6EA] p-4 shadow-[0_10px_26px_-20px_rgba(185,133,46,0.9)]">
            <p className={eyebrowClass}>Выгоднее</p>
            <p className="mt-1 text-lg font-bold text-[#151210]">$0</p>
            <p className="text-[11px] text-[#9c9186]">сегодня, без ограничений</p>
            <ul className="mt-3 space-y-1 text-[11px] text-[#3b352f]">
              <li className="flex gap-1.5"><Check className="h-3 w-3 shrink-0 text-[#2f7a45]" /> Неограниченное число песен</li>
              <li className="flex gap-1.5"><Check className="h-3 w-3 shrink-0 text-[#2f7a45]" /> Отмена в любой момент</li>
              <li className="flex gap-1.5"><Check className="h-3 w-3 shrink-0 text-[#2f7a45]" /> Сегодняшняя песня уже включена</li>
            </ul>
          </div>
        </div>

        {error && (
          <p className="mt-4 rounded-xl bg-[#fdecec] px-3 py-2 text-sm text-[#a12222]" role="alert">
            {error}
          </p>
        )}

        <p className="mt-5 text-center text-xs text-[#9c9186]">
          Далее — {fmtCurrency(subscriptionPrice)}/мес, спишем через месяц. Отмена в любой момент.
        </p>
        <button type="button" className={`${primaryButtonClass} mt-3`} style={primaryButtonStyle} disabled={busy} onClick={subscribeFromOrder}>
          {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Перейти на безлимит за $0'}
        </button>
        {/* Вторичная кнопка с рамкой, а не ссылка: на телефоне «Не сейчас»
            без рамки терялось под основной и не читалось как кликабельное. */}
        <button
          type="button"
          className="mt-3 w-full rounded-xl border border-[#E7DAC5] bg-white px-4 py-3 text-sm font-semibold text-[#3b352f] transition hover:border-[#d9c39c] hover:text-[#151210] disabled:opacity-40"
          disabled={busy}
          onClick={goToStemsOrFinish}
        >
          Не сейчас
        </button>
      </UpsellCard>
    )
  }

  return (
    <UpsellCard>
      <div className="flex items-center gap-2">
        <Sparkles className="h-4 w-4 text-[#B9852E]" />
        <span className={eyebrowClass}>Дополнительно</span>
      </div>
      <h1 className="mt-2 font-[Prata,serif] text-[22px] leading-tight text-[#151210] sm:text-[26px]">
        Пока песня свежая
      </h1>
      <p className="mt-2 text-sm leading-relaxed text-[#6b6259]">
        Добавить инструментал и вокал отдельными дорожками?
      </p>

      <div className="mt-5 rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4">
        <div className="flex items-center gap-3">
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full" style={{ background: GOLD }}>
            <Sparkles className="h-5 w-5 text-[#151210]" />
          </span>
          <div>
            <div className="text-sm font-bold text-[#151210]">+{fmtCurrency(stemsPrice)}</div>
            <div className="text-xs text-[#6b6259]">отдельный платёж</div>
          </div>
        </div>
      </div>

      {error && (
        <p className="mt-4 rounded-xl bg-[#fdecec] px-3 py-2 text-sm text-[#a12222]" role="alert">
          {error}
        </p>
      )}

      <div className="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-end">
        <button
          type="button"
          className="whitespace-nowrap rounded-xl border border-[#E7DAC5] bg-white px-4 py-3 text-sm font-semibold text-[#3b352f] transition hover:border-[#d9c39c] hover:text-[#151210] disabled:opacity-40"
          disabled={busy}
          onClick={finish}
        >
          Не нужно
        </button>
        <button type="button" className={`${primaryButtonClass} sm:w-auto`} style={primaryButtonStyle} disabled={busy} onClick={buyStems}>
          {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Да, добавить'}
        </button>
      </div>
    </UpsellCard>
  )
}

/** Экран придержанной доставки: песня готова, но срок ещё не вышел (шаг 8/13). */
export function DeliveryHeldScreen({ deliverAt, accent }: { deliverAt: string | null; accent: string }) {
  const at = deliverAt ? new Date(deliverAt) : null
  const time = at && !Number.isNaN(at.getTime())
    ? at.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
    : null

  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-12" style={{ background: CREAM }}>
      <div className="w-full max-w-sm rounded-3xl border border-[#EFE4D2] bg-white p-7 text-center shadow-[0_18px_50px_-32px_rgba(21,18,16,0.55)]">
        <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full" style={{ backgroundColor: accent + '15' }}>
          <Sparkles className="h-8 w-8" style={{ color: accent }} />
        </div>
        <h1 className="font-[Prata,serif] text-[22px] text-[#151210]">Песня в работе</h1>
        <p className="mt-3 text-sm leading-relaxed text-[#6b6259]">
          {time
            ? `Пришлём её на email примерно к ${time}. Страница обновится сама.`
            : 'Пришлём её на email, как только будет готова. Страница обновится сама.'}
        </p>
      </div>
    </div>
  )
}
