export interface CountryDialCode {
  code: string    // ISO alpha-2
  dialCode: string
  flag: string
}

/** Common countries sorted by likely usage (Russia first as default). */
export const COUNTRY_DIAL_CODES: CountryDialCode[] = [
  { code: 'RU', dialCode: '7',   flag: '🇷🇺' },
  { code: 'BY', dialCode: '375', flag: '🇧🇾' },
  { code: 'KZ', dialCode: '7',   flag: '🇰🇿' },
  { code: 'UA', dialCode: '380', flag: '🇺🇦' },
  { code: 'UZ', dialCode: '998', flag: '🇺🇿' },
  { code: 'AZ', dialCode: '994', flag: '🇦🇿' },
  { code: 'AM', dialCode: '374', flag: '🇦🇲' },
  { code: 'GE', dialCode: '995', flag: '🇬🇪' },
  { code: 'MD', dialCode: '373', flag: '🇲🇩' },
  { code: 'DE', dialCode: '49',  flag: '🇩🇪' },
  { code: 'FR', dialCode: '33',  flag: '🇫🇷' },
  { code: 'IT', dialCode: '39',  flag: '🇮🇹' },
  { code: 'ES', dialCode: '34',  flag: '🇪🇸' },
  { code: 'GB', dialCode: '44',  flag: '🇬🇧' },
  { code: 'NL', dialCode: '31',  flag: '🇳🇱' },
  { code: 'US', dialCode: '1',   flag: '🇺🇸' },
  { code: 'CA', dialCode: '1',   flag: '🇨🇦' },
  { code: 'TR', dialCode: '90',  flag: '🇹🇷' },
  { code: 'AE', dialCode: '971', flag: '🇦🇪' },
  { code: 'IL', dialCode: '972', flag: '🇮🇱' },
]

const dialCodeByCountry = new Map(COUNTRY_DIAL_CODES.map((c) => [c.code, c]))

export function getCountryByCode(isoCode: string): CountryDialCode {
  return dialCodeByCountry.get(isoCode.toUpperCase()) ?? COUNTRY_DIAL_CODES[0]
}

const DEFAULT_COUNTRY = 'RU'

/**
 * Detect user's country via IP geolocation.
 * Falls back to Russia if the API fails or times out.
 */
export async function detectCountryCode(): Promise<string> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 2000)
  try {
    const res = await fetch('https://ip-api.com/json/?fields=countryCode', {
      signal: controller.signal,
    })
    if (!res.ok) return DEFAULT_COUNTRY
    const data = await res.json()
    const cc = data?.countryCode
    return typeof cc === 'string' && cc.length === 2 ? cc.toUpperCase() : DEFAULT_COUNTRY
  } catch {
    return DEFAULT_COUNTRY
  } finally {
    clearTimeout(timeout)
  }
}
