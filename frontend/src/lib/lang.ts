import { useCallback } from 'react'
import { useLocation } from 'react-router-dom'

/**
 * Язык сайта. Русская версия живёт на корне, английская — под префиксом /en
 * (/en, /en/config, /en/order/…): язык виден в ссылке, поэтому реклама и письма
 * сразу ведут на нужную версию. Админка только на русском и без префикса.
 *
 * Строки переводятся парами прямо в коде: tr('Создать песню', 'Create a song').
 * Языков два, и пара рядом с разметкой читается проще словаря ключей.
 */
export type Lang = 'ru' | 'en'

const KEY = 'mezmo:lang'

export function langFromPath(pathname: string): Lang {
  return pathname === '/en' || pathname.startsWith('/en/') ? 'en' : 'ru'
}

/** Путь без языкового префикса: /en/config → /config, /en → /. */
export function stripLang(pathname: string): string {
  if (langFromPath(pathname) === 'ru') return pathname
  return pathname.slice(3) || '/'
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
 * Ссылка на страницу сайта на нужном языке: localePath('/config') → /en/config
 * на английской версии. Принимает путь без префикса, query и hash сохраняются.
 */
export function localePath(path: string, lang: Lang = getLang()): string {
  if (lang === 'ru') return path
  return path === '/' || path.startsWith('/?') || path.startsWith('/#') ? `/en${path.slice(1)}` : `/en${path}`
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
 * Запоминает язык, на котором человек смотрит сайт. По нему index.html
 * (скрипт до загрузки SPA) отправляет повторный заход на / или /config на ту же
 * версию; без сохранённого выбора решает язык браузера.
 */
export function rememberLang(lang: Lang) {
  try {
    localStorage.setItem(KEY, lang)
  } catch {
    // без localStorage язык при следующем заходе определится по браузеру
  }
}

/** BCP 47-тег для Intl (даты, числа). */
export function intlLocale(lang: Lang = getLang()): string {
  return lang === 'en' ? 'en-US' : 'ru-RU'
}
