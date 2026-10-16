import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'

import { CREAM, StepShell } from '@/routes/create/StepShell'
import { fmtCurrency } from '@/routes/config/utils'
import type { Loose } from '@/lib/loose'
import { COMPANY } from '@/lib/site'
import { PaymentLogos } from '@/components/landing/PaymentLogos'
import { CurrencySwitch } from '@/components/CurrencySwitch'
import { EMPTY_ANSWERS, type FlowAnswers } from './types'
import { loadFlowDraft, saveFlowDraft, searchWithStep, stepFromSearch } from './draft'
import { EmailStep, EmotionStep, OccasionStep, RecipientStep, StoryStep, answersGenre } from './steps/BriefSteps'

/**
 * Анкета заказа mezmo-song.com (плечо variant3-990).
 *
 * Бэкенда у сайта нет, поэтому заказ на сервер не уходит: шаги брифа те же,
 * что у варианта 1 на mezmo.ru, ответы живут в черновике браузера, а шаг
 * оплаты показывает сумму, обязательное согласие с офертой (требование банка)
 * и сообщает, что приём оплаты подключается. Когда появится интеграция
 * UnitPay, кнопка оплаты начнёт создавать платёж — согласие уже на месте.
 */

const ARM = 'variant3-990'
const STEPS = ['recipient', 'occasion', 'email', 'emotion', 'story', 'payment'] as const

