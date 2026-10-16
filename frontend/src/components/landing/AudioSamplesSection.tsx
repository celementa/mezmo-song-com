import { useState, useEffect, useRef, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Play, Pause, Music, SkipBack, SkipForward } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { resolveApiUrl } from '@/lib/http'

const LANDING_AUDIO_EVENT = 'landing-audio-playing'
const SOURCE_SAMPLES = 'samples'
const SOURCE_HERO = 'hero'

export type TrackItem = {
  title: string
  subtitle: string
  quote: string
  time: string
  audioUrl: string | null
  thumbnailUrl?: string | null
}

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)(?!span\b)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

/* ── Track thumbnail (fallback to music icon) ── */
function TrackThumb({ src, size = 'md' }: { src?: string | null; size?: 'sm' | 'md' }) {
  const dim = size === 'sm' ? 'h-12 w-12' : 'h-16 w-16'
  if (src?.trim()) {
    return <img src={resolveApiUrl(src.trim())} alt="" className={`${dim} shrink-0 rounded-lg object-cover`} loading="lazy" />
  }
  return (
    <div className={`${dim} shrink-0 rounded-lg bg-white/10 flex items-center justify-center`}>
      <Music className={size === 'sm' ? 'h-5 w-5 text-white/40' : 'h-6 w-6 text-white/40'} />
    </div>
  )
}

