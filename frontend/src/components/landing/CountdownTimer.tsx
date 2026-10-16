import { useEffect, useState } from 'react'

function getEvergreenTarget(cycleHours: number): Date {
  const SGT_OFFSET = 8 * 60
  const now = new Date()
  const utcMs = now.getTime()
  const sgtMs = utcMs + SGT_OFFSET * 60 * 1000
  const sgtDate = new Date(sgtMs)

  const cycleMs = cycleHours * 60 * 60 * 1000
  const sgtMidnight = new Date(Date.UTC(sgtDate.getUTCFullYear(), sgtDate.getUTCMonth(), sgtDate.getUTCDate()))
  const msSinceMidnight = sgtMs - sgtMidnight.getTime()
  const currentCycleStart = Math.floor(msSinceMidnight / cycleMs) * cycleMs
  const nextCycleEndSgt = sgtMidnight.getTime() + currentCycleStart + cycleMs

  return new Date(nextCycleEndSgt - SGT_OFFSET * 60 * 1000)
}

type Props = {
  /** @deprecated no longer rendered; kept for call-site compatibility */
  paymentAmount?: number | null
  /** @deprecated no longer rendered; kept for call-site compatibility */
  originalAmount?: number | null
  /** Optional bold uppercase badge text rendered before the countdownLabel, e.g. "Promo Spesial" */
  promoBadgeText?: string
  countdownLabel: string
  countdownTargetDate: string
  evergreenEnabled?: boolean
  evergreenCycleHours?: number
}

export function CountdownTimer({ promoBadgeText, countdownLabel, countdownTargetDate, evergreenEnabled, evergreenCycleHours }: Props) {
  const [time, setTime] = useState({ d: 0, h: 0, m: 0, s: 0 })

  useEffect(() => {
    const getTarget = (): Date => {
      if (evergreenEnabled && evergreenCycleHours && evergreenCycleHours > 0) {
        return getEvergreenTarget(evergreenCycleHours)
      }

      if (countdownTargetDate && /^\d{4}-\d{2}-\d{2}/.test(countdownTargetDate)) {
        return new Date(`${countdownTargetDate}T00:00:00`)
      }

      const now = new Date()
      let targetYear = now.getFullYear()
      if (now.getMonth() > 1 || (now.getMonth() === 1 && now.getDate() > 14)) {
        targetYear++
      }
      return new Date(`${targetYear}-02-14T00:00:00`)
    }

    const updateTimer = () => {
      const targetDate = getTarget()
      const now = new Date()
      const diff = targetDate.getTime() - now.getTime()

      if (diff <= 0) {
        if (evergreenEnabled && evergreenCycleHours && evergreenCycleHours > 0) {
          const nextTarget = getEvergreenTarget(evergreenCycleHours)
          const nextDiff = nextTarget.getTime() - now.getTime()
          if (nextDiff > 0) {
            const h = Math.floor(nextDiff / (1000 * 60 * 60))
            const m = Math.floor((nextDiff % (1000 * 60 * 60)) / (1000 * 60))
            const s = Math.floor((nextDiff % (1000 * 60)) / 1000)
            setTime({ d: 0, h, m, s })
            return
          }
        }
        setTime({ d: 0, h: 0, m: 0, s: 0 })
        return
      }

      const d = Math.floor(diff / (1000 * 60 * 60 * 24))
      const h = Math.floor((diff % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60))
      const m = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60))
      const s = Math.floor((diff % (1000 * 60)) / 1000)

      setTime({ d, h, m, s })
    }

    updateTimer()
    const timer = setInterval(updateTimer, 1000)
    return () => clearInterval(timer)
  }, [countdownTargetDate, evergreenEnabled, evergreenCycleHours])

  const showDays = !evergreenEnabled || time.d > 0
  const boxCls =
    'text-white font-bold text-xs px-1.5 py-0.5 rounded min-w-[1.75rem] text-center'
  const boxStyle = { backgroundColor: 'color-mix(in srgb, var(--theme-accent) 80%, black)' }

  return (
    <div className="px-3 py-2 text-white" style={{ backgroundColor: 'var(--theme-accent)' }}>
      <div className="mx-auto max-w-3xl flex items-center justify-center gap-2 text-sm whitespace-nowrap">
        {promoBadgeText && (
          <span className="font-bold uppercase tracking-wide text-[11px]">{promoBadgeText}</span>
        )}
        {countdownLabel && (
          <span className="text-[11px] opacity-90">{countdownLabel}</span>
        )}
        <div className="flex items-center gap-1">
          {showDays && (
            <>
              <span className={boxCls} style={boxStyle}>{String(time.d).padStart(2, '0')}</span>
              <span className="font-bold text-xs">:</span>
            </>
          )}
          <span className={boxCls} style={boxStyle}>{String(time.h).padStart(2, '0')}</span>
          <span className="font-bold text-xs">:</span>
          <span className={boxCls} style={boxStyle}>{String(time.m).padStart(2, '0')}</span>
          <span className="font-bold text-xs">:</span>
          <span className={boxCls} style={boxStyle}>{String(time.s).padStart(2, '0')}</span>
        </div>
      </div>
    </div>
  )
}
