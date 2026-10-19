/**
 * Шаг оплаты mezmo-song.com: сумма, обязательное согласие с офертой
 * (требование банка) и переход на платёжную форму UnitPay.
 *
 * Сервер заводит платёж (initPayment) только по кнопке, а не при показе шага:
 * без принятой оферты платить нельзя. Ответ — confirmationUrl формы UnitPay;
 * после оплаты UnitPay возвращает покупателя на resultUrl, который задал сервер.
 */

import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'

import { StepShell } from '@/routes/create/StepShell'
import { fmtCurrency } from '@/routes/config/utils'
import { useCurrency } from '@/lib/currency'
import { CurrencySwitch } from '@/components/CurrencySwitch'
import { PaymentLogos } from '@/components/landing/PaymentLogos'

export type PaymentMode = 'embedded' | 'redirect'

/** Ответ любой ручки, заводящей платёж (initiate-payment и т. п.). */
export type PaymentStart = {
  confirmationUrl?: string
  /** Оплата не нужна: цена 0 или mock-режим стенда. */
  paid?: boolean
  activated?: boolean
}

/** Заказ уже оплачен — уведомление UnitPay просто не доехало до сервера. */
const SETTLED_ERRORS = ['already_paid', 'not_pending']

export function UnitPayCheckout({
  shell,
  amount,
  original,
  perMonth = false,
  summary,
  start,
  onPaid,
}: {
  shell: { stepIndex: number; stepTotal: number; onBack: () => void; canGoBack: boolean }
  /** Сумма в долларах. */
  amount: number
  original?: number | null
  perMonth?: boolean
  /** Что покупается: кому, повод, куда придёт песня. */
  summary?: ReactNode
  start: (mode: PaymentMode) => Promise<PaymentStart>
  onPaid: () => void
}) {
  // Подписка на валюту: переключатель «$ / ₽» перерисовывает суммы шага сразу.
  const currency = useCurrency()
  const money = (usd: number) => fmtCurrency(usd, currency) + (perMonth ? '/мес' : '')
  const [agreed, setAgreed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const pay = async () => {
    if (!agreed) {
      setError('Чтобы перейти к оплате, примите условия Пользовательского соглашения.')
      return
    }
    setError(null)
    setBusy(true)
    try {
      const res = await start('redirect')
      if (res.confirmationUrl) {
        window.location.assign(res.confirmationUrl)
        return
      }
      if (res.paid || res.activated) {
        onPaid()
        return
      }
      setError('Не получилось перейти к оплате. Попробуйте ещё раз.')
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      if (SETTLED_ERRORS.includes(msg)) {
        onPaid()
        return
      }
      setError('Не получилось перейти к оплате. Попробуйте ещё раз.')
    }
    setBusy(false)
  }

  const linkClass = 'font-semibold text-[#8a6621] underline underline-offset-2 hover:text-[#151210]'

  return (
    <StepShell
      {...shell}
      busy={busy}
      error={error}
      eyebrow="Оплата"
      headline="Ваш заказ"
      subhead="Персональная песня по вашей истории: аудиофайл и текст, доступ навсегда."
      primaryLabel={busy ? 'Переходим к оплате…' : `Оплатить ${money(amount)}`}
      onPrimary={() => void pay()}
      primaryDisabled={!agreed || busy}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-white p-4">
        <div className="mb-2 flex items-center justify-between gap-3 text-xs text-[#9c9186]">
          <span>Валюта</span>
          <CurrencySwitch />
        </div>
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-sm text-[#6b6259]">К оплате</span>
          <span className="text-2xl font-bold text-[#151210]">
            {money(amount)}
            {original && original > amount ? (
              <span className="ml-2 text-sm font-normal text-[#b4a893] line-through">{fmtCurrency(original, currency)}</span>
            ) : null}
          </span>
        </div>
        {summary ? <div className="mt-3 text-xs text-[#6b6259]">{summary}</div> : null}
      </div>

      <label className="mt-4 flex cursor-pointer items-start gap-3 text-xs leading-relaxed text-[#3b352f]">
        <input
          type="checkbox"
          checked={agreed}
          onChange={(e) => {
            setAgreed(e.target.checked)
            setError(null)
          }}
          className="mt-0.5 h-4 w-4 shrink-0 accent-[#B9852E]"
        />
        <span>
          Я принимаю{' '}
          <Link to="/terms" target="_blank" className={linkClass}>Пользовательское соглашение</Link> (включая{' '}
          <Link to="/security" target="_blank" className={linkClass}>Политику информационной безопасности</Link>) и даю{' '}
          <Link to="/consent" target="_blank" className={linkClass}>согласие на обработку персональных данных</Link> в соответствии с{' '}
          <Link to="/privacy" target="_blank" className={linkClass}>Политикой конфиденциальности</Link>.
        </span>
      </label>

      <p className="mt-4 text-xs text-[#6b6259]">
        Оплата проходит на защищённой странице UnitPay — после неё вы вернётесь на сайт.
      </p>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
        <p className="inline-flex items-center gap-1.5 text-xs text-[#9c9186]">
          <ShieldCheck className="h-3.5 w-3.5" /> Данные карты обрабатывает UnitPay (PCI DSS), мы их не видим
        </p>
        <PaymentLogos />
      </div>
    </StepShell>
  )
}
