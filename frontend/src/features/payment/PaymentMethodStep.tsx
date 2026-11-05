import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'

import { StepShell } from '@/routes/create/StepShell'
import { useTr } from '@/lib/lang'

export type PaymentBranch = 'ru' | 'intl'

type Shell = {
  stepIndex: number
  stepTotal: number
  onBack: () => void
  canGoBack: boolean
  busy?: boolean
  error?: string | null
}

/**
 * Шаг «Как вам удобнее оплатить?» — единственное место, где во флоу появляется
 * цена. До выбора ветки ни рублей, ни долларов не показываем: валюта и
 * провайдер зависят только от выбора покупателя, а не от языка сайта.
 */
export function PaymentMethodStep({
  shell,
  value,
  onChoose,
}: {
  shell: Shell
  value: PaymentBranch | null
  onChoose: (branch: PaymentBranch) => void
}) {
  const tr = useTr()
  const [choice, setChoice] = useState<PaymentBranch | null>(value)
  useEffect(() => {
    if (value) setChoice(value)
  }, [value])

  return (
    <StepShell
      {...shell}
      eyebrow={tr('Оплата', 'Payment')}
      headline={tr('Как вам удобнее оплатить?', 'How would you like to pay?')}
      subhead={tr(
        'От выбора зависит валюта и платёжная страница.',
        'Your choice sets the currency and the payment page.',
      )}
      primaryLabel={tr('Продолжить', 'Continue')}
      onPrimary={() => {
        if (choice) onChoose(choice)
      }}
      primaryDisabled={!choice || shell.busy}
    >
      <div className="mt-4 grid gap-3" role="radiogroup" aria-label={tr('Способ оплаты', 'Payment method')}>
        <PaymentOption
          selected={choice === 'ru'}
          onSelect={() => setChoice('ru')}
          flag="🇷🇺"
          title={tr('Российская карта', 'Russian card')}
          subtitle={tr('Оплата в рублях', 'Payment in rubles')}
          methods={['Мир', 'СБП', 'SberPay', 'T-Pay', 'ЮMoney']}
        />
        <PaymentOption
          selected={choice === 'intl'}
          onSelect={() => setChoice('intl')}
          flag="🌍"
          title={tr('Международная карта', 'International card')}
          subtitle={tr('Оплата в долларах (USD)', 'Payment in US dollars (USD)')}
          methods={['Visa', 'Mastercard', 'Apple Pay', 'Google Pay']}
        />
      </div>
      <p className="mt-4 text-xs leading-relaxed text-[#9c9186]">
        {tr(
          'Оплата проходит на защищённой странице провайдера. Данные карты мы не видим.',
          'Payment is processed on the provider’s secure page. We never see your card details.',
        )}
      </p>
    </StepShell>
  )
}

function PaymentOption({
  selected,
  onSelect,
  flag,
  title,
  subtitle,
  methods,
}: {
  selected: boolean
  onSelect: () => void
  flag: string
  title: string
  subtitle: string
  methods: string[]
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
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <span className="text-2xl leading-none" aria-hidden>
            {flag}
          </span>
          <div>
            <div className="text-base font-bold text-[#151210]">{title}</div>
            <div className="mt-0.5 text-xs text-[#6b6259]">{subtitle}</div>
            <div className="mt-3 flex flex-wrap gap-1.5">
              {methods.map((m) => (
                <span key={m} className="rounded-md border border-[#E7DAC5] bg-white px-1.5 py-0.5 text-[10px] font-semibold text-[#5d554b]">
                  {m}
                </span>
              ))}
            </div>
          </div>
        </div>
        <span
          className={`mt-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-2 ${
            selected ? 'border-[#B9852E] bg-[#B9852E]' : 'border-[#d9cfbf]'
          }`}
        >
          {selected ? <Check className="h-3 w-3 text-white" /> : null}
        </span>
      </div>
    </button>
  )
}
