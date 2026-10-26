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
  isWorkBuddyChatBaseURL,
  parseWorkBuddyCredential,
  withWorkBuddyProduct,
  workBuddyChatBaseURL,
  workBuddyCredentialEndpoint,
} from '../workbuddy-credential'

const CREDENTIAL = JSON.stringify({
  type: 'workbuddy',
  realm: 'intl',
  product: 'cli',
  uid: 'uid-1',
  nickname: 'Jack',
  access_token: 'access-token',
  refresh_token: 'refresh-token',
})

describe('withWorkBuddyProduct', () => {
  test('rewrites the product and keeps the credential on a single line', () => {
    const updated = withWorkBuddyProduct(CREDENTIAL, 'vscode')

    expect(updated.includes('\n')).toBe(false)
    expect(JSON.parse(updated)).toMatchObject({
      type: 'workbuddy',
      uid: 'uid-1',
      access_token: 'access-token',
      refresh_token: 'refresh-token',
      product: 'vscode',
    })
  })

  test('leaves a value that is not a JSON object untouched', () => {
    expect(withWorkBuddyProduct('sk-plain-key', 'vscode')).toBe('sk-plain-key')
    expect(withWorkBuddyProduct('[1,2]', 'vscode')).toBe('[1,2]')
    expect(withWorkBuddyProduct('', 'vscode')).toBe('')
  })
})

describe('parseWorkBuddyCredential', () => {
  test('accepts a compact credential and rejects other shapes', () => {
    expect(parseWorkBuddyCredential(CREDENTIAL)).toMatchObject({ uid: 'uid-1' })
    expect(parseWorkBuddyCredential('  ')).toBeNull()
    expect(parseWorkBuddyCredential('not json')).toBeNull()
    expect(parseWorkBuddyCredential('"text"')).toBeNull()
  })
})

describe('workBuddyChatBaseURL', () => {
  test('uses the realm and product edge matrix', () => {
    expect(workBuddyChatBaseURL('intl', 'cli')).toBe('https://www.workbuddy.ai')
    expect(workBuddyChatBaseURL('intl', 'workbuddy')).toBe(
      'https://www.workbuddy.ai'
    )
    expect(workBuddyChatBaseURL('cn', 'cli')).toBe(
      'https://copilot.tencent.com'
    )
    expect(workBuddyChatBaseURL('cn', 'vscode')).toBe(
      'https://www.workbuddy.cn'
    )
    expect(workBuddyChatBaseURL('cn', 'workbuddy')).toBe(
      'https://www.workbuddy.cn'
    )
  })

  test('recognizes built-in edges after trimming a trailing slash', () => {
    expect(isWorkBuddyChatBaseURL('https://www.workbuddy.ai/')).toBe(true)
    expect(isWorkBuddyChatBaseURL('https://copilot.tencent.com')).toBe(true)
    expect(isWorkBuddyChatBaseURL('https://www.workbuddy.cn')).toBe(true)
    expect(isWorkBuddyChatBaseURL('https://proxy.example.com')).toBe(false)
  })
})

describe('workBuddyCredentialEndpoint', () => {
  test('restores the realm, product and derived address', () => {
    const endpoint = workBuddyCredentialEndpoint(
      JSON.stringify({
        type: 'workbuddy',
        realm: 'cn',
        product: 'workbuddy',
        uid: 'uid-3',
        access_token: 'access-token',
        refresh_token: 'refresh-token',
      })
    )

    expect(endpoint).toEqual({
      realm: 'cn',
      product: 'workbuddy',
      baseURL: 'https://www.workbuddy.cn',
    })
  })

  test('falls back to the intl CLI edge for an empty credential', () => {
    expect(workBuddyCredentialEndpoint('')).toBeNull()
  })
})
