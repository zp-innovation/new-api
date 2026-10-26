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
import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { CodexUsageDialog } from '../dialogs/codex-usage-dialog'

const resetAt = 1793953203

test('labels the free plan 30-day window by its real duration', () => {
  render(
    <CodexUsageDialog
      open
      onOpenChange={() => {}}
      channelName='OpenAI'
      channelId={5}
      response={{
        success: true,
        data: {
          plan_type: 'free',
          rate_limit: {
            allowed: true,
            limit_reached: false,
            primary_window: {
              used_percent: 55,
              limit_window_seconds: 2592000,
              reset_after_seconds: 2278621,
              reset_at: resetAt,
            },
          },
        },
      }}
    />
  )

  expect(screen.getByText('Monthly Window')).toBeInTheDocument()
  expect(screen.getByText('55%')).toBeInTheDocument()
  expect(screen.getByText(/720h/)).toBeInTheDocument()
  expect(screen.queryByText('5-Hour Window')).not.toBeInTheDocument()
})

test('renders the 5-hour and weekly windows of a paid plan', () => {
  render(
    <CodexUsageDialog
      open
      onOpenChange={() => {}}
      channelName='OpenAI'
      channelId={5}
      response={{
        success: true,
        data: {
          plan_type: 'plus',
          rate_limit: {
            allowed: true,
            limit_reached: false,
            primary_window: {
              used_percent: 12,
              limit_window_seconds: 18000,
              reset_after_seconds: 3600,
              reset_at: resetAt,
            },
            secondary_window: {
              used_percent: 55,
              limit_window_seconds: 604800,
              reset_after_seconds: 86400,
              reset_at: resetAt,
            },
          },
        },
      }}
    />
  )

  expect(screen.getByText('5-Hour Window')).toBeInTheDocument()
  expect(screen.getByText('Weekly Window')).toBeInTheDocument()
  expect(screen.getByText('12%')).toBeInTheDocument()
  expect(screen.getByText('55%')).toBeInTheDocument()
  expect(screen.queryByText('Monthly Window')).not.toBeInTheDocument()
})
