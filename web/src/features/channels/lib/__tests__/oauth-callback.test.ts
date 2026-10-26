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

import { extractAuthorizationCode } from '../oauth-callback'

const CODE =
  '4/0AXlqoi7cOA6JGECmfnefcaaDTPbu169KiOhVyrVEnZTOikz9Ox_27kbc9CrUl5EQEktTw'

const CALLBACK_URL = `http://localhost:8085/callback?state=lYJ1qelCiE8-0mvtgT21mQC4-tFvJmaVEvM9MpcagLo&iss=https://accounts.google.com&code=${CODE}&scope=email%20profile&authuser=1&prompt=consent`

test('keeps a bare authorization code unchanged', () => {
  expect(extractAuthorizationCode(CODE)).toBe(CODE)
})

test('reads the code out of a pasted callback URL', () => {
  expect(extractAuthorizationCode(CALLBACK_URL)).toBe(CODE)
})

test('accepts a bare query string and a trailing fragment', () => {
  expect(extractAuthorizationCode(`code=${CODE}`)).toBe(CODE)
  expect(extractAuthorizationCode(`${CALLBACK_URL}#tab`)).toBe(CODE)
})

test('decodes a percent-escaped code', () => {
  const encoded = `http://localhost:8085/callback?code=${encodeURIComponent(CODE)}`
  expect(extractAuthorizationCode(encoded)).toBe(CODE)
})

test('trims surrounding whitespace and ignores blank input', () => {
  expect(extractAuthorizationCode(`  ${CODE}\n`)).toBe(CODE)
  expect(extractAuthorizationCode('   ')).toBe('')
})

test('leaves a callback URL without a code untouched', () => {
  const failed = 'http://localhost:8085/callback?error=access_denied'
  expect(extractAuthorizationCode(failed)).toBe(failed)
})
