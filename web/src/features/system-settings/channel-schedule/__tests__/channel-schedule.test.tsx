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
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ChannelScheduleSection } from '..'
import { SettingsPageProvider } from '../../components/settings-page-context'
import type { ChannelScheduleStatus } from '../types'

const RULES_OPTION = JSON.stringify([
  {
    id: 'r1',
    name: 'Day · tag first',
    enabled: true,
    weekdays: [1, 2, 3, 4, 5],
    start: '07:55',
    end: '22:00',
    actions: [
      { mode: 'tag', tag: 'wb-cn', priority: 100 },
      { mode: 'channel', channel_ids: [31], weight: 10, enabled: false },
    ],
  },
])

const BASE_OPTIONS: Record<string, string> = {
  'channel_schedule_setting.enabled': 'true',
  'channel_schedule_setting.timezone': 'Asia/Shanghai',
  'channel_schedule_setting.check_minutes': '1',
  'channel_schedule_setting.rules': RULES_OPTION,
}

const DELETE_DESCRIPTION =
  'The rule is removed from the list; channels it already changed keep their current values.'

function statusFixture(
  overrides: Partial<ChannelScheduleStatus> = {}
): ChannelScheduleStatus {
  return {
    enabled: true,
    timezone: 'Asia/Shanghai',
    now: '2026-10-14 09:30:00',
    matched_rule_id: 'r1',
    matched_rule_name: 'Day · tag first',
    consistent: true,
    last_run_at: 0,
    expectations: [
      {
        scope: 'tag: wb-cn (2)',
        mode: 'tag',
        tag: 'wb-cn',
        channel_ids: [7, 8],
        participates: true,
        consistent: true,
        fields: [
          { field: 'priority', expected: '100', actual: '100', matches: true },
        ],
      },
      {
        scope: 'tag: wb-sg (1)',
        mode: 'tag',
        tag: 'wb-sg',
        channel_ids: [9],
        participates: false,
        consistent: true,
        fields: [],
      },
    ],
    ...overrides,
  }
}

let queryClient: QueryClient

function installApi(
  options: Record<string, string>,
  settings: { retryTimes?: string; status?: ChannelScheduleStatus } = {}
) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/option/channel_schedule') {
      return {
        data: {
          success: true,
          data: {
            options,
            status:
              settings.status ??
              statusFixture({
                enabled: options['channel_schedule_setting.enabled'] === 'true',
              }),
          },
        },
      }
    }
    if (url === '/api/option/request_policy') {
      return {
        data: {
          success: true,
          data: { options: { RetryTimes: settings.retryTimes ?? '0' } },
        },
      }
    }
    if (url === '/api/channel') {
      return {
        data: {
          success: true,
          data: {
            items: [
              { id: 31, name: 'WorkBuddy sg', tag: 'wb-sg' },
              { id: 7, name: 'WorkBuddy cn', tag: 'wb-cn' },
            ],
            total: 2,
            page: 1,
            page_size: 100,
          },
        },
      }
    }
    throw new Error(`Unexpected GET ${String(url)}`)
  })
  vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, data: { options, status: statusFixture() } },
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        result: { timezone: 'Asia/Shanghai', checked_at: 1, changes: [] },
        status: statusFixture(),
      },
    },
  })
}

/**
 * The page shell normally provides the portal target the save action renders
 * into, so a section rendered on its own needs the same provider.
 */
function SectionFrame(props: { children: ReactNode }) {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  return (
    <SettingsPageProvider actionsContainer={actionsContainer}>
      <div ref={setActionsContainer} />
      {props.children}
    </SettingsPageProvider>
  )
}

async function renderSection(withRule = true) {
  render(
    <QueryClientProvider client={queryClient}>
      <SectionFrame>
        <ChannelScheduleSection />
      </SectionFrame>
    </QueryClientProvider>
  )
  if (withRule) {
    await screen.findByText('Day · tag first')
  } else {
    await screen.findByRole('button', { name: 'Save Changes' })
  }
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})

afterEach(() => {
  cleanup()
  queryClient.clear()
  vi.restoreAllMocks()
})

