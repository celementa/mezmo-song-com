import { memo, useEffect, useRef, useState } from 'react'
import { Timer } from 'lucide-react'

/**
 * Self-contained countdown timer that manages its own state.
 * Isolated so its 1-second ticks don't re-render the parent component.
 */
export const DraftCountdown = memo(function DraftCountdown({
  text,
  durationSeconds = 10 * 60,
}: {
  text: string
  durationSeconds?: number
}) {
  const [countdown, setCountdown] = useState(durationSeconds)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    setCountdown(durationSeconds)
    intervalRef.current = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          if (intervalRef.current) clearInterval(intervalRef.current)
          return 0
        }
        return prev - 1
      })
    }, 1000)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [durationSeconds])

  const formatted = `${Math.floor(countdown / 60)}:${String(countdown % 60).padStart(2, '0')}`

  return (
    <div className="text-center text-[10px] sm:text-xs text-gray-500 flex justify-center items-center gap-1">
      <Timer className="h-3 w-3" /> {text.replace('{timer}', formatted)}
    </div>
  )
})
