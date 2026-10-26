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

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { AntigravityOAuthPanel } from '../antigravity-oauth-panel'

const CODE =
  '4/0AXlqoi7cOA6JGECmfnefcaaDTPbu169KiOhVyrVEnZTOikz9Ox_27kbc9CrUl5EQEktTw'

const CALLBACK_URL = `http://localhost:8085/callback?state=lYJ1qelCiE8-0mvtgT21mQC4-tFvJmaVEvM9MpcagLo&iss=https://accounts.google.com&code=${CODE}&scope=email%20profile&authuser=1&prompt=consent`

const generateAntigravityAuthURL = vi.fn()
const exchangeAntigravityCode = vi.fn()
const refreshAntigravityCredential = vi.fn()

vi.mock('@/features/channels/api', () => ({
  generateAntigravityAuthURL: (...args: unknown[]) =>
    generateAntigravityAuthURL(...args),
  exchangeAntigravityCode: (...args: unknown[]) =>
    exchangeAntigravityCode(...args),
  getAntigravityChannelQuota: vi.fn(),
  refreshAntigravityChannelCredential: vi.fn(),
  refreshAntigravityCredential: (...args: unknown[]) =>
    refreshAntigravityCredential(...args),
}))

test('pasting the whole callback URL exchanges only its code parameter', async () => {
  const user = userEvent.setup()
  const open = vi.spyOn(window, 'open').mockImplementation(() => null)
  generateAntigravityAuthURL.mockResolvedValue({
    success: true,
    data: {
      auth_url: 'https://accounts.google.com/o/oauth2/v2/auth?state=state-1',
      session_id: 'session-1',
      state: 'state-1',
    },
  })
  exchangeAntigravityCode.mockResolvedValue({
    success: true,
    data: {
      type: 'antigravity',
      access_token: 'access-token',
      project_id: 'project-1',
    },
  })
  const onCredential = vi.fn()

  render(<AntigravityOAuthPanel onCredential={onCredential} />)
  await user.click(
    screen.getByRole('button', { name: 'Get authorization link' })
  )

  const input = await screen.findByPlaceholderText(
    'Paste the authorization code'
  )
  fireEvent.change(input, { target: { value: CALLBACK_URL } })

  expect(input).toHaveValue(CODE)
  expect(
    screen.getByText('Authorization code extracted from the callback link')
  ).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Create credential' }))

  await waitFor(() => {
    expect(exchangeAntigravityCode).toHaveBeenCalledWith({
      session_id: 'session-1',
      state: 'state-1',
      code: CODE,
    })
  })
  expect(onCredential).toHaveBeenCalledTimes(1)
  open.mockRestore()
})

test('renews an unsaved credential and keeps its account fields', async () => {
  const user = userEvent.setup()
  refreshAntigravityCredential.mockResolvedValue({
    success: true,
    data: {
      type: 'antigravity',
      access_token: 'new-access',
      refresh_token: 'new-refresh',
      expires_at: '2026-02-01T00:00:00Z',
    },
  })
  const onCredential = vi.fn()
  const stored = JSON.stringify({
    type: 'antigravity',
    access_token: 'old-access',
    refresh_token: 'old-refresh',
    email: 'operator@example.com',
    project_id: 'project-1',
    plan_type: 'free',
  })

  render(
    <AntigravityOAuthPanel
      credential={stored}
      proxy='http://proxy.local'
      onCredential={onCredential}
    />
  )
  await user.click(screen.getByRole('button', { name: 'Refresh credential' }))

  await waitFor(() =>
    expect(refreshAntigravityCredential).toHaveBeenCalledWith({
      refresh_token: 'old-refresh',
      proxy: 'http://proxy.local',
    })
  )
  expect(onCredential).toHaveBeenCalledTimes(1)
  expect(JSON.parse(onCredential.mock.calls[0][0])).toMatchObject({
    access_token: 'new-access',
    refresh_token: 'new-refresh',
    project_id: 'project-1',
    email: 'operator@example.com',
    plan_type: 'free',
  })
})
