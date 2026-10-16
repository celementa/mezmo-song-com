/**
 * Шаги вокруг готовой песни: генерация («Пишем вашу песню…»), прослушивание
 * с шарингом отрывка и email-capture.
 */

import { useEffect, useRef, useState } from 'react'
import { Check, Copy, Loader2, Pause, Play } from 'lucide-react'

import { apiGet } from '@/lib/http'
import { reportEngagement } from '@/lib/engagement'
import { GOLD, StepShell } from '../StepShell'
import { shellProps, type StepProps } from '../types'

const inputClass =
  'w-full rounded-xl border border-[#E7DAC5] bg-[#FDFBF7] px-4 py-3 text-sm text-[#151210] outline-none transition placeholder:text-[#b4a893] focus:border-[#d9b978] focus:bg-white'

type OrderContent = {
  orders?: Array<{ status?: string; tracks?: string[]; lyricsText?: string }>
  // Отрывок 15 секунд: единственное, что можно проиграть до оплаты.
  previewUrl?: string | null
}

/**
 * Шаг генерации: поллит контент заказа и уезжает вперёд сам, как только песня
 * готова. Кнопки «Далее» здесь нет — ждать нечего, кроме сервера.
 */
export function GeneratingStep(
  p: StepProps & { onReady: (tracks: string[], lyrics: string, previewUrl: string | null) => void },
) {
  const [elapsed, setElapsed] = useState(0)
  const doneRef = useRef(false)

  useEffect(() => {
    const timer = setInterval(() => setElapsed((v) => v + 1), 1000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    if (!p.order.orderId) return
    let stopped = false

    const poll = async () => {
      try {
        const res = await apiGet<OrderContent>(`/api/public/order/${encodeURIComponent(p.order.orderId)}/content`)
        const ready = res.orders?.find((o) => o.status === 'completed' && (o.tracks?.length ?? 0) > 0)
        if (ready && !stopped && !doneRef.current) {
          doneRef.current = true
          p.track(p.stepId, p.stepIndex, 'next', { waitedSeconds: elapsed })
          p.onReady(ready.tracks ?? [], ready.lyricsText ?? '', res.previewUrl ?? null)
        }
      } catch {
        /* сеть моргнула — следующий тик попробует снова */
      }
    }

    void poll()
    const id = setInterval(poll, 4000)
    return () => {
      stopped = true
      clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.order.orderId])

  return (
    <StepShell {...shellProps(p)} canGoBack={false} eyebrow="Готовим" hideNav>
      <div className="flex flex-col items-center py-10 text-center">
        <div className="relative flex h-16 w-16 items-center justify-center">
          <span className="absolute inset-0 animate-ping rounded-full opacity-30" style={{ background: GOLD }} />
          <Loader2 className="h-8 w-8 animate-spin text-[#B9852E]" />
        </div>
        <h2 className="mt-6 font-[Prata,serif] text-[22px] text-[#151210]">Пишем вашу песню…</h2>
        <p className="mt-2 text-sm text-[#6b6259]">Обычно занимает около минуты.</p>
        {/* Молчащий спиннер — худшее, что можно показать: генерация иногда
            затягивается, и человек не понимает, сломалось или нет. Текст
            меняется по времени ожидания; страница дожидается сама. */}
        {elapsed >= 90 && (
          <p className="mt-3 max-w-xs text-xs leading-relaxed text-[#8a8279]">
            {elapsed >= 300
              ? 'Иногда генерация занимает до 15 минут. Страница обновится сама — можно оставить её открытой.'
              : 'Чуть дольше обычного — страница обновится сама, ничего не потеряется.'}
          </p>
        )}
        <p className="mt-4 text-xs tabular-nums text-[#b4a893]">{elapsed} с</p>
      </div>
    </StepShell>
  )
}

/** Прослушивание: полный трек играет сразу, отрывок можно расшарить. */
/**
 * Шаг «слушайте». До оплаты играет 15-секундный отрывок с отдельной ручки, а не
 * полный трек: `handleAudioProxy` без оплаты отдаёт 404, поэтому плеер с полной
 * ссылкой на этом шаге просто молчал (локально это было незаметно — там заказ
 * оплачен с рождения). После оплаты играет сама песня.
 */
export function ListenStep(
  p: StepProps & { tracks: string[]; previewUrl: string | null; paid: boolean; shareUrl: string },
) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const [copied, setCopied] = useState(false)
  const full = p.tracks[0] ?? ''
  const track = p.paid ? full : (p.previewUrl ?? '')

  const toggle = () => {
    const el = audioRef.current
    if (!el) return
    if (playing) {
      el.pause()
    } else {
      void el.play().catch(() => setPlaying(false))
    }
  }

  const copyShare = async () => {
    try {
      await navigator.clipboard.writeText(p.shareUrl)
      setCopied(true)
      p.track(p.stepId, p.stepIndex, 'skip', { action: 'share_copied' })
      setTimeout(() => setCopied(false), 2000)
    } catch {
      /* буфер недоступен — ссылка всё равно видна в поле */
    }
  }

  return (
    <StepShell
      {...shellProps(p)}
      canGoBack={false}
      eyebrow="Слушайте"
      // Имя в заголовок не подставляем: в русском оно требует склонения
      // («про Анну», не «про Анна»), а надёжно склонять имена мы не умеем.
      headline="Вот она — это про них."
      primaryLabel="Далее"
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'next', { played: playing })
        p.onNext()
      }}
    >
      <div className="rounded-2xl bg-[#151210] p-4">
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={toggle}
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full text-[#151210] transition hover:opacity-90 active:scale-95"
            style={{ background: GOLD }}
            aria-label={playing ? 'Пауза' : 'Слушать'}
          >
            {playing ? <Pause className="h-4 w-4" fill="currentColor" /> : <Play className="ml-0.5 h-4 w-4" fill="currentColor" />}
          </button>
          <div className="min-w-0">
            <div className="truncate text-sm font-bold text-white">
              {p.form.recipientName ? `Для ${p.form.recipientName}` : 'Ваша песня'}
              {p.form.occasion ? ` — ${p.form.occasion}` : ''}
            </div>
            <div className="text-xs text-white/60">
              {p.paid ? 'Играет целиком, без ограничений' : 'Отрывок 15 секунд — полная версия после оплаты'}
            </div>
          </div>
        </div>
        {track ? (
          <audio
            ref={audioRef}
            src={track}
            preload="none"
            onPlay={() => {
              setPlaying(true)
              if (p.paid) reportEngagement(p.order.orderId, 'listen')
            }}
            onPause={() => setPlaying(false)}
            onEnded={() => setPlaying(false)}
            className="mt-3 w-full"
            controls
          />
        ) : (
          // Отрывка нет (хранилище недоступно, нарезка не удалась) — честнее
          // сказать это, чем показать плеер, который молчит.
          <p className="mt-3 text-xs leading-relaxed text-white/60">
            Отрывок готовим — полную песню пришлём на email после оплаты.
          </p>
        )}
      </div>

      <div className="mt-4 rounded-2xl border border-[#EFE4D2] bg-[#FDFBF7] p-4">
        <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">Поделиться отрывком</div>
        <p className="mt-1.5 text-xs leading-relaxed text-[#6b6259]">
          15 секунд самого трогательного момента — отправьте, чтобы удивить заранее.
        </p>
        <div className="mt-3 flex items-center gap-2">
          <input readOnly value={p.shareUrl} className={`${inputClass} text-xs`} onFocus={(e) => e.currentTarget.select()} />
          <button
            type="button"
            onClick={copyShare}
            className="inline-flex shrink-0 items-center gap-1.5 rounded-xl border border-[#E7DAC5] bg-white px-3 py-3 text-xs font-semibold text-[#8a6621] transition hover:bg-[#FCF6EA]"
          >
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            {copied ? 'Скопировано' : 'Копировать'}
          </button>
        </div>
      </div>
    </StepShell>
  )
}

