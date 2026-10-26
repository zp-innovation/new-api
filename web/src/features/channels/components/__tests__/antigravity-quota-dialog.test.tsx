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

import type { AntigravityQuotaResponse } from '../../api'
import { AntigravityQuotaDialog } from '../dialogs/antigravity-quota-dialog'

function quotaResponse(
  overrides: Partial<NonNullable<AntigravityQuotaResponse['data']>> = {}
): AntigravityQuotaResponse {
  return {
    success: true,
    data: {
      channel_id: 1,
      channel_name: 'Google Antigravity',
      email: 'jack.csq@gmail.com',
      plan_type: 'Free',
      project_id: 'aicode-consumers',
      models: [
        {
          model: 'gemini-3.8-flash-high',
          display_name: 'Gemini 3.8 Flash High',
          remaining_fraction: 0.9984926,
          reset_time: '2026/10/10 22:13:52',
          recommended: true,
        },
        { model: 'claude-sonnet-4-6', remaining_fraction: 0.12 },
      ],
      ...overrides,
    },
  }
}

test('renders account info and the remaining quota of every model', () => {
  render(
    <AntigravityQuotaDialog
      open
      onOpenChange={() => {}}
      channelName='Google Antigravity'
      channelId={1}
      response={quotaResponse()}
    />
  )

  const dialog = screen.getByRole('dialog', {
    name: 'Antigravity Account & Quota',
  })
  expect(within(dialog).getByText('jack.csq@gmail.com')).toBeInTheDocument()
  expect(within(dialog).getByText('aicode-consumers')).toBeInTheDocument()
  expect(
    within(dialog).getByText('Google Antigravity (#1)')
  ).toBeInTheDocument()
  expect(within(dialog).getAllByText('Free')).toHaveLength(2)
  expect(within(dialog).getByText('gemini-3.8-flash-high')).toBeInTheDocument()
  expect(within(dialog).getByText('Gemini 3.8 Flash High')).toBeInTheDocument()
  expect(within(dialog).getByText('Recommended')).toBeInTheDocument()
  expect(within(dialog).getByText('100%')).toBeInTheDocument()
  expect(within(dialog).getByText('12%')).toBeInTheDocument()
  expect(
    within(dialog).getByText('Reset time: 2026/10/10 22:13:52')
  ).toBeInTheDocument()
  expect(within(dialog).getByText('Reset time: —')).toBeInTheDocument()
})

test('shows the empty state when the upstream reports no quota window', () => {
  render(
    <AntigravityQuotaDialog
      open
      onOpenChange={() => {}}
      response={quotaResponse({ models: [] })}
    />
  )

  expect(screen.getByText('No quota information available')).toBeInTheDocument()
  expect(
    screen.getByText(
      'The upstream did not report any Antigravity quota window.'
    )
  ).toBeInTheDocument()
})

test('surfaces the upstream failure message instead of an empty quota list', () => {
  render(
    <AntigravityQuotaDialog
      open
      onOpenChange={() => {}}
      response={{
        success: false,
        message: 'antigravity: invalid credential json',
      }}
    />
  )

  expect(
    screen.getByText('antigravity: invalid credential json')
  ).toBeInTheDocument()
  expect(screen.queryByText('Remaining quota')).not.toBeInTheDocument()
})
