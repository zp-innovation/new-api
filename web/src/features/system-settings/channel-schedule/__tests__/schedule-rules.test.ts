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
import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import {
  createEmptyRuleForm,
  createScheduleRuleFormSchema,
  formatWeekdays,
  formValuesToRule,
  parseScheduleRules,
  ruleToFormValues,
  serializeScheduleRules,
  type ScheduleRuleFormValues,
} from '../lib/schedule-rules'
import type { ChannelScheduleRule } from '../types'

/** Messages are translation keys, so echoing the key keeps assertions readable. */
const t = ((key: string) => key) as unknown as TFunction

const validRule: ChannelScheduleRule = {
  id: 'r1',
  name: 'Day · tag first',
  enabled: true,
  weekdays: [1, 2, 3, 4, 5],
  start: '07:55',
  end: '22:00',
  actions: [
    { mode: 'tag', tag: 'wb-cn', priority: 100 },
    { mode: 'channel', channel_ids: [15, 31], weight: 10, enabled: false },
  ],
}

function validFormValues(): ScheduleRuleFormValues {
  return {
    id: 'r1',
    name: 'Day · tag first',
    enabled: true,
    weekdays: [1, 2, 3, 4, 5],
    start: '07:55',
    end: '22:00',
    actions: [
      {
        mode: 'tag',
        tag: 'wb-cn',
        channelIds: [],
        usePriority: true,
        priority: 100,
        weightMode: 'none',
        weight: 1,
        useEnabled: false,
        enabled: true,
      },
      {
        mode: 'channel',
        tag: '',
        channelIds: [31, 15],
        usePriority: false,
        priority: 0,
        weightMode: 'channel',
        weight: 10,
        useEnabled: true,
        enabled: false,
      },
    ],
  }
}

/** `validFormValues` always builds two actions; this narrows them without `!`. */
function actionsOf(values: ScheduleRuleFormValues) {
  const [first, second] = values.actions
  if (!first || !second) {
    throw new Error('the fixture must build two actions')
  }
  return { first, second }
}

/** `createEmptyRuleForm` always builds exactly one action. */
function onlyAction(values: ScheduleRuleFormValues) {
  const [first] = values.actions
  if (!first) {
    throw new Error('the fixture must build one action')
  }
  return first
}

describe('channel schedule rule parsing', () => {
  it('reads a rules option back into rules', () => {
    const rules = parseScheduleRules(serializeScheduleRules([validRule]))
    expect(rules).toHaveLength(1)
    expect(rules[0]?.actions[0]).toEqual({
      mode: 'tag',
      tag: 'wb-cn',
      priority: 100,
    })
  })

  it.each([
    ['undefined', undefined],
    ['empty string', ''],
    ['malformed JSON', '{not json'],
    ['JSON that is not an array', '{"id":"r1"}'],
  ])('treats %s as no rules instead of throwing', (_label, value) => {
    expect(parseScheduleRules(value)).toEqual([])
  })

  it('drops entries that are not shaped like a rule', () => {
    const rules = parseScheduleRules(
      JSON.stringify([validRule, { id: 'broken' }, { name: 'no actions' }])
    )
    expect(rules.map((rule) => rule.id)).toEqual(['r1'])
  })

  it('keeps rules without weekdays, which means every day', () => {
    const everyDay = { ...validRule, weekdays: [] }
    const rules = parseScheduleRules(JSON.stringify([everyDay]))
    expect(rules[0]?.weekdays).toEqual([])
  })
})

