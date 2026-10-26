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
import {
  CHANNEL_TYPE_ANTIGRAVITY,
  CHANNEL_TYPE_CODEX,
  CHANNEL_TYPE_WORKBUDDY,
  CHANNEL_TYPE_OPENCODE,
  CHANNEL_TYPE_COMMAND_CODE,
} from '../constants'

/**
 * Problems reported for a single pasted credential. Severity says whether the
 * channel can still be created:
 * - error: the upstream would reject the credential, so the entry is skipped
 * - warning: usable now, but the account will break sooner or later
 */
export type CredentialIssueCode =
  | 'missing-access-token'
  | 'missing-project-id'
  | 'missing-account-id'
  | 'missing-uid'
  | 'missing-refresh-token'
  | 'credential-type-mismatch'

type CredentialIssue = {
  code: CredentialIssueCode
  severity: 'error' | 'warning'
}

export type ImportedCredential = {
  /** 1-based position of the credential inside the pasted payload. */
  index: number
  /** Compact single-line JSON, ready to be stored as the channel key. */
  key: string
  /** Best-effort account identity: email, then account id, then project id. */
  label: string
  /** Suggested channel name, already de-duplicated inside the batch. */
  name: string
  issues: CredentialIssue[]
}

type CredentialImportResult = {
  credentials: ImportedCredential[]
  /** JSON blocks that could not be parsed at all. */
  invalidCount: number
}

/**
 * Split a pasted payload into top-level JSON objects. OAuth exporters hand out
 * compact single-line JSON, pretty-printed JSON and JSON arrays interchangeably,
 * so a brace scanner that respects strings is the only reliable splitter: it
 * keeps multi-line credentials whole and never breaks on the newlines inside
 * them (the channel key column is newline-delimited, so the compact form has to
 * be rebuilt before the value is stored).
 */
