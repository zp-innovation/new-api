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

import {
  CHANNEL_TYPE_ANTIGRAVITY,
  CHANNEL_TYPE_CODEX,
  CHANNEL_TYPE_COMMAND_CODE,
  CHANNEL_TYPE_OPENCODE,
  CHANNEL_TYPE_WORKBUDDY,
} from '../../constants'
import {
  credentialHasBlockingIssue,
  parseCredentialImport,
  splitCredentialJsonObjects,
} from '../channel-credential-import'

const antigravityCredential = {
  type: 'antigravity',
  access_token: 'access-token-1',
  refresh_token: 'refresh-token-1',
  project_id: 'aicode-consumers',
  email: 'jack.csq@gmail.com',
}

test('rebuilds a pretty printed credential as a compact single-line key', () => {
  const result = parseCredentialImport(
    JSON.stringify(antigravityCredential, null, 2),
    CHANNEL_TYPE_ANTIGRAVITY,
    'Google Antigravity'
  )

  expect(result.invalidCount).toBe(0)
  expect(result.credentials).toHaveLength(1)
  const [credential] = result.credentials
  expect(credential.key).not.toContain('\n')
  expect(JSON.parse(credential.key)).toEqual(antigravityCredential)
  expect(credential.label).toBe('jack.csq@gmail.com')
  expect(credential.name).toBe('Google Antigravity jack.csq@gmail.com')
  expect(credential.issues).toEqual([])
  expect(credentialHasBlockingIssue(credential)).toBe(false)
})

test('splits concatenated objects and a JSON array into one block each', () => {
  const second = { ...antigravityCredential, email: 'zp.innovation@gmail.com' }

  expect(
    splitCredentialJsonObjects(
      `${JSON.stringify(antigravityCredential)}\n${JSON.stringify(second)}`
    )
  ).toHaveLength(2)
  expect(
    splitCredentialJsonObjects(
      JSON.stringify([antigravityCredential, second], null, 2)
    )
  ).toHaveLength(2)
})

test('keeps braces inside string values from splitting a credential', () => {
  const raw = JSON.stringify({
    type: 'antigravity',
    access_token: '{"nested":"value"}',
    project_id: 'aicode-consumers',
  })

  expect(splitCredentialJsonObjects(raw)).toEqual([raw])
})

test('de-duplicates channels that would get the same suggested name', () => {
  const result = parseCredentialImport(
    `${JSON.stringify(antigravityCredential)}\n${JSON.stringify(antigravityCredential)}`,
    CHANNEL_TYPE_ANTIGRAVITY,
    'Google Antigravity'
  )

  expect(result.credentials.map((entry) => entry.name)).toEqual([
    'Google Antigravity jack.csq@gmail.com',
    'Google Antigravity jack.csq@gmail.com (2)',
  ])
})

test('warns about a missing refresh token without blocking the import', () => {
  const { refresh_token: _ignored, ...withoutRefreshToken } =
    antigravityCredential
  const result = parseCredentialImport(
    JSON.stringify(withoutRefreshToken),
    CHANNEL_TYPE_ANTIGRAVITY,
    'Google Antigravity'
  )

  expect(result.credentials[0].issues).toEqual([
    { code: 'missing-refresh-token', severity: 'warning' },
  ])
  expect(credentialHasBlockingIssue(result.credentials[0])).toBe(false)
})

test('blocks a Codex credential without account_id', () => {
  const result = parseCredentialImport(
    JSON.stringify({ type: 'codex', access_token: 'access-token-1' }),
    CHANNEL_TYPE_CODEX,
    'OpenAI'
  )

  expect(result.credentials[0].issues).toEqual([
    { code: 'missing-account-id', severity: 'error' },
  ])
  expect(credentialHasBlockingIssue(result.credentials[0])).toBe(true)
})

test('counts unparsable blocks and keeps the valid ones', () => {
  const result = parseCredentialImport(
    `{ this is not json }\n${JSON.stringify({ type: 'codex', account_id: 'acc-1', access_token: 'at' })}`,
    CHANNEL_TYPE_CODEX,
    'OpenAI'
  )

  expect(result.invalidCount).toBe(1)
  expect(result.credentials).toHaveLength(1)
  expect(result.credentials[0].label).toBe('acc-1')
})

const workBuddyExportAccount = {
  uid: '60571649-5a95-457d-878f-02e3a0be5163',
  nickname: 'WorkBuddy One',
  domain: 'www.codebuddy.cn',
  realm: 'cn',
  platform: 'CLI',
  product: 'workbuddy',
  enterpriseId: '',
  accessToken: 'access-token-1',
  refreshToken: 'refresh-token-1',
  expiresAt: 1794953420,
  enabled: true,
}

const workBuddyCredential = {
  type: 'workbuddy',
  realm: 'cn',
  product: 'cli',
  uid: '60571649-5a95-457d-878f-02e3a0be5163',
  nickname: 'WorkBuddy One',
  domain: 'www.codebuddy.cn',
  access_token: 'access-token-1',
  refresh_token: 'refresh-token-1',
  expires_at: '2026-11-17T22:10:20Z',
}

test('imports a workbuddy-accounts export as one credential per account', () => {
  const second = {
    ...workBuddyExportAccount,
    uid: 'uid-2',
    nickname: 'WorkBuddy Two',
    expiresAt: 1794953421,
  }
  const result = parseCredentialImport(
    JSON.stringify({
      format: 'workbuddy-accounts',
      version: 1,
      exportedAt: '2026-10-11 03:46:05',
      count: 2,
      accounts: [workBuddyExportAccount, second],
    }),
    CHANNEL_TYPE_WORKBUDDY,
    'WorkBuddy'
  )

  expect(result.invalidCount).toBe(0)
  expect(result.credentials).toHaveLength(2)
  expect(JSON.parse(result.credentials[0].key)).toEqual(workBuddyCredential)
  expect(result.credentials[0].label).toBe('WorkBuddy One')
  expect(result.credentials[0].name).toBe('WorkBuddy WorkBuddy One')
  expect(result.credentials[0].issues).toEqual([])
  expect(credentialHasBlockingIssue(result.credentials[0])).toBe(false)
})

