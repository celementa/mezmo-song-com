import { useState, useRef, useMemo, useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { Play, Pause } from 'lucide-react'
import type { Loose } from '@/lib/loose'
import { SeekBar } from '@/components/landing/SeekBar'
import { PaymentLogos } from '@/components/landing/PaymentLogos'
import { COMPANY, LEGAL_LINKS } from '@/lib/site'
import type { LandingData } from '@/hooks/useLandingData'
import type { TemplateDef } from './types'

// Hero audio player bar
function GoldHeroPlayer({ track }: { track?: { audioUrl?: string | null; title?: string; time?: string } | null }) {
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

  const handlePlayPause = () => {
    if (!track?.audioUrl) return
    const a = audioRef.current
    if (!a) return
    if (playing) {
      a.pause()
    } else {
      a.play().catch(() => {})
    }
  }

  const hasAudio = Boolean(track?.audioUrl)

  return (
    <div className="mt-6">
      <div
        className="rounded-2xl p-4 border"
        style={{
          background: 'rgba(0,0,0,.3)',
          backdropFilter: 'blur(8px)',
          borderColor: 'rgba(242,197,114,.2)',
        }}
      >
        {/* Play button + title */}
        <div className="flex items-center gap-3 mb-3">
          <button
            type="button"
            onClick={handlePlayPause}
            disabled={!hasAudio}
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-black font-bold hover:opacity-80 active:scale-95 transition-all disabled:opacity-40"
            style={{ background: '#F2C572' }}
            aria-label={playing ? 'Пауза' : 'Воспроизвести'}
          >
            {playing ? (
              <Pause className="h-4 w-4" fill="currentColor" />
            ) : (
              <Play className="h-4 w-4 ml-0.5" fill="currentColor" />
            )}
          </button>
          <div className="min-w-0">
            <div className="text-white font-bold text-sm truncate">
              {track?.title ?? 'Пример песни'}
            </div>
          </div>
        </div>

        {/* Progress bar */}
        <div className="flex items-center gap-2">
          <SeekBar
            progressPct={progressPct}
            disabled={!durSec}
            trackColor="rgba(255,255,255,.2)"
            fillColor="#F2C572"
            barClassName="h-1.5"
            onSeek={(pct) => {
              if (!durSec || !audioRef.current) return
              audioRef.current.currentTime = pct * durSec
              setPosSec(pct * durSec)
            }}
          />
          <span className="text-white/60 text-xs font-mono shrink-0">
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


// Строка примера трека с реальным аудио из настроек (allTracks, как у grace)
function GoldExampleTrack({ track, active, onToggle }: {
  track: { title: string; subtitle: string; time: string; audioUrl: string | null }
  active: boolean
  onToggle: () => void
}) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [progress, setProgress] = useState(0)
  useEffect(() => {
    const a = audioRef.current
    if (!a) return
    if (active) a.play().catch(() => {})
    else a.pause()
  }, [active])
  return (
    <div className="flex items-center gap-3 bg-white rounded-2xl p-3 shadow-sm hover:shadow-md transition">
      <button
        type="button"
        onClick={onToggle}
        disabled={!track.audioUrl}
        className="w-9 h-9 rounded-full flex items-center justify-center flex-shrink-0 hover:opacity-80 transition disabled:opacity-40"
        style={{ background: active ? '#F2C572' : '#F1E7D6', color: '#8a6621' }}
        aria-label={active ? 'Пауза' : 'Слушать пример'}
      >
        {active ? (
          <Pause className="h-3.5 w-3.5" fill="currentColor" />
        ) : (
          <Play className="h-3.5 w-3.5 ml-0.5" fill="currentColor" />
        )}
      </button>
      <div className="flex-1 min-w-0">
        <div className="font-bold text-sm truncate" style={{ color: '#151210' }}>
          {track.title}
        </div>
        <div className="text-xs truncate" style={{ color: '#6b6558' }}>
          {track.subtitle}
        </div>
        {active && (
          <div className="mt-1.5 flex">
            <SeekBar
              progressPct={progress}
              trackColor="rgba(21,18,16,.1)"
              fillColor="#B9852E"
              onSeek={(pct) => {
                const a = audioRef.current
                if (!a || !(a.duration > 0)) return
                a.currentTime = pct * a.duration
                setProgress(pct * 100)
              }}
            />
          </div>
        )}
      </div>
      <div className="text-xs flex-shrink-0" style={{ color: '#6b6558' }}>
        {track.time}
      </div>
      {track.audioUrl && (
        <audio
          ref={audioRef}
          src={track.audioUrl}
          preload="none"
          onTimeUpdate={(e) => {
            const a = e.currentTarget
            if (a.duration > 0) setProgress((a.currentTime / a.duration) * 100)
          }}
          onEnded={onToggle}
          className="hidden"
        />
      )}
    </div>
  )
}

// md-брейкпоинт Tailwind. Видео-реакция монтируется ровно один раз: на мобилке — в hero
// вместо аудиоплеера, на десктопе — в своей секции. Через CSS-скрытие сделать нельзя:
// невидимая копия <video> всё равно тянет метаданные файла.
const MOBILE_QUERY = '(max-width: 767px)'

function useIsMobile() {
  const [isMobile, setIsMobile] = useState(() => window.matchMedia?.(MOBILE_QUERY).matches ?? false)
  useEffect(() => {
    const mq = window.matchMedia(MOBILE_QUERY)
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return isMobile
}

// Карточка с реакцией на песню. Стартует, только когда доехала до экрана: файл 3 МБ,
// тянуть его на загрузке страницы незачем (одного атрибута autoplay мало — часть
// браузеров в low power mode его игнорирует, поэтому play() зовём явно).
// Звука нет и кнопки звука нет: в исходной записи дорожка пустая (цифровая тишина),
// поэтому она вырезана из файла — включать было нечего.
function GoldReactionVideo({ className = '' }: { className?: string }) {
  const videoRef = useRef<HTMLVideoElement | null>(null)

  useEffect(() => {
    const el = videoRef.current
    if (!el) return
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) el.play().catch(() => {})
        else el.pause()
      },
      { threshold: 0.25 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [])

  return (
    <div className={`relative overflow-hidden rounded-2xl ${className}`} style={{ aspectRatio: '16 / 9' }}>
      <video
        ref={videoRef}
        src="/reaction.mp4"
        poster="/images/reaction-poster.jpg"
        muted
        loop
        playsInline
        preload="metadata"
        className="w-full h-full object-cover"
      />
    </div>
  )
}

function GoldRender(data: LandingData) {
  const {
    fmtCurrency,
    paymentAmount,
    originalAmount,
    hero,
    sections,
    allTracks,
  } = data

  // Активный пример трека (играет не больше одного)
  const [exampleIdx, setExampleIdx] = useState<number | null>(null)

  const isMobile = useIsMobile()

  // Ссылка с плечом (mezmo.ru/?arm=v1_price) — рекламный адрес с конкретной
  // ценой: параметр доезжает до /config, где сервер закрепляет плечо.
  // Без параметра плечо назначает серверная ротация.
  const { search } = useLocation()
  const arm = new URLSearchParams(search).get('arm')
  const configHref = arm ? `/config?arm=${encodeURIComponent(arm)}` : '/config'

  // Extract hero player data from sections if available
  const heroPlayerAudioUrl =
    (sections as Loose)?.heroPlayer?.audioUrl || hero.player.audioUrl || allTracks[0]?.audioUrl || null
  const heroPlayerTitle = (sections as Loose)?.heroPlayer?.title || 'Пример песни'
  const heroPlayerTime = (sections as Loose)?.heroPlayer?.time || '3:24'

  // На мобилке в hero оставляем только первую фразу подзаголовка — остальное съедает
  // экран до видео/плеера (фидбек Aug 2026). Берём её из тех же данных, а не дублируем текст.
  const heroSubtextDot = hero.subtext.indexOf('. ')
  const heroSubtextShort = heroSubtextDot > 0 ? hero.subtext.slice(0, heroSubtextDot + 1) : hero.subtext

  return (
    <div className="min-h-screen" style={{ background: '#FBF7F1' }}>
      {/* === HEADER === */}
      <header
        className="flex items-center justify-between px-4 md:px-10 py-4 md:py-5 sticky top-0 z-40"
        style={{ background: '#FBF7F1' }}
      >
        <img src="/images/logo-mezmo.png" alt="Mezmo" className="h-5 md:h-6 w-auto" />
        <nav className="hidden md:flex gap-7 text-sm font-medium" style={{ color: '#151210' }}>
          <a href="#how" className="hover:opacity-70 transition">Как это работает</a>
          <a href="#reactions" className="hover:opacity-70 transition">Реакции</a>
          <a href="#reviews" className="hover:opacity-70 transition">Отзывы</a>
          <a href="#faq" className="hover:opacity-70 transition">Гарантия</a>
        </nav>
        <div className="flex items-center gap-3 md:gap-5">
          <Link
            to={configHref}
            className="px-5 py-2.5 rounded-full text-white font-bold text-sm transition-all hover:opacity-90"
            style={{ background: '#151210' }}
          >
            Создать песню
          </Link>
        </div>
      </header>

      {/* === HERO === */}
      <section className="relative mx-4 md:mx-5 rounded-3xl overflow-hidden flex flex-col justify-end md:flex-row md:justify-between md:items-end min-h-[520px] md:min-h-[640px] bg-[#1d1813]">
        {/* Background image (фиксированный арт gold-дизайна; heroMedia из настроек
            не используется — он общий с grace и управляет видео на /old-design) */}
        {/* Фото: на lg+ занимает правые 60% — текст живёт на тёмной панели слева
            и геометрически не может лечь на лицо; на sm-md фото во всю ширину */}
        <img
          src="/images/hero-gold.jpg"
          alt="Девушка слушает свою песню в наушниках"
          className="hidden sm:block absolute inset-y-0 right-0 h-full w-full lg:w-[60%] object-cover"
          style={{ objectPosition: '70% 50%' }}
        />

        {/* lg+: слив левого края фото в тёмную панель */}
        <div
          className="hidden lg:block absolute inset-0"
          style={{
            background: 'linear-gradient(90deg, #1d1813 0%, #1d1813 40%, rgba(29,24,19,0) 58%)',
          }}
        />

        {/* sm-md: горизонтальная подложка под текст поверх полноширинного фото */}
        <div
          className="hidden sm:block lg:hidden absolute inset-0"
          style={{
            background: 'linear-gradient(90deg, rgba(21,18,16,.55) 0%, rgba(21,18,16,.25) 40%, transparent 62%)',
          }}
        />

        {/* Вертикальное затемнение снизу (для плеера и CTA) */}
        <div
          className="absolute inset-0"
          style={{
            background: 'linear-gradient(180deg, rgba(21,18,16,.15) 0%, rgba(21,18,16,.3) 55%, rgba(21,18,16,.85) 100%)',
          }}
        />

        {/* Top badges — на мобилке скрыты целиком, чтобы hero влезал до плеера/видео */}
        <div className="hidden z-10 md:absolute md:top-5 md:left-5 md:right-5 md:flex md:flex-row md:justify-between md:items-start md:gap-2">
          {/* Live badge */}
          <div
            className="flex items-center gap-2 text-white text-xs font-bold px-3.5 py-2 rounded-full"
            style={{
              background: 'rgba(0,0,0,.5)',
              backdropFilter: 'blur(6px)',
            }}
          >
            <span
              className="inline-block w-1.5 h-1.5 rounded-full"
              style={{
                background: '#F2C572',
                animation: 'pulse 1.4s infinite',
              }}
            />
            Мария из Казани только что заказала песню
          </div>

          {/* Price badge */}
          <div
            className="text-xs font-black px-4 py-2 rounded-full shadow-lg"
            style={{
              background: 'linear-gradient(135deg, #F2C572, #B9852E, #f6dfa3)',
              color: '#2b1a05',
            }}
          >
            {fmtCurrency(paymentAmount)} вместо {fmtCurrency(originalAmount)}
          </div>
        </div>

        {/* Hero content */}
        <div className="relative z-10 pt-6 pb-6 px-6 md:pt-0 md:pb-10 md:px-10 max-w-2xl md:max-w-lg lg:max-w-xl text-white">
          {/* Kicker pill */}
          <div
            className="inline-block mb-4 px-3.5 py-1.5 rounded-full text-xs font-bold border mb-4"
            style={{
              background: 'rgba(255,255,255,.08)',
              borderColor: 'rgba(242,197,114,.4)',
              color: '#F2C572',
              letterSpacing: '0.5px',
            }}
          >
            ★★★★★ 12 400+ песен создано
          </div>

          {/* Headline */}
          <h1
            className="text-3xl md:text-4xl lg:text-[42px] font-bold leading-tight mb-4"
            style={{ fontFamily: "'Prata', serif" }}
          >
            {hero.headlineLine1}
            <br />
            <em style={{ color: '#F2C572', fontStyle: 'italic', fontWeight: 600 }}>
              {hero.headlineLine2}
            </em>
          </h1>

          {/* Subtext */}
          <p className="text-base mb-6 max-w-lg" style={{ color: '#e7ddce' }}>
            <span className="md:hidden">{heroSubtextShort}</span>
            <span className="hidden md:inline">{hero.subtext}</span>
          </p>

          {/* CTA buttons */}
          <div className="flex items-center gap-4 flex-wrap mb-6">
            <Link
              to={configHref}
              className="inline-block px-8 py-4 rounded-xl font-black text-sm transition-all hover:opacity-90"
              style={{
                background: 'linear-gradient(135deg,#f6dfa3 0%, #F2C572 35%, #B9852E 75%, #f6dfa3 100%)',
                color: '#2b1a05',
                boxShadow: '0 8px 22px rgba(185,133,46,.45)',
              }}
            >
              Создать песню →
            </Link>
            {/* Цена услуги должна быть видна на сайте до оплаты (требование
                UnitPay), а бейдж цены в hero на мобилке скрыт. */}
            <span className="text-sm font-semibold" style={{ color: '#F2C572' }}>
              {fmtCurrency(paymentAmount)}
              {originalAmount && originalAmount > (paymentAmount ?? 0) && (
                <span className="ml-2 font-normal line-through" style={{ color: '#a89d8c' }}>{fmtCurrency(originalAmount)}</span>
              )}
            </span>
          </div>

          {/* Мобилка: вместо аудиоплеера — видео-реакция (фидбек Aug 2026). Аудиопример
              и так идёт следующей секцией «Примеры готовых треков», а секция «смотрите
              вживую» на мобилке скрыта — одно видео на экран, без дублей. */}
          {isMobile ? (
            <GoldReactionVideo />
          ) : (
            <GoldHeroPlayer
              track={{
                audioUrl: heroPlayerAudioUrl,
                title: heroPlayerTitle,
                time: heroPlayerTime,
              }}
            />
          )}
        </div>
      </section>

      {/* === STATS ROW === */}
      <section className="max-w-5xl mx-auto px-5 mt-10 md:mt-12 relative z-10 mb-16">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <div
            className="rounded-3xl p-6 text-center shadow-md"
            style={{
              background: '#fff',
            }}
          >
            <div className="text-3xl font-bold" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              96%
            </div>
            <div className="text-xs font-semibold mt-2" style={{ color: '#6b6558' }}>
              получателей растрогались
            </div>
          </div>

          <div
            className="rounded-3xl p-6 text-center shadow-md"
            style={{
              background: '#fff',
            }}
          >
            <div className="text-3xl font-bold" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              12 400+
            </div>
            <div className="text-xs font-semibold mt-2" style={{ color: '#6b6558' }}>
              песен создано
            </div>
          </div>

          <div
            className="rounded-3xl p-6 text-center shadow-md"
            style={{
              background: '#fff',
            }}
          >
            <div className="text-3xl font-bold" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              10 мин
            </div>
            <div className="text-xs font-semibold mt-2" style={{ color: '#6b6558' }}>
              от заказа до доставки
            </div>
          </div>

          <div
            className="rounded-3xl p-6 text-center shadow-md"
            style={{
              background: '#fff',
            }}
          >
            <div className="text-3xl font-bold" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              4.9★
            </div>
            <div className="text-xs font-semibold mt-2" style={{ color: '#6b6558' }}>
              отзывов в Яндекс
            </div>
          </div>
        </div>
      </section>

      {/* === EXAMPLES (Audio Samples) === */}
      <section className="audio-samples-section max-w-5xl mx-auto px-5 py-10 md:py-20">
        <div className="text-center mb-12">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.1)',
              color: '#8a6621',
            }}
          >
            послушайте сами
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3"
            style={{ fontFamily: "'Prata', serif", color: '#151210' }}
          >
            Примеры готовых треков
          </h2>
          <p className="text-base max-w-2xl mx-auto" style={{ color: '#6b6558' }}>
            Чтобы сразу было понятно, что вы получите — до того, как читать, как это работает
          </p>
        </div>

        <div className="space-y-3">
          {allTracks.slice(0, 3).map((track, i) => (
            <GoldExampleTrack
              key={i}
              track={track}
              active={exampleIdx === i}
              onToggle={() => setExampleIdx(exampleIdx === i ? null : i)}
            />
          ))}
        </div>
      </section>

      {/* === REACTIONS (Bubble Grid) === */}
      <section className="max-w-6xl mx-auto px-5 py-10 md:py-20" id="reactions">
        <div className="text-center mb-12">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.1)',
              color: '#8a6621',
            }}
          >
            не поверите на слово
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3"
            style={{ fontFamily: "'Prata', serif", color: '#151210' }}
          >
            Настоящие реакции, не описания
          </h2>
          <p className="text-base max-w-2xl mx-auto" style={{ color: '#6b6558' }}>
            Вместо абзаца о том, «какие мы эмоциональные» — вот что происходит, когда включают запись
          </p>
        </div>

        <div className="bubble-grid grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {/* Card 1 */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-md hover:shadow-lg transition">
            <div className="relative" style={{ aspectRatio: '1 / 1', background: '#f0f0f0' }}>
              <img
                src="/images/bubble-mom-crying.jpg"
                alt="мама плачет от радости"
                className="w-full h-full object-cover"
              />
              <div
                className="absolute top-2 left-2 px-2.5 py-1 rounded-full text-xs font-bold backdrop-blur"
                style={{
                  background: 'rgba(0,0,0,.7)',
                  color: '#F2C572',
                }}
              >
                🥹 растрогалась
              </div>
            </div>
            <div className="p-4">
              <div
                className="rounded-lg p-2.5 mb-2 text-sm"
                style={{
                  background: '#F1E7D6',
                  color: '#151210',
                }}
              >
                Мама растрогалась, пока слушала. Сказала, это лучший подарок за всю жизнь 💛
              </div>
              <div className="text-xs" style={{ color: '#6b6558' }}>
                Елена М. · для мамы
              </div>
            </div>
          </div>

          {/* Card 2 */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-md hover:shadow-lg transition">
            <div className="relative" style={{ aspectRatio: '1 / 1', background: '#f0f0f0' }}>
              <img
                src="/images/bubble-proposal.jpg"
                alt="пара обнимается, предложение"
                className="w-full h-full object-cover"
              />
              <div
                className="absolute top-2 left-2 px-2.5 py-1 rounded-full text-xs font-bold backdrop-blur"
                style={{
                  background: 'rgba(0,0,0,.7)',
                  color: '#F2C572',
                }}
              >
                💍 сказала «да»
              </div>
            </div>
            <div className="p-4">
              <div
                className="rounded-lg p-2.5 mb-2 text-sm"
                style={{
                  background: '#F1E7D6',
                  color: '#151210',
                }}
              >
                Сделал предложение под эту песню. Она обняла меня ещё на первом куплете
              </div>
              <div className="text-xs" style={{ color: '#6b6558' }}>
                Дмитрий Р. · предложение
              </div>
            </div>
          </div>

          {/* Card 3 */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-md hover:shadow-lg transition">
            <div className="relative" style={{ aspectRatio: '1 / 1', background: '#f0f0f0' }}>
              <img
                src="/images/bubble-grandma.jpg"
                alt="бабушка с внучкой"
                className="w-full h-full object-cover"
              />
              <div
                className="absolute top-2 left-2 px-2.5 py-1 rounded-full text-xs font-bold backdrop-blur"
                style={{
                  background: 'rgba(0,0,0,.7)',
                  color: '#F2C572',
                }}
              >
                👵 попросила ещё раз
              </div>
            </div>
            <div className="p-4">
              <div
                className="rounded-lg p-2.5 mb-2 text-sm"
                style={{
                  background: '#F1E7D6',
                  color: '#151210',
                }}
              >
                Бабушка попросила поставить ещё раз. И ещё раз. Слушаем всей семьёй уже неделю
              </div>
              <div className="text-xs" style={{ color: '#6b6558' }}>
                Мария К. · для бабушки
              </div>
            </div>
          </div>

          {/* Card 4 */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-md hover:shadow-lg transition">
            <div className="relative" style={{ aspectRatio: '1 / 1', background: '#f0f0f0' }}>
              <img
                src="/images/bubble-dad.jpg"
                alt="папа обнимает дочь"
                className="w-full h-full object-cover"
              />
              <div
                className="absolute top-2 left-2 px-2.5 py-1 rounded-full text-xs font-bold backdrop-blur"
                style={{
                  background: 'rgba(0,0,0,.7)',
                  color: '#F2C572',
                }}
              >
                🥹 отец растрогался
              </div>
            </div>
            <div className="p-4">
              <div
                className="rounded-lg p-2.5 mb-2 text-sm"
                style={{
                  background: '#F1E7D6',
                  color: '#151210',
                }}
              >
                Папа обычно сдержанный, а тут отвернулся и вытер глаза. Никогда его таким не видела
              </div>
              <div className="text-xs" style={{ color: '#6b6558' }}>
                Ольга С. · для папы
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* === VIDEO SECTION (десктоп; на мобилке это видео стоит в hero) === */}
      {!isMobile && (
      <section
        className="video-section max-w-5xl mx-4 md:mx-5 lg:mx-auto px-5 py-10 md:py-20 rounded-3xl"
        style={{ background: '#151210' }}
      >
        <div className="text-center mb-12">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.15)',
              color: '#F2C572',
            }}
          >
            смотрите вживую
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3 text-white"
            style={{ fontFamily: "'Prata', serif" }}
          >
            Ради этого момента всё и делается
          </h2>
          <p className="text-base max-w-2xl mx-auto" style={{ color: '#d9cabb' }}>
            Так выглядит, когда песня попадает прямо в сердце
          </p>
        </div>

        <div className="flex justify-center mb-8">
          <GoldReactionVideo className="max-w-2xl w-full" />
        </div>
      </section>
      )}

      {/* === HOW IT WORKS SECTION === */}
      <section className="max-w-5xl mx-auto px-5 py-10 md:py-20" id="how">
        <div className="text-center mb-16">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.1)',
              color: '#8a6621',
            }}
          >
            как это работает
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3"
            style={{ fontFamily: "'Prata', serif", color: '#151210' }}
          >
            От истории до слёз — 4 простых шага
          </h2>
          <p className="text-base max-w-2xl mx-auto" style={{ color: '#6b6558' }}>
            Никаких скрытых условий и подписок — заплатили один раз, получили свою песню сразу
          </p>
        </div>

        {/* Step 1 */}
        <div className="flex flex-col md:flex-row items-center gap-6 md:gap-11 mb-14">
          <div className="flex-1 order-1 md:order-1">
            <div
              className="w-9 h-9 rounded-full flex items-center justify-center font-bold mb-3"
              style={{
                background: '#151210',
                color: '#F2C572',
              }}
            >
              1
            </div>
            <h3 className="text-xl font-bold mb-2" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              Расскажите, для кого песня
            </h3>
            <p className="text-sm" style={{ color: '#6b6558' }}>
              Выберите получателя и введите имя. 3 минуты, есть подсказки, если не знаете с чего начать.
            </p>
          </div>
          <div className="flex-1 order-2 md:order-2 w-full md:w-auto">
            <div
              className="rounded-2xl shadow-lg overflow-hidden border"
              style={{
                background: '#fff',
                borderColor: '#f0ddc9',
              }}
            >
              <div className="px-3 py-2 flex gap-1" style={{ background: '#f4efe4' }}>
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
              </div>
              <div className="p-5 text-sm">
                <div className="font-bold mb-3 text-xs uppercase" style={{ color: '#6b6558' }}>
                  Кому дарим?
                </div>
                <div className="flex flex-wrap gap-2 mb-3">
                  <button className="px-3 py-1 rounded-full text-xs font-bold" style={{ background: '#f0ddc9', color: '#151210' }}>Муж</button>
                  <button className="px-3 py-1 rounded-full text-xs font-bold" style={{ background: '#F2C572', color: '#151210' }}>Жена</button>
                  <button className="px-3 py-1 rounded-full text-xs font-bold" style={{ background: '#f0ddc9', color: '#151210' }}>Мама</button>
                  <button className="px-3 py-1 rounded-full text-xs font-bold" style={{ background: '#f0ddc9', color: '#151210' }}>Друг</button>
                </div>
                <div className="rounded p-2 text-xs" style={{ background: '#f9f4ed', color: '#999' }}>напр. Анна</div>
              </div>
            </div>
          </div>
        </div>

        {/* Step 2 */}
        <div className="flex flex-col md:flex-row-reverse items-center gap-6 md:gap-11 mb-14">
          <div className="flex-1 order-1 md:order-2">
            <div
              className="w-9 h-9 rounded-full flex items-center justify-center font-bold mb-3"
              style={{
                background: '#151210',
                color: '#F2C572',
              }}
            >
              2
            </div>
            <h3 className="text-xl font-bold mb-2" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              Выберите стиль и звук
            </h3>
            <p className="text-sm" style={{ color: '#6b6558' }}>
              Поп-баллада, акустика, рок и другие жанры — с коротким аудио-превью прямо в списке.
            </p>
          </div>
          <div className="flex-1 order-2 md:order-1 w-full md:w-auto">
            <div
              className="rounded-2xl shadow-lg overflow-hidden border"
              style={{
                background: '#fff',
                borderColor: '#f0ddc9',
              }}
            >
              <div className="px-3 py-2 flex gap-1" style={{ background: '#f4efe4' }}>
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
              </div>
              <div className="p-5 text-sm space-y-2">
                <div
                  className="flex items-center gap-2 p-2 rounded"
                  style={{
                    background: '#F2C572',
                    color: '#151210',
                  }}
                >
                  <span>▶</span>
                  <span className="font-bold text-xs">Поп-баллада</span>
                </div>
                <div
                  className="flex items-center gap-2 p-2 rounded"
                  style={{
                    background: '#f9f4ed',
                    color: '#6b6558',
                  }}
                >
                  <span>▶</span>
                  <span className="font-bold text-xs">Акустика</span>
                </div>
                <div
                  className="flex items-center gap-2 p-2 rounded"
                  style={{
                    background: '#f9f4ed',
                    color: '#6b6558',
                  }}
                >
                  <span>▶</span>
                  <span className="font-bold text-xs">Рок</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Step 3 */}
        <div className="flex flex-col md:flex-row items-center gap-6 md:gap-11 mb-14">
          <div className="flex-1 order-1 md:order-1">
            <div
              className="w-9 h-9 rounded-full flex items-center justify-center font-bold mb-3"
              style={{
                background: '#151210',
                color: '#F2C572',
              }}
            >
              3
            </div>
            <h3 className="text-xl font-bold mb-2" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              ИИ пишет текст и музыку
            </h3>
            <p className="text-sm" style={{ color: '#6b6558' }}>
              Опишите историю свободно или ответьте на 3 коротких вопроса в быстром режиме — текст и мелодия создаются под вашу историю, не по шаблону.
            </p>
          </div>
          <div className="flex-1 order-2 md:order-2 w-full md:w-auto">
            <div
              className="rounded-2xl shadow-lg overflow-hidden border"
              style={{
                background: '#fff',
                borderColor: '#f0ddc9',
              }}
            >
              <div className="px-3 py-2 flex gap-1" style={{ background: '#f4efe4' }}>
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
              </div>
              <div className="p-5">
                <div className="font-bold mb-3 text-xs uppercase" style={{ color: '#6b6558' }}>
                  Расскажите историю
                </div>
                <div className="rounded p-2 text-xs" style={{ background: '#f9f4ed', color: '#999' }}>
                  Мы познакомились 10 лет назад в университете...
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Step 4 */}
        <div className="flex flex-col md:flex-row-reverse items-center gap-6 md:gap-11 mb-14 last:mb-0">
          <div className="flex-1 order-1 md:order-2">
            <div
              className="w-9 h-9 rounded-full flex items-center justify-center font-bold mb-3"
              style={{
                background: '#151210',
                color: '#F2C572',
              }}
            >
              4
            </div>
            <h3 className="text-xl font-bold mb-2" style={{ color: '#151210', fontFamily: "'Prata', serif" }}>
              Готовая песня — сразу
            </h3>
            <p className="text-sm" style={{ color: '#6b6558' }}>
              10–30 минут, без ожидания суток. Слушаете черновик, просите бесплатную правку, скачиваете финал и дарите.
            </p>
          </div>
          <div className="flex-1 order-2 md:order-1 w-full md:w-auto">
            <div
              className="rounded-2xl shadow-lg overflow-hidden border"
              style={{
                background: '#fff',
                borderColor: '#f0ddc9',
              }}
            >
              <div className="px-3 py-2 flex gap-1" style={{ background: '#f4efe4' }}>
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
                <div className="w-2 h-2 rounded-full" style={{ background: '#ddd0b8' }} />
              </div>
              <div className="p-5">
                <div className="flex items-center gap-2 mb-3">
                  <button
                    className="w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 font-bold"
                    style={{ background: '#F2C572', color: '#151210' }}
                  >
                    ▶
                  </button>
                  <div className="text-xs font-bold" style={{ color: '#151210' }}>Для Анны — готово</div>
                </div>
                <div className="rounded overflow-hidden" style={{ background: '#f9f4ed', height: '4px' }}>
                  <div style={{ background: '#F2C572', width: '45%', height: '100%' }} />
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* === OCCASION GRID SECTION === */}
      <section className="max-w-5xl mx-auto px-5 py-10 md:py-20">
        <div className="text-center mb-12">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.1)',
              color: '#8a6621',
            }}
          >
            для кого песня?
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3"
            style={{ fontFamily: "'Prata', serif", color: '#151210' }}
          >
            На любой повод
          </h2>
        </div>

        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4 lg:gap-4">
          {/* Occasion 1 */}
          <div className="relative rounded-2xl overflow-hidden group">
            <img
              src="/images/occasion-spouse.jpg"
              alt="жене или мужу"
              className="w-full aspect-square object-cover"
            />
            <div
              className="absolute inset-0"
              style={{
                background: 'linear-gradient(to top, rgba(21,18,16,.85) 0%, rgba(21,18,16,.35) 22%, transparent 45%)',
              }}
            />
            <div className="absolute bottom-3 left-3 right-3 text-white font-bold text-sm">
              Жене / мужу
            </div>
          </div>

          {/* Occasion 2 */}
          <div className="relative rounded-2xl overflow-hidden group">
            <img
              src="/images/occasion-partner.jpg"
              alt="любимой девушке или парню"
              className="w-full aspect-square object-cover"
            />
            <div
              className="absolute inset-0"
              style={{
                background: 'linear-gradient(to top, rgba(21,18,16,.85) 0%, rgba(21,18,16,.35) 22%, transparent 45%)',
              }}
            />
            <div className="absolute bottom-3 left-3 right-3 text-white font-bold text-sm">
              Любимой девушке / парню
            </div>
          </div>

          {/* Occasion 3 */}
          <div className="relative rounded-2xl overflow-hidden group">
            <img
              src="/images/occasion-friends.jpg"
              alt="другу или подруге"
              className="w-full aspect-square object-cover"
            />
            <div
              className="absolute inset-0"
              style={{
                background: 'linear-gradient(to top, rgba(21,18,16,.85) 0%, rgba(21,18,16,.35) 22%, transparent 45%)',
              }}
            />
            <div className="absolute bottom-3 left-3 right-3 text-white font-bold text-sm">
              Другу / подруге
            </div>
          </div>

          {/* Occasion 4 */}
          <div className="relative rounded-2xl overflow-hidden group">
            <img
              src="/images/occasion-parents.jpg"
              alt="родителям"
              className="w-full aspect-square object-cover"
            />
            <div
              className="absolute inset-0"
              style={{
                background: 'linear-gradient(to top, rgba(21,18,16,.85) 0%, rgba(21,18,16,.35) 22%, transparent 45%)',
              }}
            />
            <div className="absolute bottom-3 left-3 right-3 text-white font-bold text-sm">
              Родителям
            </div>
          </div>
        </div>
      </section>

      {/* === REVIEWS SECTION (SCREENSHOTS) === */}
      <section className="max-w-5xl mx-auto px-5 py-10 md:py-20" id="reviews">
        <div className="text-center mb-12">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.1)',
              color: '#8a6621',
            }}
          >
            говорят наши клиенты
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3"
            style={{ fontFamily: "'Prata', serif", color: '#151210' }}
          >
            2 847+ довольных клиентов
          </h2>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {/* Review 1 - Telegram */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-lg">
            <img
              src="/images/review-telegram-galya.png"
              alt="Отзыв Галя, Telegram"
              className="w-full h-auto"
            />
          </div>

          {/* Review 2 - WhatsApp */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-lg">
            <img
              src="/images/review-whatsapp-proposal.png"
              alt="Отзыв о предложении, WhatsApp"
              className="w-full h-auto"
            />
          </div>

          {/* Review 3 - MAX/Olga */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-lg">
            <img
              src="/images/review-max-olga.png"
              alt="Отзыв Максима и Ольги"
              className="w-full h-auto"
            />
          </div>

          {/* Review 4 - про процесс заказа (широкий скриншот, во всю ширину сетки) */}
          <div className="bg-white rounded-2xl overflow-hidden shadow-lg md:col-span-3">
            <img
              src="/images/review-order-process.png"
              alt="Отзыв о процессе заказа"
              className="w-full h-auto"
            />
          </div>
        </div>
      </section>

      {/* === FAQ SECTION === */}
      <section className="max-w-3xl mx-auto px-5 py-10 md:py-20" id="faq">
        <div className="text-center mb-12">
          <div
            className="inline-block mb-3 px-3.5 py-1.5 rounded-full text-xs font-bold"
            style={{
              background: 'rgba(242,197,114,.1)',
              color: '#8a6621',
            }}
          >
            вопросы
          </div>
          <h2
            className="text-3xl md:text-4xl font-bold mb-3"
            style={{ fontFamily: "'Prata', serif", color: '#151210' }}
          >
            Частые вопросы
          </h2>
        </div>

        <div className="space-y-3">
          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              Кто пишет текст песни?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              Текст и музыку от начала до конца создаёт ИИ — но это не обычный чат-бот или голосовой помощник, а модель, созданная для написания песен на основе вашей истории. Поэтому получается не набор случайных фраз, а текст, который звучит как настоящая песня. На всякий случай — у вас есть 2 бесплатные правки, чтобы довести песню до нужного звучания.
            </div>
          </div>

          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              Сколько времени занимает создание песни?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              Обычно 10–30 минут. Если заказываете доработку — чуть дольше. Скорость зависит от очереди и сложности истории.
            </div>
          </div>

          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              У вас есть гарантии?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              99% клиентов остаются довольны и возвращаются к нам снова. Если вам не понравился результат, то мы вернем 100%. Достаточно написать нам в чат.
            </div>
          </div>

          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              Какие жанры доступны?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              Поп-баллада, рок, рэп, шансон и ещё десяток других. Также вы можете указать свой жанр и стиль музыки, если вашего варианта нет в списке.
            </div>
          </div>

          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              Можно ли что-то изменить, если текст не попал в точку?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              Да — на каждый заказ есть 2 бесплатные правки. Можно поменять строчки, вставить свой текст или попросить пересобрать куплет заново, пока не получится именно то, что нужно.
            </div>
          </div>

          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              Можно подарить на любой праздник?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              Да. День рождения, годовщина, предложение руки и сердца, свадьба, рождение ребёнка, выпускной — подойдёт для любого повода. Ваша история останется в песне навсегда.
            </div>
          </div>

          <div
            className="bg-white rounded-2xl p-4 shadow-sm"
            style={{
              padding: '16px 18px',
            }}
          >
            <div className="font-bold text-sm" style={{ color: '#151210' }}>
              Куда приходит песня?
            </div>
            <div className="text-sm mt-2" style={{ color: '#6b6558' }}>
              Она придет на вашу электронную почту, которую вы указали при заказе.
            </div>
          </div>
        </div>
      </section>

      {/* === FINAL CTA SECTION === */}
      <section
        className="max-w-3xl mx-auto mx-5 rounded-3xl py-16 px-8 text-center text-white mb-16"
        style={{
          background: 'linear-gradient(135deg, #1a1410 0%, #2a1f1a 50%, #1a1410 100%)',
        }}
      >
        <h2
          className="text-3xl font-bold mb-4"
          style={{ fontFamily: "'Prata', serif" }}
        >
          Подарите то, что запомнят навсегда
        </h2>
        <p className="text-base mb-8" style={{ color: '#d9cabb' }}>
          Тысячи людей уже увидели искреннюю благодарность в глазах тех, кому сделали такой подарок. Ваша очередь.
        </p>
        <Link
          to={configHref}
          className="inline-block px-8 py-4 rounded-xl font-black text-sm transition-all hover:opacity-90"
          style={{
            background: 'linear-gradient(135deg,#f6dfa3 0%, #F2C572 35%, #B9852E 75%, #f6dfa3 100%)',
            color: '#2b1a05',
            boxShadow: '0 8px 22px rgba(185,133,46,.45)',
          }}
        >
          Создать песню →
        </Link>
      </section>

      {/* === FOOTER WITH SOCIAL ICONS === */}
      <footer className="text-center py-12 px-5">
        <div className="text-xs space-y-1 mb-6" style={{ color: '#6b6558' }}>
          <div className="font-semibold" style={{ color: '#151210' }}>{COMPANY.name}</div>
          <div>Рег. № {COMPANY.regNo}</div>
          <div>{COMPANY.address}</div>
          <div>
            <a href={`mailto:${COMPANY.email}`} className="hover:opacity-70 transition">{COMPANY.email}</a>
            {' · '}
            <a href={`tel:${COMPANY.phone.replace(/[^\d+]/g, '')}`} className="hover:opacity-70 transition">{COMPANY.phone}</a>
          </div>
        </div>

        <PaymentLogos className="justify-center mb-6" />

        <div className="text-xs" style={{ color: '#6b6558' }}>
          <div className="mb-2 flex flex-wrap justify-center gap-x-3 gap-y-1">
            {LEGAL_LINKS.map((l) => (
              <a key={l.doc} href={`/${l.doc}`} className="hover:opacity-70 transition">{l.label}</a>
            ))}
            <a href="/contacts" className="hover:opacity-70 transition">Контакты</a>
          </div>
          <div>© 2026 {COMPANY.name}. Все права защищены.</div>
        </div>
      </footer>

      {/* Keyframes for pulse animation */}
      <style>{`
        @keyframes pulse {
          0% { opacity: 1; }
          50% { opacity: 0.3; }
          100% { opacity: 1; }
        }
      `}</style>
    </div>
  )
}

const def: TemplateDef = {
  id: 'gold',
  name: 'Mezmo v3 (Gold + Black)',
  Render: GoldRender,
}

export default def
