/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import {
  convertDetectedLanguage,
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
  type InterfaceLanguageCode,
} from './languages'

// Each locale file is `{ "translation": { "<english key>": "<text>" } }`.
type LocaleBundle = { default: { translation: Record<string, string> } }

// Every locale file holds 580-870 KB of JSON, so importing all seven of them
// statically put ~4.7 MB into the entry chunk and dominated every cold load.
// Each bundle is now its own async chunk, fetched only for the language in use.
const LOCALE_BUNDLES: Record<InterfaceLanguageCode, () => Promise<LocaleBundle>> =
  {
    zhCN: () => import('./locales/zh.json'),
    en: () => import('./locales/en.json'),
    fr: () => import('./locales/fr.json'),
    ru: () => import('./locales/ru.json'),
    ja: () => import('./locales/ja.json'),
    vi: () => import('./locales/vi.json'),
    zhTW: () => import('./locales/zh-TW.json'),
  }

/**
 * Fetch and register one interface language so `i18n.t` can translate it.
 *
 * A registered language costs nothing, concurrent callers share the single
 * module request the bundler already dedupes, and registering twice is
 * idempotent. Unknown language codes resolve to English, which is also the
 * fallback language.
 */
export async function loadLanguageBundle(lng?: string | null): Promise<void> {
  const code = normalizeInterfaceLanguage(lng)
  if (i18n.hasResourceBundle(code, 'translation')) return

  const bundle = await LOCALE_BUNDLES[code]()
  i18n.addResourceBundle(
    code,
    'translation',
    bundle.default.translation,
    true,
    true
  )
}

/**
 * Switch the interface language after its bundle is in place, so the UI never
 * renders untranslated keys while the chunk is still in flight.
 */
export async function changeInterfaceLanguage(lng: string): Promise<void> {
  await loadLanguageBundle(lng)
  await i18n.changeLanguage(normalizeInterfaceLanguage(lng))
}

/** Resolves once i18next is initialized with the detected language loaded. */
export const i18nReady: Promise<void> = i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    supportedLngs: INTERFACE_LANGUAGE_OPTIONS.map((language) => language.code),
    load: 'currentOnly',
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env.DEV,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      // Browsers report `zh-CN`/`zh-TW`/`zh`; map them onto our `zhCN`/`zhTW`
      // codes (non-Chinese codes pass through for normal supportedLngs matching).
      convertDetectedLanguage,
    },
  })
  .then(() => loadLanguageBundle(i18n.resolvedLanguage || i18n.language))

export default i18n