/** Email-capture. В v1 идёт после прослушивания, в v2 открывает воронку. */
export function EmailStep(p: StepProps & { isFirstStep: boolean }) {
  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(p.form.email.trim())

  return (
    <StepShell
      {...shellProps(p)}
      eyebrow="Email"
      headline="Куда отправить песню?"
      subhead={p.isFirstStep ? 'Дальше — сразу оплата, а вопросы о песне зададим после.' : 'Пришлём песню на этот email.'}
      primaryLabel="Далее"
      primaryDisabled={!emailValid}
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'submit', { hasPhone: Boolean(p.form.phone.trim()) })
        p.onNext()
      }}
    >
      <label className="block">
        <span className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">Email</span>
        <input
          className={`${inputClass} mt-2`}
          value={p.form.email}
          onChange={(e) => p.setForm({ email: e.target.value })}
          placeholder="you@mail.ru"
          type="email"
          autoComplete="email"
          inputMode="email"
        />
      </label>
      <p className="mt-2 text-xs font-medium text-[#8a6621]">Не забудьте проверить папку со спамом!</p>

      <label className="mt-4 block">
        <span className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">
          Телефон <span className="ml-1 font-semibold normal-case tracking-normal text-[#c19a4f]">необязательно</span>
        </span>
        <input
          className={`${inputClass} mt-2`}
          value={p.form.phone}
          onChange={(e) => p.setForm({ phone: e.target.value })}
          placeholder="+7 900 123-45-67"
          type="tel"
          autoComplete="tel"
          inputMode="tel"
        />
      </label>

      <p className="mt-3 text-xs leading-relaxed text-[#9c9186]">
        Пришлём готовую песню и пару писем о заказе. Отписаться можно в один клик.
      </p>
    </StepShell>
  )
}
