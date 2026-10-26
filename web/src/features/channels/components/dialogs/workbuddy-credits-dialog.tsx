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
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDateTimeStr, formatNumber } from '@/lib/format'
import { getServerMessage } from '@/lib/server-error-message'

import type {
  WorkBuddyCreditPackage,
  WorkBuddyCredits,
  WorkBuddyCreditsResponse,
} from '../../api'
import { AccountInfoCard, InfoField } from './account-info-card'

type WorkBuddyCreditsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName?: string
  channelId?: number
  response: WorkBuddyCreditsResponse | null
  onRefresh?: () => void | Promise<void>
  isRefreshing?: boolean
}

/**
 * WorkBuddy channels have no monetary balance: the upstream reports credits per
 * package plus the CN realm's daily check-in state. This dialog renders that
 * so the balance-query entry point stays useful for subscription channels.
 */
export function WorkBuddyCreditsDialog(props: WorkBuddyCreditsDialogProps) {
  const { t } = useTranslation()
  const data = props.response?.data
  const credits = data?.credits
  const packages = credits?.packages ?? []
  const accountCount = Number(data?.account_count) || 0
  const accountLabel =
    data?.account_name?.trim() || data?.account_uid?.trim() || '-'
  const checkin = credits?.checkin
  const isInitialLoading = Boolean(props.isRefreshing) && !props.response
  const errorMessage =
    props.response && props.response.success === false
      ? getServerMessage(props.response)?.trim() ||
        t('Failed to query the WorkBuddy credits. Please try again later')
      : ''
  let channelLabel = props.channelName ?? '-'
  if (props.channelId) {
    channelLabel = `${channelLabel} (#${props.channelId})`
  }

  const remainPercent = creditPercent(credits?.remain, credits?.size)

  let packageContent: ReactNode
  if (errorMessage) {
    packageContent = null
  } else if (isInitialLoading) {
    packageContent = (
      <div className='flex flex-col gap-2'>
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-16 w-full' />
      </div>
    )
  } else if (packages.length === 0) {
    packageContent = (
      <Empty className='min-h-32 border'>
        <EmptyHeader>
          <EmptyTitle>{t('No credit information available')}</EmptyTitle>
          <EmptyDescription>
            {t('The upstream did not report any WorkBuddy credit package.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    packageContent = (
      <div className='flex flex-col gap-2'>
        <div className='text-sm font-semibold'>{t('Packages')}</div>
        <ScrollArea className='max-h-[45vh] rounded-lg border'>
          <div className='divide-y'>
            {packages.map((row) => (
              <CreditPackageRow
                key={`${row.package_code ?? row.name}-${row.resource_id ?? ''}-${row.create_time ?? ''}-${row.cycle_start_time ?? ''}`}
                row={row}
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
      title={t('WorkBuddy Account & Credits')}
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
              label={planLabel(t, credits)}
              variant={
                credits?.is_paid_user || credits?.is_enterprise
                  ? 'blue'
                  : 'neutral'
              }
              copyable={false}
            />
            <StatusBadge
              label={`${t('Packages')} ${packages.length}`}
              variant='neutral'
              copyable={false}
            />
          </div>
          <div className='mt-4 grid grid-cols-1 gap-3 md:grid-cols-3'>
            <InfoField
              label={t('Remaining credits')}
              value={formatNumber(credits?.remain)}
            />
            <InfoField
              label={t('Used')}
              value={formatNumber(credits?.used)}
            />
            <InfoField
              label={t('Total')}
              value={formatNumber(credits?.size)}
            />
          </div>
          <div className='mt-4 flex flex-col gap-1'>
            <Progress value={remainPercent} />
            <div className='text-muted-foreground text-xs'>
              {t('Remaining')}:{' '}
              {credits?.remain_percent || `${remainPercent}%`}
            </div>
          </div>
          <div className='mt-4 grid grid-cols-1 gap-3 md:grid-cols-3'>
            <InfoField label={t('Account')} value={accountLabel} />
            <InfoField label={t('Channel')} value={channelLabel} />
            <InfoField
              label={t('Last updated:')}
              value={formatUpdatedAt(credits?.updated_at)}
            />
          </div>
          {accountCount > 1 ? (
            <div className='text-muted-foreground mt-2 text-xs'>
              {t('Showing the first of {{count}} accounts', {
                count: accountCount,
              })}
            </div>
          ) : null}
        </AccountInfoCard>

        {checkin ? (
          <Card size='sm' className='bg-muted/30 gap-0 py-0'>
            <CardHeader className='p-4 pb-2'>
              <CardTitle className='text-muted-foreground text-xs font-medium'>
                {t('Check-in')}
              </CardTitle>
            </CardHeader>
            <CardContent className='p-4 pt-0'>
              <StatusBadge
                label={
                  checkin.today_checked_in
                    ? t('Checked in')
                    : t('Not checked in')
                }
                variant={checkin.today_checked_in ? 'success' : 'neutral'}
                copyable={false}
              />
              <div className='mt-4 grid grid-cols-1 gap-3 md:grid-cols-3'>
                <InfoField
                  label={t('Streak')}
                  value={String(checkin.streak_days)}
                />
                <InfoField
                  label={t('Daily credit')}
                  value={formatNumber(checkin.daily_credit)}
                />
                <InfoField
                  label={t("Today's credit")}
                  value={formatNumber(checkin.today_credit)}
                />
              </div>
            </CardContent>
          </Card>
        ) : null}

        {credits?.earliest_expiring ? (
          <Card size='sm' className='bg-muted/30 gap-0 py-0'>
            <CardHeader className='p-4 pb-2'>
              <CardTitle className='text-muted-foreground text-xs font-medium'>
                {t('Earliest expiring')}
              </CardTitle>
            </CardHeader>
            <CardContent className='p-4 pt-0'>
              <div className='flex flex-wrap items-start justify-between gap-2'>
                <div className='min-w-0'>
                  <div className='truncate text-sm font-medium'>
                    {credits.earliest_expiring.name}
                  </div>
                  <div className='text-muted-foreground mt-0.5 text-xs'>
                    {t('Expires')}:{' '}
                    {credits.earliest_expiring.cycle_end_time || '—'}
                  </div>
                </div>
                <StatusBadge
                  label={`${credits.earliest_expiring.days_left} ${t('Days left')}`}
                  variant='warning'
                  copyable={false}
                />
              </div>
            </CardContent>
          </Card>
        ) : null}

        {packageContent}
      </div>
    </Dialog>
  )
}

function planLabel(
  t: (key: string) => string,
  credits?: WorkBuddyCredits
): string {
  if (!credits) return t('Unknown')
  if (credits.is_enterprise) return t('Enterprise')
  if (credits.is_paid_user) return t('Paid user')
  return t('Free user')
}

function creditPercent(remain?: number, size?: number): number {
  if (!size || size <= 0) return 0
  const value = ((remain ?? 0) / size) * 100
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, Math.round(value)))
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

function formatUpdatedAt(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return formatDateTimeStr(date)
}

function CreditPackageRow({ row }: { row: WorkBuddyCreditPackage }) {
  const { t } = useTranslation()
  const percent = creditPercent(row.remain, row.size)
  const unit = row.unit?.trim()
  const expiry = row.expire_time || row.cycle_end_time || ''

  let expiryBadge: ReactNode = null
  if (row.is_expired) {
    expiryBadge = (
      <StatusBadge label={t('Expired')} variant='danger' copyable={false} />
    )
  } else if (row.no_expiry) {
    expiryBadge = (
      <StatusBadge label={t('No expiry')} variant='neutral' copyable={false} />
    )
  }

  return (
    <div className='flex flex-col gap-2 p-3'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <div className='min-w-0'>
          <div className='truncate text-sm font-medium'>
            {row.name || row.package_code || t('Unknown')}
          </div>
          {row.grant_reason ? (
            <div className='text-muted-foreground mt-0.5 truncate text-xs'>
              {row.grant_reason}
            </div>
          ) : null}
        </div>
        <div className='flex shrink-0 flex-wrap items-center justify-end gap-1'>
          {row.in_usage ? (
            <StatusBadge label={t('In use')} variant='info' copyable={false} />
          ) : null}
          {expiryBadge}
          {row.auto_renew ? (
            <StatusBadge
              label={t('Auto renew')}
              variant='success'
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
      <div className='text-muted-foreground flex flex-wrap justify-between gap-2 text-xs'>
        <span>
          {t('Remaining')}: {formatNumber(row.remain)}
          {unit ? ` ${unit}` : ''}
        </span>
        <span>
          {t('Used')}: {formatNumber(row.used)}
          {unit ? ` ${unit}` : ''}
        </span>
        <span>
          {t('Total')}: {formatNumber(row.size)}
          {unit ? ` ${unit}` : ''}
        </span>
      </div>
      {expiry ? (
        <div className='text-muted-foreground text-xs'>
          {t('Expires')}: {expiry}
        </div>
      ) : null}
    </div>
  )
}
