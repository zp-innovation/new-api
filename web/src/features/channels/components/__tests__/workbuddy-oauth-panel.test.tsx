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

import { WorkBuddyOAuthPanel } from '../workbuddy-oauth-panel'

const CREDENTIAL = JSON.stringify({
  type: 'workbuddy',
  realm: 'intl',
  product: 'cli',
  uid: 'uid-1',
  access_token: 'access-token',
  refresh_token: 'refresh-token',
})

const CN_DESKTOP_CREDENTIAL = JSON.stringify({
  type: 'workbuddy',
  realm: 'cn',
  product: 'workbuddy',
  uid: 'uid-cn',
  access_token: 'access-token',
  refresh_token: 'refresh-token',
})

const startWorkBuddyLogin = vi.fn()
const pollWorkBuddyLogin = vi.fn()
const refreshWorkBuddyCredential = vi.fn()

vi.mock('@/features/channels/api', () => ({
  startWorkBuddyLogin: (...args: unknown[]) => startWorkBuddyLogin(...args),
  pollWorkBuddyLogin: (...args: unknown[]) => pollWorkBuddyLogin(...args),
  refreshWorkBuddyCredential: (...args: unknown[]) =>
    refreshWorkBuddyCredential(...args),
}))

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

// The device-code poll runs on setInterval; faking only the interval keeps the
// real timers that testing-library's waitFor relies on.
function fakeIntervalOnly() {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
}

test('a device login polls until the upstream issues the credential', async () => {
  fakeIntervalOnly()
  const user = userEvent.setup()
  const open = vi.spyOn(window, 'open').mockImplementation(() => null)
  startWorkBuddyLogin.mockResolvedValue({
    success: true,
    data: {
      state: 'state-1',
      auth_url: 'https://www.workbuddy.ai/login?state=state-1',
      realm: 'intl',
      product: 'cli',
      expires_in: 600,
    },
  })
  pollWorkBuddyLogin
    .mockResolvedValueOnce({ success: true, data: { status: 'pending' } })
    .mockResolvedValueOnce({
      success: true,
      data: { status: 'ready', credential: CREDENTIAL },
    })
  const onCredential = vi.fn()

  render(<WorkBuddyOAuthPanel onCredential={onCredential} />)
  await user.click(
    screen.getByRole('button', { name: 'Get authorization link' })
  )

  await waitFor(() => {
    expect(startWorkBuddyLogin).toHaveBeenCalledWith({
      realm: 'intl',
      product: 'cli',
    })
  })
  expect(open).toHaveBeenCalledWith(
    'https://www.workbuddy.ai/login?state=state-1',
    '_blank',
    'noopener,noreferrer'
  )
  expect(screen.getByText('Waiting for authorization…')).toBeInTheDocument()

  await vi.advanceTimersByTimeAsync(3000)
  await waitFor(() => expect(pollWorkBuddyLogin).toHaveBeenCalledTimes(1))
  expect(pollWorkBuddyLogin).toHaveBeenCalledWith({ state: 'state-1' })
  expect(onCredential).not.toHaveBeenCalled()

  await vi.advanceTimersByTimeAsync(3000)
  await waitFor(() => expect(onCredential).toHaveBeenCalledWith(CREDENTIAL))

  open.mockRestore()
})

test('switching the product rewrites the credential without signing in again', async () => {
  fakeIntervalOnly()
  const user = userEvent.setup()
  const open = vi.spyOn(window, 'open').mockImplementation(() => null)
  startWorkBuddyLogin.mockResolvedValue({
    success: true,
    data: {
      state: 'state-2',
      auth_url: 'https://www.workbuddy.ai/login?state=state-2',
      realm: 'intl',
      product: 'cli',
      expires_in: 600,
    },
  })
  pollWorkBuddyLogin.mockResolvedValue({
    success: true,
    data: { status: 'ready', credential: CREDENTIAL },
  })
  const onCredential = vi.fn()

  render(<WorkBuddyOAuthPanel onCredential={onCredential} />)
  await user.click(
    screen.getByRole('button', { name: 'Get authorization link' })
  )
  await vi.advanceTimersByTimeAsync(3000)
  await waitFor(() => expect(onCredential).toHaveBeenCalledWith(CREDENTIAL))

  await user.click(screen.getAllByRole('combobox')[1])
  await user.click(await screen.findByRole('option', { name: 'VSCode' }))

  expect(startWorkBuddyLogin).toHaveBeenCalledTimes(1)
  await waitFor(() => {
    expect(
      JSON.parse(onCredential.mock.calls.at(-1)?.[0] as string)
    ).toMatchObject({ product: 'vscode', access_token: 'access-token' })
  })

  open.mockRestore()
})

