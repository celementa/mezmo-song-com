import { lazy, Suspense, useState, useRef, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { ShieldCheck, Zap, Play, Pause } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Loose } from '@/lib/loose'
import { ActivityToast } from '@/components/activity-toast/ActivityToast'
import { CountdownTimer } from '@/components/landing/CountdownTimer'
import type { LandingData } from '@/hooks/useLandingData'
import type { TrackItem } from '@/components/landing/AudioSamplesSection'
import type { TemplateDef } from './types'

const ReviewsSection = lazy(() =>
  import('@/components/landing/ReviewsSection').then((m) => ({ default: m.ReviewsSection }))
)

const AudioSamplesSection = lazy(() =>
  import('@/components/landing/AudioSamplesSection').then((m) => ({ default: m.AudioSamplesSection }))
)
const FaqSection = lazy(() =>
  import('@/components/landing/FaqSection').then((m) => ({ default: m.FaqSection }))
)
const FooterCtaSection = lazy(() =>
  import('@/components/landing/FooterCtaSection').then((m) => ({ default: m.FooterCtaSection }))
)
const LandingFooter = lazy(() =>
  import('@/components/landing/LandingFooter').then((m) => ({ default: m.LandingFooter }))
)

const GRACE_SECTION_ORDER = ['howItWorks', 'reviews', 'audioSamples', 'faq', 'footerCta'] as const

// Audio event coordination
const LANDING_AUDIO_EVENT = 'landing-audio-playing'
const SOURCE_GRACE_HERO = 'grace-hero'

// Bottom-of-video audio player bar (like grace-melody's "Listen to an example" bar)
function GraceHeroAudioBar({ track }: { track: TrackItem | null }) {
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
      if (detail?.source !== SOURCE_GRACE_HERO) {
        audioRef.current?.pause()
        setPlaying(false)
      }
    }
    window.addEventListener(LANDING_AUDIO_EVENT, handler)
    return () => window.removeEventListener(LANDING_AUDIO_EVENT, handler)
  }, [])

  const handlePlayPause = () => {
    if (!track?.audioUrl) return
    const a = audioRef.current
    if (!a) return
    if (playing) {
      a.pause()
    } else {
      a.play()
        .then(() => {
          window.dispatchEvent(
            new CustomEvent(LANDING_AUDIO_EVENT, { detail: { source: SOURCE_GRACE_HERO } })
          )
        })
        .catch(() => {})
    }
  }

  const hasAudio = Boolean(track?.audioUrl)

  return (
    <div className="absolute bottom-0 inset-x-0 z-20 bg-gradient-to-t from-black/75 to-transparent pt-10 pb-4 px-4">
      <div className="flex items-center gap-3 max-w-xl">
        {/* Gold play/pause button */}
        <button
          type="button"
          onClick={handlePlayPause}
          disabled={!hasAudio}
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full text-[var(--theme-button-text)] hover:opacity-80 active:scale-95 transition-all disabled:opacity-40 shadow-lg"
          style={{ background: 'linear-gradient(135deg, #D4AF37, #B5952F)' }}
          aria-label={playing ? 'Пауза' : 'Воспроизвести'}
        >
          {playing
            ? <Pause className="h-5 w-5" fill="currentColor" />
            : <Play className="h-5 w-5 ml-0.5" fill="currentColor" />
          }
        </button>

        {/* Track info */}
        <div className="min-w-0 shrink-0">
          <div className="text-white font-bold text-sm leading-tight truncate max-w-[160px] sm:max-w-none">
            {track?.title ?? ''}
          </div>
        </div>

        {/* Progress bar + duration */}
        <div className="flex-1 flex items-center gap-2">
          <div
            className="flex-1 h-3 flex items-center cursor-pointer group"
            onClick={(e) => {
              if (!durSec || !audioRef.current) return
              const rect = e.currentTarget.getBoundingClientRect()
              const pct = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
              audioRef.current.currentTime = pct * durSec
              setPosSec(pct * durSec)
            }}
          >
            <div className="flex-1 h-0.5 group-hover:h-1 bg-white/30 rounded-full overflow-hidden transition-all duration-150">
              <div
                className="h-full bg-white rounded-full transition-all duration-150"
                style={{ width: `${progressPct}%` }}
              />
            </div>
          </div>
          <span className="text-white/70 text-xs font-mono shrink-0">
            {durSec ? fmt(posSec) : (track?.time ?? '3:24')}
          </span>
        </div>
      </div>

      {track?.audioUrl && (
        <audio
          ref={(el) => { audioRef.current = el }}
          src={track.audioUrl}
          preload="none"
          onTimeUpdate={(e) => setPosSec(e.currentTarget.currentTime)}
          onLoadedMetadata={(e) => {
            const d = e.currentTarget.duration
            setDurSec(Number.isFinite(d) && d > 0 ? d : null)
          }}
          onEnded={() => setPlaying(false)}
          onPause={() => setPlaying(false)}
          onPlay={() => setPlaying(true)}
          className="hidden"
        />
      )}
    </div>
  )
}

