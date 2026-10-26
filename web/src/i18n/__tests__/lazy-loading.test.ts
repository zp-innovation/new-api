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
import { afterEach, describe, expect, it } from 'vitest'

import i18n, { changeInterfaceLanguage, loadLanguageBundle } from '../config'

afterEach(async () => {
  localStorage.clear()
  await i18n.changeLanguage('en')
})

describe('interface language bundles', () => {
  it('registers a language only once its bundle is requested', async () => {
    expect(i18n.hasResourceBundle('ja', 'translation')).toBe(false)

    await loadLanguageBundle('ja')

    expect(i18n.hasResourceBundle('ja', 'translation')).toBe(true)
    expect(i18n.t('Change language', { lng: 'ja' })).toBe('言語を変更')
  })

  it('switches the interface language only after loading its bundle', async () => {
    await changeInterfaceLanguage('zhTW')

    expect(i18n.resolvedLanguage).toBe('zhTW')
    expect(i18n.t('Change language')).toBe('更改語言')
  })

  it('falls back to English for a language without a bundle', async () => {
    await changeInterfaceLanguage('de-DE')

    expect(i18n.hasResourceBundle('en', 'translation')).toBe(true)
    expect(i18n.resolvedLanguage).toBe('en')
    expect(i18n.t('Change language')).toBe('Change language')
  })
})
