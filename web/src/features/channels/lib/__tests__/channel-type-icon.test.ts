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
import { describe, expect, test } from 'vitest'

import {
  CHANNEL_TYPE_ANTIGRAVITY,
  CHANNEL_TYPE_COMMAND_CODE,
  CHANNEL_TYPE_GEMINI_WEB,
  CHANNEL_TYPE_OPTIONS,
  CHANNEL_TYPE_WORKBUDDY,
} from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'
import { getChannelTypeIcon } from '../channel-utils'

describe('Google Antigravity channel icon', () => {
  test('uses the Antigravity brand icon instead of the generic OpenAI fallback', () => {
    expect(getChannelTypeIcon(CHANNEL_TYPE_ANTIGRAVITY)).toBe('Antigravity')
    expect(getChannelTypeConfig(CHANNEL_TYPE_ANTIGRAVITY).icon).toBe(
      'Antigravity'
    )
  })

  test('stays selectable in the provider picker', () => {
    expect(CHANNEL_TYPE_OPTIONS).toContainEqual({
      value: CHANNEL_TYPE_ANTIGRAVITY,
      label: 'Google Antigravity',
    })
  })
})

describe('WorkBuddy channel icon', () => {
  test('uses the CodeBuddy brand icon instead of the generic OpenAI fallback', () => {
    expect(getChannelTypeIcon(CHANNEL_TYPE_WORKBUDDY)).toBe('CodeBuddy')
    expect(getChannelTypeConfig(CHANNEL_TYPE_WORKBUDDY).icon).toBe('CodeBuddy')
  })

  test('stays selectable in the provider picker', () => {
    expect(CHANNEL_TYPE_OPTIONS).toContainEqual({
      value: CHANNEL_TYPE_WORKBUDDY,
      label: 'WorkBuddy',
    })
  })
})

describe('Command Code channel icon', () => {
  test('uses the Command Code fallback icon', () => {
    expect(getChannelTypeIcon(CHANNEL_TYPE_COMMAND_CODE)).toBe('CommandCode')
    expect(getChannelTypeConfig(CHANNEL_TYPE_COMMAND_CODE).icon).toBe(
      'CommandCode'
    )
  })

  test('stays selectable in the provider picker', () => {
    expect(CHANNEL_TYPE_OPTIONS).toContainEqual({
      value: CHANNEL_TYPE_COMMAND_CODE,
      label: 'Command Code',
    })
  })
})

describe('Gemini Web channel icon', () => {
  test('uses the Gemini brand icon', () => {
    expect(getChannelTypeIcon(CHANNEL_TYPE_GEMINI_WEB)).toBe('Gemini')
    expect(getChannelTypeConfig(CHANNEL_TYPE_GEMINI_WEB).icon).toBe('Gemini')
  })

  test('stays selectable in the provider picker', () => {
    expect(CHANNEL_TYPE_OPTIONS).toContainEqual({
      value: CHANNEL_TYPE_GEMINI_WEB,
      label: 'Gemini Web',
    })
  })
})