test('converts a bare camelCase workbuddy account object', () => {
  const result = parseCredentialImport(
    JSON.stringify(workBuddyExportAccount),
    CHANNEL_TYPE_WORKBUDDY,
    'WorkBuddy'
  )

  expect(result.credentials).toHaveLength(1)
  expect(JSON.parse(result.credentials[0].key)).toEqual(workBuddyCredential)
})

test('keeps the native workbuddy credential format', () => {
  const result = parseCredentialImport(
    JSON.stringify(workBuddyCredential),
    CHANNEL_TYPE_WORKBUDDY,
    'WorkBuddy'
  )

  expect(JSON.parse(result.credentials[0].key)).toEqual(workBuddyCredential)
  expect(result.credentials[0].issues).toEqual([])
})

test('blocks a workbuddy credential without uid or access token', () => {
  const result = parseCredentialImport(
    JSON.stringify({ type: 'workbuddy', refresh_token: 'refresh-token-1' }),
    CHANNEL_TYPE_WORKBUDDY,
    'WorkBuddy'
  )

  expect(result.credentials[0].issues).toEqual([
    { code: 'missing-uid', severity: 'error' },
    { code: 'missing-access-token', severity: 'error' },
  ])
  expect(credentialHasBlockingIssue(result.credentials[0])).toBe(true)
})

const openCodeAuthExport = {
  id: 'cred_1',
  integrationID: 'opencode',
  label: 'default',
  active: true,
  value: {
    type: 'oauth',
    methodID: 'device',
    access: 'access-token-1',
    refresh: 'refresh-token-1',
    expires: 1893456000000,
    metadata: {
      server: 'https://opencode.ai/console',
      accountID: 'user-1',
      email: 'user@example.com',
      orgID: 'org-1',
      orgName: 'Example Org',
    },
  },
}

test('imports an opencode auth export entry as a channel credential', () => {
  const result = parseCredentialImport(
    JSON.stringify([openCodeAuthExport]),
    CHANNEL_TYPE_OPENCODE,
    'OpenCode'
  )

  expect(result.invalidCount).toBe(0)
  expect(result.credentials).toHaveLength(1)
  expect(JSON.parse(result.credentials[0].key)).toEqual({
    type: 'opencode',
    mode: 'zen',
    server: 'https://opencode.ai/console',
    access_token: 'access-token-1',
    refresh_token: 'refresh-token-1',
    expires_at: '2030-01-01T00:00:00Z',
    account_id: 'user-1',
    email: 'user@example.com',
    org_id: 'org-1',
    org_name: 'Example Org',
  })
  expect(result.credentials[0].label).toBe('user@example.com')
  expect(result.credentials[0].issues).toEqual([])
})

test('blocks an opencode credential without an access token', () => {
  const result = parseCredentialImport(
    JSON.stringify({
      type: 'opencode',
      refresh_token: 'refresh-token-1',
    }),
    CHANNEL_TYPE_OPENCODE,
    'OpenCode'
  )

  expect(result.credentials[0].issues).toEqual([
    { code: 'missing-access-token', severity: 'error' },
  ])
  expect(credentialHasBlockingIssue(result.credentials[0])).toBe(true)
})

test('imports a sub2api OpenCode API key account', () => {
  const result = parseCredentialImport(
    JSON.stringify({
      api_key: 'oc_sk_sub2',
      account_mode: 'go',
      base_url: 'https://opencode.ai/zen/go/v1',
      protocol_rules: [{ pattern: 'gpt-*', protocol: 'responses' }],
    }),
    CHANNEL_TYPE_OPENCODE,
    'OpenCode'
  )

  expect(result.credentials).toHaveLength(1)
  expect(JSON.parse(result.credentials[0].key)).toEqual({
    type: 'opencode',
    auth_type: 'api_key',
    mode: 'go',
    access_token: 'oc_sk_sub2',
    protocol_rules: [{ pattern: 'gpt-*', protocol: 'responses' }],
  })
  expect(result.credentials[0].issues).toEqual([])
})

test('normalizes the Command Code auth.json into a channel key', () => {
  const result = parseCredentialImport(
    JSON.stringify({
      apiKey: 'user_abc123',
      userId: 'u1',
      userName: 'Alice',
      keyName: 'cli',
      authenticatedAt: '2026-10-17T00:00:00Z',
    }),
    CHANNEL_TYPE_COMMAND_CODE,
    'Command Code'
  )

  expect(result.invalidCount).toBe(0)
  expect(result.credentials).toHaveLength(1)
  expect(JSON.parse(result.credentials[0].key)).toEqual({
    type: 'command_code',
    auth_type: 'api_key',
    api_key: 'user_abc123',
    user_id: 'u1',
    user_name: 'Alice',
    key_name: 'cli',
    authenticated_at: '2026-10-17T00:00:00Z',
  })
  expect(result.credentials[0].label).toBe('Alice')
  expect(result.credentials[0].issues).toEqual([])
})

test('blocks a Command Code credential without an API key', () => {
  const result = parseCredentialImport(
    JSON.stringify({ userId: 'u1' }),
    CHANNEL_TYPE_COMMAND_CODE,
    'Command Code'
  )

  expect(result.credentials[0].issues).toEqual([
    { code: 'missing-access-token', severity: 'error' },
  ])
  expect(credentialHasBlockingIssue(result.credentials[0])).toBe(true)
})
