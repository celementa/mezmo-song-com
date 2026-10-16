export type RecipientGender = 'male' | 'female'

// Пол получателя, однозначно заданный выбором «Кому дарим?» на первом шаге.
//
// «Друг», «Другое», «Партнёр» и любое незнакомое значение пола не задают: чипы
// настраиваются в админке, а «Друг» в этой форме выбирают и для подруги — там
// показывается тоггл «он/она». Определять пол по имени нельзя: Саша, Женя,
// Никита, Илья в уменьшительной форме кончаются на -а/-я и при этом мужские.
//
// Та же карта есть на бэкенде (internal/httpserver/recipient.go) — она разбирает
// заказы, пришедшие без явного пола: из мока, по старой ссылке, из админки.
const BY_RELATIONSHIP: Record<string, RecipientGender> = {
  'муж': 'male',
  'парень': 'male',
  'папа': 'male',
  'сын': 'male',
  'брат': 'male',
  'дедушка': 'male',
  'любимый': 'male',
  'жена': 'female',
  'девушка': 'female',
  'мама': 'female',
  'дочь': 'female',
  'сестра': 'female',
  'бабушка': 'female',
  'подруга': 'female',
  'любимая': 'female',
}

/** Пол получателя из выбранного отношения; null — отношение пол не задаёт. */
export function genderFromRelationship(relationship: string): RecipientGender | null {
  return BY_RELATIONSHIP[relationship.trim().toLowerCase()] ?? null
}
