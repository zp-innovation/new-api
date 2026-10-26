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
import { z } from 'zod'

import {
  CHANNEL_SCHEDULE_MAX_PRIORITY,
  CHANNEL_SCHEDULE_MAX_SHARE,
  CHANNEL_SCHEDULE_MAX_WEIGHT,
  type ChannelScheduleAction,
  type ChannelScheduleRule,
  type ChannelScheduleWeightMode,
} from '../types'

/** 0 = Sunday, matching the backend's rule field. */
export const WEEKDAY_VALUES = [1, 2, 3, 4, 5, 6, 0] as const

const CLOCK_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/

const WEEKDAY_LABEL_KEYS: Record<number, string> = {
  0: 'Sun',
  1: 'Mon',
  2: 'Tue',
  3: 'Wed',
  4: 'Thu',
  5: 'Fri',
  6: 'Sat',
}

export function weekdayLabelKey(day: number): string {
  return WEEKDAY_LABEL_KEYS[day] ?? String(day)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * Reads the rules option. A malformed value renders as "no rules" instead of
 * throwing, so one bad save can never blank the whole settings page.
 */
export function parseScheduleRules(
  value: string | undefined
): ChannelScheduleRule[] {
  if (!value) return []
  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    return []
  }
  if (!Array.isArray(parsed)) return []
  return parsed.filter(isChannelScheduleRule)
}

function isChannelScheduleRule(value: unknown): value is ChannelScheduleRule {
  if (!isRecord(value)) {
    return false
  }
  if (typeof value.id !== 'string' || typeof value.name !== 'string') {
    return false
  }
  if (typeof value.start !== 'string' || typeof value.end !== 'string') {
    return false
  }
  if (!Array.isArray(value.actions)) {
    return false
  }
  return true
}

export function serializeScheduleRules(rules: ChannelScheduleRule[]): string {
  return JSON.stringify(rules)
}

export function formatWeekdays(
  rule: ChannelScheduleRule,
  t: TFunction
): string {
  if (!rule.weekdays || rule.weekdays.length === 0) return t('Every day')
  return [...rule.weekdays]
    .sort((left, right) => left - right)
    .map((day) => t(weekdayLabelKey(day)))
    .join(', ')
}

/**
 * Form-only shape. Priority and the enabled state carry their own checkbox; the
 * weight carries a mode, because "write this number to every account" and
 * "split this tag's share across its accounts" are different instructions.
 */
export type ScheduleWeightMode = 'none' | ChannelScheduleWeightMode

export type ScheduleActionFormValues = {
  mode: 'tag' | 'channel'
  tag: string
  channelIds: number[]
  usePriority: boolean
  priority: number
  weightMode: ScheduleWeightMode
  weight: number
  useEnabled: boolean
  enabled: boolean
}

export type ScheduleRuleFormValues = {
  id: string
  name: string
  enabled: boolean
  weekdays: number[]
  start: string
  end: string
  actions: ScheduleActionFormValues[]
}

export function createEmptyActionForm(): ScheduleActionFormValues {
  return {
    mode: 'tag',
    tag: '',
    channelIds: [],
    usePriority: true,
    priority: 0,
    weightMode: 'none',
    weight: 1,
    useEnabled: false,
    enabled: true,
  }
}

export function createEmptyRuleForm(id: string): ScheduleRuleFormValues {
  return {
    id,
    name: '',
    enabled: true,
    weekdays: [],
    start: '00:00',
    end: '00:00',
    actions: [createEmptyActionForm()],
  }
}

export function ruleToFormValues(
  rule: ChannelScheduleRule
): ScheduleRuleFormValues {
  return {
    id: rule.id,
    name: rule.name,
    enabled: rule.enabled,
    weekdays: rule.weekdays ? [...rule.weekdays] : [],
    start: rule.start,
    end: rule.end,
    actions: rule.actions.map((action) => ({
      mode: action.mode,
      tag: action.tag ?? '',
      channelIds: action.channel_ids ? [...action.channel_ids] : [],
      usePriority: action.priority !== undefined,
      priority: action.priority ?? 0,
      weightMode: weightModeOf(action),
      weight: action.weight ?? 1,
      useEnabled: action.enabled !== undefined,
      enabled: action.enabled ?? true,
    })),
  }
}

