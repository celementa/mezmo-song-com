import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { localePath, useTr } from '@/lib/lang'

const LANDING_AUDIO_EVENT = 'landing-audio-playing'
const SOURCE_HERO = 'hero'
const SOURCE_SAMPLES = 'samples'

type Props = {
  onClose: () => void
  videoRef: React.RefObject<HTMLVideoElement | null>
  videoUrl: string | null
  audioUrl: string | null
  playingBadgeText: string
  cornerBadgeText: string
  verifiedBadgeText: string
  quote: string
  authorName: string
  authorSubline: string
  authorAvatarUrl: string | null
  stopAfterOnePlay?: boolean
  useVideoAudio?: boolean
  ctaButtonText?: string
  ctaLink?: string
}

export function HeroPlayerInline({
  onClose,
  videoRef,
  videoUrl,
  audioUrl,
  playingBadgeText,
  cornerBadgeText,
  verifiedBadgeText,
  quote,
  authorName,
  authorSubline,
  authorAvatarUrl,
  stopAfterOnePlay,
  useVideoAudio,
  ctaButtonText: ctaButtonTextProp,
  ctaLink = '/config',
}: Props) {
  const tr = useTr()
  const ctaButtonText = ctaButtonTextProp ?? tr('Создать свою песню', 'Create your song')
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const [finished, setFinished] = useState(false)

  // When useVideoAudio is on, unmute and play the video with its own audio track
  const useVidAudio = Boolean(stopAfterOnePlay && useVideoAudio && videoUrl)

  const closeAndStop = () => {
    const a = audioRef.current
    if (a) a.pause()
    if (useVidAudio) {
      const v = videoRef.current
      if (v) { v.pause(); v.muted = true }
    }
    setPlaying(false)
    // Restore background video state
    const v = videoRef.current
    if (v) {
      v.muted = true
      v.loop = true
      v.currentTime = 0
      v.play().catch(() => {})
    }
    onClose()
  }

  useEffect(() => {
    const v = videoRef.current
    if (useVidAudio) {
      // Use the video element's own audio track
      if (!v) return
      v.muted = false
      v.loop = false
      v.currentTime = 0
      v.play()
        .then(() => setPlaying(true))
        .catch(() => {
          // If unmuted autoplay is blocked, try muted first then unmute
          v.muted = true
          v.play().then(() => {
            v.muted = false
            setPlaying(true)
          }).catch(() => setPlaying(false))
        })
      return
    }
    // best-effort autoplay after user-initiated click opened the player
    if (!audioUrl) return
    const a = audioRef.current
    if (!a) return
    a.currentTime = 0
    a.play()
      .then(() => setPlaying(true))
      .catch(() => setPlaying(false))
  }, [audioUrl, useVidAudio, videoRef])

  // Attach onended handler to the shared video element
  useEffect(() => {
    const v = videoRef.current
    if (!v || !useVidAudio) return
    const handleEnded = () => {
      setPlaying(false)
      setFinished(true)
    }
    v.addEventListener('ended', handleEnded)
    return () => v.removeEventListener('ended', handleEnded)
  }, [useVidAudio, videoRef])

  // Pause when samples player starts (only one audio at a time)
  useEffect(() => {
    const handler = (e: CustomEvent<{ source: string }>) => {
      if (e.detail?.source === SOURCE_SAMPLES) {
        const a = audioRef.current
        if (a) a.pause()
        setPlaying(false)
      }
    }
    window.addEventListener(LANDING_AUDIO_EVENT, handler as EventListener)
    return () => window.removeEventListener(LANDING_AUDIO_EVENT, handler as EventListener)
  }, [])

  const dispatchPlaying = () => {
    window.dispatchEvent(new CustomEvent(LANDING_AUDIO_EVENT, { detail: { source: SOURCE_HERO } }))
  }

  const handleAudioEnded = () => {
    setPlaying(false)
    if (stopAfterOnePlay) {
      setFinished(true)
      const v = videoRef.current
      if (v) v.pause()
    }
  }

  return (
    <div className="relative h-full w-full">
      {finished ? (
        /* Finished state: dark overlay with CTA button */
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60" />
          <Link
            to={localePath(ctaLink)}
            className="relative z-10 rounded-full bg-[var(--theme-accent)] px-8 py-4 text-white text-lg sm:text-xl font-bold shadow-lg hover:scale-105 transition-transform"
          >
            {ctaButtonText}
          </Link>
        </div>
      ) : (
        /* Active player overlay */
        <div
          className="absolute inset-0 cursor-pointer"
          onClick={() => closeAndStop()}
        >
          {!useVidAudio && (
            <>
              <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/30 to-transparent" />

              {/* Playing badge (toggle pause/play) */}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  if (!audioUrl) return
                  const a = audioRef.current
                  if (!a) return
                  if (playing) {
                    closeAndStop()
                    return
                  }
                  a.play()
                    .then(() => setPlaying(true))
                    .catch(() => setPlaying(false))
                }}
                className="absolute left-auto right-3 top-3 sm:right-4 sm:top-4 md:right-auto md:left-4 rounded-full bg-emerald-500/90 px-3 py-1.5 sm:px-4 sm:py-2 text-white text-xs sm:text-sm font-semibold backdrop-blur"
              >
                {playing ? '▮▮ ' : '▶ '} {playingBadgeText}
              </button>

              {/* Corner badge — hidden on mobile to make room for logo */}
              <div className="hidden md:block absolute right-3 top-3 sm:right-4 sm:top-4 rounded-full bg-[var(--theme-accent)] px-3 py-1.5 sm:px-4 sm:py-2 text-white text-xs sm:text-sm font-semibold backdrop-blur">
                {cornerBadgeText}
              </div>

              {/* Close */}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  closeAndStop()
                }}
                className="absolute right-3 bottom-3 sm:right-4 sm:bottom-4 rounded-full bg-white/20 text-white h-8 w-8 sm:h-10 sm:w-10 flex items-center justify-center backdrop-blur hover:bg-white/30"
                aria-label="Tutup pemutar"
              >
                ×
              </button>

              {/* Bottom content */}
              <div className="absolute inset-x-0 bottom-0 p-4 sm:p-6">
                <div className="flex items-end justify-between gap-4 sm:gap-6">
                  <div className="max-w-[75%] sm:max-w-[70%] text-white">
                    <div className="font-serif italic text-xl sm:text-3xl leading-tight">"{quote}"</div>
                    <div className="mt-2 sm:mt-4 flex items-center gap-2 sm:gap-3">
                      {authorAvatarUrl ? (
                        <img src={authorAvatarUrl} alt="" className="h-8 w-8 sm:h-10 sm:w-10 rounded-full border border-white/60" loading="lazy" width="40" height="40" />
                      ) : (
                        <div className="h-8 w-8 sm:h-10 sm:w-10 rounded-full bg-white/20 border border-white/60" />
                      )}
                      <div>
                        <div className="font-bold text-sm sm:text-base">{authorName}</div>
                        {authorSubline ? <div className="text-white/75 text-xs sm:text-sm">{authorSubline}</div> : null}
                      </div>
                    </div>
                  </div>

                  <div className="shrink-0">
                    <div className="rounded-xl bg-white/90 px-3 py-1.5 sm:px-4 sm:py-2 text-emerald-700 text-xs sm:text-sm font-semibold">
                      ✓ <span className="hidden sm:inline">{verifiedBadgeText}</span><span className="sm:hidden">Verified</span>
                    </div>
                  </div>
                </div>
              </div>
            </>
          )}
        </div>
      )}

      {audioUrl ? (
        <audio
          ref={(el) => {
            audioRef.current = el
          }}
          src={audioUrl}
          preload="auto"
          loop={!stopAfterOnePlay}
          onPlay={dispatchPlaying}
          onEnded={handleAudioEnded}
          onPause={() => setPlaying(false)}
        />
      ) : null}
    </div>
  )
}
