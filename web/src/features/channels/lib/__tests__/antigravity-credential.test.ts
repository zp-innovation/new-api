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

import { expect, test } from 'vitest'

import { mergeAntigravityCredential } from '../antigravity-credential'
import { readCredentialField } from '../oauth-credential'

const STORED = JSON.stringify({
  type: 'antigravity',
  access_token: 'old-access',
  refresh_token: 'old-refresh',
  expires_at: '2026-01-01T00:00:00Z',
  email: 'operator@example.com',
  project_id: 'project-1',
  plan_type: 'free',
})

const REFRESHED = {
  type: 'antigravity',
  access_token: 'new-access',
  refresh_token: 'new-refresh',
  expires_at: '2026-02-01T00:00:00Z',
  last_refresh: '2026-01-31T00:00:00Z',
}

test('keeps the account fields the refresh response omits', () => {
  const merged = mergeAntigravityCredential(STORED, REFRESHED)
  expect(merged).not.toBeNull()
  expect(JSON.parse(merged as string)).toEqual({
    type: 'antigravity',
    access_token: 'new-access',
    refresh_token: 'new-refresh',
    expires_at: '2026-02-01T00:00:00Z',
    last_refresh: '2026-01-31T00:00:00Z',
    email: 'operator@example.com',
    project_id: 'project-1',
    plan_type: 'free',
  })
})

test('keeps the previous refresh token when the response carries none', () => {
  const merged = mergeAntigravityCredential(STORED, {
    access_token: 'new-access',
    project_id: '',
  })
  expect(JSON.parse(merged as string).refresh_token).toBe('old-refresh')
})

test('refuses to rewrite a stored key that has no project_id', () => {
  const withoutProject = JSON.stringify({
    access_token: 'old-access',
    refresh_token: 'old-refresh',
  })
  expect(mergeAntigravityCredential(withoutProject, REFRESHED)).toBeNull()
  expect(mergeAntigravityCredential('not json', REFRESHED)).toBeNull()
})

test('reads one string field out of a channel key', () => {
  expect(readCredentialField(STORED, 'refresh_token')).toBe('old-refresh')
  expect(readCredentialField(STORED, 'project_id')).toBe('project-1')
  expect(readCredentialField(STORED, 'missing')).toBe('')
  expect(readCredentialField('half-typed {', 'refresh_token')).toBe('')
  expect(readCredentialField(undefined, 'refresh_token')).toBe('')
  expect(readCredentialField('{"refresh_token": 7}', 'refresh_token')).toBe('')
})
