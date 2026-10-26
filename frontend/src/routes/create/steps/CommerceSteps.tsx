/**
 * Коммерческие шаги воронки: апсейл ускорения, переход к оплате UnitPay,
 * апсейл подписки, апсейл стемов и финал.
 *
 * Шаг payment — сводка с согласием на оферту и переход на форму UnitPay
 * (features/payment/UnitPayCheckout). Апсейлы до оплаты добавляют свою цену в
 * основной платёж, после оплаты оформляются отдельным платежом.
 */

import { useEffect } from 'react'
import { Check, Download, PartyPopper, Zap } from 'lucide-react'

import { GOLD, StepShell } from '../StepShell'
import { UnitPayCheckout, type PaymentMode, type PaymentStart } from '@/features/payment/UnitPayCheckout'
import { fmtCurrency } from '@/routes/config/utils'
import { shellProps, type StepProps } from '../types'
import { intlLocale, tr as trText, useTr } from '@/lib/lang'
import { reportEngagement } from '@/lib/engagement'

// Цены .com в долларах; «$ / ₽» на шаге оплаты переключает показ.
const rub = (n: number) => fmtCurrency(n)

/** Время доставки в человеческом виде; мусор в дате не должен ломать экран. */
function formatTime(iso: string): string {
  const at = new Date(iso)
  return Number.isNaN(at.getTime())
    ? trText('скоро', 'soon')
    : at.toLocaleTimeString(intlLocale(), { hour: '2-digit', minute: '2-digit' })
}

/** Апсейл ускорения. До оплаты — в основной платёж, после — доплата. */
export function UpsellSpeedStep(p: StepProps & { onAccept: () => void; afterPayment: boolean }) {
  const tr = useTr()
  const price = p.order.extraPrices.speed ?? 0
  const already = p.order.extras.includes('speed')

  // Цена 0 = допник не продаётся: либо выключен в админке, либо выполнять его
  // нечем (в воронке v2 задержки доставки нет, а ускорение продаёт именно её
  // снятие). Тогда шаг просто пропускается — предлагать то, что не будет
  // выполнено, нельзя.
  const disabled = Object.keys(p.order.extraPrices).length > 0 && price <= 0
  useEffect(() => {
    if (disabled) p.onNext()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabled])
  if (disabled) return null

  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={p.canGoBack && !p.afterPayment}
      eyebrow={p.afterPayment ? tr('Дополнительно', 'Extra') : tr('Перед оплатой', 'Before payment')}
      headline={tr('Хотите получить песню быстрее?', 'Want your song faster?')}
      subhead={
        p.afterPayment
          ? tr(
              'Обычно песня готова за 2 часа. Доплатите — и получите её за 10–30 минут.',
              'Songs usually take about 2 hours. Pay the difference and get yours in 10–30 minutes.',
            )
          : tr(
              'Обычно песня готова за 2 часа. Добавьте ускорение — и получите её за 10–30 минут.',
              'Songs usually take about 2 hours. Add the speed-up and get yours in 10–30 minutes.',
            )
      }
      primaryLabel={already ? tr('Далее', 'Next') : tr(`Добавить за ${rub(price)}`, `Add for ${rub(price)}`)}
      onPrimary={() => {
        if (already) {
          p.onNext()
          return
        }
        p.track(p.stepId, p.stepIndex, 'submit', { extra: 'speed', price })
        p.onAccept()
      }}
      secondaryLabel={already ? undefined : tr('Не нужно', 'No thanks')}
      onSecondary={() => {
        p.track(p.stepId, p.stepIndex, 'skip', { extra: 'speed' })
        p.onNext()
      }}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4">
        <div className="flex items-center gap-3">
          <span className="flex h-10 w-10 items-center justify-center rounded-full" style={{ background: GOLD }}>
            <Zap className="h-5 w-5 text-[#151210]" />
          </span>
          <div>
            <div className="text-sm font-bold text-[#151210]">+{rub(price)}</div>
            <div className="text-xs text-[#6b6259]">
              {p.afterPayment
                ? tr('Отдельный платёж', 'Separate payment')
                : tr(`итого ${rub(p.price + price)}`, `total ${rub(p.price + price)}`)}
            </div>
          </div>
        </div>
      </div>
      {already ? (
        <p className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-[#2f7a45]">
          <Check className="h-4 w-4" /> {tr('Ускорение добавлено к заказу', 'Speed-up added to your order')}
        </p>
      ) : null}
    </StepShell>
  )
}

/**
 * Оплата: сводка и согласие с офертой, по кнопке — форма UnitPay. После оплаты
 * UnitPay возвращает на resultUrl сервера (/create?order=…&paid=1), и воронка
 * продолжается со следующего шага.
 */
