/**
 * Шаг оплаты mezmo-song.com: сумма, обязательное согласие с офертой
 * (требование банка) и переход на платёжную форму UnitPay.
 *
 * Сервер заводит платёж (initPayment) только по кнопке, а не при показе шага:
 * без принятой оферты платить нельзя. Ответ — widgetUrl: та же форма UnitPay
 * открывается окном поверх сайта (UnitPayWidget), после оплаты покупатель
 * уходит на returnUrl. Нет widgetUrl — переход на форму по confirmationUrl,
 * откуда UnitPay возвращает на resultUrl, который задал сервер.
 */

import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'

import { StepShell } from '@/routes/create/StepShell'
import { fmtCurrency } from '@/routes/config/utils'
import { useCurrency } from '@/lib/currency'
import { localePath, useTr } from '@/lib/lang'
import { CurrencySwitch } from '@/components/CurrencySwitch'
import { PaymentLogos } from '@/components/landing/PaymentLogos'

export type PaymentMode = 'embedded' | 'redirect'

/** Ответ любой ручки, заводящей платёж (initiate-payment и т. п.). */
export type PaymentStart = {
  confirmationUrl?: string
  /** Форма UnitPay для открытия во iframe и куда вести после оплаты в нём. */
  widgetUrl?: string
  returnUrl?: string
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
  const tr = useTr()
  const money = (usd: number) => fmtCurrency(usd, currency) + (perMonth ? tr('/мес', '/mo') : '')
  const [agreed, setAgreed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [widget, setWidget] = useState<{ url: string; returnUrl: string } | null>(null)

  const pay = async () => {
    if (!agreed) {
      setError(
        tr(
          'Чтобы перейти к оплате, примите условия Пользовательского соглашения.',
          'To continue to payment, please accept the Terms of Use.',
        ),
      )
      return
    }
    setError(null)
    setBusy(true)
    try {
      const res = await start('redirect')
      if (res.widgetUrl && res.returnUrl) {
        setWidget({ url: res.widgetUrl, returnUrl: res.returnUrl })
        return
      }
      if (res.confirmationUrl) {
        window.location.assign(res.confirmationUrl)
        return
      }
      if (res.paid || res.activated) {
        onPaid()
        return
      }
      setError(tr('Не получилось перейти к оплате. Попробуйте ещё раз.', "Couldn't open payment. Please try again."))
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      if (SETTLED_ERRORS.includes(msg)) {
        onPaid()
        return
      }
      setError(tr('Не получилось перейти к оплате. Попробуйте ещё раз.', "Couldn't open payment. Please try again."))
    }
    setBusy(false)
  }

  const linkClass = 'font-semibold text-[#8a6621] underline underline-offset-2 hover:text-[#151210]'

  return (
    <StepShell
      {...shell}
      busy={busy}
      error={error}
      eyebrow={tr('Оплата', 'Payment')}
      headline={tr('Ваш заказ', 'Your order')}
      subhead={tr(
        'Персональная песня по вашей истории: аудиофайл и текст, доступ навсегда.',
        'A personal song based on your story: audio file and lyrics, lifetime access.',
      )}
      primaryLabel={busy ? tr('Открываем оплату…', 'Opening payment…') : tr(`Оплатить ${money(amount)}`, `Pay ${money(amount)}`)}
      onPrimary={() => void pay()}
      primaryDisabled={!agreed || busy}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-white p-4">
        <div className="mb-2 flex items-center justify-between gap-3 text-xs text-[#9c9186]">
          <span>{tr('Валюта', 'Currency')}</span>
          <CurrencySwitch />
        </div>
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-sm text-[#6b6259]">{tr('К оплате', 'Total')}</span>
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
          {tr('Я принимаю', 'I accept the')}{' '}
          <Link to={localePath('/terms')} target="_blank" className={linkClass}>
            {tr('Пользовательское соглашение', 'Terms of Use')}
          </Link>{' '}
          ({tr('включая', 'including the')}{' '}
          <Link to={localePath('/security')} target="_blank" className={linkClass}>
            {tr('Политику информационной безопасности', 'Information Security Policy')}
          </Link>
          ) {tr('и даю', 'and give')}{' '}
          <Link to={localePath('/consent')} target="_blank" className={linkClass}>
            {tr('согласие на обработку персональных данных', 'consent to the processing of personal data')}
          </Link>{' '}
          {tr('в соответствии с', 'in accordance with the')}{' '}
          <Link to={localePath('/privacy')} target="_blank" className={linkClass}>
            {tr('Политикой конфиденциальности', 'Privacy Policy')}
          </Link>
          .
        </span>
      </label>

      <p className="mt-4 text-xs text-[#6b6259]">
        {tr('Оплата проходит в защищённом окне UnitPay прямо на сайте.', 'Payment is processed in UnitPay’s secure window right on this site.')}
      </p>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
        <p className="inline-flex items-center gap-1.5 text-xs text-[#9c9186]">
          <ShieldCheck className="h-3.5 w-3.5" />{' '}
          {tr(
            'Данные карты обрабатывает UnitPay (PCI DSS), мы их не видим',
            'Card data is handled by UnitPay (PCI DSS). We never see it.',
          )}
        </p>
        <PaymentLogos />
      </div>
      {widget ? (
        <UnitPayWidget
          url={widget.url}
          onSuccess={() => window.location.assign(widget.returnUrl)}
          onClose={() => {
            setWidget(null)
            setBusy(false)
          }}
        />
      ) : null}
    </StepShell>
  )
}

const WIDGET_ORIGIN = 'https://widget.unitpay.money'

/**
 * Платёжный виджет UnitPay: их форма во iframe на весь экран. Делает то же,
 * что их widget.js, но без него: тот вешает глобальный window.onmessage и не
 * проверяет, от кого пришло сообщение. Форма шлёт родителю JSON-строки
 * {action: 'success' | 'close' | ...}. Оплату засчитывает только уведомление
 * UnitPay серверу — 'success' лишь уводит на страницу заказа, которая ждёт его.
 */
function UnitPayWidget({ url, onSuccess, onClose }: { url: string; onSuccess: () => void; onClose: () => void }) {
  const tr = useTr()
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== WIDGET_ORIGIN || typeof e.data !== 'string') return
      let action: unknown
      try {
        action = (JSON.parse(e.data) as { action?: unknown }).action
      } catch {
        return
      }
      if (action === 'success') onSuccess()
      if (action === 'close') onClose()
    }
    window.addEventListener('message', onMessage)
    document.body.classList.add('overflow-hidden')
    return () => {
      window.removeEventListener('message', onMessage)
      document.body.classList.remove('overflow-hidden')
    }
  }, [onSuccess, onClose])

  return (
    <div className="fixed inset-0 z-[99999] bg-black/60">
      <iframe src={url} title={tr('Оплата UnitPay', 'UnitPay payment')} allow="payment" className="h-full w-full border-0 bg-transparent" />
    </div>
  )
}
