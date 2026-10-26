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

import { ImportAccountsDialog } from '../dialogs/import-accounts-dialog'

const createChannel = vi.fn()
const fetchUpstreamModels = vi.fn()
const searchChannels = vi.fn()
const updateChannel = vi.fn()

vi.mock('@/features/channels/api', () => ({
  createChannel: (...args: unknown[]) => createChannel(...args),
  fetchUpstreamModels: (...args: unknown[]) => fetchUpstreamModels(...args),
  searchChannels: (...args: unknown[]) => searchChannels(...args),
  updateChannel: (...args: unknown[]) => updateChannel(...args),
}))

const CREDENTIAL = {
  type: 'antigravity',
  access_token: 'access-token-1',
  refresh_token: 'refresh-token-1',
  project_id: 'aicode-consumers',
  email: 'jack.csq@gmail.com',
}

test('creates one channel per pasted credential and fetches its models', async () => {
  const user = userEvent.setup()
  const onImported = vi.fn()
  const name = 'Google Antigravity jack.csq@gmail.com'
  createChannel.mockResolvedValue({ success: true })
  searchChannels.mockResolvedValue({
    success: true,
    data: { items: [{ id: 42, name }], total: 1 },
  })
  fetchUpstreamModels.mockResolvedValue({
    success: true,
    data: ['gemini-3.8-flash-high'],
  })
  updateChannel.mockResolvedValue({ success: true })

  render(
    <ImportAccountsDialog
      open
      onOpenChange={vi.fn()}
      channelType={64}
      channelTypeName='Google Antigravity'
      group='default'
      onImported={onImported}
    />
  )

  fireEvent.change(screen.getByLabelText('Paste credential JSON'), {
    target: { value: JSON.stringify(CREDENTIAL, null, 2) },
  })

  expect(screen.getByText(name)).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Import 1 account(s)' }))

  await waitFor(() => {
    expect(createChannel).toHaveBeenCalledWith({
      mode: 'single',
      channel: expect.objectContaining({
        name,
        type: 64,
        key: JSON.stringify(CREDENTIAL),
        models: '',
        group: 'default',
        status: 1,
      }),
    })
  })
  await waitFor(() => {
    expect(fetchUpstreamModels).toHaveBeenCalledWith(42)
  })
  await waitFor(() => {
    expect(updateChannel).toHaveBeenCalledWith(42, {
      models: 'gemini-3.8-flash-high',
    })
  })
  await waitFor(() => {
    expect(onImported).toHaveBeenCalledWith(1)
  })
})

test('reports a Codex credential without account_id as unimportable', async () => {
  const user = userEvent.setup()
  createChannel.mockClear()

  render(
    <ImportAccountsDialog
      open
      onOpenChange={vi.fn()}
      channelType={57}
      channelTypeName='OpenAI Codex'
    />
  )

  fireEvent.change(screen.getByLabelText('Paste credential JSON'), {
    target: {
      value: JSON.stringify({ type: 'codex', access_token: 'access-token-1' }),
    },
  })

  expect(screen.getByText('Missing account_id')).toBeInTheDocument()
  const importButton = screen.getByRole('button', {
    name: 'Import 0 account(s)',
  })
  expect(importButton).toBeDisabled()

  await user.click(importButton)
  expect(createChannel).not.toHaveBeenCalled()
})

