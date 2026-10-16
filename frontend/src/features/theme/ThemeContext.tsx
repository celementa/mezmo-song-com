import { createContext, useContext } from 'react'

type ThemeContextValue = {
  themeSlug: string | null
}

const ThemeContext = createContext<ThemeContextValue>({ themeSlug: null })

export function ThemeProvider({ themeSlug, children }: { themeSlug: string | null; children: React.ReactNode }) {
  return <ThemeContext.Provider value={{ themeSlug }}>{children}</ThemeContext.Provider>
}

export function useThemeSlug() {
  return useContext(ThemeContext).themeSlug
}
