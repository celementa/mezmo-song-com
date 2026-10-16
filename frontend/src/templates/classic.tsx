import { lazy, Suspense, type ComponentProps } from 'react'
import { Link } from 'react-router-dom'
import {
  Play,
  Check,
  ShieldCheck,
  Zap,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { ActivityToast } from '@/components/activity-toast/ActivityToast'
import { HeroPlayerInline } from '@/routes/HeroPlayerInline'
import { CountdownTimer } from '@/components/landing/CountdownTimer'
import type { LandingData } from '@/hooks/useLandingData'
import type { TemplateDef } from './types'

const AudioSamplesSection = lazy(() => import('@/components/landing/AudioSamplesSection').then(m => ({ default: m.AudioSamplesSection })))
const StatsBar = lazy(() => import('@/components/landing/StatsBar').then(m => ({ default: m.StatsBar })))
const ComparisonSection = lazy(() => import('@/components/landing/ComparisonSection').then(m => ({ default: m.ComparisonSection })))
const ReviewsSection = lazy(() => import('@/components/landing/ReviewsSection').then(m => ({ default: m.ReviewsSection })))
const HowItWorksSection = lazy(() => import('@/components/landing/HowItWorksSection').then(m => ({ default: m.HowItWorksSection })))
const GuaranteeSection = lazy(() => import('@/components/landing/GuaranteeSection').then(m => ({ default: m.GuaranteeSection })))
const FaqSection = lazy(() => import('@/components/landing/FaqSection').then(m => ({ default: m.FaqSection })))
const FooterCtaSection = lazy(() => import('@/components/landing/FooterCtaSection').then(m => ({ default: m.FooterCtaSection })))
const LandingFooter = lazy(() => import('@/components/landing/LandingFooter').then(m => ({ default: m.LandingFooter })))

function ClassicRender(d: LandingData) {
  const {
    themeSlug, themeStyle, logoUrl,
    fmtCurrency, paymentAmount, originalAmount, deliveryEta, showPrice,
    promoBanner, hero, miscText,
    sectionOrder, sectionVisibility,
    sections, allTracks, toastConfig,
    heroOpen, setHeroOpen,
    heroVideoPlaying, setHeroVideoPlaying,
    selectedTrackIndex, setSelectedTrackIndex,
    autoPlayOnSelect, setAutoPlayOnSelect,
    showMobileCta, videoReady,
    heroRef, heroVideoRef,
    sanitizeHtml,
  } = d

  const { headlineFont } = hero

  const sectionFallback = null

  return (
    <div className="min-h-screen bg-[var(--theme-accent-soft)] font-sans pb-32 overflow-x-hidden" style={themeStyle}>
      <ActivityToast config={toastConfig} />

      {/* Sticky Top Banner */}
      <div className="sticky top-0 z-50">
        {promoBanner.enabled && (
          <CountdownTimer promoBadgeText={promoBanner.promoBadgeText || 'Promo Spesial'} countdownLabel={promoBanner.countdownLabel} countdownTargetDate={promoBanner.countdownTargetDate} evergreenEnabled={promoBanner.evergreenEnabled} evergreenCycleHours={promoBanner.evergreenCycleHours} />
        )}
        <div className="hidden md:block border-b border-[var(--theme-accent-soft)] bg-white/95 px-7 sm:px-9 py-2 backdrop-blur-sm">
          <div className="mx-auto flex max-w-7xl items-center justify-between gap-2 sm:gap-3">
            <span />
            <div className="text-right flex flex-col items-end gap-0.5 sm:flex-row sm:items-center sm:gap-3">
              {promoBanner.promoBadgeText && (
                <div className="hidden md:block text-[10px] font-medium text-[var(--theme-accent-text)] bg-[var(--theme-accent-soft)] px-2 py-0.5 rounded-full">
                  {promoBanner.promoBadgeText}
                </div>
              )}
              <div className="leading-tight flex items-center gap-1.5">
                {showPrice.header && <span className="text-[10px] text-gray-400 line-through">{fmtCurrency(originalAmount)}</span>}
                {showPrice.header && <span className="text-sm sm:text-lg font-bold text-[var(--theme-accent-text)]">{fmtCurrency(paymentAmount)}</span>}
                {promoBanner.quotaBadgeText && (
                  <Badge variant="destructive" className="h-4 px-1 py-0 text-[9px]">
                    {promoBanner.quotaBadgeText}
                  </Badge>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>

      <main className="mx-auto max-w-7xl w-full px-7 sm:px-9 md:px-11 pt-0 sm:pt-12 space-y-8 sm:space-y-20">
        {/* HERO SECTION */}
        <section ref={heroRef} aria-labelledby="hero-title" className="grid grid-cols-1 md:grid-cols-2 gap-4 sm:gap-8 md:gap-12 items-start md:items-center min-h-[calc(100svh-5rem)] md:min-h-0 content-start md:content-center">
          <div className="text-center md:text-left space-y-3 md:space-y-6 md:col-start-1 md:row-start-1 order-2 md:order-none">
            {(miscText?.heroTagline) && (
              <p className="text-[11px] sm:text-xs font-bold uppercase tracking-widest text-gray-500">{miscText.heroTagline}</p>
            )}

            <h1 id="hero-title" className={`${headlineFont === 'serif' ? 'font-serif' : headlineFont === 'sans-serif' ? 'font-sans' : ''} text-3xl sm:text-5xl md:text-6xl lg:text-7xl font-bold leading-[1.1] tracking-tight`} style={headlineFont !== 'serif' && headlineFont !== 'sans-serif' ? { fontFamily: `'${headlineFont}', serif` } : undefined}>
              <span className="text-gray-900">{hero.headlineLine1}</span>
              <br className="hidden sm:inline" /> <span className="text-[var(--theme-accent-text)]">{hero.headlineLine2}</span>
            </h1>

            <p className="text-sm sm:text-lg text-gray-600 leading-normal max-w-lg mx-auto md:mx-0" dangerouslySetInnerHTML={{ __html: sanitizeHtml(hero.subtext) }} />
          </div>

          <div className="relative mx-auto w-full max-w-full sm:max-w-md md:max-w-full min-w-0 md:col-start-2 md:row-start-1 md:row-span-2 order-1 md:order-none">
             <div className="relative aspect-[16/9] sm:aspect-[4/3] w-[calc(100%+3.5rem)] sm:w-full overflow-hidden rounded-none sm:rounded-2xl shadow-2xl -mx-7 sm:mx-0">
               <div className="absolute inset-0 bg-gray-900/10 z-10"></div>
               <div className="absolute inset-x-0 top-0 h-1/4 bg-gradient-to-b from-black/40 to-transparent z-20 md:hidden" />
               {hero.videoUrl ? (
                 <>
                   <video
                     ref={heroVideoRef}
                     className="absolute inset-0 h-full w-full object-cover"
                     src={videoReady ? hero.videoUrl : undefined}
                     muted
                     loop
                     playsInline
                     preload="none"
                     onPlaying={() => {
                       setHeroVideoPlaying(true)
                       const poster = document.getElementById('hero-poster')
                       if (poster) poster.classList.add('opacity-0')
                     }}
                     onPause={() => {
                       setHeroVideoPlaying(false)
                       const poster = document.getElementById('hero-poster')
                       if (poster) poster.classList.remove('opacity-0')
                     }}
                   />
                   <img
                     src={hero.imageUrl}
                     alt="Пара с персональной песней — имя в тексте"
                     className="absolute inset-0 h-full w-full object-cover transition-opacity duration-700"
                     loading="eager"
                     fetchPriority="high"
                     decoding="async"
                     id="hero-poster"
                   />
                 </>
               ) : (
                 <img
                   src={hero.imageUrl}
                   alt="Пара с персональной песней — имя в тексте"
                   className="h-full w-full object-cover"
                   loading="eager"
                   fetchPriority="high"
                   decoding="async"
                 />
               )}
               {heroOpen && hero.player.enabled ? (
                 <div className="absolute inset-0 z-20">
                   <HeroPlayerInline
                     onClose={() => { setHeroOpen(false); heroVideoRef.current?.pause() }}
                     videoRef={heroVideoRef}
                     videoUrl={hero.videoUrl}
                     audioUrl={hero.player.audioUrl}
                     playingBadgeText={hero.player.playingBadgeText}
                     cornerBadgeText={hero.player.cornerBadgeText}
                     verifiedBadgeText={hero.player.verifiedBadgeText}
                     quote={hero.player.quote}
                     authorName={hero.player.authorName}
                     authorSubline={hero.player.authorSubline}
                     authorAvatarUrl={hero.player.authorAvatarUrl}
                     stopAfterOnePlay={hero.player.stopAfterOnePlay}
                     useVideoAudio={hero.player.useVideoAudio}
                     ctaButtonText={hero.player.stopCtaButtonText}
                     ctaLink={themeSlug ? `/${themeSlug}/config` : '/config'}
                   />
                 </div>
               ) : (
                 <div className="absolute inset-0 z-20 flex flex-col items-center justify-center text-white p-6 text-center" onClick={() => { const v = heroVideoRef.current; if (v && heroVideoPlaying) v.pause() }}>
                   {!heroVideoPlaying && (
                     <button
                       type="button"
                       onClick={(e) => {
                         e.stopPropagation()
                         const v = heroVideoRef.current
                         if (v) { v.play() } else if (hero.player.enabled) { setHeroOpen(true) }
                       }}
                       className="h-16 w-16 rounded-full bg-white/20 backdrop-blur-sm flex items-center justify-center mb-4 hover:scale-110 transition-transform"
                       aria-label="Play hero song"
                     >
                       <div className="h-12 w-12 rounded-full bg-[var(--theme-accent)] flex items-center justify-center pl-1 shadow-lg">
                         <Play className="h-6 w-6 text-white" fill="currentColor" />
                       </div>
                     </button>
                   )}
                   {!heroVideoPlaying && (
                     <>
                       <div className="font-serif italic text-lg sm:text-2xl md:text-3xl leading-snug">"{hero.overlay.quote}"</div>
                       <div className="mt-2 text-xs sm:text-sm font-medium opacity-90 flex items-center gap-2">
                         {hero.overlay.authorAvatarUrl ? (
                           <img src={hero.overlay.authorAvatarUrl} alt="" className="w-6 h-6 rounded-full border border-white" loading="lazy" width="24" height="24" />
                         ) : (
                           <div className="w-6 h-6 rounded-full border border-white bg-white/20" />
                         )}
                         {hero.overlay.authorLocation ? `${hero.overlay.authorName}, ${hero.overlay.authorLocation}` : hero.overlay.authorName}
                         <span className="bg-white/20 px-1.5 py-0.5 rounded text-[10px] font-bold flex items-center gap-0.5">
                           <Check className="w-3 h-3" /> Проверено
                         </span>
                       </div>
                     </>
                   )}
                 </div>
               )}
             </div>
          </div>

          <div className="text-center md:text-left space-y-4 md:space-y-6 md:col-start-1 md:row-start-2 order-3 md:order-none">
            <div className="space-y-2">
              <p className="text-sm font-semibold text-gray-700">{miscText?.heroChipsHeadline || 'Для кого песня?'}</p>
              <div className="flex flex-wrap justify-center md:justify-start gap-2">
              {hero.chips.map((chip, idx) => (
                <Link
                  key={idx}
                  to={`${themeSlug ? `/${themeSlug}` : ''}/config?rel=${encodeURIComponent(chip.value)}`}
                  className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full border border-gray-200 bg-white text-sm sm:text-lg font-semibold text-gray-700 hover:border-[var(--theme-accent)] hover:text-[var(--theme-accent-text)] hover:bg-[var(--theme-accent-soft)] transition-all shadow-sm"
                >
                  {chip.icon && <span>{chip.icon}</span>}
                  {chip.label}
                </Link>
              ))}
              </div>
            </div>

            <div className="space-y-2">
              <Button asChild size="lg" className="w-full sm:w-auto h-14 px-10 rounded-full bg-[var(--theme-button)] text-lg font-bold shadow-lg shadow-[var(--theme-accent-soft)] hover:opacity-90 transition-all">
                <Link to={themeSlug ? `/${themeSlug}/config` : '/config'} className="flex items-center justify-center gap-2">
                  {miscText?.heroCtaButtonText || 'Создать свою песню'}{showPrice.heroCtaButton ? ` — ${fmtCurrency(paymentAmount)}` : ''}
                  {showPrice.heroCtaButton && <span className="text-[var(--theme-accent-soft)] line-through font-normal text-sm sm:text-base ml-1">{fmtCurrency(originalAmount)}</span>}
                </Link>
              </Button>

              <div className="flex items-center justify-center md:justify-start gap-2 text-[9px] font-bold text-gray-400 uppercase tracking-tighter sm:tracking-wider sm:text-xs">
                <span className="flex items-center gap-0.5"><Zap className="h-2.5 w-2.5 text-amber-500" /> {hero.trustBadges.badge1}</span>
                <span>•</span>
                <span className="flex items-center gap-0.5"><ShieldCheck className="h-2.5 w-2.5 text-green-500" /> {hero.trustBadges.badge2}</span>
                <span>•</span>
                <span className="text-[var(--theme-accent-text)]">{hero.trustBadges.badge3}</span>
              </div>

              <div className="flex flex-row items-center justify-center gap-2 pt-1">
                <span className="text-xs font-medium text-gray-500">{miscText?.heroStarLine || '2 847 счастливых историй'}</span>
              </div>
            </div>
          </div>
        </section>

        {/* Data-driven section rendering */}
        {sectionOrder.map((id) => {
          if (sectionVisibility[id] === false) return null
          switch (id) {
            case 'audioSamples':
              return (
                <Suspense key={id} fallback={sectionFallback}>
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
                  />
                </Suspense>
              )
            case 'statsBar':
              return (
                <Suspense key={id} fallback={sectionFallback}>
                  <StatsBar items={sections.statsBar.items} />
                </Suspense>
              )
            case 'comparison':
              return (
                <Suspense key={id} fallback={sectionFallback}>
                  <ComparisonSection
                    fmtCurrency={fmtCurrency}
                    paymentAmount={paymentAmount}
                    originalAmount={originalAmount}
                    showPrice={showPrice.comparisonSection}
                    deliveryEtaSentence={deliveryEta.sentenceLower}
                    headline={sections.comparison?.headline}
                    giftItems={sections.comparison?.giftItems as ComponentProps<typeof ComparisonSection>['giftItems']}
                    forgottenLabel={sections.comparison?.forgottenLabel}
                    foreverLabel={sections.comparison?.foreverLabel}
                    bestPriceBadge={sections.comparison?.bestPriceBadge}
                    productTitle={sections.comparison?.productTitle}
                    checklistItems={sections.comparison?.checklistItems as ComponentProps<typeof ComparisonSection>['checklistItems']}
                  />
                </Suspense>
              )
            case 'reviews':
              return (
                <Suspense key={id} fallback={sectionFallback}>
                  <ReviewsSection
                    sectionLabel={sections.reviews.sectionLabel}
                    sectionHeadline={sections.reviews.sectionHeadline}
                    sectionSubtext={sections.reviews.sectionSubtext}
                    items={sections.reviews.items}
                  />
                </Suspense>
              )
            case 'howItWorks':
              return (
                <Suspense key={id} fallback={sectionFallback}>
                  <HowItWorksSection
                    deliveryEtaSentence={deliveryEta.sentenceLower}
                    fmtCurrency={fmtCurrency}
                    paymentAmount={paymentAmount}
                    originalAmount={originalAmount}
                    showPriceInButton={showPrice.howItWorksButton}
                    themeSlug={themeSlug}
                    sectionLabel={sections.howItWorks?.label}
                    sectionHeadline={sections.howItWorks?.headline}
                    steps={sections.howItWorks?.steps as ComponentProps<typeof HowItWorksSection>['steps']}
                    ctaButtonText={sections.howItWorks?.ctaButtonText || miscText?.ctaButtonText}
                    backgroundType={sections.howItWorks?.backgroundType}
                    backgroundColor={sections.howItWorks?.backgroundColor}
                    backgroundImageUrl={sections.howItWorks?.backgroundImageUrl}
                    backgroundOverlayOpacity={sections.howItWorks?.backgroundOverlayOpacity}
                    ctaButtonColor={sections.howItWorks?.ctaButtonColor}
                  />
                </Suspense>
              )
            case 'guarantee':
              return (
                <Suspense key={id} fallback={sectionFallback}>
                  <GuaranteeSection
                    deliveryEtaShort={deliveryEta.short}
                    badgeText={sections.guarantee?.badge}
                    headline={sections.guarantee?.headline}
                    description={sections.guarantee?.description}
                    badges={sections.guarantee?.badges}
                    videoUrl={sections.guarantee?.videoUrl}
                  />
                </Suspense>
              )
            case 'faq':
              return (
                <Suspense key={id} fallback={sectionFallback}>
                  <FaqSection
                    items={sections.faqItems}
                    sectionHeadline={sections.faq?.headline}
                  />
                </Suspense>
              )
            case 'footerCta':
              return (
                <Suspense key={id} fallback={sectionFallback}>
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
                  />
                </Suspense>
              )
            default:
              return null
          }
        })}
      </main>

      <Suspense fallback={sectionFallback}>
        <LandingFooter
          logoUrl={logoUrl}
          themeSlug={themeSlug}
          tagline={sections.footer?.tagline}
          companyName={sections.footer?.companyName}
          email={sections.footer?.email}
          phone={sections.footer?.phone}
          disclaimer={sections.footer?.disclaimer}
          copyrightLine={sections.footer?.copyrightLine}
        />
      </Suspense>

      {/* STICKY BOTTOM CTA (Mobile Only) */}
      <div
        className={cn(
          "md:hidden fixed inset-x-0 bottom-0 z-[999] bg-white border-t border-gray-100 p-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] shadow-[0_-4px_6px_-1px_rgba(0,0,0,0.05)] transition-transform duration-300 transform",
          showMobileCta ? "translate-y-0" : "translate-y-full"
        )}
      >
        <div className="mx-auto max-w-md space-y-2">
           <div className="flex justify-center gap-4 text-[10px] font-bold text-gray-500 uppercase tracking-wide">
             <span className="flex items-center gap-1"><Zap className="h-3 w-3 text-green-500" /> {deliveryEta.short}</span>
             <span className="flex items-center gap-1"><ShieldCheck className="h-3 w-3 text-green-500" /> Гарантия</span>
           </div>
           <Button asChild size="lg" className="w-full h-14 rounded-full bg-[var(--theme-button)] text-lg font-bold shadow-lg shadow-[var(--theme-accent-soft)] hover:opacity-90 active:scale-95 transition-all">
            <Link to={themeSlug ? `/${themeSlug}/config` : '/config'} className="flex items-center justify-center gap-2 flex-wrap text-center leading-tight">
              <span>{miscText?.ctaButtonText || 'Создать свою песню'}{showPrice.mobileStickyButton ? ` — ${fmtCurrency(paymentAmount)}` : ''}</span>
              {miscText?.mobileCtaQuotaBadge && (
                <Badge variant="secondary" className="bg-white/20 text-white hover:bg-white/30 text-xs whitespace-nowrap">
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

const classic: TemplateDef = {
  id: 'classic',
  name: 'Classic',
  Render: ClassicRender,
}

export default classic
