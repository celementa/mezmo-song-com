import { useCallback } from 'react'
import { useLocation } from 'react-router-dom'

/**
 * Язык сайта. Обе версии живут под языковым префиксом: /ru и /en
 * (/ru/config, /en/order/…). Корень сайта редиректит по сохранённому выбору
 * или Accept-Language; ссылка с языком в пути открывается на этом языке всегда
 * и не перебивается ни кукой, ни браузером.
 *
 * Строки переводятся парами прямо в коде: tr('Создать песню', 'Create a song').
 * Языков два, и пара рядом с разметкой читается проще словаря ключей.
 */
export type Lang = 'ru' | 'en'

const KEY = 'mezmo:lang'
const COOKIE = 'mezmo_lang'

function isLangPrefix(pathname: string, lang: Lang): boolean {
  return pathname === `/${lang}` || pathname.startsWith(`/${lang}/`)
}

export function langFromPath(pathname: string): Lang {
  if (isLangPrefix(pathname, 'en')) return 'en'
  if (isLangPrefix(pathname, 'ru')) return 'ru'
  // Непрефиксованные адреса — легаси и корень: считаем русской версией.
  return 'ru'
}

/** Путь без языкового префикса: /en/config → /config, /ru → /. */
export function stripLang(pathname: string): string {
  for (const lang of ['en', 'ru'] as const) {
    if (pathname === `/${lang}`) return '/'
    if (pathname.startsWith(`/${lang}/`)) return pathname.slice(3) || '/'
  }
  return pathname
}

/** Язык текущей страницы — для кода вне компонентов (fetch, аналитика, редиректы). */
export function getLang(): Lang {
  return typeof window === 'undefined' ? 'ru' : langFromPath(window.location.pathname)
}

/** Язык текущей страницы; компонент перерисуется при переключении. */
export function useLang(): Lang {
  return langFromPath(useLocation().pathname)
}

/**
 * Ссылка на страницу сайта на нужном языке: localePath('/config') → /ru/config
 * или /en/config. Принимает путь без префикса; query и hash сохраняются.
 */
export function localePath(path: string, lang: Lang = getLang()): string {
  const prefix = lang === 'en' ? '/en' : '/ru'
  if (path === '/' || path.startsWith('/?') || path.startsWith('/#')) {
    return `${prefix}${path.slice(1)}`
  }
  return `${prefix}${path}`
}

export function tr(ru: string, en: string, lang: Lang = getLang()): string {
  return lang === 'en' ? en : ru
}

/** tr, привязанный к языку страницы: const tr = useTr(); tr('Назад', 'Back'). */
export function useTr() {
  const lang = useLang()
  return useCallback((ru: string, en: string) => (lang === 'en' ? en : ru), [lang])
}

/**
 * Язык, который надо открыть на корне: сохранённый выбор (кука, затем
 * localStorage) или Accept-Language. ru* → русский, остальное → английский.
 */
export function preferredLang(): Lang {
  if (typeof window === 'undefined') return 'ru'
  try {
    const cookie = document.cookie.match(new RegExp(`(?:^|;\\s*)${COOKIE}=(ru|en)`))
    if (cookie) return cookie[1] as Lang
  } catch {
    /* cookie недоступны */
  }
  try {
    const saved = localStorage.getItem(KEY)
    if (saved === 'ru' || saved === 'en') return saved
  } catch {
    /* приватный режим */
  }
  const languages = navigator.languages?.length ? navigator.languages : [navigator.language || '']
  return /^ru\b/i.test(languages[0] || '') ? 'ru' : 'en'
}

/**
 * Запоминает язык, на котором человек смотрит сайт. Кука читается index.html
 * до загрузки SPA и серверным редиректом корня; localStorage остаётся для
 * приватного режима, где куки могут быть недоступны.
 */
export function rememberLang(lang: Lang) {
  try {
    localStorage.setItem(KEY, lang)
  } catch {
    /* без localStorage язык определится по браузеру */
  }
  try {
    document.cookie = `${COOKIE}=${lang}; Path=/; Max-Age=31536000; SameSite=Lax`
  } catch {
    /* cookie недоступны */
  }
}

/** BCP 47-тег для Intl (даты, числа). */
export function intlLocale(lang: Lang = getLang()): string {
  return lang === 'en' ? 'en-US' : 'ru-RU'
}