describe('channel schedule rule form conversion', () => {
  it('round-trips a rule through the form shape', () => {
    const values = ruleToFormValues(validRule)
    expect(values.actions[0]?.usePriority).toBe(true)
    expect(values.actions[0]?.weightMode).toBe('none')
    expect(values.actions[1]?.weightMode).toBe('channel')
    expect(values.actions[1]?.useEnabled).toBe(true)
    expect(formValuesToRule(values)).toEqual(validRule)
  })

  it('omits fields that are not selected so the backend leaves them alone', () => {
    const rule = formValuesToRule(validFormValues())
    expect(rule.actions[0]).not.toHaveProperty('weight')
    expect(rule.actions[0]).not.toHaveProperty('weight_mode')
    expect(rule.actions[0]).not.toHaveProperty('enabled')
    expect(rule.actions[1]).not.toHaveProperty('priority')
  })

  it('reads a rule saved before the weight mode existed as per-account', () => {
    const values = ruleToFormValues(validRule)
    expect(values.actions[1]?.weightMode).toBe('channel')
    expect(values.actions[1]?.weight).toBe(10)
  })

  it('keeps the per-tag mode so the split survives a reload', () => {
    const shareRule: ChannelScheduleRule = {
      ...validRule,
      actions: [
        { mode: 'tag', tag: 'wb-cn', weight: 1, weight_mode: 'tag' },
        { mode: 'tag', tag: 'wb-intl', weight: 1, weight_mode: 'tag' },
      ],
    }
    const values = ruleToFormValues(shareRule)
    expect(values.actions[0]?.weightMode).toBe('tag')
    expect(values.actions[0]?.weight).toBe(1)
    expect(formValuesToRule(values)).toEqual(shareRule)
  })

  it('sorts weekdays and channel ids and trims the name', () => {
    const values = validFormValues()
    values.name = '  spaced  '
    values.weekdays = [5, 1, 3]
    actionsOf(values).second.channelIds = [31, 15, 3]
    const rule = formValuesToRule(values)
    expect(rule.name).toBe('spaced')
    expect(rule.weekdays).toEqual([1, 3, 5])
    expect(rule.actions[1]?.channel_ids).toEqual([3, 15, 31])
  })

  it('never sends a tag on a channel action or channel ids on a tag action', () => {
    const values = validFormValues()
    const { first, second } = actionsOf(values)
    first.tag = 'wb-cn'
    first.channelIds = [9]
    second.tag = 'wb-intl'
    const rule = formValuesToRule(values)
    expect(rule.actions[0]?.tag).toBe('wb-cn')
    expect(rule.actions[0]).not.toHaveProperty('channel_ids')
    expect(rule.actions[1]).not.toHaveProperty('tag')
    expect(rule.actions[1]?.channel_ids).toEqual([15, 31])
  })

  it('describes every day when no weekday is selected', () => {
    expect(formatWeekdays({ ...validRule, weekdays: [] }, t)).toBe('Every day')
    expect(formatWeekdays({ ...validRule, weekdays: undefined }, t)).toBe(
      'Every day'
    )
  })

  it('formats selected weekdays in week order', () => {
    expect(formatWeekdays({ ...validRule, weekdays: [5, 0, 1] }, t)).toBe(
      'Sun, Mon, Fri'
    )
  })
})