test('imports a workbuddy-accounts export as one channel per account', async () => {
  const user = userEvent.setup()
  const onOpenChange = vi.fn()
  const onEditChannel = vi.fn()
  const name = 'WorkBuddy WorkBuddy One'
  createChannel.mockClear()
  createChannel.mockResolvedValue({ success: true })
  searchChannels.mockResolvedValue({
    success: true,
    data: { items: [{ id: 7, name }], total: 1 },
  })
  fetchUpstreamModels.mockResolvedValue({
    success: true,
    data: ['hy4-preview-f'],
  })
  updateChannel.mockResolvedValue({ success: true })

  render(
    <ImportAccountsDialog
      open
      onOpenChange={onOpenChange}
      onEditChannel={onEditChannel}
      channelType={65}
      channelTypeName='WorkBuddy'
      group='default'
    />
  )

  fireEvent.change(screen.getByLabelText('Paste credential JSON'), {
    target: {
      value: JSON.stringify({
        format: 'workbuddy-accounts',
        version: 1,
        count: 1,
        accounts: [
          {
            uid: 'uid-1',
            nickname: 'WorkBuddy One',
            domain: 'www.codebuddy.cn',
            realm: 'cn',
            platform: 'CLI',
            product: 'workbuddy',
            enterpriseId: '',
            accessToken: 'access-token-1',
            refreshToken: 'refresh-token-1',
            expiresAt: 1794953420,
          },
        ],
      }),
    },
  })

  expect(screen.getByText(name)).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Import 1 account(s)' }))

  await waitFor(() => {
    expect(createChannel).toHaveBeenCalledWith({
      mode: 'single',
      channel: expect.objectContaining({
        name,
        type: 65,
        key: JSON.stringify({
          type: 'workbuddy',
          realm: 'cn',
          product: 'cli',
          uid: 'uid-1',
          nickname: 'WorkBuddy One',
          domain: 'www.codebuddy.cn',
          access_token: 'access-token-1',
          refresh_token: 'refresh-token-1',
          expires_at: '2026-11-17T22:10:20Z',
        }),
        group: 'default',
        status: 1,
      }),
    })
  })
  await waitFor(() => {
    expect(fetchUpstreamModels).toHaveBeenCalledWith(7)
  })

  const edit = await screen.findByRole('button', { name: 'Edit' })
  await user.click(edit)
  expect(onEditChannel).toHaveBeenCalledWith({ id: 7, name })

  const done = await screen.findByRole('button', { name: 'Done' })
  expect(
    screen.queryByRole('button', { name: 'Import 1 account(s)' })
  ).not.toBeInTheDocument()

  await user.click(done)
  expect(onOpenChange).toHaveBeenCalledWith(false)
})

test('resolves the created channel only when the Edit action needs it', async () => {
  const user = userEvent.setup()
  const onEditChannel = vi.fn()
  const name = 'WorkBuddy WorkBuddy One'
  createChannel.mockClear()
  searchChannels.mockClear()
  fetchUpstreamModels.mockClear()
  updateChannel.mockClear()
  createChannel.mockResolvedValue({ success: true })
  searchChannels.mockResolvedValue({
    success: true,
    data: { items: [{ id: 9, name }], total: 1 },
  })

  render(
    <ImportAccountsDialog
      open
      onOpenChange={vi.fn()}
      onEditChannel={onEditChannel}
      channelType={65}
      channelTypeName='WorkBuddy'
      group='default'
    />
  )

  await user.click(
    screen.getByRole('checkbox', {
      name: 'Fetch models from upstream after import',
    })
  )

  fireEvent.change(screen.getByLabelText('Paste credential JSON'), {
    target: {
      value: JSON.stringify({
        format: 'workbuddy-accounts',
        version: 1,
        count: 1,
        accounts: [
          {
            uid: 'uid-1',
            nickname: 'WorkBuddy One',
            realm: 'cn',
            platform: 'CLI',
            accessToken: 'access-token-1',
            refreshToken: 'refresh-token-1',
          },
        ],
      }),
    },
  })

  await user.click(screen.getByRole('button', { name: 'Import 1 account(s)' }))

  const edit = await screen.findByRole('button', { name: 'Edit' })
  expect(searchChannels).not.toHaveBeenCalled()

  await user.click(edit)
  await waitFor(() =>
    expect(onEditChannel).toHaveBeenCalledWith({ id: 9, name })
  )
  expect(searchChannels).toHaveBeenCalledWith({ keyword: name, page_size: 20 })
})
