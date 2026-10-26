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
 * Channel keys hold one JSON credential object. The editor field can also hold
 * a half-typed value, so reading a field never throws: a missing or malformed
 * key simply reads as empty, which is what the action gates need.
 */
export function readCredentialField(
  raw: string | undefined,
  field: string
): string {
  if (!raw) return ''
  const trimmed = raw.trim()
  if (!trimmed.startsWith('{')) return ''
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return ''
    }
    const value = (parsed as Record<string, unknown>)[field]
    return typeof value === 'string' ? value : ''
  } catch {
    return ''
  }
}
