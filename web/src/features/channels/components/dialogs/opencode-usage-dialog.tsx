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
import { formatDateTimeStr, formatPercent } from '@/lib/format'
import { getServerMessage } from '@/lib/server-error-message'

import type {
  OpenCodeUsageEntry,
  OpenCodeUsageResponse,
  OpenCodeUsageWindow,
} from '../../api'
import { AccountInfoCard, InfoField } from './account-info-card'

type OpenCodeUsageDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName?: string
  channelId?: number
  response: OpenCodeUsageResponse | null
  onRefresh?: () => void | Promise<void>
  isRefreshing?: boolean
}

/**
 * OpenCode Go channels expose rolling, weekly, and monthly subscription
 * windows. Zen uses pay-as-you-go billing and has no equivalent usage endpoint.
 */
export function OpenCodeUsageDialog(props: OpenCodeUsageDialogProps) {
  const { t } = useTranslation()
  const data = props.response?.data
  const entries = data?.entries ?? []
  const first = entries[0]
  const mode = data?.mode === 'go' ? 'go' : 'zen'
  const isInitialLoading = Boolean(props.isRefreshing) && !props.response
  const errorMessage =
    props.response && props.response.success === false
      ? getServerMessage(props.response)?.trim() ||
        t('Failed to query the OpenCode usage. Please try again later')
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
            {t('The upstream did not report any OpenCode usage window.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    usageContent = (
      <div className='flex flex-col gap-2'>
        <div className='text-sm font-semibold'>{t('Usage windows')}</div>
        <ScrollArea className='max-h-[50vh] rounded-lg border'>
          <div className='divide-y'>
            {entries.map((entry, index) => (
              <UsageEntryRow
                key={
                  entry.account_id ??
                  entry.email ??
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
      title={t('OpenCode Account & Usage')}
      contentClassName='sm:max-w-[720px]'
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
              label={mode === 'go' ? t('OpenCode Go') : t('OpenCode Zen')}
              variant={mode === 'go' ? 'blue' : 'neutral'}
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
              value={first?.email || first?.account_id}
            />
            <InfoField
              label={t('Organization')}
              value={first?.org_name || first?.org_id}
            />
            <InfoField label={t('Channel')} value={channelLabel} />
            <InfoField
              label={t('Mode')}
              value={mode === 'go' ? t('OpenCode Go') : t('OpenCode Zen')}
            />
          </div>
        </AccountInfoCard>

        {usageContent}
      </div>
    </Dialog>
  )
}

function UsageEntryRow(props: { entry: OpenCodeUsageEntry }) {
  const { t } = useTranslation()
  const entry = props.entry
  return (
    <div className='flex flex-col gap-3 p-4'>
      <div className='min-w-0'>
        <div className='truncate text-sm font-medium'>
          {entry.email || entry.account_id || t('Account')}
        </div>
        {entry.org_name || entry.org_id ? (
          <div className='text-muted-foreground mt-0.5 truncate text-xs'>
            {entry.org_name || entry.org_id}
          </div>
        ) : null}
      </div>
      <div className='grid grid-cols-1 gap-3 md:grid-cols-3'>
        <WindowCard label={t('Rolling')} window={entry.rolling} />
        <WindowCard label={t('Weekly')} window={entry.weekly} />
        <WindowCard label={t('Monthly')} window={entry.monthly} />
      </div>
    </div>
  )
}

function WindowCard(props: { label: string; window?: OpenCodeUsageWindow }) {
  const { t } = useTranslation()
  if (!props.window) {
    return (
      <div className='border-border/60 rounded-md border px-3 py-2'>
        <div className='text-muted-foreground text-xs'>{props.label}</div>
        <div className='mt-1 text-sm'>{t('No rolling window')}</div>
      </div>
    )
  }
  return (
    <div className='border-border/60 flex flex-col gap-2 rounded-md border px-3 py-2'>
      <div className='flex items-center justify-between gap-2 text-xs'>
        <span className='text-muted-foreground'>{props.label}</span>
        <span>{formatPercent(props.window.percent)}</span>
      </div>
      <Progress value={clampPercent(props.window.percent)} />
      <div className='text-muted-foreground truncate text-xs'>
        {t('Resets at')}: {formatResetsAt(props.window.resets_at)}
      </div>
    </div>
  )
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

function formatResetsAt(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return formatDateTimeStr(date)
}
