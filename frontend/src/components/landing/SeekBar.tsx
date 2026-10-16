import { useRef } from 'react'

// Полоса прогресса с перемоткой: pointer events покрывают и мышь, и палец
// (touch-action: none — чтобы drag по полосе не скроллил страницу).
export function SeekBar({ progressPct, onSeek, disabled, trackColor, fillColor, barClassName = 'h-1' }: {
  progressPct: number
  onSeek: (pct: number) => void
  disabled?: boolean
  trackColor: string
  fillColor: string
  barClassName?: string
}) {
  const draggingRef = useRef(false)

  const seekFromPointer = (e: React.PointerEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const pct = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
    onSeek(pct)
  }

  return (
    <div
      className={`flex-1 py-2 -my-2 flex items-center ${disabled ? '' : 'cursor-pointer'}`}
      style={{ touchAction: 'none' }}
      onPointerDown={(e) => {
        if (disabled) return
        draggingRef.current = true
        try { e.currentTarget.setPointerCapture(e.pointerId) } catch { /* noop */ }
        seekFromPointer(e)
      }}
      onPointerMove={(e) => {
        if (draggingRef.current) seekFromPointer(e)
      }}
      onPointerUp={() => { draggingRef.current = false }}
      onPointerCancel={() => { draggingRef.current = false }}
    >
      <div className={`flex-1 ${barClassName} rounded-full overflow-hidden`} style={{ background: trackColor }}>
        <div
          className="h-full rounded-full"
          style={{ width: `${Math.min(100, Math.max(0, progressPct))}%`, background: fillColor }}
        />
      </div>
    </div>
  )
}
