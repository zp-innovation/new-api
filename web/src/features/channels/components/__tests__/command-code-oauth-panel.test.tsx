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

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { CommandCodeOAuthPanel } from '../command-code-oauth-panel'

const CREDENTIAL = JSON.stringify({
  type: 'command_code',
  auth_type: 'api_key',
  api_key: 'user_abc123',
  user_name: 'Alice',
})

const startCommandCodeLogin = vi.fn()
const pollCommandCodeLogin = vi.fn()
const validateCommandCodeCredential = vi.fn()
const validateCommandCodeChannelCredential = vi.fn()
const getCommandCodeChannelUsage = vi.fn()

vi.mock('@/features/channels/api', () => ({
  startCommandCodeLogin: (...args: unknown[]) => startCommandCodeLogin(...args),
  pollCommandCodeLogin: (...args: unknown[]) => pollCommandCodeLogin(...args),
  validateCommandCodeCredential: (...args: unknown[]) =>
    validateCommandCodeCredential(...args),
  validateCommandCodeChannelCredential: (...args: unknown[]) =>
    validateCommandCodeChannelCredential(...args),
  getCommandCodeChannelUsage: (...args: unknown[]) =>
    getCommandCodeChannelUsage(...args),
}))

afterEach(() => {
  vi.clearAllMocks()
})

test('validates a pasted API key and hands the credential to the form', async () => {
  const user = userEvent.setup()
  validateCommandCodeCredential.mockResolvedValue({
    success: true,
    data: { credential: CREDENTIAL, user_name: 'Alice' },
  })
  const onCredential = vi.fn()

  render(<CommandCodeOAuthPanel onCredential={onCredential} />)
  await user.type(
    screen.getByLabelText('Command Code API key or auth.json'),
    'user_abc123'
  )
  await user.click(screen.getByRole('button', { name: 'Validate and use' }))

  await waitFor(() => {
    expect(validateCommandCodeCredential).toHaveBeenCalledWith({
      credential: 'user_abc123',
    })
  })
  expect(onCredential).toHaveBeenCalledWith(CREDENTIAL)
})

test('queries and renders Command Code credits', async () => {
  const user = userEvent.setup()
  getCommandCodeChannelUsage.mockResolvedValue({
    success: true,
    data: {
      entries: [
        {
          user_name: 'Alice',
          monthly_credits: 12.5,
          purchased_credits: 3,
          free_credits: 1,
          monthly_used: 7.5,
          monthly_cap: 20,
          five_hour: { used: 2, cap: 10, percent: 20 },
        },
      ],
    },
  })

  render(<CommandCodeOAuthPanel channelId={7} onCredential={vi.fn()} />)
  await user.click(screen.getByRole('button', { name: 'Query usage' }))

  await waitFor(() => {
    expect(getCommandCodeChannelUsage).toHaveBeenCalledWith(7)
  })
  expect(await screen.findByText(/Monthly credits/)).toBeInTheDocument()
  expect(screen.getAllByText('Alice').length).toBeGreaterThan(0)
})
