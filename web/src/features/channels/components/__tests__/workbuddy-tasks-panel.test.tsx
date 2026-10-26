import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { WorkBuddyTasksPanel } from '../workbuddy-tasks-panel'

const settings = {
  enabled: true,
  auto_checkin: true,
  auto_activity_report: true,
  auto_travel: true,
  auto_streak_bonus: true,
  auto_growth_tasks: false,
  auto_night_cat: false,
  auto_daily_chat: true,
  daily_chat_web: true,
  schedule_hours_intl: '09:00,21:00',
  daily_request_limit_intl: 15,
  schedule_hours_cn: '09:00,21:00',
  daily_request_limit_cn: 40,
  growth_request_limit: 60,
  night_hour: 1,
  account_gap_seconds: 1,
  run_retention_days: 90,
}

const summaries = {
  intl: {
    realm: 'intl',
    accounts: 1,
    done_today: 0,
    streak_average: 0,
    remain_credit: 187.93,
    failed_last_run: 0,
    last_run_at: 0,
  },
  cn: {
    realm: 'cn',
    accounts: 0,
    done_today: 0,
    streak_average: 0,
    remain_credit: 0,
    failed_last_run: 0,
    last_run_at: 0,
  },
}

const account = {
  channel_id: 15,
  channel_name: 'WorkBuddy steven',
  channel_status: 1,
  account_uid: 'uid-1',
  nickname: 'steven.openai@gmail.com',
  realm: 'intl',
  product: 'cli',
  tasks_enabled: true,
  checked_in_today: false,
  daily_chat_today: false,
  activity_report_today: false,
  last_checkin_at: 0,
  last_daily_chat_at: 0,
  last_activity_report_at: 0,
  last_run_at: 0,
  last_task: '',
  last_status: '',
  last_message: '',
  streak_days: 0,
  daily_credit: 0,
  today_credit: 0,
  remain_credit: 187.93,
}

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <WorkBuddyTasksPanel />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

describe('WorkBuddy tasks panel', () => {
  test('lists the accounts the channel-derived endpoint returns', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (String(url).includes('/workbuddy/accounts')) {
        return {
          data: {
            success: true,
            data: { accounts: [account], settings, realm: 'all', summaries },
          },
        } as never
      }
      return { data: { success: true, data: { runs: [], total: 0 } } } as never
    })

    renderPanel()

    expect(
      await screen.findByText('steven.openai@gmail.com')
    ).toBeInTheDocument()
    // The cached balance shows on the realm card and on the account row.\n    expect(screen.getAllByText('187.93')).toHaveLength(2)
    // The growth centre is off by default, so its switch must be off.
    expect(
      screen.getByRole('switch', { name: 'Growth tasks (China)' })
    ).not.toBeChecked()
  })

  test('switching the realm segment asks the server for that realm', async () => {
    const user = userEvent.setup()
    const get = vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (String(url).includes('/workbuddy/accounts')) {
        return {
          data: {
            success: true,
            data: { accounts: [account], settings, realm: 'all', summaries },
          },
        } as never
      }
      return { data: { success: true, data: { runs: [], total: 0 } } } as never
    })

    renderPanel()
    await screen.findByText('steven.openai@gmail.com')
    await user.click(screen.getByRole('button', { name: /China/ }))

    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/channel/workbuddy/accounts', {
        params: { realm: 'cn' },
      })
    )
  })

  test('refreshing the credits posts the refresh action', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (String(url).includes('/workbuddy/accounts')) {
        return {
          data: {
            success: true,
            data: { accounts: [account], settings, realm: 'all', summaries },
          },
        } as never
      }
      return { data: { success: true, data: { runs: [], total: 0 } } } as never
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { queued: false } },
    } as never)

    renderPanel()
    await screen.findByText('steven.openai@gmail.com')
    await user.click(screen.getByRole('button', { name: 'Refresh credits' }))

    expect(post).toHaveBeenCalledWith('/api/channel/workbuddy/run', {
      channel_ids: [],
      account_uid: '',
      realm: 'all',
      actions: ['refresh_credits'],
    })
  })

  test('the account task switch is independent from channel routing', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (String(url).includes('/workbuddy/accounts')) {
        return {
          data: {
            success: true,
            data: { accounts: [account], settings, realm: 'all', summaries },
          },
        } as never
      }
      return { data: { success: true, data: { runs: [], total: 0 } } } as never
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: { account: { ...account, tasks_enabled: false } },
      },
    } as never)

    renderPanel()
    await screen.findByText('steven.openai@gmail.com')
    await user.click(screen.getByRole('switch', { name: 'Tasks enabled' }))

    expect(post).toHaveBeenCalledWith('/api/channel/workbuddy/accounts/tasks', {
      channel_id: 15,
      account_uid: 'uid-1',
      enabled: false,
    })
  })

  test('checking every account in posts the check-in action', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (String(url).includes('/workbuddy/accounts')) {
        return {
          data: {
            success: true,
            data: { accounts: [account], settings, realm: 'all', summaries },
          },
        } as never
      }
      return { data: { success: true, data: { runs: [], total: 0 } } } as never
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { queued: false } },
    } as never)

    renderPanel()
    await screen.findByText('steven.openai@gmail.com')
    await user.click(screen.getByRole('button', { name: 'Check in all' }))

    expect(post).toHaveBeenCalledWith('/api/channel/workbuddy/run', {
      channel_ids: [],
      account_uid: '',
      realm: 'all',
      actions: ['checkin'],
    })
  })

  test('renders the daily rollup returned by the usage history endpoint', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (String(url).includes('/workbuddy/accounts')) {
        return {
          data: {
            success: true,
            data: { accounts: [account], settings, realm: 'all', summaries },
          },
        } as never
      }
      if (String(url).includes('/workbuddy/usage/history')) {
        return {
          data: {
            success: true,
            data: {
              days: 14,
              rows: [
                { day: '2026-10-24', realm: 'intl', total_tokens: 1000 },
                { day: '2026-10-25', realm: 'intl', total_tokens: 3000 },
              ],
            },
          },
        } as never
      }
      return { data: { success: true, data: { runs: [], total: 0 } } } as never
    })

    renderPanel()

    expect(await screen.findByText('Usage history')).toBeInTheDocument()
    expect(await screen.findByText('10-24')).toBeInTheDocument()
    expect(screen.getByText('10-25')).toBeInTheDocument()
    expect(screen.getByText('1.0K')).toBeInTheDocument()
    expect(screen.getByText('3.0K')).toBeInTheDocument()
  })
})
