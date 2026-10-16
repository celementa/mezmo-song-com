import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

type Props = {
  onClose: () => void
  videoUrl: string | null
  imageUrl: string
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

export function HeroPlayerModal({
  onClose,
  videoUrl,
  imageUrl,
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
  ctaButtonText = 'Создать свою песню',
  ctaLink = '/config',
}: Props) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const [finished, setFinished] = useState(false)

  const useVidAudio = Boolean(stopAfterOnePlay && useVideoAudio && videoUrl)

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  useEffect(() => {
    if (useVidAudio) {
      const v = videoRef.current
      if (!v) return
      v.muted = false
      v.loop = false
      v.currentTime = 0
      v.play()
        .then(() => setPlaying(true))
        .catch(() => {
          v.muted = true
          v.play().then(() => {
            v.muted = false
            setPlaying(true)
          }).catch(() => setPlaying(false))
        })
      return
    }
    // autoplay audio when modal opens (best-effort, may be blocked)
    if (!audioUrl) return
    const a = audioRef.current
    if (!a) return
    a.currentTime = 0
    a.play()
      .then(() => setPlaying(true))
      .catch(() => setPlaying(false))
  }, [audioUrl, useVidAudio])

  const handleAudioEnded = () => {
    setPlaying(false)
    if (stopAfterOnePlay) {
      setFinished(true)
      const v = videoRef.current
      if (v) v.pause()
    }
  }

  const handleVideoEnded = () => {
    if (useVidAudio) {
      setPlaying(false)
      setFinished(true)
    }
  }

  return (
    <div className="fixed inset-0 z-[60]">
      <button
        type="button"
        className="absolute inset-0 bg-black/60"
        aria-label="Close"
        onClick={onClose}
      />

      <div className="absolute inset-0 flex items-center justify-center p-4">
        <div className="w-full max-w-4xl">
          <div className="relative overflow-hidden rounded-3xl shadow-2xl bg-black">
            {/* Media */}
            <div className="relative aspect-[16/9] w-full">
              {videoUrl ? (
                <video
                  ref={(el) => { videoRef.current = el }}
                  className="h-full w-full object-cover"
                  src={videoUrl}
                  autoPlay
                  muted={!useVidAudio}
                  loop={!useVidAudio}
                  playsInline
                  onEnded={handleVideoEnded}
                />
              ) : (
                <img className="h-full w-full object-cover" src={imageUrl} alt="Hero media" />
              )}

              {finished ? (
                /* Finished state: dark overlay with CTA button */
                <div className="absolute inset-0 flex items-center justify-center">
                  <div className="absolute inset-0 bg-black/60" />
                  <Link
                    to={ctaLink}
                    className="relative z-10 rounded-full bg-[var(--theme-accent)] px-8 py-4 text-white text-lg sm:text-xl font-bold shadow-lg hover:scale-105 transition-transform"
                  >
                    {ctaButtonText}
                  </Link>
                </div>
              ) : (
                <>
                  {!useVidAudio && (
                    <>
                      {/* Top Left: Playing badge */}
                      <button
                        type="button"
                        onClick={() => {
                          if (!audioUrl) return
                          const a = audioRef.current
                          if (!a) return
                          if (playing) {
                            a.pause()
                            setPlaying(false)
                            return
                          }
                          a.play()
                            .then(() => setPlaying(true))
                            .catch(() => setPlaying(false))
                        }}
                        className="absolute left-3 top-3 sm:left-4 sm:top-4 rounded-full bg-emerald-500/90 px-3 py-1.5 sm:px-4 sm:py-2 text-white text-xs sm:text-sm font-semibold backdrop-blur"
                      >
                        {playing ? '▮▮ ' : '▶ '} {playingBadgeText}
                      </button>

                      {/* Top Right: Corner badge */}
                      <div className="absolute right-3 top-3 sm:right-4 sm:top-4 rounded-full bg-[var(--theme-accent)] px-3 py-1.5 sm:px-4 sm:py-2 text-white text-xs sm:text-sm font-semibold backdrop-blur">
                        {cornerBadgeText}
                      </div>

                      {/* Bottom content */}
                      <div className="absolute inset-x-0 bottom-0 p-4 sm:p-6">
                        <div className="relative flex items-end justify-between gap-4 sm:gap-6">
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

                      {/* Close */}
                      <button
                        type="button"
                        onClick={onClose}
                        className="absolute right-3 bottom-3 sm:right-4 sm:bottom-4 rounded-full bg-white/20 text-white h-8 w-8 sm:h-10 sm:w-10 flex items-center justify-center backdrop-blur hover:bg-white/30"
                        aria-label="Close"
                      >
                        ×
                      </button>
                    </>
                  )}
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {audioUrl ? (
        <audio
          ref={(el) => {
            audioRef.current = el
          }}
          src={audioUrl}
          preload="auto"
          loop={!stopAfterOnePlay}
          onEnded={handleAudioEnded}
          onPause={() => setPlaying(false)}
        />
      ) : null}
    </div>
  )
}
