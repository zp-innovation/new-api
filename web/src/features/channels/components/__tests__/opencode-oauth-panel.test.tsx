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

import { OpenCodeOAuthPanel } from '../opencode-oauth-panel'

const CREDENTIAL = JSON.stringify({
  type: 'opencode',
  mode: 'zen',
  access_token: 'access-token',
  refresh_token: 'refresh-token',
})

const startOpenCodeLogin = vi.fn()
const pollOpenCodeLogin = vi.fn()
const refreshOpenCodeChannelCredential = vi.fn()
const refreshOpenCodeCredential = vi.fn()
const getOpenCodeChannelUsage = vi.fn()

vi.mock('@/features/channels/api', () => ({
  startOpenCodeLogin: (...args: unknown[]) => startOpenCodeLogin(...args),
  pollOpenCodeLogin: (...args: unknown[]) => pollOpenCodeLogin(...args),
  refreshOpenCodeChannelCredential: (...args: unknown[]) =>
    refreshOpenCodeChannelCredential(...args),
  refreshOpenCodeCredential: (...args: unknown[]) =>
    refreshOpenCodeCredential(...args),
  getOpenCodeChannelUsage: (...args: unknown[]) =>
    getOpenCodeChannelUsage(...args),
}))

afterEach(() => {
  vi.clearAllMocks()
})

test('a device login polls until the upstream issues the credential', async () => {
  const user = userEvent.setup()
  const open = vi.spyOn(window, 'open').mockImplementation(() => null)
  startOpenCodeLogin.mockResolvedValue({
    success: true,
    data: {
      session_id: 'session-1',
      auth_url: 'https://opencode.ai/console/device?user_code=code',
      user_code: 'code',
      expires_in: 60,
      interval: 1,
      mode: 'zen',
    },
  })
  pollOpenCodeLogin.mockResolvedValue({
    success: true,
    data: { status: 'ready', credential: CREDENTIAL },
  })
  const onCredential = vi.fn()

  render(<OpenCodeOAuthPanel onCredential={onCredential} />)
  await user.click(
    screen.getByRole('button', { name: 'Get authorization link' })
  )

  await waitFor(() => {
    expect(startOpenCodeLogin).toHaveBeenCalledWith({ mode: 'zen' })
  })
  expect(open).toHaveBeenCalledWith(
    'https://opencode.ai/console/device?user_code=code',
    '_blank',
    'noopener,noreferrer'
  )
  await waitFor(() => expect(onCredential).toHaveBeenCalledWith(CREDENTIAL), {
    timeout: 2500,
  })

  open.mockRestore()
})

test('queries and renders Go usage windows', async () => {
  const user = userEvent.setup()
  getOpenCodeChannelUsage.mockResolvedValue({
    success: true,
    data: {
      mode: 'go',
      entries: [
        {
          account_id: 'user-1',
          email: 'user@example.com',
          org_name: 'Example Org',
          rolling: { percent: 12.5 },
          weekly: { percent: 30 },
          monthly: { percent: 40 },
        },
      ],
    },
  })

  render(
    <OpenCodeOAuthPanel
      channelId={7}
      onCredential={vi.fn()}
      onRefreshed={vi.fn()}
    />
  )
  await user.click(screen.getByRole('button', { name: 'Query usage' }))

  await waitFor(() => {
    expect(getOpenCodeChannelUsage).toHaveBeenCalledWith(7)
  })
  expect(screen.getByText('user@example.com')).toBeInTheDocument()
  expect(screen.getByText('Rolling: 12.5%')).toBeInTheDocument()
  expect(screen.getByText('Weekly: 30.0%')).toBeInTheDocument()
  expect(screen.getByText('Monthly: 40.0%')).toBeInTheDocument()
})

test('renews an unsaved credential without a channel row', async () => {
  const user = userEvent.setup()
  const refreshed = JSON.stringify({
    type: 'opencode',
    mode: 'zen',
    access_token: 'new-access',
    refresh_token: 'new-refresh',
  })
  refreshOpenCodeCredential.mockResolvedValue({
    success: true,
    data: { credential: refreshed },
  })
  const onCredential = vi.fn()

  render(<OpenCodeOAuthPanel credential={CREDENTIAL} onCredential={onCredential} />)
  await user.click(screen.getByRole('button', { name: 'Refresh credential' }))

  await waitFor(() =>
    expect(refreshOpenCodeCredential).toHaveBeenCalledWith({
      credential: CREDENTIAL,
    })
  )
  expect(refreshOpenCodeChannelCredential).not.toHaveBeenCalled()
  expect(onCredential).toHaveBeenCalledWith(refreshed)
})
