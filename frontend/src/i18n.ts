// Interface languages. Russian text is the key; `en` holds the English version.
// A phrase missing from the dictionary falls back to Russian.
import { en } from './i18n.en'

export type Lang = 'ru' | 'en'

let lang: Lang = 'ru'

export function setLang(l: Lang) {
  lang = l
  document.documentElement.lang = l
}

export function getLang(): Lang {
  return lang
}

export function locale(): string {
  return lang === 'en' ? 'en-GB' : 'ru-RU'
}

/** Translates a phrase; {name} placeholders are replaced from vars. */
export function t(ru: string, vars?: Record<string, string | number>): string {
  let s = lang === 'en' ? en[ru] ?? ru : ru
  if (vars) s = s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m))
  return s
}

export function defaultLang(): Lang {
  return navigator.language?.toLowerCase().startsWith('ru') ? 'ru' : 'en'
}
