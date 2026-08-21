import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { DICTIONARY, type Dictionary, type Language } from '@/lib/i18n'

const STORAGE_KEY = 'boardproxy.panel.language'

interface LanguageContextValue {
  language: Language
  t: Dictionary
  setLanguage: (language: Language) => void
}

const LanguageContext = createContext<LanguageContextValue | null>(null)

function initialLanguage(): Language {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'ru' || stored === 'en') return stored
  return navigator.language.startsWith('ru') ? 'ru' : 'en'
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageState] = useState<Language>(initialLanguage)

  const setLanguage = useCallback((next: Language) => {
    localStorage.setItem(STORAGE_KEY, next)
    document.documentElement.lang = next
    setLanguageState(next)
  }, [])

  const value = useMemo<LanguageContextValue>(
    () => ({ language, t: DICTIONARY[language], setLanguage }),
    [language, setLanguage],
  )

  return <LanguageContext value={value}>{children}</LanguageContext>
}

export function useLanguage(): LanguageContextValue {
  const value = useContext(LanguageContext)
  if (!value) throw new Error('useLanguage вызван вне LanguageProvider')
  return value
}

/** Короткий доступ к словарю — самый частый случай. */
export function useT(): Dictionary {
  return useLanguage().t
}
