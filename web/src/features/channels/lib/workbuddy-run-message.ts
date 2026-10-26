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

/**
 * Translates the fixed English run messages the WorkBuddy task runner stores.
 *
 * The backend keeps its messages in English, so the console renders them
 * through the locale files. Whole messages translate directly; failure chains
 * translate each known prefix and keep the raw upstream detail.
 */
export type RunMessageTranslate = (
  key: string,
  params?: Record<string, string | number>
) => string

// Fixed messages the backend stores verbatim.
const WHOLE_MESSAGES = new Set([
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
  'no growth tasks available',
  'nothing to do',
  'daily activity chat completed',
  'daily activity chat completed (web channel)',
  'already done today',
  'daily request budget spent',
  'unknown action',
  'unknown error',
  'the daily activity chat is international-only',
  'this action is China-realm only',
  'enterprise accounts have no growth centre',
  'the night-owl task only counts between 23:00 and 08:00',
  'desktop daily chat throttled',
  'desktop daily chat rate-limited',
])

const WHOLE_MESSAGE_PATTERNS: Array<{
  pattern: RegExp
  key: string
  params: (
    match: RegExpMatchArray,
    t: RunMessageTranslate
  ) => Record<string, string | number>
}> = [
  {
    pattern: /^activity reported \(streak (\d+) days\)$/,
    key: 'activity reported (streak {{days}} days)',
    params: (match) => ({ days: Number(match[1]) }),
  },
  {
    pattern: /^credits refreshed \(remaining (.+)\)$/,
    key: 'credits refreshed (remaining {{remaining}})',
    params: (match) => ({ remaining: match[1] }),
  },
  {
    pattern: /^accepted (\d+) task\(s\)$/,
    key: 'accepted {{count}} task(s)',
    params: (match) => ({ count: Number(match[1]) }),
  },
  {
    pattern: /^accept failed for (\d+) task\(s\)$/,
    key: 'accept failed for {{count}} task(s)',
    params: (match) => ({ count: Number(match[1]) }),
  },
  {
    pattern: /^claimed (.+) \(\+([^)]+)\)$/,
    key: 'claimed {{name}} (+{{credit}})',
    params: (match) => ({ name: match[1], credit: match[2] }),
  },
  {
    pattern: /^completed (.+) \(\+([^)]+)\)$/,
    key: 'completed {{name}} (+{{credit}})',
    params: (match) => ({ name: match[1], credit: match[2] }),
  },
  {
    pattern: /^claim (.+) failed: (.+)$/,
    key: 'claim {{name}} failed: {{reason}}',
    params: (match) => ({ name: match[1], reason: match[2] }),
  },
  {
    pattern: /^report (.+) failed: (.+)$/,
    key: 'report {{name}} failed: {{reason}}',
    params: (match) => ({ name: match[1], reason: match[2] }),
  },
  {
    pattern: /^(.+) is not accepted yet, skipped$/,
    key: '{{name}} is not accepted yet, skipped',
    params: (match) => ({ name: match[1] }),
  },
  {
    pattern: /^(.+) progress (\d+)\/(\d+), claim deferred$/,
    key: '{{name}} progress {{current}}/{{target}}, claim deferred',
    params: (match) => ({
      name: match[1],
      current: Number(match[2]),
      target: Number(match[3]),
    }),
  },
  {
    pattern: /^made up (.+) with a makeup card$/,
    key: 'made up {{date}} with a makeup card',
    params: (match) => ({ date: match[1] }),
  },
  {
    pattern: /^claimed the (.+): \+(.+)$/,
    key: 'claimed the {{item}}: +{{credit}}',
    params: (match, t) => ({
      item: translateDetail(t, match[1]),
      credit: match[2],
    }),
  },
  {
    pattern: /^redeemed streak tier (.+)$/,
    key: 'redeemed streak tier {{tier}}',
    params: (match) => ({ tier: match[1] }),
  },
  {
    pattern: /^drew the growth lottery (\d+) time\(s\)$/,
    key: 'drew the growth lottery {{count}} time(s)',
    params: (match) => ({ count: Number(match[1]) }),
  },
  {
    pattern: /^state: (.+)$/,
    key: 'state: {{state}}',
    params: (match) => ({ state: match[1] }),
  },
]

