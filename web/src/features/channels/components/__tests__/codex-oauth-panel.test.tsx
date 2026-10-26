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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CodexOAuthPanel } from '../codex-oauth-panel'

const CODE = 'ac_4d8f1c0e7b'
const CALLBACK_URL = `http://localhost:1455/auth/callback?code=${CODE}&state=state-1&scope=openid+profile`

const generateCodexAuthURL = vi.fn()
const exchangeCodexCode = vi.fn()

vi.mock('@/features/channels/api', () => ({
  generateCodexAuthURL: (...args: unknown[]) => generateCodexAuthURL(...args),
  exchangeCodexCode: (...args: unknown[]) => exchangeCodexCode(...args),
  refreshCodexCredential: vi.fn(),
}))

describe('CodexOAuthPanel', () => {
  beforeEach(() => {
    generateCodexAuthURL.mockReset()
    exchangeCodexCode.mockReset()
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  it('extracts the code from a pasted callback URL and exchanges it', async () => {
    generateCodexAuthURL.mockResolvedValue({
      success: true,
      data: {
        auth_url: 'https://auth.openai.com/oauth/authorize?client_id=app_x',
        session_id: 'session-1',
        state: 'state-1',
        redirect_uri: 'http://localhost:1455/auth/callback',
      },
    })
    exchangeCodexCode.mockResolvedValue({
      success: true,
      data: {
        type: 'codex',
        access_token: 'access-1',
        refresh_token: 'refresh-1',
        account_id: 'acc-1',
      },
    })
    const onCredential = vi.fn()

    render(<CodexOAuthPanel onCredential={onCredential} />)

    await userEvent.click(
      screen.getByRole('button', { name: 'Get authorization link' })
    )
    await waitFor(() => expect(generateCodexAuthURL).toHaveBeenCalledWith({}))

    const input = await screen.findByPlaceholderText(
      'Paste the authorization code'
    )
    fireEvent.change(input, { target: { value: CALLBACK_URL } })

    expect(input).toHaveValue(CODE)
    expect(
      screen.getByText('Authorization code extracted from the callback link')
    ).toBeInTheDocument()

    await userEvent.click(
      screen.getByRole('button', { name: 'Create credential' })
    )

    await waitFor(() =>
      expect(exchangeCodexCode).toHaveBeenCalledWith({
        session_id: 'session-1',
        state: 'state-1',
        code: CODE,
      })
    )
    expect(onCredential).toHaveBeenCalledTimes(1)
  })
})