function GraceRender(d: LandingData) {
  const {
    themeSlug,
    themeStyle,
    logoUrl,
    fmtCurrency,
    paymentAmount,
    originalAmount,
    deliveryEta,
    showPrice,
    promoBanner,
    hero,
    miscText,
    sectionVisibility,
    sections,
    allTracks,
    toastConfig,
    selectedTrackIndex,
    setSelectedTrackIndex,
    autoPlayOnSelect,
    setAutoPlayOnSelect,
    showMobileCta,
    heroRef,
    heroVideoRef,
    videoReady,
    sanitizeHtml,
  } = d

  const featuredTrack = allTracks[1] ?? allTracks[0] ?? null

  // Autoplay hero video as soon as src is available
  useEffect(() => {
    if (!videoReady || !hero.videoUrl) return
    const v = heroVideoRef.current
    if (!v) return
    v.play().catch(() => {})
  }, [videoReady, hero.videoUrl])

  // Hide the sticky mobile CTA while an in-section CTA (tagged data-landing-cta)
  // is on screen — avoids two CTAs overloading one screen. A MutationObserver
  // re-attaches the IntersectionObserver to CTAs in lazily-mounted sections.
  const [sectionCtaVisible, setSectionCtaVisible] = useState(false)
  useEffect(() => {
    const visible = new Set<Element>()
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) visible.add(e.target)
          else visible.delete(e.target)
        }
        setSectionCtaVisible(visible.size > 0)
      },
      { rootMargin: '0px 0px -72px 0px' },
    )
    const observeAll = () => {
      document.querySelectorAll('[data-landing-cta]').forEach((el) => io.observe(el))
    }
    observeAll()
    const mo = new MutationObserver(observeAll)
    mo.observe(document.body, { childList: true, subtree: true })
    return () => {
      mo.disconnect()
      io.disconnect()
    }
  }, [])

  return (
    <div
      className="min-h-screen font-sans overflow-x-hidden"
      style={{ ...themeStyle, backgroundColor: 'var(--theme-accent-soft, #FDFBF7)' } as React.CSSProperties}
    >
      <ActivityToast config={toastConfig} />

      {/* Sticky promo banner only */}
      {promoBanner.enabled && (
        <div className="sticky top-0 z-50">
          <CountdownTimer
            promoBadgeText={promoBanner.promoBadgeText}
            countdownLabel={promoBanner.countdownLabel}
            countdownTargetDate={promoBanner.countdownTargetDate}
            evergreenEnabled={promoBanner.evergreenEnabled}
            evergreenCycleHours={promoBanner.evergreenCycleHours}
          />
        </div>
      )}

      {/* HERO */}
      <section ref={heroRef} aria-labelledby="hero-title" className="bg-gradient-to-b from-[var(--theme-accent-soft,#FDFBF7)] to-white pb-8 md:pb-12">

        {/* Top eyebrow */}
        <p className="text-center text-xs font-bold uppercase tracking-widest px-4 pt-4" style={{ color: '#B5952F' }}>
          Подарок, который трогает душу
        </p>

        {/* Video window — constrained like grace-melody */}
        <div className="px-4 sm:px-6 pt-4">
        <div
          className="relative w-full overflow-hidden bg-gray-900"
          style={{ aspectRatio: '16/9', maxWidth: '36rem', marginLeft: 'auto', marginRight: 'auto' }}
        >
          {/* Video */}
          {hero.videoUrl && (
            <video
              ref={heroVideoRef}
              className="absolute inset-0 h-full w-full object-cover"
              src={videoReady ? hero.videoUrl : undefined}
              poster={hero.imageUrl || undefined}
              muted
              loop
              playsInline
              preload="metadata"
            />
          )}

          {/* Top gradient for readability */}
          <div className="absolute inset-x-0 top-0 h-24 bg-gradient-to-b from-black/50 to-transparent z-10" />

          {/* Top overlay: logo (left) + CTA button (right) */}
          <div className="absolute top-0 inset-x-0 z-20 flex items-start justify-between p-4 sm:p-5">
            <span />
            <Link
              to={themeSlug ? `/${themeSlug}/config` : '/config'}
              onClick={(e) => e.stopPropagation()}
              className="px-4 py-2 rounded-full bg-[var(--theme-button)] text-[var(--theme-button-text)] text-xs sm:text-sm font-bold shadow-lg hover:opacity-90 transition-opacity whitespace-nowrap"
            >
              {miscText?.heroCtaButtonText || 'Создать свою песню'}
              {showPrice.header && paymentAmount ? ` — ${fmtCurrency(paymentAmount)}` : ''}
            </Link>
          </div>

          {/* Bottom: audio player bar */}
          <GraceHeroAudioBar track={featuredTrack} />

        </div>
        </div>{/* end video window wrapper */}

        {/* Below video: tagline + headline + content */}
        <div className="mx-auto max-w-3xl px-6 pt-6 pb-8 md:pt-8 md:pb-10 text-center space-y-4">

          {/* Tagline/badge */}
          {(miscText?.heroTagline) && (
            <p className="text-xs font-bold uppercase tracking-widest" style={{ color: '#B5952F' }}>
              {miscText.heroTagline}
            </p>
          )}

          {/* Headline */}
          <h1
            id="hero-title"
            className="font-bold leading-tight tracking-tight text-[#1A1918]"
            style={{ fontSize: 'clamp(1.375rem, 6vw, 2.5rem)' }}
          >
            {hero.headlineLine1}{' '}
            <span
              className="font-serif italic"
              style={{
                background: 'linear-gradient(135deg,#D4AF37 0%,#B5952F 50%,#8E731F 100%)',
                WebkitBackgroundClip: 'text',
                WebkitTextFillColor: 'transparent',
                backgroundClip: 'text',
              }}
            >
              {hero.headlineLine2}
            </span>
          </h1>

          {/* Subtext */}
          <p
            className="text-base md:text-lg max-w-md mx-auto"
            style={{ color: 'rgba(26,25,24,0.6)' }}
            dangerouslySetInnerHTML={{ __html: sanitizeHtml(hero.subtext) }}
          />

          {/* Relationship chips */}
          <div className="space-y-2">
            <p className="text-xs font-bold uppercase tracking-widest" style={{ color: '#B5952F' }}>
              {miscText?.heroChipsHeadline || 'Для кого песня?'}
            </p>
            <div className="flex flex-wrap justify-center gap-2 max-w-[22rem] mx-auto">
              {hero.chips.map((chip, idx) => (
                <Link
                  key={idx}
                  to={`${themeSlug ? `/${themeSlug}` : ''}/config?rel=${encodeURIComponent(chip.value)}`}
                  className="inline-flex items-center gap-1 rounded-full bg-white font-semibold text-[#1A1918] hover:shadow-[0_4px_12px_rgba(212,175,55,0.2)] transition-all"
                  style={{ padding: '0.375rem 0.7rem', border: '2px solid rgba(26,25,24,0.15)', fontSize: '0.8125rem' }}
                  onMouseEnter={e => (e.currentTarget.style.borderColor = '#D4AF37')}
                  onMouseLeave={e => (e.currentTarget.style.borderColor = 'rgba(26,25,24,0.15)')}
                >
                  {chip.icon && <span>{chip.icon}</span>}
                  {chip.label}
                </Link>
              ))}
            </div>
          </div>

          {/* Main CTA button */}
          <Link
            to={themeSlug ? `/${themeSlug}/config` : '/config'}
            className="flex items-center justify-center gap-2 w-full max-w-[24rem] mx-auto text-white px-8 py-4 rounded-full font-bold text-sm uppercase tracking-wider shadow-xl hover:opacity-90 active:scale-95 transition-all"
            style={{ backgroundColor: '#1A1918' }}
          >
            <svg className="w-5 h-5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
            </svg>
            <span>Создать →</span>
          </Link>

          {/* Trust line */}
          <p className="text-xs font-medium" style={{ color: 'rgba(26,25,24,0.4)' }}>
            Написано живыми авторами · Доставим {deliveryEta.sentenceLower}
          </p>

          {/* Star rating */}
          <div className="flex items-center justify-center gap-2">
            <span className="text-sm font-semibold" style={{ color: '#D4AF37' }}>★★★★★</span>
            <span className="text-xs font-semibold" style={{ color: 'rgba(26,25,24,0.6)' }}>
              {(miscText?.heroStarLine || '2 847+ довольных клиентов').replace(/^[★\s]+/, '')}
            </span>
          </div>
        </div>
      </section>

      {/* Sections — grace-melody order: full-width color blocks */}
      {GRACE_SECTION_ORDER.map((id) => {
        if (sectionVisibility[id] === false) return null

        switch (id) {
          case 'howItWorks': {
            const steps: Array<{ icon?: string; title: string; desc?: string }> = (sections.howItWorks?.steps?.filter((s: Loose) => s?.title) as Array<{ icon?: string; title: string; desc?: string }>) ?? [
              { title: 'Расскажите историю', desc: '3 минуты на заполнение формы' },
              { title: 'Мы создаём песню', desc: 'Живые авторы + нейросеть' },
              { title: 'Доставляем на почту', desc: 'Менее чем за 24 часа' },
            ]
            return (
              <section key={id} className="py-12 md:py-16" style={{ backgroundColor: '#1A1918' }}>
                <div className="max-w-md mx-auto px-6">
                  <h2 className="font-sans text-2xl md:text-3xl font-bold text-white text-center mb-8">
                    {sections.howItWorks?.headline || 'Три шага к незабываемому моменту'}
                  </h2>
                  <div className="rounded-2xl overflow-hidden mb-8" style={{ background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.08)' }}>
                    {steps.map((s, i) => (
                      <div
                        key={i}
                        className="flex items-center gap-4 px-5 py-4"
                        style={i < steps.length - 1 ? { borderBottom: '1px solid rgba(255,255,255,0.06)' } : undefined}
                      >
                        <div className="w-10 h-10 rounded-full flex items-center justify-center shrink-0 font-bold text-sm" style={{ backgroundColor: '#D4AF37', color: '#1A1918' }}>
                          {i + 1}
                        </div>
                        <div>
                          <h3 className="font-bold text-white">{s.title}</h3>
                          {s.desc && <p className="text-xs" style={{ color: 'rgba(255,255,255,0.35)' }}>{s.desc}</p>}
                        </div>
                      </div>
                    ))}
                  </div>
                  <Link
                    data-landing-cta
                    to={themeSlug ? `/${themeSlug}/config` : '/config'}
                    className="flex items-center justify-center gap-2 w-full py-4 rounded-full font-bold text-sm uppercase tracking-wider shadow-lg transition-all active:scale-95 hover:opacity-90"
                    style={{ backgroundColor: '#D4AF37', color: '#1A1918' }}
                  >
                    <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                    </svg>
                    <span>{miscText?.ctaButtonText || 'Создать свою песню'} →</span>
                  </Link>
                </div>
              </section>
            )
          }

          case 'reviews':
            return (
              <div key={id} className="mx-auto max-w-4xl w-full px-6 sm:px-9 md:px-11 py-12 sm:py-20">
                <Suspense fallback={null}>
                  <ReviewsSection
                    sectionLabel={sections.reviews.sectionLabel}
                    sectionHeadline={sections.reviews.sectionHeadline}
                    sectionSubtext={sections.reviews.sectionSubtext}
                    items={sections.reviews.items}
                  />
                </Suspense>
              </div>
            )

          case 'audioSamples':
            return (
              <Suspense key={id} fallback={null}>
                <AudioSamplesSection
                  allTracks={allTracks}
                  selectedTrackIndex={selectedTrackIndex}
                  setSelectedTrackIndex={setSelectedTrackIndex}
                  autoPlayOnSelect={autoPlayOnSelect}
                  setAutoPlayOnSelect={setAutoPlayOnSelect}
                  heroVerifiedBadgeText={hero.player.verifiedBadgeText}
                  fmtCurrency={fmtCurrency}
                  paymentAmount={paymentAmount}
                  originalAmount={originalAmount}
                  showPriceInButton={showPrice.audioSamplesButton}
                  themeSlug={themeSlug}
                  sectionBadge={sections.audioSamples?.badge}
                  sectionHeadline={sections.audioSamples?.headline}
                  sectionSubtext={sections.audioSamples?.subtext}
                  otherLabel={sections.audioSamples?.otherLabel}
                  ctaLine={sections.audioSamples?.ctaLine}
                  ctaButtonText={sections.audioSamples?.ctaButtonText}
                  backgroundColor={sections.audioSamples?.backgroundColor}
                  sectionClassName="mx-0 sm:mx-0 rounded-none sm:rounded-none"
                />
              </Suspense>
            )

          case 'faq':
            return (
              <div key={id} className="mx-auto max-w-4xl w-full px-6 sm:px-9 md:px-11 py-12 sm:py-20">
                <Suspense fallback={null}>
                  <FaqSection
                    items={sections.faqItems}
                    sectionHeadline={sections.faq?.headline}
                  />
                </Suspense>
              </div>
            )

          case 'footerCta':
            return (
              <div key={id} className="mx-auto max-w-4xl w-full px-6 sm:px-9 md:px-11 py-8 sm:py-12">
                <Suspense fallback={null}>
                  <FooterCtaSection
                    headline={sections.footerCta.headline}
                    subtitle={sections.footerCta.subtitle}
                    fmtCurrency={fmtCurrency}
                    paymentAmount={paymentAmount}
                    originalAmount={originalAmount}
                    showPriceInButton={showPrice.footerCtaButton}
                    themeSlug={themeSlug}
                    securityBadge={sections.footerCta.securityBadge}
                    quotaLine={sections.footerCta.quotaLine}
                    ctaButtonText={miscText?.ctaButtonText}
                    showCtaButton={false}
                  />
                </Suspense>
              </div>
            )

          default:
            return null
        }
      })}

      <Suspense fallback={null}>
        <LandingFooter
          logoUrl={logoUrl}
          themeSlug={themeSlug}
          tagline={sections.footer?.tagline}
          companyName={sections.footer?.companyName}
          email={sections.footer?.email}
          phone={sections.footer?.phone}
          disclaimer={sections.footer?.disclaimer}
          copyrightLine={sections.footer?.copyrightLine}
          centered={true}
        />
      </Suspense>

      {/* Sticky mobile CTA */}
      <div
        className={cn(
          'md:hidden fixed inset-x-0 bottom-0 z-[999] bg-white border-t border-gray-100 p-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] shadow-[0_-4px_6px_-1px_rgba(0,0,0,0.05)] transition-transform duration-300 transform',
          showMobileCta && !sectionCtaVisible ? 'translate-y-0' : 'translate-y-full'
        )}
      >
        <div className="mx-auto max-w-md space-y-2">
          <div className="flex justify-center gap-4 text-[10px] font-bold text-gray-500 uppercase tracking-wide">
            <span className="flex items-center gap-1">
              <Zap className="h-3 w-3 text-green-500" />
              {deliveryEta.short}
            </span>
            <span className="flex items-center gap-1">
              <ShieldCheck className="h-3 w-3 text-green-500" />
              Гарантия
            </span>
          </div>
          <Button
            asChild
            size="lg"
            className="w-full h-14 rounded-full bg-[var(--theme-button)] hover:bg-[var(--theme-button)] text-[var(--theme-button-text)] text-sm sm:text-lg font-bold shadow-lg hover:opacity-90 active:scale-95 transition-all"
          >
            <Link
              to={themeSlug ? `/${themeSlug}/config` : '/config'}
              className="flex items-center justify-center gap-2 flex-wrap text-center leading-tight"
            >
              <span>
                {miscText?.ctaButtonText || 'Создать свою песню'}
                {showPrice.mobileStickyButton ? ` — ${fmtCurrency(paymentAmount)}` : ''}
              </span>
              {miscText?.mobileCtaQuotaBadge && (
                <Badge
                  variant="secondary"
                  className="bg-white/20 text-white hover:bg-white/30 text-xs whitespace-nowrap"
                >
                  {miscText.mobileCtaQuotaBadge}
                </Badge>
              )}
            </Link>
          </Button>
        </div>
      </div>
    </div>
  )
}

const grace: TemplateDef = {
  id: 'grace',
  name: 'Grace',
  Render: GraceRender,
}

export default grace
