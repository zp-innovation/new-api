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
 * Option keys of the channel time schedule. Values are always strings, the way
 * every other option in the console is stored.
 */
export const CHANNEL_SCHEDULE_ENABLED_KEY = 'channel_schedule_setting.enabled'
export const CHANNEL_SCHEDULE_TIMEZONE_KEY = 'channel_schedule_setting.timezone'
export const CHANNEL_SCHEDULE_CHECK_MINUTES_KEY =
  'channel_schedule_setting.check_minutes'
export const CHANNEL_SCHEDULE_RULES_KEY = 'channel_schedule_setting.rules'

export const CHANNEL_SCHEDULE_DEFAULT_TIMEZONE = 'Asia/Shanghai'
export const CHANNEL_SCHEDULE_DEFAULT_CHECK_MINUTES = 1
export const CHANNEL_SCHEDULE_MAX_CHECK_MINUTES = 1440
export const CHANNEL_SCHEDULE_MAX_PRIORITY = 1_000_000
export const CHANNEL_SCHEDULE_MAX_WEIGHT = 1_000_000
/** A per-tag share is a small relative number, not a per-account weight. */
export const CHANNEL_SCHEDULE_MAX_SHARE = 100

export const CHANNEL_SCHEDULE_TIMEZONES = [
  'Asia/Shanghai',
  'Asia/Singapore',
  'Asia/Tokyo',
  'Europe/London',
  'America/New_York',
  'UTC',
] as const

export type ChannelScheduleActionMode = 'tag' | 'channel'

export type ChannelScheduleWeightMode = 'channel' | 'tag'

/**
 * One target plus the fields it rewrites. An absent field means "do not modify",
 * which the console renders as Not modified.
 */
export type ChannelScheduleAction = {
  mode: ChannelScheduleActionMode
  tag?: string
  channel_ids?: number[]
  priority?: number
  weight?: number
  /**
   * What `weight` means. Absent and `channel` write the number to every account
   * in scope, so the split follows how many accounts each scope holds. `tag`
   * treats the number as the scope tag's share and splits it across the tag's
   * accounts, so the ratio between two tags survives accounts changing.
   */
  weight_mode?: ChannelScheduleWeightMode
  enabled?: boolean
}

export type ChannelScheduleRule = {
  id: string
  name: string
  enabled: boolean
  /** 0 = Sunday … 6 = Saturday. Absent or empty means every day. */
  weekdays?: number[]
  /** HH:MM, window is [start, end); end < start wraps past midnight. */
  start: string
  end: string
  actions: ChannelScheduleAction[]
}

export type ChannelScheduleStatusField = {
  field: string
  expected: string
  actual: string
  matches: boolean
}

export type ChannelScheduleExpectation = {
  scope: string
  mode: ChannelScheduleActionMode
  tag?: string
  channel_ids: number[]
  participates: boolean
  consistent: boolean
  fields: ChannelScheduleStatusField[]
}

export type ChannelScheduleStatus = {
  enabled: boolean
  timezone: string
  now: string
  matched_rule_id?: string
  matched_rule_name?: string
  consistent: boolean
  last_run_at: number
  expectations: ChannelScheduleExpectation[]
}

export type ChannelScheduleChange = {
  channel_id: number
  channel_name: string
  scope: string
  field: string
  from: string
  to: string
}

export type ChannelScheduleRunResult = {
  rule_id?: string
  rule_name?: string
  timezone: string
  checked_at: number
  changes: ChannelScheduleChange[]
  skipped?: string
}

export type ChannelScheduleConfig = {
  options: Record<string, string>
  status: ChannelScheduleStatus
}

export type ChannelScheduleRunResponse = {
  result: ChannelScheduleRunResult
  status: ChannelScheduleStatus
}
