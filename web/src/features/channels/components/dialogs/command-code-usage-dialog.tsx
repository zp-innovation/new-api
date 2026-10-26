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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDateTimeStr, formatNumber, formatPercent } from '@/lib/format'
import { getServerMessage } from '@/lib/server-error-message'

import type {
  CommandCodeUsageEntry,
  CommandCodeUsageResponse,
  CommandCodeUsageWindow,
} from '../../api'
import { AccountInfoCard, InfoField } from './account-info-card'

type CommandCodeUsageDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName?: string
  channelId?: number
  response: CommandCodeUsageResponse | null
  onRefresh?: () => void | Promise<void>
  isRefreshing?: boolean
}

/**
 * Command Code channels have no monetary balance: the upstream reports plan
 * credits and rolling subscription windows. This dialog renders that so the
 * balance-query entry point stays useful for subscription channels.
 */
export function CommandCodeUsageDialog(
  props: CommandCodeUsageDialogProps
) {
  const { t } = useTranslation()
  const entries = props.response?.data?.entries ?? []
  const first = entries[0]
  const isInitialLoading = Boolean(props.isRefreshing) && !props.response
  const errorMessage =
    props.response && props.response.success === false
      ? getServerMessage(props.response)?.trim() ||
        t('Failed to query the Command Code usage. Please try again later')
      : ''
  let channelLabel = props.channelName ?? '-'
  if (props.channelId) {
    channelLabel = `${channelLabel} (#${props.channelId})`
  }

  let usageContent: ReactNode
  if (errorMessage) {
    usageContent = null
  } else if (isInitialLoading) {
    usageContent = (
      <div className='flex flex-col gap-2'>
        <Skeleton className='h-28 w-full' />
        <Skeleton className='h-28 w-full' />
      </div>
    )
  } else if (entries.length === 0) {
    usageContent = (
      <Empty className='min-h-32 border'>
        <EmptyHeader>
          <EmptyTitle>{t('No usage information available')}</EmptyTitle>
          <EmptyDescription>
            {t('The upstream did not report any Command Code credit window.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    usageContent = (
      <div className='flex flex-col gap-2'>
        <div className='text-sm font-semibold'>{t('Credit windows')}</div>
        <ScrollArea className='max-h-[50vh] rounded-lg border'>
          <div className='divide-y'>
            {entries.map((entry, index) => (
              <UsageEntryRow
                key={
                  entry.user_id ??
                  entry.user_name ??
                  entry.org_id ??
                  `entry-${index}`
                }
                entry={entry}
              />
            ))}
          </div>
        </ScrollArea>
      </div>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Command Code Account & Usage')}
      contentClassName='sm:max-w-[760px]'
      contentHeight='auto'
      bodyClassName='flex flex-col gap-4'
      footer={
        <Button
          type='button'
          variant='outline'
          onClick={() => props.onOpenChange(false)}
        >
          {t('Close')}
        </Button>
      }
    >
      <div className='flex flex-col gap-4'>
        {errorMessage ? (
          <div className='border-destructive/40 bg-destructive/10 text-destructive rounded-lg border px-4 py-3 text-sm'>
            {errorMessage}
          </div>
        ) : null}

        <AccountInfoCard
          onRefresh={props.onRefresh}
          isRefreshing={props.isRefreshing}
        >
          <div className='flex flex-wrap items-center gap-2'>
            <StatusBadge
              label={first?.plan_name?.trim() || t('Unknown')}
              variant='blue'
              copyable={false}
            />
            <StatusBadge
              label={`${t('Accounts')} ${entries.length}`}
              variant='neutral'
              copyable={false}
            />
          </div>
          <div className='mt-4 grid grid-cols-1 gap-3 md:grid-cols-2'>
            <InfoField
              label={t('Account')}
              value={first?.user_name || first?.user_id}
            />
            <InfoField label={t('Organization')} value={first?.org_name} />
            <InfoField label={t('Channel')} value={channelLabel} />
            <InfoField label={t('Plan')} value={first?.plan_name} />
          </div>
        </AccountInfoCard>

        {usageContent}
      </div>
    </Dialog>
  )
}

function UsageEntryRow(props: { entry: CommandCodeUsageEntry }) {
  const { t } = useTranslation()
  const entry = props.entry
  const monthlyPercent =
    entry.monthly_cap > 0 ? (entry.monthly_used / entry.monthly_cap) * 100 : 0
  return (
    <div className='flex flex-col gap-4 p-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='truncate text-sm font-medium'>
          {entry.user_name || entry.user_id || t('Account')}
        </span>
        {entry.plan_name ? (
          <StatusBadge
            label={entry.plan_name}
            variant='blue'
            copyable={false}
          />
        ) : null}
      </div>

      <div className='grid grid-cols-1 gap-3 md:grid-cols-3'>
        <InfoField
          label={t('Monthly credits')}
          value={formatNumber(entry.monthly_credits)}
        />
        <InfoField
          label={t('Purchased credits')}
          value={formatNumber(entry.purchased_credits)}
        />
        <InfoField
          label={t('Free credits')}
          value={formatNumber(entry.free_credits)}
        />
      </div>

      <div className='flex flex-col gap-1'>
        <div className='flex items-center justify-between text-xs'>
          <span className='text-muted-foreground'>{t('Monthly usage')}</span>
          <span>
            {formatNumber(entry.monthly_used)} /{' '}
            {formatNumber(entry.monthly_cap)}
          </span>
        </div>
        <Progress value={clampPercent(monthlyPercent)} />
      </div>

      <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
        <WindowCard label={t('5-hour window')} window={entry.five_hour} />
        <WindowCard label={t('Weekly window')} window={entry.weekly} />
      </div>

      {entry.period_start || entry.period_end ? (
        <div className='text-muted-foreground text-xs'>
          {t('Period')}: {formatPeriod(entry.period_start)} -{' '}
          {formatPeriod(entry.period_end)}
        </div>
      ) : null}
    </div>
  )
}

function WindowCard(props: {
  label: string
  window?: CommandCodeUsageWindow
}) {
  const { t } = useTranslation()
  if (!props.window) {
    return (
      <div className='border-border/60 rounded-md border px-3 py-2'>
        <div className='text-muted-foreground text-xs'>{props.label}</div>
        <div className='mt-1 text-sm'>{t('No rolling window')}</div>
      </div>
    )
  }
  const percent = clampPercent(props.window.percent)
  return (
    <div className='border-border/60 flex flex-col gap-1 rounded-md border px-3 py-2'>
      <div className='flex items-center justify-between text-xs'>
        <span className='text-muted-foreground'>{props.label}</span>
        <span>
          {formatNumber(props.window.used)} / {formatNumber(props.window.cap)}
        </span>
      </div>
      <Progress value={percent} />
      <div className='text-muted-foreground flex items-center justify-between text-xs'>
        <span>{formatPercent(props.window.percent)}</span>
        <span>
          {t('Resets at')}: {formatPeriod(props.window.resets_at)}
        </span>
      </div>
    </div>
  )
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

function formatPeriod(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return formatDateTimeStr(date)
}