export function PaymentStep(p: StepProps & { start: (mode: PaymentMode) => Promise<PaymentStart>; onPaid: () => void }) {
  const tr = useTr()
  const extrasSum = p.order.extras.reduce((sum, id) => sum + (p.order.extraPrices[id] ?? 0), 0)
  const total = p.order.paymentAmount ?? p.price + extrasSum

  return (
    <UnitPayCheckout
      shell={shellProps(p)}
      amount={total}
      original={p.originalPrice}
      summary={
        p.form.email ? <p>{tr(`Ссылка на песню придёт на ${p.form.email}`, `The song link will be sent to ${p.form.email}`)}</p> : null
      }
      start={(mode) => {
        p.track(p.stepId, p.stepIndex, 'submit', { total })
        return p.start(mode)
      }}
      onPaid={p.onPaid}
    />
  )
}

/**
 * Апсейл подписки сразу после оплаты — «0 ₽ сегодня» (v11): оплаченная песня
 * и есть первый месяц, карта уже сохранена платежом за неё, списание через
 * месяц. Если карта не сохранилась (кошелёк без сохранения), сервер отвечает
 * no_saved_method — тогда кнопка ведёт на обычное оформление /subscribe.
 */
export function UpsellSubscriptionStep(
  p: StepProps & { subscriptionPrice: number; subscribed: boolean; onSubscribe: () => void },
) {
  const tr = useTr()
  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={false}
      eyebrow={tr('Спасибо за оплату', 'Thank you for your payment')}
      headline={tr(
        'Ваша песня оплачена — но у нас есть идея получше',
        'Your song is paid for — but we have a better idea',
      )}
      subhead={tr(
        'За те же деньги вы можете получать неограниченное число песен каждый месяц, а не одну разово.',
        'For the same price you can get unlimited songs every month, not just one.',
      )}
      primaryLabel={p.subscribed ? tr('Далее', 'Next') : tr('Перейти на безлимит за $0', 'Get unlimited for $0')}
      onPrimary={() => {
        if (p.subscribed) {
          p.onNext()
          return
        }
        p.track(p.stepId, p.stepIndex, 'submit', { price: 0, monthly: p.subscriptionPrice })
        p.onSubscribe()
      }}
      secondaryLabel={p.subscribed ? undefined : tr('Не сейчас', 'Not now')}
      onSecondary={() => {
        p.track(p.stepId, p.stepIndex, 'skip')
        p.onNext()
      }}
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-2xl border border-[#EFE4D2] bg-white p-4">
          <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#9c9186]">
            {tr('То, что вы купили', 'What you bought')}
          </div>
          <div className="mt-1.5 text-xl font-bold text-[#151210]">{rub(p.price)}</div>
          <div className="text-xs text-[#9c9186]">{tr('разово, 1 песня', 'one-time, 1 song')}</div>
          <ul className="mt-3 space-y-1.5 text-xs text-[#6b6259]">
            <li>• {tr('1 готовая песня', '1 finished song')}</li>
            <li>• {tr('Доступ навсегда', 'Lifetime access')}</li>
          </ul>
        </div>
        <div className="relative rounded-2xl border-2 border-[#e3c98f] bg-[#FCF6EA] p-4">
          <span
            className="absolute -top-2.5 right-3 rounded-full px-2 py-0.5 text-[10px] font-bold text-[#151210]"
            style={{ background: GOLD }}
          >
            {tr('Выгоднее', 'Best value')}
          </span>
          <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">
            {tr('Безлимит', 'Unlimited')}
          </div>
          <div className="mt-1.5 text-xl font-bold text-[#151210]">$0</div>
          <div className="text-xs text-[#9c9186]">{tr('сегодня, без ограничений', 'today, no limits')}</div>
          <ul className="mt-3 space-y-1.5 text-xs text-[#6b6259]">
            <li>• {tr('Неограниченное число песен', 'Unlimited number of songs')}</li>
            <li>• {tr('Отмена в любой момент', 'Cancel anytime')}</li>
            <li>• {tr('Сегодняшняя песня уже включена', "Today's song is already included")}</li>
          </ul>
        </div>
      </div>
      {p.subscribed ? (
        <p className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-[#2f7a45]">
          <Check className="h-4 w-4" /> {tr('Подписка оформлена — письмо с деталями уже у вас', 'Subscription activated — the details are in your inbox')}
        </p>
      ) : (
        <p className="mt-3 text-center text-xs text-[#9c9186]">
          {tr(
            `Далее — ${rub(p.subscriptionPrice)}/мес, спишем через месяц. Отмена в любой момент.`,
            `Next — ${rub(p.subscriptionPrice)}/month, billed in a month. Cancel anytime.`,
          )}
        </p>
      )}
    </StepShell>
  )
}

/** Апсейл стемов — доплата после оплаты песни. */
export function UpsellStemsStep(p: StepProps & { onAccept: () => void }) {
  const tr = useTr()
  // Как и у ускорения: нулевая цена значит «отдать нечем» (разделение дорожек
  // у Kie.ai не подключено), и шаг пропускается.
  const stemsDisabled = Object.keys(p.order.extraPrices).length > 0 && (p.order.extraPrices.stems ?? 0) <= 0
  useEffect(() => {
    if (stemsDisabled) p.onNext()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stemsDisabled])
  if (stemsDisabled) return null
  const price = p.order.extraPrices.stems ?? 190
  const already = p.order.extras.includes('stems')

  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={false}
      eyebrow={tr('Пока песня свежая', 'While the song is fresh')}
      headline={tr('Добавить инструментал и вокал отдельно?', 'Add instrumental and vocals separately?')}
      subhead={tr(
        'Отдельные дорожки — чтобы спеть самому под минус или смонтировать видео.',
        'Separate tracks, so you can sing along or edit a video.',
      )}
      // Допник уже куплен (возврат с отдельного платежа): единственная дорога
      // дальше — «Далее», а не «Не нужно» под только что оплаченным.
      primaryLabel={already ? tr('Далее', 'Next') : tr(`Да, добавить за ${rub(price)}`, `Yes, add for ${rub(price)}`)}
      onPrimary={() => {
        if (already) {
          p.onNext()
          return
        }
        p.track(p.stepId, p.stepIndex, 'submit', { extra: 'stems', price })
        p.onAccept()
      }}
      secondaryLabel={already ? undefined : tr('Не нужно', 'No thanks')}
      onSecondary={() => {
        p.track(p.stepId, p.stepIndex, 'skip', { extra: 'stems' })
        p.onNext()
      }}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4 text-sm text-[#6b6259]">
        {tr(
          `Отдельный платёж на ${rub(price)} — вернёмся сюда сразу после оплаты.`,
          `A separate payment of ${rub(price)} — we will bring you back here right after it.`,
        )}
      </div>
      {already ? (
        <p className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-[#2f7a45]">
          <Check className="h-4 w-4" /> {tr('Дорожки добавлены к заказу', 'Tracks added to your order')}
        </p>
      ) : null}
    </StepShell>
  )
}

/** Финал: скачать MP3 и перейти на страницу заказа. */
export function DoneStep(p: StepProps & { tracks: string[]; orderUrl: string; onRestart: () => void }) {
  const tr = useTr()
  const track = p.tracks[0] ?? ''
  return (
    <StepShell {...shellProps(p)} canGoBack={false} eyebrow={tr('Готово', 'Done')} hideNav>
      <div className="flex flex-col items-center py-4 text-center">
        <PartyPopper className="h-10 w-10 text-[#B9852E]" />
        <h2 className="mt-4 font-[Prata,serif] text-[24px] text-[#151210]">
          {tr('Ваша песня готова!', 'Your song is ready!')}
        </h2>
        <p className="mt-2 text-sm text-[#6b6259]">
          {p.form.email
            ? tr(
                `Копию отправили на почту ${p.form.email} — не потеряется.`,
                `We sent a copy to ${p.form.email} — it won't get lost.`,
              )
            : tr('Копию отправили на почту — не потеряется.', "We sent a copy to your inbox — it won't get lost.")}
        </p>

        {p.order.deliveryHeld ? (
          // Доставка придержана: полный трек прокси пока не отдаёт (и не должен —
          // именно снятие этого ожидания продаёт ускорение). Кнопка «Скачать»
          // здесь вернула бы 404, поэтому вместо неё честный срок.
          <p className="mt-6 rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] px-4 py-3 text-sm leading-relaxed text-[#6b6259]">
            {tr('Песня готовится. Пришлём её на email', 'Your song is being prepared. We will email it to you')}
            {p.order.deliverAt
              ? tr(` примерно к ${formatTime(p.order.deliverAt)}`, ` around ${formatTime(p.order.deliverAt)}`)
              : tr(' совсем скоро', ' very soon')}
            {tr(' — страница заказа обновится сама.', ' — the order page will update on its own.')}
          </p>
        ) : track ? (
          <a
            href={track}
            download
            onClick={() => {
              p.track(p.stepId, p.stepIndex, 'submit', { action: 'download' })
              reportEngagement(p.order.orderId, 'download')
            }}
            className="mt-6 inline-flex items-center gap-2 rounded-xl px-6 py-3 text-sm font-bold text-[#151210] shadow-[0_10px_24px_-14px_rgba(185,133,46,0.9)] transition active:scale-[0.98]"
            style={{ background: `linear-gradient(135deg,#f6dfa3 0%, ${GOLD} 40%, #B9852E 100%)` }}
          >
            <Download className="h-4 w-4" />
            {tr('Скачать MP3', 'Download MP3')}
          </a>
        ) : null}

        <a href={p.orderUrl} className="mt-4 text-sm font-medium text-[#8a6621] underline underline-offset-4">
          {tr('Открыть страницу заказа', 'Open order page')}
        </a>

        {/* Вторая песня начинается отсюда: письмо-догонка с промокодом ведёт на
            /create, и без этой кнопки человек попадал на этот же экран со своим
            прошлым заказом — купить ещё одну было нельзя. */}
        <button
          type="button"
          onClick={() => {
            p.track(p.stepId, p.stepIndex, 'submit', { action: 'restart' })
            p.onRestart()
          }}
          className="mt-3 text-sm font-medium text-[#6b6259] underline underline-offset-4"
        >
          {tr('Создать ещё одну песню', 'Create another song')}
        </button>
      </div>
    </StepShell>
  )
}
