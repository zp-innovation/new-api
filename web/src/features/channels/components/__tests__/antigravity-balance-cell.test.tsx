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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  CHANNEL_TYPE_ANTIGRAVITY,
  CHANNEL_TYPE_COMMAND_CODE,
  CHANNEL_TYPE_GEMINI_WEB,
  CHANNEL_TYPE_OPENCODE,
  CHANNEL_TYPE_WORKBUDDY,
} from '../../constants'
import { channelSchema } from '../../types'
import { ChannelRowActionsLayoutContext } from '../channel-row-actions-context'
import { BalanceCell } from '../channels-columns'
import { ChannelsDialogs } from '../channels-dialogs'
import { ChannelsProvider, useChannels } from '../channels-provider'

const originalAuth = useAuthStore.getState().auth
let client: QueryClient

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 1, username: 'root', role: ROLE.SUPER_ADMIN },
    },
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  useAuthStore.setState({ auth: originalAuth })
})

it('opens the quota dialog from the balance cell of an Antigravity channel', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/64/antigravity/quota') {
      return {
        data: {
          success: true,
          data: {
            channel_id: 64,
            channel_name: 'Google Antigravity',
            email: 'jack.csq@gmail.com',
            plan_type: 'Free',
            project_id: 'aicode-consumers',
            models: [
              {
                model: 'gemini-3.8-flash-high',
                remaining_fraction: 0.9984926,
                reset_time: '2026/10/10 22:13:52',
              },
            ],
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const channel = channelSchema.parse({
    id: 64,
    type: CHANNEL_TYPE_ANTIGRAVITY,
    key: '',
    name: 'Google Antigravity',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelRowActionsLayoutContext.Provider value='table'>
          <BalanceCell channel={channel} />
        </ChannelRowActionsLayoutContext.Provider>
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.click(screen.getByText('Account Info'))

  expect(
    await screen.findByRole('dialog', {
      name: 'Antigravity Account & Quota',
    })
  ).toBeInTheDocument()
  expect(await screen.findByText('gemini-3.8-flash-high')).toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => url === '/api/channel/64/antigravity/quota')
  ).toBe(true)
  expect(get.mock.calls.some(([url]) => url.includes('update_balance'))).toBe(
    false
  )
})

it('opens the credits dialog from the balance cell of a WorkBuddy channel', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/65/workbuddy/credits') {
      return {
        data: {
          success: true,
          data: {
            channel_id: 65,
            channel_name: 'WorkBuddy',
            credits: {
              remain: 25,
              used: 75,
              size: 100,
              used_percent: '75.0%',
              remain_percent: '25.0%',
              is_paid_user: false,
              is_enterprise: false,
              packages: [
                {
                  name: 'Free pack',
                  remain: 25,
                  used: 75,
                  size: 100,
                  unit: 'credits',
                  in_usage: true,
                  auto_renew: false,
                  no_expiry: false,
                  is_expired: false,
                  status: 0,
                },
              ],
              updated_at: '2026-10-11T06:00:00Z',
            },
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const channel = channelSchema.parse({
    id: 65,
    type: CHANNEL_TYPE_WORKBUDDY,
    key: '',
    name: 'WorkBuddy',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelRowActionsLayoutContext.Provider value='table'>
          <BalanceCell channel={channel} />
        </ChannelRowActionsLayoutContext.Provider>
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.click(screen.getByText('Account Info'))

  expect(
    await screen.findByRole('dialog', {
      name: 'WorkBuddy Account & Credits',
    })
  ).toBeInTheDocument()
  expect(await screen.findByText('Free pack')).toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => url === '/api/channel/65/workbuddy/credits')
  ).toBe(true)
  expect(get.mock.calls.some(([url]) => url.includes('update_balance'))).toBe(
    false
  )
})

it('opens the usage dialog from the balance cell of a Command Code channel', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/67/command-code/usage') {
      return {
        data: {
          success: true,
          data: {
            entries: [
              {
                user_name: 'Alice',
                plan_name: 'Pro',
                monthly_credits: 12.5,
                purchased_credits: 3,
                free_credits: 1,
                monthly_used: 7.5,
                monthly_cap: 20,
                five_hour: { used: 2, cap: 10, percent: 20 },
                weekly: { used: 4, cap: 20, percent: 20 },
              },
            ],
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const channel = channelSchema.parse({
    id: 67,
    type: CHANNEL_TYPE_COMMAND_CODE,
    key: '',
    name: 'Command Code',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelRowActionsLayoutContext.Provider value='table'>
          <BalanceCell channel={channel} />
        </ChannelRowActionsLayoutContext.Provider>
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.click(screen.getByText('Account Info'))

  expect(
    await screen.findByRole('dialog', {
      name: 'Command Code Account & Usage',
    })
  ).toBeInTheDocument()
  expect(await screen.findByText('Credit windows')).toBeInTheDocument()
  expect(screen.getAllByText('Alice').length).toBeGreaterThan(0)
  expect(
    get.mock.calls.some(([url]) => url === '/api/channel/67/command-code/usage')
  ).toBe(true)
  expect(get.mock.calls.some(([url]) => url.includes('update_balance'))).toBe(
    false
  )
})

function OpenBalanceQuery(props: {
  channel: ReturnType<typeof channelSchema.parse>
}) {
  const { setCurrentRow, setOpen } = useChannels()
  useEffect(() => {
    setCurrentRow(props.channel)
    setOpen('balance-query')
  }, [props.channel, setCurrentRow, setOpen])
  return null
}

it('routes the row-action balance query to the Command Code usage dialog', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/67/command-code/usage') {
      return {
        data: {
          success: true,
          data: {
            entries: [
              {
                user_name: 'Alice',
                plan_name: 'GOAT',
                monthly_credits: 52.7,
                purchased_credits: 0,
                free_credits: 0,
                monthly_used: 17.3,
                monthly_cap: 70,
                five_hour: { used: 0.005, cap: 14, percent: 0.03 },
                weekly: { used: 0.005, cap: 35, percent: 0.01 },
              },
            ],
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const channel = channelSchema.parse({
    id: 67,
    type: CHANNEL_TYPE_COMMAND_CODE,
    key: '',
    name: 'Command Code',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })

  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <OpenBalanceQuery channel={channel} />
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  expect(
    await screen.findByRole('dialog', {
      name: 'Command Code Account & Usage',
    })
  ).toBeInTheDocument()
  expect(await screen.findByText('Credit windows')).toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => url === '/api/channel/67/command-code/usage')
  ).toBe(true)
  expect(get.mock.calls.some(([url]) => url.includes('update_balance'))).toBe(
    false
  )
})

it('opens the usage dialog from the balance cell of an OpenCode channel', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/66/opencode/usage') {
      return {
        data: {
          success: true,
          data: {
            mode: 'go',
            entries: [
              {
                account_id: 'user-1',
                email: 'user@example.com',
                org_name: 'Example Org',
                rolling: {
                  status: 'ok',
                  percent: 12.5,
                  resets_at: '2026-10-16T10:00:00Z',
                },
                weekly: { status: 'ok', percent: 30 },
                monthly: { status: 'ok', percent: 40 },
              },
            ],
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const channel = channelSchema.parse({
    id: 66,
    type: CHANNEL_TYPE_OPENCODE,
    key: '',
    name: 'OpenCode',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelRowActionsLayoutContext.Provider value='table'>
          <BalanceCell channel={channel} />
        </ChannelRowActionsLayoutContext.Provider>
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.click(screen.getByText('Account Info'))

  expect(
    await screen.findByRole('dialog', {
      name: 'OpenCode Account & Usage',
    })
  ).toBeInTheDocument()
  expect(await screen.findByText('Usage windows')).toBeInTheDocument()
  expect(screen.getAllByText('user@example.com').length).toBeGreaterThan(0)
  expect(screen.getByText('12.5%')).toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => url === '/api/channel/66/opencode/usage')
  ).toBe(true)
  expect(get.mock.calls.some(([url]) => url.includes('update_balance'))).toBe(
    false
  )
})

it('shows Gemini Web account status without a balance update action', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: [] },
  }))
  const channel = channelSchema.parse({
    id: 68,
    type: CHANNEL_TYPE_GEMINI_WEB,
    key: '',
    name: 'Gemini Web',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelRowActionsLayoutContext.Provider value='table'>
          <BalanceCell channel={channel} />
        </ChannelRowActionsLayoutContext.Provider>
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  expect(screen.queryByText('Account Info')).not.toBeInTheDocument()
  expect(screen.queryByText('Update Balance')).not.toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => String(url).includes('update_balance'))
  ).toBe(false)
})
