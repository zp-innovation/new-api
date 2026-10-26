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

import type { AntigravityCredentialResponse } from '../api'

type AntigravityCredentialFields = NonNullable<
  AntigravityCredentialResponse['data']
>

/**
 * The refresh response only carries the token fields, so the account fields
 * are optional here even though the exchange response always has them.
 */
type AntigravityRefreshedFields = Partial<AntigravityCredentialFields>

/**
 * The refresh endpoint returns the token fields only: Google does not repeat
 * project_id, email or plan_type, and the host rejects a key without
 * project_id. Merge the refreshed token over the stored credential so a manual
 * refresh cannot turn a working key into an unusable one.
 *
 * Returns null when the stored key carries no project_id: there is nothing
 * valid to merge into, and the caller must keep the key untouched.
 */
export function mergeAntigravityCredential(
  stored: string,
  refreshed: AntigravityRefreshedFields
): string | null {
  const merged: Record<string, unknown> = {}
  const trimmed = stored.trim()
  if (trimmed.startsWith('{')) {
    try {
      const parsed: unknown = JSON.parse(trimmed)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        Object.assign(merged, parsed as Record<string, unknown>)
      }
    } catch {
      // A malformed stored key still receives the refreshed token below, but
      // has no account fields, so the project_id gate rejects it.
    }
  }

  for (const [field, value] of Object.entries(refreshed)) {
    if (typeof value !== 'string' || value === '') continue
    merged[field] = value
  }

  if (typeof merged.project_id !== 'string' || merged.project_id === '') {
    return null
  }
  return JSON.stringify(merged, null, 2)
}