describe('channel schedule rule schema', () => {
  const schema = createScheduleRuleFormSchema(t)

  it('accepts a complete rule', () => {
    expect(schema.safeParse(validFormValues()).success).toBe(true)
  })

  it('accepts a rule with no weekday selected', () => {
    const values = { ...validFormValues(), weekdays: [] }
    expect(schema.safeParse(values).success).toBe(true)
  })

  it('rejects a rule without a name', () => {
    const values = { ...validFormValues(), name: '   ' }
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(result.error?.issues[0]?.message).toBe('Rule name is required')
  })

  it.each([
    ['start', '7:55', 'Start time must use HH:MM'],
    ['start', '24:00', 'Start time must use HH:MM'],
    ['end', '22:0', 'End time must use HH:MM'],
    ['end', 'aa:bb', 'End time must use HH:MM'],
  ])('rejects an invalid %s value %s', (field, value, message) => {
    const values = { ...validFormValues(), [field]: value }
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some((issue) => issue.message === message)
    ).toBe(true)
  })

  it('rejects a rule with no action at all', () => {
    const values = { ...validFormValues(), actions: [] }
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(result.error?.issues[0]?.message).toBe('Add at least one action')
  })

  it('rejects a tag action without a tag', () => {
    const values = validFormValues()
    actionsOf(values).first.tag = '  '
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(result.error?.issues[0]?.message).toBe('Tag cannot be empty')
    expect(result.error?.issues[0]?.path).toEqual(['actions', 0, 'tag'])
  })

  it('accepts a tag that no channel carries yet', () => {
    // An administrator has to be able to write the rule before tagging any
    // channel, which is the case on a fresh install.
    const values = validFormValues()
    actionsOf(values).first.tag = 'wb-cn'
    expect(schema.safeParse(values).success).toBe(true)
  })

  it('rejects a channel action without channels', () => {
    const values = validFormValues()
    actionsOf(values).second.channelIds = []
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) => issue.message === 'Pick at least one channel'
      )
    ).toBe(true)
  })

  it('rejects an action that changes nothing', () => {
    const values = validFormValues()
    const first = actionsOf(values).first
    first.usePriority = false
    first.weightMode = 'none'
    first.useEnabled = false
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) =>
          issue.message ===
          'An action must change priority, weight or the enabled state.'
      )
    ).toBe(true)
  })

  it('accepts a per-tag share on a tag scope', () => {
    const values = validFormValues()
    const first = actionsOf(values).first
    actionsOf(values).second.weightMode = 'none'
    first.weightMode = 'tag'
    first.weight = 1
    const result = schema.safeParse(values)
    expect(result.success).toBe(true)
  })

  it('rejects a per-tag weight on a channel scope', () => {
    const values = validFormValues()
    const second = actionsOf(values).second
    second.weightMode = 'tag'
    second.weight = 1
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) => issue.message === 'A per-tag weight needs a tag scope.'
      )
    ).toBe(true)
  })

  it('rejects a tag share above the bound', () => {
    const values = validFormValues()
    const first = actionsOf(values).first
    actionsOf(values).second.weightMode = 'none'
    first.weightMode = 'tag'
    first.weight = 101
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) => issue.message === 'A tag share must be between 1 and 100.'
      )
    ).toBe(true)
  })

  it('rejects a tag share below one', () => {
    const values = validFormValues()
    const first = actionsOf(values).first
    actionsOf(values).second.weightMode = 'none'
    first.weightMode = 'tag'
    first.weight = 0
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) => issue.path.join('.') === 'actions.0.weight'
      )
    ).toBe(true)
  })

  it('rejects a rule that mixes per-account and per-tag weights', () => {
    const values = validFormValues()
    const first = actionsOf(values).first
    first.weightMode = 'tag'
    first.weight = 1
    // The second action keeps its per-account weight, which the backend also
    // refuses: the two are split on different scales.
    const result = schema.safeParse(values)
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) =>
          issue.message ===
          'A rule cannot mix per-account and per-tag weights.'
      )
    ).toBe(true)
  })

  it.each([-1, 1_000_001])('rejects a priority of %s', (priority) => {
    const values = validFormValues()
    actionsOf(values).first.priority = priority
    expect(schema.safeParse(values).success).toBe(false)
  })

  it('starts a new rule on a full-day window that still needs a target', () => {
    const values = createEmptyRuleForm('rule-1')
    expect(values.start).toBe('00:00')
    expect(values.end).toBe('00:00')
    // No weekday selected means every day, which the schema accepts on its own.
    expect(values.weekdays).toEqual([])

    const missingTarget = schema.safeParse({ ...values, name: 'always' })
    expect(missingTarget.success).toBe(false)
    expect(
      missingTarget.error?.issues.some(
        (issue) => issue.message === 'Tag cannot be empty'
      )
    ).toBe(true)

    const ready = { ...values, name: 'always' }
    onlyAction(ready).tag = 'wb-cn'
    expect(schema.safeParse(ready).success).toBe(true)
  })
})