const NEXT_LABEL: Record<string, string> = {
  recipient: 'Далее',
  occasion: 'Далее',
  email: 'Продолжить',
  emotion: 'Далее',
  story: 'К оплате',
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

/** Что не так с ответами на шаге step; null — шаг заполнен. */
function stepError(step: string, answers: FlowAnswers): string | null {
  if (step === 'recipient') {
    if (!answers.relationship) return 'Выберите, кому дарим песню.'
    if (!answers.recipientName.trim()) return 'Укажите имя.'
    if (!answers.recipientGender && (answers.relationship === 'Друг' || answers.relationship === 'Другое')) {
      return 'Уточните, ему или ей.'
    }
  }
  if (step === 'occasion' && !answers.occasion) return 'Выберите повод.'
  if (step === 'email' && !EMAIL_RE.test(answers.email.trim())) return 'Укажите корректный email.'
  if (step === 'emotion' && !answers.emotion && !answersGenre(answers)) return 'Выберите эмоцию или жанр.'
  if (step === 'story' && !answers.story.trim()) return 'Расскажите хотя бы пару предложений.'
  return null
}

/**
 * Дальше первого незаполненного шага не пускаем: шаг лежит в адресе (`?s=N`),
 * и по прямой ссылке иначе можно оказаться на оплате с пустой анкетой.
 */
function reachableStep(requested: number, answers: FlowAnswers): number {
  const firstIncomplete = STEPS.findIndex((step) => stepError(step, answers) !== null)
  const limit = firstIncomplete === -1 ? STEPS.length - 1 : firstIncomplete
  return Math.max(0, Math.min(requested, limit))
}

/** Цена из вшитого снимка настроек (frontend/intl/settings.json). */
function inlinedPrice(): { price: number; original: number | null } {
  const s = (window as Loose).__SETTINGS__
  const price = typeof s?.paymentAmount === 'number' ? s.paymentAmount : 25
  const original = typeof s?.originalAmount === 'number' && s.originalAmount > price ? s.originalAmount : null
  return { price, original }
}

export function IntlFlowRoute() {
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  const [draft] = useState(() => loadFlowDraft(ARM))
  const [answers, setAnswers] = useState<FlowAnswers>(draft?.answers ?? EMPTY_ANSWERS)
  const lastStep = STEPS.length - 1
  const [stepIndex, setStepIndex] = useState(() =>
    reachableStep(stepFromSearch(search) ?? draft?.stepIndex ?? 0, draft?.answers ?? EMPTY_ANSWERS),
  )
  const [error, setError] = useState<string | null>(null)
  const current = STEPS[stepIndex]

  const answersRef = useRef(answers)
  useEffect(() => {
    answersRef.current = answers
  }, [answers])

  const stepRef = useRef(stepIndex)
  useEffect(() => {
    stepRef.current = stepIndex
  }, [stepIndex])

  const setStep = useCallback(
    (next: number, replace = false) => {
      const idx = Math.min(Math.max(next, 0), lastStep)
      setStepIndex(idx)
      setError(null)
      window.scrollTo(0, 0)
      navigate({ pathname, search: searchWithStep(window.location.search, idx) }, { replace })
    },
    [navigate, pathname, lastStep],
  )

  // У текущего шага должна быть своя запись в истории — «назад» браузера ходит
  // по шагам. Номер в адресе правится и когда шаг оказался недоступен.
  useEffect(() => {
    if (stepFromSearch(window.location.search) !== stepRef.current) {
      navigate({ pathname, search: searchWithStep(window.location.search, stepRef.current) }, { replace: true })
    }
  }, [navigate, pathname])

  const urlStep = stepFromSearch(search)
  useEffect(() => {
    if (urlStep === null || urlStep === stepRef.current) return
    setStepIndex(reachableStep(urlStep, answersRef.current))
    window.scrollTo(0, 0)
  }, [urlStep])

  useEffect(() => {
    saveFlowDraft({ flowVariant: ARM, answers, orderRef: null, stepIndex, payVia: 'order' })
  }, [answers, stepIndex])

  const patch = useCallback((fields: Partial<FlowAnswers>) => {
    setAnswers((prev) => ({ ...prev, ...fields }))
    setError(null)
  }, [])

  const validate = () => stepError(current, answers)

  const handleNext = () => {
    const invalid = validate()
    if (invalid) {
      setError(invalid)
      return
    }
    setStep(stepIndex + 1)
  }

  const shell = {
    stepIndex,
    stepTotal: STEPS.length,
    onBack: stepIndex === 0 ? () => navigate('/') : () => setStep(stepIndex - 1),
    canGoBack: true,
    error,
  }
  const briefProps = { answers, patch, shell, onNext: handleNext, primaryLabel: NEXT_LABEL[current] ?? 'Далее' }

  return (
    <div className="min-h-screen font-sans" style={{ background: CREAM }}>
      {current === 'recipient' && <RecipientStep {...briefProps} />}
      {current === 'occasion' && <OccasionStep {...briefProps} />}
      {current === 'email' && <EmailStep {...briefProps} />}
      {current === 'emotion' && <EmotionStep {...briefProps} />}
      {current === 'story' && <StoryStep {...briefProps} />}
      {current === 'payment' && <PaymentStep shell={shell} answers={answers} />}
    </div>
  )
}

function PaymentStep({
  shell,
  answers,
}: {
  shell: { stepIndex: number; stepTotal: number; onBack: () => void; canGoBack: boolean }
  answers: FlowAnswers
}) {
  const { price, original } = inlinedPrice()
  const [agreed, setAgreed] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState(false)

  const pay = () => {
    if (!agreed) {
      setError('Чтобы перейти к оплате, примите условия Пользовательского соглашения.')
      return
    }
    setError(null)
    setNotice(true)
  }

  const linkClass = 'font-semibold text-[#8a6621] underline underline-offset-2 hover:text-[#151210]'

  return (
    <StepShell
      {...shell}
      error={error}
      eyebrow="Оплата"
      headline="Ваш заказ"
      subhead="Персональная песня по вашей истории: аудиофайл и текст, доступ навсегда."
      primaryLabel={`Оплатить ${fmtCurrency(price)}`}
      onPrimary={pay}
      primaryDisabled={!agreed}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-white p-4">
        <div className="mb-2 flex items-center justify-between gap-3 text-xs text-[#9c9186]">
          <span>Валюта</span>
          <CurrencySwitch />
        </div>
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-sm text-[#6b6259]">К оплате</span>
          <span className="text-2xl font-bold text-[#151210]">
            {fmtCurrency(price)}
            {original && <span className="ml-2 text-sm font-normal text-[#b4a893] line-through">{fmtCurrency(original)}</span>}
          </span>
        </div>
        <ul className="mt-3 space-y-1 text-xs text-[#6b6259]">
          <li>Кому: {answers.recipientName.trim()}{answers.relationship ? ` (${answers.relationship.toLowerCase()})` : ''}</li>
          {answers.occasion && <li>Повод: {answers.occasion}</li>}
          <li>Песня придёт на {answers.email.trim()}</li>
        </ul>
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

      {notice && (
        <div role="status" className="mt-4 rounded-2xl border border-[#E7DAC5] bg-[#FCF6EA] p-4 text-sm leading-relaxed text-[#3b352f]">
          Приём оплаты на сайте скоро заработает. Ваши ответы сохранены в этом браузере — вернитесь чуть позже или напишите нам на{' '}
          <a href={`mailto:${COMPANY.email}`} className={linkClass}>{COMPANY.email}</a>.
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <p className="inline-flex items-center gap-1.5 text-xs text-[#9c9186]">
          <ShieldCheck className="h-3.5 w-3.5" /> Данные карты обрабатывает UnitPay (PCI DSS), мы их не видим
        </p>
        <PaymentLogos />
      </div>
    </StepShell>
  )
}
