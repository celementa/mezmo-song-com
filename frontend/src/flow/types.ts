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

/** Чипы «Кому дарим?» — порядок из документа (шаг 1). */
export const RELATIONSHIPS = [
  'Муж', 'Жена', 'Парень', 'Девушка', 'Мама', 'Папа', 'Бабушка', 'Дедушка', 'Друг', 'Другое',
] as const

/** Поводы (шаг 2). */
export const OCCASIONS = [
  { value: 'День рождения', icon: '🎂' },
  { value: 'Свадьба', icon: '💍' },
  { value: 'Годовщина', icon: '💛' },
  { value: 'Юбилей', icon: '🎉' },
  { value: 'Романтика', icon: '🌹' },
  { value: 'Просто так', icon: '✨' },
] as const

/**
 * Эмоция песни (шаг 4). С v11 документа это основной выбор вместо жанра:
 * в отдельном тесте «жанр vs эмоция» эмоция дала +21% к конверсии. Значение
 * уходит в заказ полем `emotion` и попадает в промпты текста и стиля.
 */
export const EMOTIONS = [
  { value: 'Любовь', icon: '❤️' },
  { value: 'Радость', icon: '🎉' },
  { value: 'Благодарность', icon: '🙏' },
  { value: 'Ностальгия', icon: '🕰️' },
  { value: 'Поддержка', icon: '🤗' },
  { value: 'Другое', icon: '✨' },
] as const

/**
 * Жанры — доп. экран по ссылке «Выбрать жанр вместо этого». Шесть плиток без
 * акустики, электроники и классики (убраны в v11). id уходит в генерацию как
 * musicPreferences.genre; `custom` открывает поле свободного описания стиля.
 */
export const CUSTOM_GENRE = 'custom'
export const GENRES = [
  { id: 'Pop', label: 'Поп', icon: '🎤' },
  { id: 'Rock', label: 'Рок', icon: '🎸' },
  { id: 'Hip-Hop', label: 'Хип-хоп', icon: '🎧' },
  { id: 'R&B Soul', label: 'R&B', icon: '🎷' },
  { id: 'Chanson', label: 'Шансон', icon: '🪗' },
  { id: CUSTOM_GENRE, label: 'Свой стиль', icon: '✍️' },
] as const

/** Жанр для брифа: выбранная плитка либо текст «своего стиля». */
export function resolveGenre(genre: string, customStyle: string): string {
  if (genre === CUSTOM_GENRE) return customStyle.trim()
  return genre
}

/** Подсказки для истории (шаг 5). */
export const STORY_PROMPTS = ['Как познакомились', 'Что цените', '«Наши» шутки', 'Мечты на будущее'] as const
