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
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
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
import { getServerMessage } from '@/lib/server-error-message'

import type { AntigravityQuotaModel, AntigravityQuotaResponse } from '../../api'
import { AccountInfoCard, InfoField } from './account-info-card'

type AntigravityQuotaDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName?: string
  channelId?: number
  response: AntigravityQuotaResponse | null
  onRefresh?: () => void | Promise<void>
  isRefreshing?: boolean
}

/**
 * Antigravity channels have no monetary balance: the upstream reports a
 * remaining fraction per model instead. This dialog shows that quota so the
 * balance-query entry point stays useful for subscription channels.
 */
export function AntigravityQuotaDialog(props: AntigravityQuotaDialogProps) {
  const { t } = useTranslation()
  const data = props.response?.data
  const models = data?.models ?? []
  const isInitialLoading = Boolean(props.isRefreshing) && !props.response
  const errorMessage =
    props.response && props.response.success === false
      ? getServerMessage(props.response)?.trim() ||
        t('Failed to query the Antigravity quota. Please try again later')
      : ''
  let channelLabel = props.channelName ?? '-'
  if (props.channelId) {
    channelLabel = `${channelLabel} (#${props.channelId})`
  }

  let quotaContent: ReactNode
  if (errorMessage) {
    quotaContent = null
  } else if (isInitialLoading) {
    quotaContent = (
      <div className='flex flex-col gap-2'>
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
      </div>
    )
  } else if (models.length === 0) {
    quotaContent = (
      <Empty className='min-h-32 border'>
        <EmptyHeader>
          <EmptyTitle>{t('No quota information available')}</EmptyTitle>
          <EmptyDescription>
            {t('The upstream did not report any Antigravity quota window.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    quotaContent = (
      <div className='flex flex-col gap-2'>
        <div className='text-sm font-semibold'>{t('Remaining quota')}</div>
        <ScrollArea className='max-h-[50vh] rounded-lg border'>
          <div className='divide-y'>
            {models.map((row) => (
              <QuotaRow key={row.model} row={row} />
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
      title={t('Antigravity Account & Quota')}
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
              label={data?.plan_type?.trim() ? data.plan_type : t('Unknown')}
              variant='blue'
              copyable={false}
            />
            <StatusBadge
              label={`${t('Quota')} ${models.length}`}
              variant='neutral'
              copyable={false}
            />
          </div>
          <div className='mt-4 grid grid-cols-1 gap-3 md:grid-cols-2'>
            <InfoField label={t('Email')} value={data?.email} />
            <InfoField label={t('Channel')} value={channelLabel} />
            <InfoField label={t('Project')} value={data?.project_id} />
            <InfoField label={t('Plan')} value={data?.plan_type} />
          </div>
        </AccountInfoCard>

        {quotaContent}
      </div>
    </Dialog>
  )
}

function remainingPercent(row: AntigravityQuotaModel): number {
  const fraction = Number(row.remaining_fraction)
  if (!Number.isFinite(fraction)) {
    return 0
  }
  return Math.min(100, Math.max(0, Math.round(fraction * 100)))
}

function remainingVariant(percent: number): StatusBadgeProps['variant'] {
  if (percent >= 50) {
    return 'success'
  }
  if (percent >= 20) {
    return 'warning'
  }
  return 'danger'
}

function QuotaRow({ row }: { row: AntigravityQuotaModel }) {
  const { t } = useTranslation()
  const percent = remainingPercent(row)
  const hasDisplayName = Boolean(
    row.display_name && row.display_name !== row.model
  )

  return (
    <div className='flex flex-col gap-2 p-3'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <div className='min-w-0'>
          <div className='truncate font-mono text-xs'>{row.model}</div>
          {hasDisplayName ? (
            <div className='text-muted-foreground mt-0.5 truncate text-xs'>
              {row.display_name}
            </div>
          ) : null}
        </div>
        <div className='flex shrink-0 items-center gap-2'>
          {row.recommended ? (
            <StatusBadge
              label={t('Recommended')}
              variant='info'
              copyable={false}
            />
          ) : null}
          <StatusBadge
            label={`${percent}%`}
            variant={remainingVariant(percent)}
            copyable={false}
          />
        </div>
      </div>
      <Progress value={percent} />
      <div className='text-muted-foreground text-xs'>
        {t('Reset time')}: {row.reset_time || '—'}
      </div>
    </div>
  )
}