export function splitCredentialJsonObjects(raw: string): string[] {
  const blocks: string[] = []
  let depth = 0
  let start = -1
  let inString = false
  let escaped = false

  for (let i = 0; i < raw.length; i += 1) {
    const char = raw[i]
    if (inString) {
      if (escaped) {
        escaped = false
      } else if (char === '\\') {
        escaped = true
      } else if (char === '"') {
        inString = false
      }
      continue
    }
    if (char === '"') {
      inString = true
      continue
    }
    if (char === '{') {
      if (depth === 0) {
        start = i
      }
      depth += 1
      continue
    }
    if (char === '}') {
      if (depth === 0) continue
      depth -= 1
      if (depth === 0 && start >= 0) {
        blocks.push(raw.slice(start, i + 1))
        start = -1
      }
    }
  }

  return blocks
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function readString(source: Record<string, unknown>, key: string): string {
  const value = source[key]
  return typeof value === 'string' ? value.trim() : ''
}

/**
 * The reference gateway exports `expiresAt` as epoch seconds, while the
 * credential stores the RFC3339 string the backend parses. A missing value is
 * left out: the backend then reads the JWT exp claim itself.
 */
function readWorkBuddyExpiresAt(account: Record<string, unknown>): string {
  const direct = readString(account, 'expires_at')
  if (direct) return direct

  const raw = account.expiresAt
  let seconds = Number.NaN
  if (typeof raw === 'number') {
    seconds = raw
  } else if (typeof raw === 'string' && raw.trim() !== '') {
    seconds = Number(raw)
  }
  if (!Number.isFinite(seconds) || seconds <= 0) return ''
  return new Date(seconds * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z')
}

/**
 * Rebuilds one WorkBuddy account as the snake_case credential the channel key
 * stores, keeping the backend struct's field order. The reference gateway
 * export uses camelCase and a `platform` field that names the client identity
 * (CLI, VSCode, ...), which maps onto the credential's `product`.
 */
function normalizeWorkBuddyCredential(
  account: Record<string, unknown>
): Record<string, unknown> {
  const credential: Record<string, unknown> = {
    type: readString(account, 'type') || 'workbuddy',
  }

  const realm = readString(account, 'realm')
  if (realm) credential.realm = realm

  const product =
    readString(account, 'platform') || readString(account, 'product')
  if (product) credential.product = product.toLowerCase()

  const uid = readString(account, 'uid')
  if (uid) credential.uid = uid

  const nickname = readString(account, 'nickname')
  if (nickname) credential.nickname = nickname

  const domain = readString(account, 'domain')
  if (domain) credential.domain = domain

  const accessToken =
    readString(account, 'access_token') || readString(account, 'accessToken')
  if (accessToken) credential.access_token = accessToken

  const refreshToken =
    readString(account, 'refresh_token') || readString(account, 'refreshToken')
  if (refreshToken) credential.refresh_token = refreshToken

  const expiresAt = readWorkBuddyExpiresAt(account)
  if (expiresAt) credential.expires_at = expiresAt

  const lastRefresh =
    readString(account, 'last_refresh') || readString(account, 'lastRefresh')
  if (lastRefresh) credential.last_refresh = lastRefresh

  const enterpriseId =
    readString(account, 'enterprise_id') || readString(account, 'enterpriseId')
  if (enterpriseId) credential.enterprise_id = enterpriseId

  return credential
}

function expandWorkBuddyCredentials(
  parsed: Record<string, unknown>
): Record<string, unknown>[] {
  if (!Array.isArray(parsed.accounts)) {
    return [normalizeWorkBuddyCredential(parsed)]
  }
  const format = readString(parsed, 'format')
  if (format && format !== 'workbuddy-accounts') {
    return [normalizeWorkBuddyCredential(parsed)]
  }

  const credentials: Record<string, unknown>[] = []
  for (const account of parsed.accounts) {
    if (isPlainObject(account)) {
      credentials.push(normalizeWorkBuddyCredential(account))
    }
  }
  return credentials
}

/**
 * Rebuilds one OpenCode auth-export entry (or a direct credential) as the
 * snake_case credential the channel key stores. `opencode auth export` nests
 * the tokens under `value` and the account metadata under `value.metadata`.
 */
function normalizeOpenCodeCredential(
  entry: Record<string, unknown>
): Record<string, unknown> {
  const apiKey = readString(entry, 'api_key')
  if (apiKey) {
    const accountMode = readString(entry, 'account_mode')
    const baseURL = readString(entry, 'base_url')
    return {
      type: 'opencode',
      auth_type: 'api_key',
      mode: accountMode || (baseURL.includes('/zen/go') ? 'go' : 'zen'),
      access_token: apiKey,
      ...(entry.protocol_rules !== undefined
        ? { protocol_rules: entry.protocol_rules }
        : {}),
    }
  }

  const declaredType = readString(entry, 'type')
  if (declaredType === 'opencode' || declaredType === '') {
    const directAccess =
      readString(entry, 'access_token') || readString(entry, 'accessToken')
    if (directAccess) {
      return {
        ...entry,
        type: 'opencode',
        ...(directAccess.startsWith('oc_') &&
        !readString(entry, 'refresh_token')
          ? { auth_type: 'api_key' }
          : {}),
      }
    }
  }

  const value = isPlainObject(entry.value) ? entry.value : {}
  const metadata = isPlainObject(value.metadata) ? value.metadata : {}
  const access =
    readString(value, 'access') || readString(entry, 'access_token')
  const refresh =
    readString(value, 'refresh') || readString(entry, 'refresh_token')
  const expires = Number(value.expires ?? entry.expires)
  return {
    type: 'opencode',
    mode: readString(metadata, 'mode') || readString(entry, 'mode') || 'zen',
    server:
      readString(metadata, 'server') ||
      readString(entry, 'server') ||
      'https://opencode.ai/console',
    ...(access ? { access_token: access } : {}),
    ...(refresh ? { refresh_token: refresh } : {}),
    ...(Number.isFinite(expires) && expires > 0
      ? {
          expires_at: new Date(expires).toISOString().replace(/\.\d{3}Z$/, 'Z'),
        }
      : {}),
    ...(readString(metadata, 'accountID') || readString(entry, 'account_id')
      ? {
          account_id:
            readString(metadata, 'accountID') ||
            readString(entry, 'account_id'),
        }
      : {}),
    ...(readString(metadata, 'email') || readString(entry, 'email')
      ? { email: readString(metadata, 'email') || readString(entry, 'email') }
      : {}),
    ...(readString(metadata, 'orgID') || readString(entry, 'org_id')
      ? { org_id: readString(metadata, 'orgID') || readString(entry, 'org_id') }
      : {}),
    ...(readString(metadata, 'orgName') || readString(entry, 'org_name')
      ? {
          org_name:
            readString(metadata, 'orgName') || readString(entry, 'org_name'),
        }
      : {}),
  }
}

/**
 * Rebuilds one Command Code credential from the camelCase auth.json the
 * official CLI writes, or from a direct snake_case credential.
 */
function normalizeCommandCodeCredential(
  entry: Record<string, unknown>
): Record<string, unknown> {
  const apiKey =
    readString(entry, 'api_key') || readString(entry, 'apiKey')
  const credential: Record<string, unknown> = {
    type: 'command_code',
    auth_type: 'api_key',
    ...(apiKey ? { api_key: apiKey } : {}),
  }
  const userId = readString(entry, 'user_id') || readString(entry, 'userId')
  if (userId) credential.user_id = userId
  const userName =
    readString(entry, 'user_name') || readString(entry, 'userName')
  if (userName) credential.user_name = userName
  const keyName = readString(entry, 'key_name') || readString(entry, 'keyName')
  if (keyName) credential.key_name = keyName
  const authenticatedAt =
    readString(entry, 'authenticated_at') ||
    readString(entry, 'authenticatedAt')
  if (authenticatedAt) credential.authenticated_at = authenticatedAt
  return credential
}

function collectIssues(
  credential: Record<string, unknown>,
  channelType: number
): CredentialIssue[] {
  const issues: CredentialIssue[] = []
  const accessToken = readString(credential, 'access_token')
  const refreshToken = readString(credential, 'refresh_token')
  const declaredType = readString(credential, 'type')

  if (channelType === CHANNEL_TYPE_ANTIGRAVITY) {
    // The relay needs the access token and the Cloud Code Assist project; the
    // refresh token only matters once the access token expires.
    if (!accessToken) {
      issues.push({ code: 'missing-access-token', severity: 'error' })
    }
    if (!readString(credential, 'project_id')) {
      issues.push({ code: 'missing-project-id', severity: 'error' })
    }
    if (!refreshToken) {
      issues.push({ code: 'missing-refresh-token', severity: 'warning' })
    }
    if (declaredType && declaredType !== 'antigravity') {
      issues.push({ code: 'credential-type-mismatch', severity: 'error' })
    }
    return issues
  }

  if (channelType === CHANNEL_TYPE_CODEX) {
    // controller.RejectNonCodexKey refuses a key without account_id.
    if (!readString(credential, 'account_id')) {
      issues.push({ code: 'missing-account-id', severity: 'error' })
    }
    if (!accessToken && !refreshToken) {
      issues.push({ code: 'missing-access-token', severity: 'error' })
    }
    if (declaredType && declaredType !== 'codex') {
      issues.push({ code: 'credential-type-mismatch', severity: 'error' })
    }
  }

  if (channelType === CHANNEL_TYPE_WORKBUDDY) {
    // pkg/workbuddy.ParseCredential requires the uid and the access token; the
    // refresh token is what keeps the account alive past the token expiry.
    if (!readString(credential, 'uid')) {
      issues.push({ code: 'missing-uid', severity: 'error' })
    }
    if (!accessToken) {
      issues.push({ code: 'missing-access-token', severity: 'error' })
    }
    if (!refreshToken) {
      issues.push({ code: 'missing-refresh-token', severity: 'warning' })
    }
    if (declaredType && declaredType !== 'workbuddy') {
      issues.push({ code: 'credential-type-mismatch', severity: 'error' })
    }
  }

  if (channelType === CHANNEL_TYPE_OPENCODE) {
    // The relay needs the access token; the refresh token keeps the account
    // alive because OpenCode rotates it.
    const apiKey = readString(credential, 'api_key')
    const authType = readString(credential, 'auth_type')
    const isAPIKey =
      authType === 'api_key' ||
      apiKey !== '' ||
      (accessToken.startsWith('oc_') && !refreshToken)
    if (!accessToken) {
      issues.push({ code: 'missing-access-token', severity: 'error' })
    }
    if (!refreshToken && !isAPIKey) {
      issues.push({ code: 'missing-refresh-token', severity: 'warning' })
    }
    if (declaredType && declaredType !== 'opencode') {
      issues.push({ code: 'credential-type-mismatch', severity: 'error' })
    }
  }

  if (channelType === CHANNEL_TYPE_COMMAND_CODE) {
    const apiKey =
      readString(credential, 'api_key') || readString(credential, 'apiKey')
    if (!apiKey) {
      issues.push({ code: 'missing-access-token', severity: 'error' })
    }
    if (declaredType && declaredType !== 'command_code') {
      issues.push({ code: 'credential-type-mismatch', severity: 'error' })
    }
  }

  return issues
}

function credentialLabel(
  credential: Record<string, unknown>,
  position: number
): string {
  return (
    readString(credential, 'email') ||
    readString(credential, 'account_id') ||
    readString(credential, 'project_id') ||
    readString(credential, 'nickname') ||
    readString(credential, 'uid') ||
    readString(credential, 'user_name') ||
    readString(credential, 'key_name') ||
    `#${position}`
  )
}

function buildChannelName(
  prefix: string,
  label: string,
  usedNames: Map<string, number>
): string {
  const base = `${prefix} ${label}`.trim()
  const seen = usedNames.get(base) ?? 0
  usedNames.set(base, seen + 1)
  return seen === 0 ? base : `${base} (${seen + 1})`
}

/**
 * Parse a pasted payload into one creatable channel per credential.
 */
export function parseCredentialImport(
  raw: string,
  channelType: number,
  namePrefix: string
): CredentialImportResult {
  const credentials: ImportedCredential[] = []
  const usedNames = new Map<string, number>()
  let invalidCount = 0

  splitCredentialJsonObjects(raw).forEach((block) => {
    let parsed: unknown
    try {
      parsed = JSON.parse(block)
    } catch {
      invalidCount += 1
      return
    }
    if (!isPlainObject(parsed)) {
      invalidCount += 1
      return
    }

    let entries: Record<string, unknown>[]
    if (channelType === CHANNEL_TYPE_WORKBUDDY) {
      entries = expandWorkBuddyCredentials(parsed)
    } else if (channelType === CHANNEL_TYPE_OPENCODE) {
      entries = [normalizeOpenCodeCredential(parsed)]
    } else if (channelType === CHANNEL_TYPE_COMMAND_CODE) {
      entries = [normalizeCommandCodeCredential(parsed)]
    } else {
      entries = [parsed]
    }

    for (const entry of entries) {
      const index = credentials.length + 1
      const label = credentialLabel(entry, index)
      credentials.push({
        index,
        key: JSON.stringify(entry),
        label,
        name: buildChannelName(namePrefix, label, usedNames),
        issues: collectIssues(entry, channelType),
      })
    }
  })

  return { credentials, invalidCount }
}

export function credentialHasBlockingIssue(
  credential: ImportedCredential
): boolean {
  return credential.issues.some((issue) => issue.severity === 'error')
}
