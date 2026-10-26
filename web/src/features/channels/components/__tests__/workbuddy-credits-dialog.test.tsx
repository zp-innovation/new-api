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
import { render, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import type { WorkBuddyCredits, WorkBuddyCreditsResponse } from '../../api'
import { WorkBuddyCreditsDialog } from '../dialogs/workbuddy-credits-dialog'

const baseCredits: WorkBuddyCredits = {
  remain: 25,
  used: 75,
  size: 100,
  used_percent: '75.0%',
  remain_percent: '25.0%',
  is_paid_user: false,
  is_enterprise: false,
  checkin: {
    today_checked_in: true,
    streak_days: 3,
    daily_credit: 10,
    today_credit: 10,
    active: true,
  },
  earliest_expiring: {
    name: 'Free pack',
    package_code: 'p1',
    remain: 25,
    cycle_end_time: '2026-11-01 00:00:00',
    days_left: 20.5,
  },
  packages: [
    {
      name: 'Free pack',
      package_code: 'p1',
      grant_reason: 'official activity',
      remain: 25,
      used: 75,
      size: 100,
      unit: 'credits',
      in_usage: true,
      auto_renew: false,
      no_expiry: false,
      is_expired: false,
      cycle_end_time: '2026-11-01 00:00:00',
      days_left: 20.5,
      status: 0,
    },
    {
      name: 'Bonus pack',
      package_code: 'p2',
      remain: 0,
      used: 10,
      size: 10,
      unit: 'credits',
      in_usage: false,
      auto_renew: false,
      no_expiry: false,
      is_expired: true,
      days_left: -3,
      status: 3,
    },
  ],
  updated_at: '2026-10-11T06:00:00Z',
}

function creditsResponse(
  overrides: Partial<WorkBuddyCredits> = {}
): WorkBuddyCreditsResponse {
  return {
    success: true,
    data: {
      channel_id: 5,
      channel_name: 'WorkBuddy',
      account_uid: 'uid-1',
      account_count: 1,
      credits: { ...baseCredits, ...overrides },
    },
  }
}

test('renders the credits summary, check-in state and packages', () => {
  render(
    <WorkBuddyCreditsDialog
      open
      onOpenChange={() => {}}
      channelName='WorkBuddy'
      channelId={5}
      response={creditsResponse()}
    />
  )

  const dialog = screen.getByRole('dialog', {
    name: 'WorkBuddy Account & Credits',
  })
  expect(within(dialog).getByText('WorkBuddy (#5)')).toBeInTheDocument()
  expect(within(dialog).getByText('uid-1')).toBeInTheDocument()
  expect(within(dialog).getAllByText('25').length).toBeGreaterThan(0)
  expect(within(dialog).getAllByText('75').length).toBeGreaterThan(0)
  expect(within(dialog).getAllByText('100').length).toBeGreaterThan(0)
  expect(within(dialog).getByText('Free user')).toBeInTheDocument()
  expect(within(dialog).getByText('Checked in')).toBeInTheDocument()
  expect(within(dialog).getAllByText('Free pack').length).toBeGreaterThan(0)
  expect(within(dialog).getByText('official activity')).toBeInTheDocument()
  expect(within(dialog).getByText('Bonus pack')).toBeInTheDocument()
  expect(within(dialog).getByText('In use')).toBeInTheDocument()
  expect(within(dialog).getByText('Expired')).toBeInTheDocument()
  expect(within(dialog).getByText('Earliest expiring')).toBeInTheDocument()
})

test('shows the empty state when the upstream reports no package', () => {
  render(
    <WorkBuddyCreditsDialog
      open
      onOpenChange={() => {}}
      response={creditsResponse({
        packages: [],
        checkin: undefined,
        earliest_expiring: undefined,
      })}
    />
  )

  expect(
    screen.getByText('No credit information available')
  ).toBeInTheDocument()
  expect(
    screen.getByText(
      'The upstream did not report any WorkBuddy credit package.'
    )
  ).toBeInTheDocument()
})

test('surfaces the upstream failure message instead of the credits', () => {
  render(
    <WorkBuddyCreditsDialog
      open
      onOpenChange={() => {}}
      response={{
        success: false,
        message: 'workbuddy: invalid credential json',
      }}
    />
  )

  expect(
    screen.getByText('workbuddy: invalid credential json')
  ).toBeInTheDocument()
  expect(
    screen.queryByText('No credit information available')
  ).not.toBeInTheDocument()
})

test('names the account a multi-account channel is reporting on', () => {
  const response = creditsResponse()
  if (!response.data) throw new Error('missing credits response data')
  render(
    <WorkBuddyCreditsDialog
      open
      onOpenChange={() => {}}
      channelName='WorkBuddy'
      channelId={5}
      response={{ ...response, data: { ...response.data, account_count: 3 } }}
    />
  )

  expect(
    screen.getByText('Showing the first of 3 accounts')
  ).toBeInTheDocument()
})