// A growth log entry names an upstream task plus why the gateway skipped it.
const GROWTH_PATTERNS: Array<{ pattern: RegExp; key: string }> = [
  {
    pattern: /^(.+?) needs the desktop client \((.+)\)$/,
    key: '{{name}} needs the desktop client ({{reason}})',
  },
  {
    pattern: /^(.+?) needs a real action \((.+)\)$/,
    key: '{{name}} needs a real action ({{reason}})',
  },
]

const TRANSLATABLE_DETAILS = new Set([
  'open the library in the desktop client and read the intro',
  'requires following the Tencent WorkBuddy WeChat account for 24 hours',
  'open any Buddy app in the desktop client',
  'open the teacher assistant in the desktop client',
  'start a conversation in the desktop client',
  'a real donation',
  'starter gift',
  'campaign compensation',
])

// Failure chains stack fixed English prefixes before the raw upstream error.
// Each entry keeps the wire prefix and the locale key it maps to, so the key
// never picks up the trailing colon.
const MESSAGE_PREFIXES: Array<{ prefix: string; key: string }> = [
  { prefix: 'daily chat failed: ', key: 'daily chat failed' },
  { prefix: 'web daily chat failed: ', key: 'web daily chat failed' },
  { prefix: 'desktop daily chat failed: ', key: 'desktop daily chat failed' },
  { prefix: 'check-in failed: ', key: 'check-in failed' },
  { prefix: 'cat travel failed: ', key: 'cat travel failed' },
  { prefix: 'activity report failed: ', key: 'activity report failed' },
  { prefix: 'refresh credits failed: ', key: 'refresh credits failed' },
  { prefix: 'list growth tasks: ', key: 'list growth tasks' },
  { prefix: 'build client: ', key: 'build client' },
  { prefix: 'accept: ', key: 'accept' },
  { prefix: 'cat: ', key: 'Cat travel' },
]

function translateDetail(t: RunMessageTranslate, detail: string): string {
  return TRANSLATABLE_DETAILS.has(detail) ? t(detail) : detail
}

function translatePrefixChain(
  t: RunMessageTranslate,
  text: string
): string | null {
  const parts: string[] = []
  let rest = text
  for (;;) {
    const hit = MESSAGE_PREFIXES.find((entry) => rest.startsWith(entry.prefix))
    if (!hit) break
    parts.push(t(hit.key))
    rest = rest.slice(hit.prefix.length)
  }
  if (parts.length === 0) return null
  const detail = rest ? translateEntry(t, rest) : ''
  return `${parts.join(': ')}: ${detail}`
}

function translateEntry(t: RunMessageTranslate, entry: string): string {
  const text = entry.trim()
  if (!text) return text
  if (WHOLE_MESSAGES.has(text) || TRANSLATABLE_DETAILS.has(text)) return t(text)

  for (const rule of WHOLE_MESSAGE_PATTERNS) {
    const match = text.match(rule.pattern)
    if (match) return t(rule.key, rule.params(match, t))
  }

  for (const rule of GROWTH_PATTERNS) {
    const match = text.match(rule.pattern)
    if (match) {
      return t(rule.key, {
        name: match[1],
        reason: translateDetail(t, match[2]),
      })
    }
  }

  return translatePrefixChain(t, text) ?? text
}

/** Translates a stored run message, keeping unknown upstream text as-is. */
export function workBuddyRunMessage(
  t: RunMessageTranslate,
  message: string
): string {
  const trimmed = message.trim()
  if (!trimmed) return trimmed
  return trimmed
    .split('; ')
    .map((entry) => translateEntry(t, entry))
    .join('; ')
}
