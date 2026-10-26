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
 * Reads the authorization code out of whatever was pasted into the channel
 * editor.
 *
 * The OAuth providers used by this console redirect to a localhost callback
 * that is not running (Google: `localhost:8085`, OpenAI: `localhost:1455`), so
 * the usual way to capture the code is copying the whole address bar, i.e. a
 * full callback URL carrying `code`, `state`, `scope` and friends. Only the
 * `code` parameter may reach the exchange call, and a bare code must keep
 * working.
 */
export function extractAuthorizationCode(raw: string): string {
  const text = raw.trim()
  if (!text) return ''

  const match = /(?:^|[?&#])code=([^&\s#]+)/.exec(text)
  if (!match) return text

  const value = match[1]
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}
