/**
 * Оболочка шага: прогресс «ШАГ N ИЗ M», заголовок, тело и нижняя навигация.
 * Одна вёрстка на все брейкпоинты — на узком экране карточка занимает всю
 * ширину, на широком центрируется и ограничена по максимальной ширине.
 * Отдельной мобильной страницы нет, как и в текущем флоу.
 */

import type { ReactNode } from 'react'
import { ArrowLeft, ArrowRight, Loader2 } from 'lucide-react'

import { cn } from '@/lib/utils'

export const GOLD = '#F2C572'
export const INK = '#151210'
export const CREAM = '#FBF7F1'

export function StepShell({
  stepIndex,
  stepTotal,
  eyebrow,
  headline,
  subhead,
  children,
  onBack,
  canGoBack,
  primaryLabel,
  onPrimary,
  primaryDisabled,
  secondaryLabel,
  onSecondary,
  busy,
  error,
  hideNav,
  footer,
}: {
  stepIndex: number
  stepTotal: number
  eyebrow: string
  headline?: string
  subhead?: string
  children: ReactNode
  onBack: () => void
  canGoBack: boolean
  primaryLabel?: string
  onPrimary?: () => void
  primaryDisabled?: boolean
  secondaryLabel?: string
  onSecondary?: () => void
  busy?: boolean
  error?: string | null
  hideNav?: boolean
  /** Что рисовать под навигацией (ссылка-фолбэк «выбрать жанр»). */
  footer?: ReactNode
}) {
  const progress = Math.round(((stepIndex + 1) / stepTotal) * 100)

  return (
    <div className="min-h-screen px-4 py-6 sm:py-10" style={{ background: CREAM }}>
      <div className="mx-auto w-full max-w-xl">
        <div className="mb-3 flex items-center justify-between text-[11px] font-semibold uppercase tracking-[0.14em] text-[#8a6621]">
          <span>
            Шаг {stepIndex + 1} из {stepTotal}
          </span>
          <span className="tabular-nums text-[#b39a6d]">{progress}%</span>
        </div>
        <div className="mb-5 h-1 overflow-hidden rounded-full bg-[#EDE3D3]">
          <div
            className="h-full rounded-full transition-[width] duration-500 ease-out"
            style={{ width: `${progress}%`, background: `linear-gradient(90deg,#f6dfa3,${GOLD},#B9852E)` }}
          />
        </div>

        <div className="rounded-3xl border border-[#EFE4D2] bg-white p-5 shadow-[0_18px_50px_-32px_rgba(21,18,16,0.55)] sm:p-7">
          <div className="text-[11px] font-bold uppercase tracking-[0.16em] text-[#8a6621]">{eyebrow}</div>
          {headline ? (
            <h1 className="mt-2 font-[Prata,serif] text-[22px] leading-tight text-[#151210] sm:text-[26px]">{headline}</h1>
          ) : null}
          {subhead ? <p className="mt-2 text-sm leading-relaxed text-[#6b6259]">{subhead}</p> : null}

          <div className={cn(headline || subhead ? 'mt-5' : 'mt-4')}>{children}</div>

          {error ? (
            <p className="mt-4 rounded-xl bg-[#fdecec] px-3 py-2 text-sm text-[#a12222]" role="alert">
              {error}
            </p>
          ) : null}

          {hideNav ? null : (
            <div className="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
              {canGoBack ? (
                <button
                  type="button"
                  onClick={onBack}
                  disabled={busy}
                  className="inline-flex items-center justify-center gap-1.5 py-1 text-sm font-medium text-[#6b6259] transition hover:text-[#151210] disabled:opacity-40 sm:justify-start sm:py-0"
                >
                  <ArrowLeft className="h-4 w-4" />
                  Назад
                </button>
              ) : (
                <span />
              )}

              <div className="flex flex-col-reverse items-stretch gap-3 sm:ml-auto sm:flex-row sm:items-center">
                {secondaryLabel && onSecondary ? (
                  <button
                    type="button"
                    onClick={onSecondary}
                    disabled={busy}
                    // Вторичная кнопка («Не сейчас», «Не нужно») остаётся настоящей
                    // кнопкой с рамкой на всех ширинах: как ссылка без рамки она на
                    // телефоне терялась под основной и не читалась как кликабельная.
                    className="whitespace-nowrap rounded-xl border border-[#E7DAC5] bg-white px-4 py-3 text-sm font-semibold text-[#3b352f] transition hover:border-[#d9c39c] hover:text-[#151210] disabled:opacity-40"
                  >
                    {secondaryLabel}
                  </button>
                ) : null}
                {primaryLabel && onPrimary ? (
                  <button
                    type="button"
                    onClick={onPrimary}
                    disabled={primaryDisabled || busy}
                    className="inline-flex items-center justify-center gap-2 rounded-xl px-5 py-3 text-sm font-bold text-[#151210] shadow-[0_10px_24px_-14px_rgba(185,133,46,0.9)] transition active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-45"
                    style={{ background: `linear-gradient(135deg,#f6dfa3 0%, ${GOLD} 40%, #B9852E 100%)` }}
                  >
                    {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                    {primaryLabel}
                    {busy ? null : <ArrowRight className="h-4 w-4" />}
                  </button>
                ) : null}
              </div>
            </div>
          )}
          {footer}
        </div>
      </div>
    </div>
  )
}

/** Кнопка-чип выбора (кому дарим, повод, стиль) — общая для нескольких шагов. */
export function Chip({
  label,
  selected,
  onClick,
  badge,
  desc,
}: {
  label: string
  selected: boolean
  onClick: () => void
  badge?: string
  desc?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'relative rounded-full border px-4 py-2 text-left text-sm font-medium transition active:scale-[0.98]',
        selected
          ? 'border-transparent bg-[#151210] text-white shadow-[0_10px_22px_-16px_rgba(21,18,16,0.9)]'
          : 'border-[#E7DAC5] bg-white text-[#3b352f] hover:border-[#d9c39c]',
      )}
    >
      {label}
      {desc ? <span className={cn('ml-1.5 text-xs', selected ? 'text-white/60' : 'text-[#9c9186]')}>· {desc}</span> : null}
      {badge ? (
        <span
          className="absolute -right-1 -top-2 rounded-full px-1.5 py-0.5 text-[10px] font-bold text-[#151210]"
          style={{ background: GOLD }}
        >
          {badge}
        </span>
      ) : null}
    </button>
  )
}
