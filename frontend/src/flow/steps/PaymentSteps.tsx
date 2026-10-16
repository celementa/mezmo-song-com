/**
 * Шаги вокруг оплаты варианта 1: экран сравнения «разовая vs подписка»
 * (только ценовые плечи — разовая 990 ₽ против подписки 750 ₽/мес, поэтому
 * выбор предлагается ДО оплаты; что предвыбрано, задаёт плечо, v20) и оплата —
 * виджет ЮKassa на своём домене, без ухода с сайта (v20).
 */

import { useState } from 'react'
import { Check, Lightbulb, Loader2, ShieldCheck } from 'lucide-react'

import { StepShell } from '@/routes/create/StepShell'
import { YooKassaWidget } from '@/features/payment/YooKassaWidget'
import { useEmbeddedPayment, type PaymentMode, type PaymentStart } from '@/features/payment/useEmbeddedPayment'
import type { ShellPassthrough } from './BriefSteps'


export function CompareStep({
  shell,
  oneTimePrice,
  subscriptionPrice,
  defaultChoice,
  busy,
  onSubscribe,
  onOneTime,
}: {
  shell: ShellPassthrough
  oneTimePrice: number
  subscriptionPrice: number
  /** Что предвыбрано — задаёт плечо (v20: тест предвыбора по умолчанию). */
  defaultChoice: 'subscription' | 'onetime'
  busy: boolean
  onSubscribe: () => void
  onOneTime: () => void
}) {
  const [choice, setChoice] = useState<'subscription' | 'onetime'>(defaultChoice)
  const fmt = (n: number) => n.toLocaleString('ru-RU')

  return (
    <StepShell
      {...shell}
      canGoBack={false}
      busy={busy}
      eyebrow="Перед оплатой"
      headline="Прежде чем платить за одну песню — у нас есть идея получше"
      subhead="Подписка дешевле разовой покупки и даёт песни каждый месяц, а не одну."
      primaryLabel="Продолжить"
      onPrimary={choice === 'subscription' ? onSubscribe : onOneTime}
    >
      <div className="flex items-center gap-2 text-[#B9852E]">
        <Lightbulb className="h-4 w-4" />
      </div>
      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <CompareCard
          selected={choice === 'subscription'}
          onSelect={() => setChoice('subscription')}
          price={`${fmt(subscriptionPrice)} ₽`}
          note="в месяц, без ограничений"
          items={['Неограниченное число песен', 'Отмена в любой момент']}
        />
        <CompareCard
          selected={choice === 'onetime'}
          onSelect={() => setChoice('onetime')}
          price={`${fmt(oneTimePrice)} ₽`}
          note="разово, 1 песня"
          items={['1 готовая песня', 'Доступ навсегда']}
        />
      </div>
    </StepShell>
  )
}

function CompareCard({
  selected,
  onSelect,
  price,
  note,
  items,
}: {
  selected: boolean
  onSelect: () => void
  price: string
  note: string
  items: string[]
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={`rounded-2xl border-2 p-4 text-left transition ${
        selected ? 'border-[#e3c98f] bg-[#FCF6EA]' : 'border-[#EFE4D2] bg-white hover:border-[#e3d0ad]'
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        <div>
          <p className="text-xl font-bold text-[#151210]">{price}</p>
          <p className="text-[11px] text-[#9c9186]">{note}</p>
        </div>
        <span
          className={`mt-1 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2 ${
            selected ? 'border-[#B9852E]' : 'border-[#d9cfbf]'
          }`}
        >
          {selected ? <span className="h-2 w-2 rounded-full bg-[#B9852E]" /> : null}
        </span>
      </div>
      <ul className={`mt-3 space-y-1 text-[11px] ${selected ? 'text-[#3b352f]' : 'text-[#6b6259]'}`}>
        {items.map((it) => (
          <li key={it} className="flex gap-1.5">
            <Check className={`h-3 w-3 shrink-0 ${selected ? 'text-[#2f7a45]' : 'text-[#b4a893]'}`} /> {it}
          </li>
        ))}
      </ul>
    </button>
  )
}

/**
 * Оплата: виджет ЮKassa прямо на шаге, редирект — только если виджет не
 * поднялся. После оплаты виджет уводит на returnUrl сервера.
 */
export function PaymentWidgetStep({
  shell,
  amount,
  perMonth = false,
  start,
  onPaid,
}: {
  shell: ShellPassthrough
  amount: number
  /** Первый платёж подписки: сумма подписана «в месяц». */
  perMonth?: boolean
  start: (mode: PaymentMode) => Promise<PaymentStart>
  onPaid: () => void
}) {
  const { state, widgetFail, retry } = useEmbeddedPayment({ start, onPaid })
  const amountLabel = `${amount.toLocaleString('ru-RU')} ₽${perMonth ? '/мес' : ''}`

  return (
    <StepShell
      {...shell}
      canGoBack={false}
      error={state.status === 'error' ? state.message : null}
      eyebrow="Оплата"
      headline={state.status === 'error' ? 'Не получилось перейти к оплате' : `Оплата ${amountLabel}`}
      primaryLabel={state.status === 'error' ? `Оплатить ${amountLabel}` : undefined}
      onPrimary={state.status === 'error' ? retry : undefined}
      hideNav={state.status !== 'error'}
    >
      {state.status === 'widget' ? (
        <YooKassaWidget confirmationToken={state.token} returnUrl={state.returnUrl} onFail={widgetFail} />
      ) : state.status === 'error' ? null : (
        <div className="flex flex-col items-center py-6 text-center">
          <Loader2 className="h-8 w-8 animate-spin text-[#B9852E]" />
          <p className="mt-4 text-sm text-[#6b6259]">
            {state.status === 'redirecting' ? 'Переходим на страницу ЮKassa…' : 'Готовим форму оплаты…'}
          </p>
        </div>
      )}
      <p className="mt-3 inline-flex items-center gap-1.5 text-xs text-[#9c9186]">
        <ShieldCheck className="h-3.5 w-3.5" /> Данные карты обрабатывает ЮKassa, мы их не видим
      </p>
    </StepShell>
  )
}
