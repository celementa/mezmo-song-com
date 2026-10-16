import { useState, useCallback, useEffect } from 'react'
import useEmblaCarousel from 'embla-carousel-react'
import { Play, ChevronLeft, ChevronRight } from 'lucide-react'
import { resolveApiUrl } from '@/lib/http'

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

type ReviewItem = {
  style?: 'accent' | 'dark-chat' | 'white' | 'video' | 'image'
  quote?: string
  chatMessages?: string[]
  authorName?: string
  authorMeta?: string
  authorAvatarUrl?: string
  videoUrl?: string
  videoThumbnailUrl?: string
  imageUrl?: string
}

type Props = {
  sectionLabel: string
  sectionHeadline: string
  sectionSubtext: string
  items: ReviewItem[]
}

/* ── Video Modal (9:16, autoplay, no controls) ── */
function VideoModal({ videoUrl, onClose }: { videoUrl: string; onClose: () => void }) {
  return (
    <div
      className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-4"
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-sm aspect-[9/16] rounded-2xl overflow-hidden bg-black"
        onClick={onClose}
      >
        <video
          src={resolveApiUrl(videoUrl)}
          className="w-full h-full object-cover pointer-events-none"
          autoPlay
          playsInline
          onEnded={onClose}
        />
        <button
          type="button"
          onClick={onClose}
          className="absolute top-3 right-3 w-8 h-8 rounded-full bg-black/50 text-white flex items-center justify-center text-lg"
        >
          ×
        </button>
      </div>
    </div>
  )
}

/* ── Dot indicators ── */
function DotButtons({ count, selected }: { count: number; selected: number }) {
  return (
    <div className="flex justify-center gap-2 mt-6">
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className={`h-2 rounded-full transition-all duration-300 ${
            i === selected ? 'w-6 bg-gray-800' : 'w-2 bg-gray-300'
          }`}
        />
      ))}
    </div>
  )
}

/* ── Video Card ── */
function VideoCard({ review, onPlay }: { review: ReviewItem; onPlay: () => void }) {
  return (
    <div className="flex flex-col gap-3 h-full">
      <button
        type="button"
        onClick={onPlay}
        className="relative aspect-[3/4] w-full rounded-2xl overflow-hidden group cursor-pointer"
      >
        <img
          src={resolveApiUrl(review.videoThumbnailUrl || '')}
          alt=""
          className="w-full h-full object-cover"
          loading="lazy"
        />
        <div className="absolute inset-0 bg-black/20 group-hover:bg-black/30 transition-colors flex items-center justify-center">
          <div className="w-14 h-14 rounded-full bg-white/90 flex items-center justify-center shadow-lg">
            <Play className="h-6 w-6 text-gray-800 ml-1" fill="currentColor" />
          </div>
        </div>
        {/* Song title bar at bottom */}
        {review.quote && (
          <div className="absolute bottom-0 inset-x-0 bg-gradient-to-t from-black/80 to-transparent p-4 pt-8">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-full bg-[var(--theme-accent)] flex items-center justify-center">
                <Play className="h-3 w-3 text-white ml-0.5" fill="currentColor" />
              </div>
              <div className="text-left">
                <div className="text-white font-bold text-sm truncate">{review.quote}</div>
                <div className="text-white/70 text-xs">{review.authorName}</div>
              </div>
            </div>
          </div>
        )}
      </button>
      {/* Author below */}
      <div className="flex items-center gap-2 px-1">
        {review.authorAvatarUrl && (
          <img src={resolveApiUrl(review.authorAvatarUrl)} alt="" className="w-8 h-8 rounded-full object-cover" loading="lazy" />
        )}
        <div>
          <div className="font-bold text-sm text-gray-900">{review.authorName}</div>
          {review.authorMeta && <div className="text-xs text-gray-500">{review.authorMeta}</div>}
        </div>
      </div>
    </div>
  )
}

/* ── Text Card ── */
function TextCard({ review }: { review: ReviewItem }) {
  if (review.style === 'dark-chat') {
    const msgs = review.chatMessages || []
    return (
      <div className="bg-gray-900 text-white p-6 rounded-2xl shadow-lg h-full flex flex-col">
        <div className="space-y-3 font-sans text-sm flex-1">
          {msgs.map((msg, mi) => (
            <div key={mi} className={mi === 0 ? 'bg-white/10 rounded-2xl rounded-tl-sm p-3 self-start max-w-[90%]' : 'bg-blue-600 rounded-2xl rounded-tr-sm p-3 self-end ml-auto max-w-[90%]'}>
              {msg}
            </div>
          ))}
        </div>
        <div className="mt-4 flex items-center gap-3 pt-4 border-t border-white/10">
          {review.authorAvatarUrl
            ? <img src={resolveApiUrl(review.authorAvatarUrl)} alt="" className="w-9 h-9 rounded-full border-2 border-white/50 object-cover" loading="lazy" />
            : <div className="w-9 h-9 rounded-full bg-white/20 text-white flex items-center justify-center font-bold text-sm shrink-0">{review.authorName?.charAt(0).toUpperCase()}</div>
          }
          <div>
            <div className="font-bold text-sm">{review.authorName}</div>
            <div className="text-xs opacity-80">{review.authorMeta}</div>
          </div>
        </div>
      </div>
    )
  }

  const isAccent = review.style === 'accent'
  const initial = review.authorName?.charAt(0).toUpperCase() ?? '?'

  return (
    <div className={`p-6 rounded-2xl shadow-sm h-full flex flex-col ${isAccent ? 'bg-[var(--theme-accent)] text-white border border-transparent' : 'bg-white border border-gray-100 text-gray-800'}`}>
      <div className={`text-lg font-bold mb-3 tracking-wider ${isAccent ? 'text-yellow-300' : 'text-gray-900'}`}>
        ★★★★★
      </div>
      <p className="font-medium text-base leading-relaxed flex-1 mb-4" dangerouslySetInnerHTML={{ __html: `"${sanitizeHtml(review.quote || '')}"` }} />
      <div className="flex items-center gap-3">
        {review.authorAvatarUrl ? (
          <img
            src={resolveApiUrl(review.authorAvatarUrl)}
            alt=""
            className={`w-9 h-9 rounded-full object-cover border-2 shrink-0 ${isAccent ? 'border-white/50' : 'border-gray-100'}`}
            loading="lazy"
          />
        ) : (
          <div className={`w-9 h-9 rounded-full flex items-center justify-center font-bold text-sm shrink-0 ${isAccent ? 'bg-white/20 text-white' : 'bg-gray-900 text-white'}`}>
            {initial}
          </div>
        )}
        <div>
          <div className={`font-bold text-sm ${isAccent ? '' : 'text-gray-900'}`}>{review.authorName}</div>
          <div className={`text-xs ${isAccent ? 'opacity-80' : 'text-gray-500'}`}>{review.authorMeta}</div>
        </div>
      </div>
    </div>
  )
}

