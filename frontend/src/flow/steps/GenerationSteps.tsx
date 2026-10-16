/**
 * Шаги 6–8 варианта 1 в дизайне движка /create (StepShell): генерация,
 * 15-секундный отрывок под замком и апсейл ускорения перед оплатой.
 */

import { useEffect, useRef, useState } from 'react'
import { Loader2, Lock, Pause, Play, Zap } from 'lucide-react'

import { GOLD, StepShell } from '@/routes/create/StepShell'
import type { ShellPassthrough } from './BriefSteps'

/** Шаг 6 — «Пишем вашу песню…». Опрос статуса ведёт FlowRoute. */
export function GeneratingStep({ shell, error, onRetry }: { shell: ShellPassthrough; error: string | null; onRetry: () => void }) {
  return (
    <StepShell {...shell} canGoBack={false} eyebrow="Готовим" hideNav>
      <div className="flex flex-col items-center py-10 text-center">
        {error ? (
          <>
            <p className="text-sm font-medium text-[#151210]">{error}</p>
            <button
              type="button"
              onClick={onRetry}
              className="mt-4 rounded-xl border border-[#E7DAC5] bg-white px-4 py-3 text-sm font-semibold text-[#8a6621] transition hover:bg-[#FCF6EA]"
            >
              Попробовать снова
            </button>
          </>
        ) : (
          <>
            <div className="relative flex h-16 w-16 items-center justify-center">
              <span className="absolute inset-0 animate-ping rounded-full opacity-30" style={{ background: GOLD }} />
              <Loader2 className="h-8 w-8 animate-spin text-[#B9852E]" />
            </div>
            <h2 className="mt-6 font-[Prata,serif] text-[22px] text-[#151210]">Пишем вашу песню…</h2>
            <p className="mt-2 text-sm text-[#6b6259]">Обычно занимает около минуты.</p>
          </>
        )}
      </div>
    </StepShell>
  )
}

/**
 * Шаг 7 — 15-секундный отрывок под замком.
 *
 * Играется отдельный объект с сервера, а не полный трек с остановкой на 15-й
 * секунде: во втором случае ссылка на всю песню видна в devtools, и замок не
 * запирает ничего.
 */
export function PreviewStep({
  shell,
  previewUrl,
  recipientName,
  occasion,
  onUnlock,
}: {
  shell: ShellPassthrough
  previewUrl: string | null
  recipientName: string
  occasion: string
  onUnlock: () => void
}) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)

  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return
    const onEnded = () => setPlaying(false)
    audio.addEventListener('ended', onEnded)
    return () => audio.removeEventListener('ended', onEnded)
  }, [previewUrl])

  const toggle = () => {
    const audio = audioRef.current
    if (!audio) return
    if (playing) {
      audio.pause()
      setPlaying(false)
      return
    }
    void audio.play().then(() => setPlaying(true)).catch(() => setPlaying(false))
  }

  const title = [recipientName && `Для ${recipientName}`, occasion].filter(Boolean).join(' — ')

  return (
    <StepShell
      {...shell}
      canGoBack={false}
      eyebrow="Готово"
      headline="Ваша песня готова к превью."
      primaryLabel="Открыть полностью"
      onPrimary={onUnlock}
    >
      <div className="rounded-2xl bg-[#151210] p-4">
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={toggle}
            disabled={!previewUrl}
            aria-label={playing ? 'Пауза' : 'Слушать отрывок'}
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full text-[#151210] transition hover:opacity-90 active:scale-95 disabled:opacity-40"
            style={{ background: GOLD }}
          >
            {playing ? <Pause className="h-4 w-4" fill="currentColor" /> : <Play className="ml-0.5 h-4 w-4" fill="currentColor" />}
          </button>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-bold text-white">{title || 'Ваша песня'}</p>
            <p className="text-xs text-white/60">Отрывок 15 секунд</p>
          </div>
          <span className="flex shrink-0 items-center gap-1 rounded-full bg-white/10 px-2 py-1 text-[10px] font-bold uppercase tracking-wider" style={{ color: GOLD }}>
            <Lock className="h-3 w-3" /> закрыто
          </span>
          {previewUrl && <audio ref={audioRef} src={previewUrl} preload="none" />}
        </div>
      </div>

      <p className="mt-4 text-xs leading-relaxed text-[#9c9186]">
        Делимся 15-секундным отрывком — самый трогательный момент трека.
      </p>
    </StepShell>
  )
}

/** «2 часа», «3 часа», «5 часов» — для копирайта ускорения. */
function hoursText(n: number) {
  const mod10 = n % 10
  const mod100 = n % 100
  if (mod10 === 1 && mod100 !== 11) return `${n} час`
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return `${n} часа`
  return `${n} часов`
}

/**
 * Шаг 8 — апсейл ускорения. Цена приходит с сервера и там же прибавляется к
 * заказу: сумма платежа считается по base_price, из браузера её не назначить.
 *
 * Шаг показывается только когда задержка обычной доставки включена — иначе
 * ускорять нечего (сервер такой шаг во флоу и не присылает).
 */
export function SpeedupStep({
  shell,
  price,
  basePrice,
  delayHours,
  busy,
  onAccept,
  onSkip,
}: {
  shell: ShellPassthrough
  price: number
  basePrice: number
  delayHours: number
  busy: boolean
  onAccept: () => void
  onSkip: () => void
}) {
  return (
    <StepShell
      {...shell}
      canGoBack={false}
      busy={busy}
      eyebrow="Перед оплатой"
      headline="Хотите быстрее?"
      subhead={`Обычно песня готова за ${hoursText(delayHours)}. Добавьте ускорение — и получите её за 10–30 минут.`}
      primaryLabel="Добавить и продолжить"
      onPrimary={onAccept}
      secondaryLabel="Не нужно"
      onSecondary={onSkip}
    >
      <div className="rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4">
        <div className="flex items-center gap-3">
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full" style={{ background: GOLD }}>
            <Zap className="h-5 w-5 text-[#151210]" />
          </span>
          <div>
            <div className="text-sm font-bold text-[#151210]">+{price} ₽</div>
            <div className="text-xs text-[#6b6259]">итого {basePrice + price} ₽</div>
          </div>
        </div>
      </div>
    </StepShell>
  )
}
