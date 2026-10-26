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
 * Helpers for the WorkBuddy channel key.
 *
 * A WorkBuddy key is the credential JSON itself, written on a single line: the
 * channel key column is split on newlines so that a multi-account channel can
 * keep one credential per line. Pretty-printing the credential therefore turns
 * one account into several broken keys, and every helper here keeps the value
 * compact.
 */

/** Official clients the outbound identity can imitate. */
const WORKBUDDY_PRODUCTS = ['cli', 'vscode', 'workbuddy'] as const

export type WorkBuddyProduct = (typeof WORKBUDDY_PRODUCTS)[number]

/** Realms differ by upstream host and billing behaviour, never by credential. */
const WORKBUDDY_REALMS = ['intl', 'cn'] as const

export type WorkBuddyRealm = (typeof WORKBUDDY_REALMS)[number]

export function normalizeWorkBuddyRealm(value: string): WorkBuddyRealm {
  return value === 'cn' ? 'cn' : 'intl'
}

export function normalizeWorkBuddyProduct(value: string): WorkBuddyProduct {
  if (value === 'vscode') return 'vscode'
  if (value === 'workbuddy') return 'workbuddy'
  return 'cli'
}

/**
 * Chat API addresses by realm and product.
 *
 * The desktop and VSCode identities ride the workbuddy.cn edge on the CN
 * realm. The CLI identity keeps the copilot.tencent.com edge. The
 * international realm has one edge for every product.
 */
export function workBuddyChatBaseURL(realm: string, product: string): string {
  if (realm !== 'cn') return 'https://www.workbuddy.ai'
  if (product === 'cli') return 'https://copilot.tencent.com'
  return 'https://www.workbuddy.cn'
}

/** True when a stored base URL is one of the built-in WorkBuddy edges. */
export function isWorkBuddyChatBaseURL(value: string): boolean {
  const normalized = value.trim().replace(/\/+$/, '')
  return (
    normalized === 'https://www.workbuddy.ai' ||
    normalized === 'https://copilot.tencent.com' ||
    normalized === 'https://www.workbuddy.cn'
  )
}

/**
 * Rewrites the product of a stored credential without touching the tokens.
 *
 * The product only selects the outbound identity, so switching it must not
 * force another sign-in: the operator re-authorizes for a new realm, not for a
 * new client. Values that are not a JSON object are returned untouched.
 */
export function withWorkBuddyProduct(
  credential: string,
  product: string
): string {
  const parsed = parseWorkBuddyCredential(credential)
  if (!parsed) return credential
  parsed.product = product
  return JSON.stringify(parsed)
}

/**
 * Parses a stored credential. Returns null when the text is not a JSON object,
 * which is how the key field detects a pasted token or a broken paste.
 */
export function parseWorkBuddyCredential(
  credential: string
): Record<string, unknown> | null {
  const trimmed = credential.trim()
  if (!trimmed) return null
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (
      typeof parsed !== 'object' ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      return null
    }
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
}

/** Reads the realm, product and derived chat address from a credential. */
export function workBuddyCredentialEndpoint(credential: string): {
  realm: WorkBuddyRealm
  product: WorkBuddyProduct
  baseURL: string
} | null {
  const parsed = parseWorkBuddyCredential(credential)
  if (!parsed) return null

  const realm = normalizeWorkBuddyRealm(
    typeof parsed.realm === 'string' ? parsed.realm : ''
  )
  const product = normalizeWorkBuddyProduct(
    typeof parsed.product === 'string' ? parsed.product : ''
  )

  return {
    realm,
    product,
    baseURL: workBuddyChatBaseURL(realm, product),
  }
}
