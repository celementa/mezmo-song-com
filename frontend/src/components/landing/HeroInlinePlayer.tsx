import { useState, useRef, useEffect, useMemo } from 'react'
import { Play, Pause, Music2 } from 'lucide-react'

const LANDING_AUDIO_EVENT = 'landing-audio-playing'
const SOURCE_HERO_INLINE = 'hero-inline'

type Props = {
  audioUrl: string | null
  trackTitle: string
  trackTime: string
}

export function HeroInlinePlayer({ audioUrl, trackTitle, trackTime }: Props) {
  const [playing, setPlaying] = useState(false)
  const [posSec, setPosSec] = useState(0)
  const [durSec, setDurSec] = useState<number | null>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  const progressPct = useMemo(() => {
    const dur = durSec && durSec > 0 ? durSec : null
    if (!dur) return 0
    return Math.min(100, (posSec / dur) * 100)
  }, [durSec, posSec])

  function fmt(sec: number) {
    const s = Math.max(0, Math.floor(sec))
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
  }

  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ source: string }>).detail
      if (detail?.source !== SOURCE_HERO_INLINE) {
        audioRef.current?.pause()
        setPlaying(false)
      }
    }
    window.addEventListener(LANDING_AUDIO_EVENT, handler)
    return () => window.removeEventListener(LANDING_AUDIO_EVENT, handler)
  }, [])

  const dispatchPlaying = () => {
    window.dispatchEvent(new CustomEvent(LANDING_AUDIO_EVENT, { detail: { source: SOURCE_HERO_INLINE } }))
  }

  const handlePlayPause = () => {
    if (!audioUrl) return
    const a = audioRef.current
    if (!a) return
    if (playing) {
      a.pause()
      setPlaying(false)
    } else {
      a.play().then(() => { setPlaying(true); dispatchPlaying() }).catch(() => {})
    }
  }

  const hasAudio = Boolean(audioUrl)

  return (
    <div className="flex items-center gap-3 bg-white rounded-2xl shadow-md border border-gray-100 px-4 py-3 w-full max-w-sm mx-auto">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-[var(--theme-accent-soft)]">
        <Music2 className="h-5 w-5 text-[var(--theme-accent-text)]" />
      </div>

      <div className="flex-1 min-w-0">
        <div className="font-bold text-sm text-gray-900 truncate">{trackTitle}</div>
        <div className="mt-1 h-1.5 w-full rounded-full bg-gray-100 overflow-hidden">
          <div
            className="h-full rounded-full bg-[var(--theme-accent)] transition-all duration-150"
            style={{ width: `${progressPct}%` }}
          />
        </div>
        <div className="mt-0.5 text-[10px] text-gray-400 font-mono">
          {playing ? fmt(posSec) : durSec ? fmt(durSec) : trackTime}
        </div>
      </div>

      <button
        type="button"
        onClick={handlePlayPause}
        disabled={!hasAudio}
        className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-[var(--theme-accent)] text-white shadow hover:opacity-90 active:scale-95 transition-all disabled:opacity-40 disabled:cursor-not-allowed"
        aria-label={playing ? 'Пауза' : 'Слушать пример'}
      >
        {playing
          ? <Pause className="h-4 w-4" fill="currentColor" />
          : <Play className="h-4 w-4 ml-0.5" fill="currentColor" />
        }
      </button>

      {audioUrl && (
        <audio
          ref={(el) => { audioRef.current = el }}
          src={audioUrl}
          preload="metadata"
          onTimeUpdate={(e) => setPosSec(e.currentTarget.currentTime)}
          onLoadedMetadata={(e) => {
            const d = e.currentTarget.duration
            setDurSec(Number.isFinite(d) && d > 0 ? d : null)
          }}
          onEnded={() => setPlaying(false)}
          onPause={() => setPlaying(false)}
          onPlay={() => { dispatchPlaying(); setPlaying(true) }}
          className="hidden"
        />
      )}
    </div>
  )
}