describe('channel schedule settings section', () => {
  it('lists a rule with its scope and the fields it modifies', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()

    expect(screen.getByText('tag: wb-cn')).toBeInTheDocument()
    expect(screen.getByText('#31')).toBeInTheDocument()
    expect(screen.getByText('100')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    // Fields an action does not set are shown as untouched.
    expect(screen.getAllByText('Not modified').length).toBeGreaterThan(0)
    expect(screen.getByText('Mon, Tue, Wed, Thu, Fri')).toBeInTheDocument()
  })

  it('warns that the lower layer is unreachable while the retry budget is zero', async () => {
    installApi(BASE_OPTIONS, { retryTimes: '0' })
    await renderSection()

    expect(
      screen.getByTestId('channel-schedule-retry-warning')
    ).toBeInTheDocument()
  })

  it('marks a per-tag weight so a share is not read as a per-account number', async () => {
    const shareRules = JSON.stringify([
      {
        id: 'r1',
        name: 'Night · split',
        enabled: true,
        weekdays: [],
        start: '23:00',
        end: '08:00',
        actions: [
          { mode: 'tag', tag: 'wb-intl', weight: 1, weight_mode: 'tag' },
          { mode: 'tag', tag: 'wb-cn', weight: 1, weight_mode: 'tag' },
        ],
      },
    ])
    installApi({
      ...BASE_OPTIONS,
      'channel_schedule_setting.rules': shareRules,
    })
    await renderSection(false)
    await screen.findByText('Night · split')

    expect(screen.getByText('tag: wb-intl')).toBeInTheDocument()
    // Both scopes carry the share plus the badge that says it is split per tag.
    expect(screen.getAllByText('Per tag')).toHaveLength(2)
  })

  it('hides the retry warning once a retry budget exists', async () => {
    installApi(BASE_OPTIONS, { retryTimes: '3' })
    await renderSection()

    expect(
      screen.queryByTestId('channel-schedule-retry-warning')
    ).not.toBeInTheDocument()
  })

  it('does not write options when nothing changed', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    expect(api.patch).not.toHaveBeenCalled()
  })

  it('sends only the changed key when the schedule is switched off', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    await user.click(
      screen.getByRole('switch', { name: 'Enable channel time schedule' })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledWith('/api/option/channel_schedule', {
        options: { 'channel_schedule_setting.enabled': 'false' },
      })
    )
  })

  it('blocks the save and writes nothing when the interval is out of range', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    const interval = screen.getByLabelText('Check interval')
    fireEvent.change(interval, { target: { value: '0' } })
    expect(interval).toHaveValue(0)
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    expect(
      await screen.findByText('Enter a value between 1 and 1440')
    ).toBeInTheDocument()
    expect(api.patch).not.toHaveBeenCalled()
  })

  it('asks for confirmation and removes the rule only after confirming', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Delete rule' }))
    expect(await screen.findByText(DELETE_DESCRIPTION)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() =>
      expect(screen.queryByText(DELETE_DESCRIPTION)).not.toBeInTheDocument()
    )
    expect(screen.getByText('Day · tag first')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Delete rule' }))
    await screen.findByText(DELETE_DESCRIPTION)
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    expect(screen.queryByText('Day · tag first')).not.toBeInTheDocument()
    expect(
      screen.getByText('No rules yet. Add one to switch channels by time.')
    ).toBeInTheDocument()
  })

  it('shows the removed rule as a pending change instead of saving immediately', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Delete rule' }))
    await screen.findByText(DELETE_DESCRIPTION)
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    // Removing a rule is a draft edit; it only reaches the server on save.
    expect(api.patch).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledWith('/api/option/channel_schedule', {
        options: { 'channel_schedule_setting.rules': '[]' },
      })
    )
  })

  it('shows expected versus actual values and marks scopes outside the schedule', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    await user.click(screen.getByRole('tab', { name: 'Applied state' }))

    expect(await screen.findByText('tag: wb-cn (2)')).toBeInTheDocument()
    expect(
      screen.getByText('2026-10-14 09:30:00 (Asia/Shanghai)')
    ).toBeInTheDocument()
    expect(screen.getAllByText('100')).toHaveLength(2)
    expect(screen.getByText('Not part of the schedule')).toBeInTheDocument()
    expect(screen.getByText('Never run')).toBeInTheDocument()
  })

  it('reports drift as pending correction instead of consistent', async () => {
    installApi(BASE_OPTIONS, {
      retryTimes: '2',
      status: statusFixture({
        consistent: false,
        expectations: [
          {
            scope: 'tag: wb-cn (1)',
            mode: 'tag',
            tag: 'wb-cn',
            channel_ids: [7],
            participates: true,
            consistent: false,
            fields: [
              {
                field: 'priority',
                expected: '100',
                actual: '0',
                matches: false,
              },
            ],
          },
        ],
      }),
    })
    await renderSection()
    const user = userEvent.setup()

    await user.click(screen.getByRole('tab', { name: 'Applied state' }))

    // The header consistency badge and the drifted field both report it.
    expect(
      (await screen.findAllByText('Pending correction')).length
    ).toBeGreaterThan(0)
    expect(screen.queryAllByText('Consistent')).toHaveLength(0)
  })

  it('creates a rule with an id minted outside render', async () => {
    installApi(BASE_OPTIONS)
    await renderSection()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    await user.type(await screen.findByPlaceholderText('Rule name'), 'Night')
    await user.type(screen.getByPlaceholderText('Tag Name'), 'wb-intl')
    await user.click(screen.getByRole('button', { name: 'Save rule' }))
    expect(await screen.findByText('Night')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    // The draft id is minted when the editor opens, so it survives the renders
    // the dialog performs while the operator fills the form.
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledWith('/api/option/channel_schedule', {
        options: {
          'channel_schedule_setting.rules': expect.stringMatching(
            /^\[\{"id":"r1"[\s\S]*\{"id":"rule-\d+","name":"Night"/
          ),
        },
      })
    )
  })
})