test('the realm and product selectors fill the matching chat address', async () => {
  const user = userEvent.setup()
  const onBaseURLChange = vi.fn()

  render(
    <WorkBuddyOAuthPanel
      onBaseURLChange={onBaseURLChange}
      onCredential={vi.fn()}
    />
  )

  await waitFor(() =>
    expect(onBaseURLChange).toHaveBeenCalledWith('https://www.workbuddy.ai')
  )

  await user.click(screen.getAllByRole('combobox')[0])
  await user.click(
    await screen.findByRole('option', { name: 'China Mainland' })
  )
  await waitFor(() =>
    expect(onBaseURLChange).toHaveBeenLastCalledWith(
      'https://copilot.tencent.com'
    )
  )

  await user.click(screen.getAllByRole('combobox')[1])
  await user.click(await screen.findByRole('option', { name: 'Desktop' }))
  await waitFor(() =>
    expect(onBaseURLChange).toHaveBeenLastCalledWith('https://www.workbuddy.cn')
  )
})

test('an existing credential restores the selectors but does not rewrite its base URL', async () => {
  const onBaseURLChange = vi.fn()

  render(
    <WorkBuddyOAuthPanel
      autoFillBaseURL={false}
      credential={CN_DESKTOP_CREDENTIAL}
      onBaseURLChange={onBaseURLChange}
      onCredential={vi.fn()}
    />
  )

  await waitFor(() => {
    expect(screen.getAllByRole('combobox')[0]).toHaveTextContent(
      'China Mainland'
    )
    expect(screen.getAllByRole('combobox')[1]).toHaveTextContent('Desktop')
  })
  expect(onBaseURLChange).not.toHaveBeenCalled()
})

test('changing the realm while editing still fills the matching base URL', async () => {
  const user = userEvent.setup()
  const onBaseURLChange = vi.fn()

  render(
    <WorkBuddyOAuthPanel
      autoFillBaseURL={false}
      credential={CREDENTIAL}
      onBaseURLChange={onBaseURLChange}
      onCredential={vi.fn()}
    />
  )

  expect(onBaseURLChange).not.toHaveBeenCalled()

  await user.click(screen.getAllByRole('combobox')[0])
  await user.click(
    await screen.findByRole('option', { name: 'China Mainland' })
  )

  await waitFor(() =>
    expect(onBaseURLChange).toHaveBeenLastCalledWith(
      'https://copilot.tencent.com'
    )
  )
})

test('renews a saved credential without opening the device login', async () => {
  const user = userEvent.setup()
  const refreshed = JSON.stringify({
    type: 'workbuddy',
    realm: 'intl',
    product: 'cli',
    uid: 'uid-1',
    access_token: 'new-access',
    refresh_token: 'new-refresh',
  })
  refreshWorkBuddyCredential.mockResolvedValue({
    success: true,
    data: { credential: refreshed },
  })
  const onCredential = vi.fn()

  render(
    <WorkBuddyOAuthPanel credential={CREDENTIAL} onCredential={onCredential} />
  )
  await user.click(screen.getByRole('button', { name: 'Refresh credential' }))

  await waitFor(() =>
    expect(refreshWorkBuddyCredential).toHaveBeenCalledWith({
      credential: CREDENTIAL,
    })
  )
  expect(startWorkBuddyLogin).not.toHaveBeenCalled()
  expect(onCredential).toHaveBeenCalledWith(refreshed)
})
