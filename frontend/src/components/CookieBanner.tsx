import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { effectiveConsent, hasGpc, onOpenCookieSettings, saveConsent, useConsentChoice } from '@/lib/consent'
import { localePath, useTr } from '@/lib/lang'

/**
 * Баннер cookie внизу экрана: «Принять все» / «Только необходимые» /
 * «Настроить». Показывается, пока выбор не сделан; повторно открывается
 * ссылкой «Cookie settings» в футере (openCookieSettings).
 */
export function CookieBanner() {
  const tr = useTr()
  const choice = useConsentChoice()
  const [open, setOpen] = useState(false)
  const [settings, setSettings] = useState(false)
  const [draft, setDraft] = useState(() => effectiveConsent(choice))

  useEffect(
    () =>
      onOpenCookieSettings(() => {
        setDraft(effectiveConsent())
        setSettings(true)
        setOpen(true)
      }),
    [],
  )

  if (choice && !open) return null

  const done = (next: { analytics: boolean; ads: boolean }) => {
    saveConsent(next)
    setOpen(false)
    setSettings(false)
  }
  const gpc = hasGpc()

  return (
    <div
      role="dialog"
      aria-live="polite"
      aria-label={tr('Настройки cookie', 'Cookie settings')}
      className="fixed inset-x-0 bottom-0 z-50 p-3 sm:p-4"
    >
      <div className="mx-auto max-w-3xl rounded-2xl border border-[#e6dfd3] bg-white p-4 text-sm text-[#3b352c] shadow-[0_18px_40px_-18px_rgba(21,18,16,0.35)] sm:p-5">
        <p className="leading-relaxed">
          {tr(
            'Мы используем необходимые cookie, чтобы сайт работал, а с вашего согласия — cookie аналитики и рекламы.',
            'We use essential cookies to make the site work and, with your consent, analytics and advertising cookies.',
          )}{' '}
          <Link to={localePath('/privacy')} className="underline underline-offset-2 hover:text-[#151210]">
            {tr('Подробнее', 'Learn more')}
          </Link>
        </p>

        {settings && (
          <div className="mt-3 space-y-2 border-t border-[#efe8dc] pt-3">
            <label className="flex items-start gap-3 text-[#6b6558]">
              <input type="checkbox" checked disabled className="mt-0.5 h-4 w-4" />
              <span>
                <span className="font-semibold text-[#151210]">{tr('Необходимые', 'Essential')}</span>
                {' — '}
                {tr('заказ, оплата, язык и валюта. Всегда включены.', 'order, payment, language and currency. Always on.')}
              </span>
            </label>
            <label className="flex items-start gap-3">
              <input
                type="checkbox"
                checked={draft.analytics}
                onChange={(e) => setDraft({ ...draft, analytics: e.target.checked })}
                className="mt-0.5 h-4 w-4 accent-[#151210]"
              />
              <span>
                <span className="font-semibold text-[#151210]">{tr('Аналитика', 'Analytics')}</span>
                {' — '}
                {tr('посещения и действия на сайте (Google Analytics, Яндекс Метрика).', 'visits and actions on the site (Google Analytics, Yandex Metrica).')}
              </span>
            </label>
            <label className="flex items-start gap-3">
              <input
                type="checkbox"
                checked={draft.ads && !gpc}
                disabled={gpc}
                onChange={(e) => setDraft({ ...draft, ads: e.target.checked })}
                className="mt-0.5 h-4 w-4 accent-[#151210]"
              />
              <span>
                <span className="font-semibold text-[#151210]">{tr('Реклама', 'Advertising')}</span>
                {' — '}
                {gpc
                  ? tr('выключена сигналом Global Privacy Control вашего браузера.', 'turned off by your browser’s Global Privacy Control signal.')
                  : tr('измерение и показ нашей рекламы (Google, Meta, Яндекс).', 'measuring and showing our ads (Google, Meta, Yandex).')}
              </span>
            </label>
          </div>
        )}

        <div className="mt-3 flex flex-wrap gap-2">
          {settings ? (
            <button
              type="button"
              onClick={() => done(draft)}
              className="rounded-full bg-[#151210] px-4 py-2 text-xs font-bold text-white hover:opacity-90"
            >
              {tr('Сохранить выбор', 'Save choices')}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => done({ analytics: true, ads: true })}
              className="rounded-full bg-[#151210] px-4 py-2 text-xs font-bold text-white hover:opacity-90"
            >
              {tr('Принять все', 'Accept all')}
            </button>
          )}
          <button
            type="button"
            onClick={() => done({ analytics: false, ads: false })}
            className="rounded-full border border-[#d9cfbf] px-4 py-2 text-xs font-bold text-[#151210] hover:bg-[#f7f2ea]"
          >
            {tr('Только необходимые', 'Essential only')}
          </button>
          {!settings && (
            <button
              type="button"
              onClick={() => setSettings(true)}
              className="px-2 py-2 text-xs font-semibold text-[#6b6558] underline underline-offset-2 hover:text-[#151210]"
            >
              {tr('Настроить', 'Customize')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