/* ── Main Carousel Section ── */
export function ReviewsSection({ sectionLabel, sectionHeadline, sectionSubtext, items }: Props) {
  const [emblaRef, emblaApi] = useEmblaCarousel({
    align: 'start',
    slidesToScroll: 1,
    containScroll: 'trimSnaps',
  })
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [canScrollPrev, setCanScrollPrev] = useState(false)
  const [canScrollNext, setCanScrollNext] = useState(false)
  const [videoUrl, setVideoUrl] = useState<string | null>(null)

  const onSelect = useCallback(() => {
    if (!emblaApi) return
    setSelectedIndex(emblaApi.selectedScrollSnap())
    setCanScrollPrev(emblaApi.canScrollPrev())
    setCanScrollNext(emblaApi.canScrollNext())
  }, [emblaApi])

  useEffect(() => {
    if (!emblaApi) return
    onSelect()
    emblaApi.on('select', onSelect)
    emblaApi.on('reInit', onSelect)
    return () => {
      emblaApi.off('select', onSelect)
      emblaApi.off('reInit', onSelect)
    }
  }, [emblaApi, onSelect])

  return (
    <section className="space-y-8">
      {/* Header */}
      <div className="text-center space-y-2">
        <div className="text-[var(--theme-accent-text)] text-sm font-bold uppercase tracking-wider">{sectionLabel}</div>
        <h2 className="font-serif text-3xl sm:text-4xl font-bold text-gray-900" dangerouslySetInnerHTML={{ __html: sanitizeHtml(sectionHeadline) }} />
        <p className="text-gray-600">{sectionSubtext}</p>
      </div>

      {/* Desktop: 3-col grid */}
      <div className="hidden md:grid grid-cols-3 gap-4">
        {items.map((review, idx) => (
          <div key={idx} className="h-full">
            {review.style === 'video'
              ? <VideoCard review={review} onPlay={() => setVideoUrl(review.videoUrl || '')} />
              : <TextCard review={review} />
            }
          </div>
        ))}
      </div>

      {/* Mobile: carousel */}
      <div className="md:hidden">
        <div className="relative -mx-7 sm:-mx-9">
          <div className="overflow-hidden px-7 sm:px-9" ref={emblaRef}>
            <div className="flex gap-4">
              {items.map((review, idx) => (
                <div
                  key={idx}
                  className={`flex-none ${review.style === 'video' ? 'w-[260px] sm:w-[280px]' : 'w-[300px] sm:w-[340px]'}`}
                >
                  {review.style === 'video' ? (
                    <VideoCard review={review} onPlay={() => setVideoUrl(review.videoUrl || '')} />
                  ) : (
                    <TextCard review={review} />
                  )}
                </div>
              ))}
            </div>
          </div>

          {canScrollPrev && (
            <button
              type="button"
              onClick={() => emblaApi?.scrollPrev()}
              className="hidden sm:flex absolute left-0 top-1/2 -translate-y-1/2 -translate-x-4 w-10 h-10 rounded-full bg-white shadow-lg border items-center justify-center hover:bg-gray-50 transition-colors z-10"
            >
              <ChevronLeft className="h-5 w-5 text-gray-700" />
            </button>
          )}
          {canScrollNext && (
            <button
              type="button"
              onClick={() => emblaApi?.scrollNext()}
              className="hidden sm:flex absolute right-0 top-1/2 -translate-y-1/2 translate-x-4 w-10 h-10 rounded-full bg-white shadow-lg border items-center justify-center hover:bg-gray-50 transition-colors z-10"
            >
              <ChevronRight className="h-5 w-5 text-gray-700" />
            </button>
          )}
        </div>

        <DotButtons count={items.length} selected={selectedIndex} />
      </div>

      {/* Video Modal */}
      {videoUrl && <VideoModal videoUrl={videoUrl} onClose={() => setVideoUrl(null)} />}
    </section>
  )
}
