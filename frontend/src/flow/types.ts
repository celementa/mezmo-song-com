import type { Lang } from '@/lib/lang'

/**
 * Ответы брифа в варианте 1. Плоский объект вместо react-hook-form: полей мало,
 * валидация тривиальная, а заказ на сервере создаётся уже на шаге 3 — он и есть
 * основное хранилище черновика.
 */
export type FlowAnswers = {
  relationship: string
  recipientName: string
  recipientGender: string
  occasion: string
  email: string
  phone: string
  emotion: string
  genre: string
  customStyle: string
  story: string
}

export const EMPTY_ANSWERS: FlowAnswers = {
  relationship: '',
  recipientName: '',
  recipientGender: '',
  occasion: '',
  email: '',
  phone: '',
  emotion: '',
  genre: '',
  customStyle: '',
  story: '',
}

/**
 * Русские значения ответов — это ключи логики (сравнения, карта пола), они не
 * меняются. На английской версии в интерфейсе и на сервер уходит английский
 * текст из `*_EN`: см. answerText / answerFromWire ниже.
 */

/** Чипы «Кому дарим?» — порядок из документа (шаг 1). */
export const RELATIONSHIPS = [
  'Муж', 'Жена', 'Парень', 'Девушка', 'Мама', 'Папа', 'Бабушка', 'Дедушка', 'Друг', 'Другое',
] as const

export const RELATIONSHIP_EN: Record<string, string> = {
  'Муж': 'Husband',
  'Жена': 'Wife',
  'Парень': 'Boyfriend',
  'Девушка': 'Girlfriend',
  'Мама': 'Mom',
  'Папа': 'Dad',
  'Бабушка': 'Grandma',
  'Дедушка': 'Grandpa',
  'Друг': 'Friend',
  'Другое': 'Other',
}

/** Поводы (шаг 2). */
export const OCCASIONS = [
  { value: 'День рождения', en: 'Birthday', icon: '🎂' },
  { value: 'Свадьба', en: 'Wedding', icon: '💍' },
  { value: 'Годовщина', en: 'Anniversary', icon: '💛' },
  { value: 'Юбилей', en: 'Milestone', icon: '🎉' },
  { value: 'Романтика', en: 'Romance', icon: '🌹' },
  { value: 'Просто так', en: 'Just because', icon: '✨' },
] as const

/**
 * Эмоция песни (шаг 4). С v11 документа это основной выбор вместо жанра:
 * в отдельном тесте «жанр vs эмоция» эмоция дала +21% к конверсии. Значение
 * уходит в заказ полем `emotion` и попадает в промпты текста и стиля.
 */
export const EMOTIONS = [
  { value: 'Любовь', en: 'Love', icon: '❤️' },
  { value: 'Радость', en: 'Joy', icon: '🎉' },
  { value: 'Благодарность', en: 'Gratitude', icon: '🙏' },
  { value: 'Ностальгия', en: 'Nostalgia', icon: '🕰️' },
  { value: 'Поддержка', en: 'Support', icon: '🤗' },
  { value: 'Другое', en: 'Other', icon: '✨' },
] as const

/**
 * Значение ответа для отображения и для отправки на сервер: на русской версии —
 * как есть, на английской — английский текст. Ключ логики не подменяется.
 */
export function answerText(value: string, lang: Lang): string {
  if (lang === 'ru') return value
  const opt = OCCASIONS.find((o) => o.value === value) ?? EMOTIONS.find((e) => e.value === value)
  if (opt) return opt.en
  return RELATIONSHIP_EN[value] ?? value
}

/** Обратно: значение с сервера (английское) → ключ логики; неизвестное — как есть. */
export function answerFromWire(value: string): string {
  const occ = OCCASIONS.find((o) => o.en === value)
  if (occ) return occ.value
  const emo = EMOTIONS.find((e) => e.en === value)
  if (emo) return emo.value
  const rel = Object.keys(RELATIONSHIP_EN).find((k) => RELATIONSHIP_EN[k] === value)
  return rel ?? value
}

/**
 * Жанры — доп. экран по ссылке «Выбрать жанр вместо этого». Шесть плиток без
 * акустики, электроники и классики (убраны в v11). id уходит в генерацию как
 * musicPreferences.genre; `custom` открывает поле свободного описания стиля.
 */
export const CUSTOM_GENRE = 'custom'
export const GENRES = [
  { id: 'Pop', label: 'Поп', en: 'Pop', icon: '🎤' },
  { id: 'Rock', label: 'Рок', en: 'Rock', icon: '🎸' },
  { id: 'Hip-Hop', label: 'Хип-хоп', en: 'Hip-Hop', icon: '🎧' },
  { id: 'R&B Soul', label: 'R&B', en: 'R&B', icon: '🎷' },
  { id: 'Chanson', label: 'Шансон', en: 'Chanson', icon: '🪗' },
  { id: CUSTOM_GENRE, label: 'Свой стиль', en: 'Custom style', icon: '✍️' },
] as const

/** Жанр для брифа: выбранная плитка либо текст «своего стиля». */
export function resolveGenre(genre: string, customStyle: string): string {
  if (genre === CUSTOM_GENRE) return customStyle.trim()
  return genre
}

/** Подсказки для истории (шаг 5). */
export const STORY_PROMPTS = ['Как познакомились', 'Что цените', '«Наши» шутки', 'Мечты на будущее'] as const

/** Подсказки истории на английской версии: вставляются в текст, уходящий на сервер. */
export const STORY_PROMPTS_EN: Record<(typeof STORY_PROMPTS)[number], string> = {
  'Как познакомились': 'How we met',
  'Что цените': 'What we value',
  '«Наши» шутки': 'Our inside jokes',
  'Мечты на будущее': 'Dreams for the future',
}