/* ── Featured Player ── */
function FeaturedAudioPlayer({
  track,
  allTracks,
  selectedIndex,
  onSelectTrack,
  verifiedBadgeText: _verifiedBadgeText = 'Проверено',
  autoPlay,
  onAutoPlayDone,
}: {
  track: TrackItem
  allTracks: TrackItem[]
  selectedIndex: number
  onSelectTrack: (index: number) => void
  verifiedBadgeText?: string
  autoPlay?: boolean
  onAutoPlayDone?: () => void
}) {
  const [playing, setPlaying] = useState(false)
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [posSec, setPosSec] = useState(0)
  const [durSec, setDurSec] = useState<number | null>(null)
  const currentIndex = selectedIndex ?? 0

  useEffect(() => {
    setPlaying(false)
    setPosSec(0)
    setDurSec(null)
  }, [selectedIndex])

  useEffect(() => {
    if (audioRef.current) {
      audioRef.current.pause()
      audioRef.current.currentTime = 0
    }
  }, [track.audioUrl])

  const progressPct = useMemo(() => {
    const dur = durSec && durSec > 0 ? durSec : null
    if (!dur) return 0
    return Math.min(100, Math.max(0, (posSec / dur) * 100))
  }, [durSec, posSec])

  function fmt(sec: number) {
    const s = Math.max(0, Math.floor(sec))
    const mm = String(Math.floor(s / 60)).padStart(1, '0')
    const ss = String(s % 60).padStart(2, '0')
    return `${mm}:${ss}`
  }

  const hasAudio = Boolean(track.audioUrl)

  useEffect(() => {
    if (autoPlay && hasAudio && audioRef.current) {
      audioRef.current.play().then(() => setPlaying(true)).catch(() => {})
      onAutoPlayDone?.()
    }
  }, [autoPlay, hasAudio, onAutoPlayDone])

  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ source: string }>).detail
      if (detail?.source === SOURCE_HERO) {
        const a = audioRef.current
        if (a) a.pause()
        setPlaying(false)
      }
    }
    window.addEventListener(LANDING_AUDIO_EVENT, handler)
    return () => window.removeEventListener(LANDING_AUDIO_EVENT, handler)
  }, [])

  const dispatchPlaying = () => {
    window.dispatchEvent(new CustomEvent(LANDING_AUDIO_EVENT, { detail: { source: SOURCE_SAMPLES } }))
  }

  const handlePlayPause = () => {
    if (!hasAudio) return
    const a = audioRef.current
    if (!a) return
    if (playing) {
      a.pause()
      setPlaying(false)
    } else {
      a.play().then(() => setPlaying(true)).catch(() => setPlaying(false))
    }
  }

  const handlePrev = () => { if (currentIndex > 0) onSelectTrack(currentIndex - 1) }
  const handleNext = () => { if (currentIndex < allTracks.length - 1) onSelectTrack(currentIndex + 1) }

  return (
    <div className="space-y-4">
      {/* Track info */}
      <div className="flex items-start gap-4">
        <TrackThumb src={track.thumbnailUrl} size="md" />
        <div className="flex-1 min-w-0">
          <div className="text-white/60 text-xs font-bold uppercase tracking-wider">{track.subtitle}</div>
          <h3 className="font-serif text-xl font-bold text-white italic mt-0.5">{track.title}</h3>
          {track.quote && (
            <p className="text-sm text-white/50 italic mt-1 line-clamp-2">"{track.quote}"</p>
          )}
        </div>
      </div>

      {/* Progress bar */}
      <div className="space-y-1">
        <div
          className="h-3 w-full flex items-center cursor-pointer group"
          onClick={(e) => {
            if (!durSec || !audioRef.current) return
            const rect = e.currentTarget.getBoundingClientRect()
            const pct = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
            audioRef.current.currentTime = pct * durSec
            setPosSec(pct * durSec)
          }}
        >
          <div className="h-1 group-hover:h-2 w-full overflow-hidden rounded-full bg-white/20 transition-all duration-150">
            <div
              className="h-full bg-[var(--theme-accent)] transition-all duration-150"
              style={{ width: `${progressPct}%` }}
            />
          </div>
        </div>
        <div className="flex justify-between text-xs text-white/40 font-mono">
          <span>{fmt(posSec)}</span>
          <span>{durSec ? fmt(durSec) : track.time}</span>
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center justify-center gap-6">
        <button type="button" onClick={handlePrev} disabled={currentIndex <= 0}
          className="p-2 text-white/50 hover:text-white disabled:opacity-30 transition-colors" aria-label="Previous">
          <SkipBack className="h-5 w-5" />
        </button>
        <button type="button" onClick={handlePlayPause} disabled={!hasAudio}
          className={cn(
            'flex h-14 w-14 items-center justify-center rounded-full transition-transform',
            hasAudio ? 'bg-[var(--theme-accent)] text-[var(--theme-button-text)] hover:opacity-90 active:scale-95' : 'bg-white/20 text-white/40 cursor-not-allowed'
          )} aria-label={playing ? 'Pause' : 'Play'}>
          {playing ? <Pause className="h-6 w-6" fill="currentColor" /> : <Play className="h-6 w-6 ml-1" fill="currentColor" />}
        </button>
        <button type="button" onClick={handleNext} disabled={currentIndex >= allTracks.length - 1}
          className="p-2 text-white/50 hover:text-white disabled:opacity-30 transition-colors" aria-label="Next">
          <SkipForward className="h-5 w-5" />
        </button>
      </div>

      {track.audioUrl && (
        <audio
          ref={(el) => { audioRef.current = el }}
          src={track.audioUrl}
          preload="none"
          onTimeUpdate={(e) => setPosSec(e.currentTarget.currentTime || 0)}
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

/* ── Main Section ── */
type Props = {
  allTracks: TrackItem[]
  selectedTrackIndex: number
  setSelectedTrackIndex: (i: number) => void
  autoPlayOnSelect: boolean
  setAutoPlayOnSelect: (v: boolean) => void
  heroVerifiedBadgeText: string
  fmtCurrency: (v: number | null | undefined) => string
  paymentAmount: number | null
  originalAmount: number | null
  showPriceInButton?: boolean
  themeSlug: string | null
  sectionBadge?: string
  sectionHeadline?: string
  sectionSubtext?: string
  otherLabel?: string
  ctaLine?: string
  ctaButtonText?: string
  backgroundColor?: string
  sectionClassName?: string
}

export function AudioSamplesSection({
  allTracks,
  selectedTrackIndex,
  setSelectedTrackIndex,
  autoPlayOnSelect,
  setAutoPlayOnSelect,
  heroVerifiedBadgeText,
  fmtCurrency,
  paymentAmount,
  originalAmount,
  showPriceInButton = true,
  themeSlug,
  sectionBadge: _sectionBadge = 'Нажми «Играть»',
  sectionHeadline =
    'Добавьте <span class="text-white italic">имя</span> вашего близкого в песню',
  sectionSubtext: _sectionSubtext = 'Наши аранжировки, ваши имена, живые эмоции.',
  otherLabel: _otherLabel = 'Ещё примеры',
  ctaLine: _ctaLine = 'Представьте его/её имя в такой песне…',
  ctaButtonText = 'Создать свою песню',
  backgroundColor = '#030712',
  sectionClassName,
}: Props) {
  const currentTrack = allTracks[selectedTrackIndex] ?? allTracks[0]

  return (
    <section
      aria-labelledby="audio-samples-title"
      className={cn("relative overflow-hidden -mx-7 sm:mx-0 sm:rounded-3xl", sectionClassName)}
      style={{ backgroundColor }}
    >
      <div className="px-6 py-12 sm:px-10 sm:py-16 max-w-2xl mx-auto">
        {/* Headline */}
        <div className="text-center mb-10">
          <h2
            id="audio-samples-title"
            className="font-serif text-3xl sm:text-4xl font-bold text-white leading-tight"
            dangerouslySetInnerHTML={{ __html: sanitizeHtml(sectionHeadline) }}
          />
        </div>

        {/* Player card */}
        <div className="bg-white/5 rounded-2xl p-5 sm:p-6 border border-white/10">
          <FeaturedAudioPlayer
            track={currentTrack}
            allTracks={allTracks}
            selectedIndex={selectedTrackIndex}
            onSelectTrack={(i) => {
              setSelectedTrackIndex(i)
              const t = allTracks[i]
              if (t?.audioUrl) setAutoPlayOnSelect(true)
            }}
            verifiedBadgeText={heroVerifiedBadgeText}
            autoPlay={autoPlayOnSelect}
            onAutoPlayDone={() => setAutoPlayOnSelect(false)}
          />
        </div>

        {/* Playlist */}
        <div className="mt-4 divide-y divide-white/10">
          {allTracks.map((t, i) => (
            <button
              key={i}
              type="button"
              onClick={() => {
                setSelectedTrackIndex(i)
                if (t.audioUrl) setAutoPlayOnSelect(true)
              }}
              className={cn(
                'flex w-full items-center gap-3 py-3 px-2 rounded-lg transition-colors text-left',
                selectedTrackIndex === i ? 'bg-white/10' : 'hover:bg-white/5',
                t.audioUrl ? 'cursor-pointer' : 'opacity-40 cursor-not-allowed'
              )}
            >
              <TrackThumb src={t.thumbnailUrl} size="sm" />
              <div className="flex-1 min-w-0">
                <div className="font-bold text-sm text-white">
                  {t.title}
                </div>
                <div className="text-xs text-white/40 mt-0.5">{t.subtitle}</div>
              </div>
              <div className="text-xs text-white/40 shrink-0 font-mono">{t.time}</div>
            </button>
          ))}
        </div>

        {/* CTA */}
        <div className="mt-8 text-center">
          <Button asChild size="lg" className="w-full rounded-full bg-[var(--theme-accent)] hover:bg-[var(--theme-accent)] text-[var(--theme-button-text)] text-sm sm:text-base font-bold uppercase tracking-wider shadow-lg hover:opacity-90 h-auto py-3">
            <Link data-landing-cta to={themeSlug ? `/${themeSlug}/config` : '/config'} className="flex flex-col items-center justify-center gap-0.5 px-4 w-full">
              <span className="leading-tight">{ctaButtonText}{showPriceInButton ? ` — ${fmtCurrency(paymentAmount)}` : ''}</span>
              {showPriceInButton && originalAmount && originalAmount > (paymentAmount ?? 0) && (
                <span className="text-xs font-normal line-through opacity-60 normal-case tracking-normal">{fmtCurrency(originalAmount)}</span>
              )}
            </Link>
          </Button>
        </div>
      </div>
    </section>
  )
}