/** A rule saved before the mode existed has a weight, so it reads as per-account. */
function weightModeOf(action: ChannelScheduleAction): ScheduleWeightMode {
  if (action.weight === undefined) return 'none'
  return action.weight_mode === 'tag' ? 'tag' : 'channel'
}

/** Drops unset fields entirely so the backend keeps treating them as "do not modify". */
export function formValuesToRule(
  values: ScheduleRuleFormValues
): ChannelScheduleRule {
  const actions: ChannelScheduleAction[] = values.actions.map((action) => {
    const next: ChannelScheduleAction = { mode: action.mode }
    if (action.mode === 'tag') {
      next.tag = action.tag.trim()
    } else {
      next.channel_ids = [...action.channelIds].sort(
        (left, right) => left - right
      )
    }
    if (action.usePriority) next.priority = action.priority
    if (action.weightMode !== 'none') {
      next.weight = action.weight
      if (action.weightMode === 'tag') next.weight_mode = 'tag'
    }
    if (action.useEnabled) next.enabled = action.enabled
    return next
  })
  return {
    id: values.id,
    name: values.name.trim(),
    enabled: values.enabled,
    weekdays: [...values.weekdays].sort((left, right) => left - right),
    start: values.start,
    end: values.end,
    actions,
  }
}

function createScheduleActionSchema(t: TFunction) {
  return z
    .object({
      mode: z.enum(['tag', 'channel']),
      tag: z.string(),
      channelIds: z.array(z.number().int().positive()),
      usePriority: z.boolean(),
      priority: z.number().int().min(0).max(CHANNEL_SCHEDULE_MAX_PRIORITY),
      weightMode: z.enum(['none', 'channel', 'tag']),
      weight: z.number().int().min(0).max(CHANNEL_SCHEDULE_MAX_WEIGHT),
      useEnabled: z.boolean(),
      enabled: z.boolean(),
    })
    .superRefine((action, ctx) => {
      if (action.mode === 'tag' && action.tag.trim().length === 0) {
        ctx.addIssue({
          code: 'custom',
          path: ['tag'],
          message: t('Tag cannot be empty'),
        })
      }
      if (action.mode === 'channel' && action.channelIds.length === 0) {
        ctx.addIssue({
          code: 'custom',
          path: ['channelIds'],
          message: t('Pick at least one channel'),
        })
      }
      if (action.weightMode === 'tag' && action.mode !== 'tag') {
        ctx.addIssue({
          code: 'custom',
          path: ['weightMode'],
          message: t('A per-tag weight needs a tag scope.'),
        })
      }
      if (
        action.weightMode === 'tag' &&
        (action.weight < 1 || action.weight > CHANNEL_SCHEDULE_MAX_SHARE)
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['weight'],
          message: t('A tag share must be between 1 and 100.'),
        })
      }
      if (
        action.weightMode === 'none' &&
        !action.usePriority &&
        !action.useEnabled
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['weightMode'],
          message: t(
            'An action must change priority, weight or the enabled state.'
          ),
        })
      }
    })
}

/**
 * Validates one rule in the editor. Messages come from the active translator, so
 * the dialog shows localized errors without a second pass.
 */
export function createScheduleRuleFormSchema(t: TFunction) {
  return z
    .object({
      id: z.string(),
      name: z
        .string()
        .refine((value) => value.trim().length > 0, t('Rule name is required')),
      enabled: z.boolean(),
      weekdays: z.array(z.number().int().min(0).max(6)),
      start: z.string().regex(CLOCK_PATTERN, t('Start time must use HH:MM')),
      end: z.string().regex(CLOCK_PATTERN, t('End time must use HH:MM')),
      actions: z
        .array(createScheduleActionSchema(t))
        .min(1, t('Add at least one action')),
    })
    .superRefine((rule, ctx) => {
      // A share is split on a much larger scale than a per-account value, so a
      // rule that carries both would silently starve whichever scope got the
      // small number. The backend rejects the same combination.
      const usesPerTag = rule.actions.some(
        (action) => action.weightMode === 'tag'
      )
      const usesPerAccount = rule.actions.some(
        (action) => action.weightMode === 'channel'
      )
      if (usesPerTag && usesPerAccount) {
        ctx.addIssue({
          code: 'custom',
          path: ['actions'],
          message: t('A rule cannot mix per-account and per-tag weights.'),
        })
      }
    })
}
