import fs from 'node:fs/promises'
import path from 'node:path'

import { beforeAll, describe, expect, test } from 'vitest'

// The recent-runs panel renders backend messages through these keys. Every
// supported locale must translate them, or the console silently falls back to
// English even when the interface language is not English.
const REQUIRED_KEYS = [
  'Activity report',
  'Cat travel',
  'Streak housekeeping',
  'Growth tasks',
  'Night-owl task',
  'Daily activity chat',
  'Refresh credits',
  'Manual',
  'Scheduled',
  'checked in',
  'already checked in upstream',
  'activity reported',
  'streak state is fine',
  'the night-owl task is not in the list',
  'the night-owl task is already claimed',
  'the night-owl task could not be accepted',
  'the night-owl task is up to date',
  'the cat is still travelling',
  'already travelled today',
  'travel reward claimed',
  'the cat is on its way',
  'daily activity chat completed',
  'daily activity chat completed (web channel)',
  'already done today',
  'no growth tasks available',
  'nothing to do',
  'daily request budget spent',
  'desktop daily chat throttled',
  'desktop daily chat rate-limited',
  'activity reported (streak {{days}} days)',
  'credits refreshed (remaining {{remaining}})',
  'accepted {{count}} task(s)',
  'accept failed for {{count}} task(s)',
  'claimed {{name}} (+{{credit}})',
  'completed {{name}} (+{{credit}})',
  'claim {{name}} failed: {{reason}}',
  'report {{name}} failed: {{reason}}',
  '{{name}} is not accepted yet, skipped',
  '{{name}} progress {{current}}/{{target}}, claim deferred',
  'made up {{date}} with a makeup card',
  'claimed the {{item}}: +{{credit}}',
  'redeemed streak tier {{tier}}',
  'drew the growth lottery {{count}} time(s)',
  'state: {{state}}',
  'starter gift',
  'campaign compensation',
  'list growth tasks',
  'accept',
  'daily chat failed',
  'web daily chat failed',
  'desktop daily chat failed',
  'check-in failed',
  'cat travel failed',
  'activity report failed',
  'refresh credits failed',
]

const LOCALES = ['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi'] as const
let translations: Record<(typeof LOCALES)[number], Record<string, string>>

beforeAll(async () => {
  const localesDir = path.resolve('src/i18n/locales')
  const entries = await Promise.all(
    LOCALES.map(async (locale) => {
      const json = JSON.parse(
        await fs.readFile(path.join(localesDir, `${locale}.json`), 'utf8')
      )
      return [locale, json.translation] as const
    })
  )
  translations = Object.fromEntries(entries) as typeof translations
})

describe('WorkBuddy run message translations', () => {
  test('every supported locale defines and translates every recent-runs key', () => {
    for (const locale of LOCALES) {
      for (const key of REQUIRED_KEYS) {
        const value = translations[locale][key]
        expect(value, `${locale} key missing: ${key}`).toBeTruthy()
        if (locale !== 'en') {
          expect(value, `${locale} still untranslated: ${key}`).not.toBe(
            translations.en[key]
          )
        }
      }
    }
  })

  test('interpolation placeholders survive every translation', () => {
    for (const key of REQUIRED_KEYS) {
      const placeholders = [...key.matchAll(/{{([^}]+)}}/g)].map(
        (match) => match[1]
      )
      if (placeholders.length === 0) continue
      for (const locale of LOCALES) {
        for (const name of placeholders) {
          expect(
            translations[locale][key],
            `${locale} lost placeholder {{${name}}} in: ${key}`
          ).toContain(`{{${name}}}`)
        }
      }
    }
  })
})
