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

import { channelSchema } from '../../types'
import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  normalizeMaxInputTokens,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const validFormValues = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Test channel',
  models: 'gpt-4o',
  group: ['default'],
}

function savedChannel(setting: Record<string, unknown>) {
  return channelSchema.parse({
    id: 1,
    name: 'Test channel',
    key: '',
    type: 1,
    status: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    setting: JSON.stringify(setting),
  })
}

describe('channel max input tokens setting', () => {
  test('new channels leave the local input limit unset', () => {
    expect(CHANNEL_FORM_DEFAULT_VALUES.max_input_tokens).toBe(0)
    expect(
      JSON.parse(buildSettingJSON(CHANNEL_FORM_DEFAULT_VALUES)).max_input_tokens
    ).toBeUndefined()
  })

  test('a positive limit survives create, update and reload', () => {
    const values = { ...CHANNEL_FORM_DEFAULT_VALUES, max_input_tokens: 128000 }
    const payloads = [
      transformFormDataToCreatePayload(values).channel,
      transformFormDataToUpdatePayload(values, 1),
    ]
    for (const payload of payloads) {
      expect(typeof payload.setting).toBe('string')
      expect(JSON.parse(payload.setting as string).max_input_tokens).toBe(
        128000
      )
      expect(
        transformChannelToFormDefaults({
          ...savedChannel({}),
          setting: payload.setting,
        }).max_input_tokens
      ).toBe(128000)
    }
  })

  test('a saved limit loads into the form and is written back unchanged', () => {
    const values = transformChannelToFormDefaults(
      savedChannel({ max_input_tokens: 64000 })
    )
    expect(values.max_input_tokens).toBe(64000)
    expect(JSON.parse(buildSettingJSON(values)).max_input_tokens).toBe(64000)
  })

  test('unusable saved values fall back to no local limit', () => {
    expect(
      transformChannelToFormDefaults(savedChannel({ max_input_tokens: 0 }))
        .max_input_tokens
    ).toBe(0)
    expect(
      transformChannelToFormDefaults(savedChannel({ max_input_tokens: -5 }))
        .max_input_tokens
    ).toBe(0)
    expect(
      transformChannelToFormDefaults(savedChannel({ max_input_tokens: 'oops' }))
        .max_input_tokens
    ).toBe(0)
    expect(
      transformChannelToFormDefaults(savedChannel({ max_input_tokens: 12.8 }))
        .max_input_tokens
    ).toBe(12)
  })

  test('the schema only accepts a non-negative whole number', () => {
    for (const value of [-1, 1.5]) {
      expect(
        channelFormSchema.safeParse({
          ...validFormValues,
          max_input_tokens: value,
        }).success
      ).toBe(false)
    }
    for (const value of [0, 128000]) {
      expect(
        channelFormSchema.safeParse({
          ...validFormValues,
          max_input_tokens: value,
        }).success
      ).toBe(true)
    }
  })

  test('normalizeMaxInputTokens floors positives and rejects the rest', () => {
    expect(normalizeMaxInputTokens(12.8)).toBe(12)
    expect(normalizeMaxInputTokens('64000')).toBe(64000)
    expect(normalizeMaxInputTokens(undefined)).toBe(0)
    expect(normalizeMaxInputTokens(null)).toBe(0)
  })
})
