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
import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
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
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  getAntigravityQuotaHistory,
  getAntigravityQuotaOverview,
  type AntigravityQuotaOverviewAccount,
} from '../api'

function formatCountdown(ms: number): string {
  if (ms <= 0) return '0s'
  const totalSeconds = Math.floor(ms / 1000)
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const pad = (value: number) => String(value).padStart(2, '0')
  if (days > 0) return `${days}d ${pad(hours)}h`
  if (hours > 0) return `${hours}h ${pad(minutes)}m`
  return `${minutes}m ${pad(seconds)}s`
}

function formatRemaining(fraction: number): string {
  const percent = fraction * 100
  if (percent <= 0) return '0%'
  return percent < 1 ? `${percent.toFixed(2)}%` : `${Math.round(percent)}%`
}

/**
 * The account pool drains per model, not per channel: an Antigravity account
 * whose Claude quota is gone still serves Gemini. This board reads the stored
 * snapshot the router uses, so it shows exactly which accounts can serve each
 * model right now and when the drained ones come back.
 */
export function AntigravityQuotaBoard() {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  // The upstream catalogue advertises far more models than the operator routes
  // to, so the board defaults to the models the channels actually serve.
  const [showAllModels, setShowAllModels] = useState(false)

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])

  const quotaQuery = useQuery({
    queryKey: ['antigravity-quota-overview', showAllModels],
    queryFn: async () =>
      requireServerSuccess(await getAntigravityQuotaOverview(showAllModels)),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  const historyQuery = useQuery({
    queryKey: ['antigravity-quota-history'],
    queryFn: async () =>
      requireServerSuccess(await getAntigravityQuotaHistory(24)),
    refetchInterval: 5 * 60_000,
    staleTime: 4 * 60_000,
  })
  const historyByModel = new Map(
    (historyQuery.data?.data?.models ?? []).map((series) => [
      series.model,
      series.points,
    ])
  )

  const models = quotaQuery.data?.data?.models ?? []
  const hiddenModels = quotaQuery.data?.data?.hidden_models ?? 0
  const generatedAt = quotaQuery.data?.data?.generated_at
  const generatedLabel = generatedAt
    ? new Date(generatedAt * 1000).toLocaleTimeString(
        toIntlLocale(document.documentElement.lang),
        { hour: '2-digit', minute: '2-digit' }
      )
    : '-'

  let content: ReactNode
  if (quotaQuery.isLoading) {
    content = (
      <div className='flex flex-col gap-3'>
        <Skeleton className='h-40 w-full' />
        <Skeleton className='h-40 w-full' />
      </div>
    )
  } else if (models.length === 0) {
    content = (
      <Empty className='min-h-40 border'>
        <EmptyHeader>
          <EmptyTitle>{t('No data')}</EmptyTitle>
          <EmptyDescription>
            {t('No quota readings yet. The first scan runs within 15 minutes.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = models.map((model) => (
      <Card key={model.model}>
        <CardHeader>
          <CardTitle className='text-sm font-semibold'>
            {model.model}
          </CardTitle>
          <CardAction className='flex items-center gap-2'>
            <Sparkline points={historyByModel.get(model.model) ?? []} />
            <Badge
              variant={
                model.available_accounts > 0 ? 'secondary' : 'destructive'
              }
            >
              {t('{{available}} / {{total}} accounts available', {
                available: model.available_accounts,
                total: model.total_accounts,
              })}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Account')}</TableHead>
                <TableHead className='w-56'>{t('Remaining')}</TableHead>
                <TableHead className='w-24'>{t('Change')}</TableHead>
                <TableHead className='w-32'>{t('Recovers in')}</TableHead>
                <TableHead className='w-44'>{t('Reset')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {model.accounts.map((account) => (
                <QuotaAccountRow
                  key={account.channel_id}
                  account={account}
                  now={now}
                />
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    ))
  }

  return (
    <div className='flex flex-col gap-4 pb-2'>
      <div className='flex flex-wrap items-center gap-3'>
        <div className='text-sm text-muted-foreground'>
          {t('Readings refresh every 15 minutes')}
          {' · '}
          {t('Last updated')}: {generatedLabel}
        </div>
        <div className='ms-auto flex items-center gap-2'>
          <Switch
            id='antigravity-show-all-models'
            checked={showAllModels}
            onCheckedChange={setShowAllModels}
          />
          <Label
            htmlFor='antigravity-show-all-models'
            className='cursor-pointer text-sm font-normal'
          >
            {t('Show all upstream models')}
          </Label>
        </div>
        <Button
          variant='outline'
          size='sm'
          disabled={quotaQuery.isFetching}
          onClick={() => void quotaQuery.refetch()}
        >
          <RefreshCw
            className={quotaQuery.isFetching ? 'animate-spin' : ''}
            data-icon='inline-start'
          />
          {t('Refresh')}
        </Button>
      </div>

      {content}
      {hiddenModels > 0 && !showAllModels ? (
        <div className='text-xs text-muted-foreground'>
          {t(
            '{{count}} more upstream models are hidden because no channel routes them.',
            { count: hiddenModels }
          )}
        </div>
      ) : null}
    </div>
  )
}

type QuotaAccountRowProps = {
  account: AntigravityQuotaOverviewAccount
  now: number
}

function QuotaAccountRow({ account, now }: QuotaAccountRowProps) {
  const remainingPercent = Math.max(
    0,
    Math.min(100, account.remaining_fraction * 100)
  )
  const resetAt = account.reset_time * 1000
  const countdown = account.reset_time > 0 ? formatCountdown(resetAt - now) : '-'
  const resetLabel =
    account.reset_time > 0
      ? new Date(resetAt).toLocaleString(
          toIntlLocale(document.documentElement.lang),
          {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
          }
        )
      : '-'

  return (
    <TableRow>
      <TableCell className='max-w-72 truncate'>
        {account.channel_name || `#${account.channel_id}`}
      </TableCell>
      <TableCell>
        <div className='flex items-center gap-2'>
          <Progress value={remainingPercent} className='h-2 w-28' />
          <span className='tabular-nums text-xs text-muted-foreground'>
            {formatRemaining(account.remaining_fraction)}
          </span>
        </div>
      </TableCell>
      <TableCell className='tabular-nums text-xs'>
        {typeof account.remaining_change_per_hour === 'number' ? (
          <span
            className={
              account.remaining_change_per_hour > 0
                ? 'text-destructive'
                : 'text-emerald-600'
            }
          >
            {account.remaining_change_per_hour > 0 ? '-' : '+'}
            {Math.abs(account.remaining_change_per_hour * 100).toFixed(1)}%/h
            {account.exhausted_at
              ? ` · ${formatCountdown(account.exhausted_at * 1000 - now)}`
              : ''}
          </span>
        ) : (
          '-'
        )}
      </TableCell>
      <TableCell className='tabular-nums'>
        {account.exhausted ? countdown : '-'}
      </TableCell>
      <TableCell className='tabular-nums text-xs text-muted-foreground'>
        {resetLabel}
      </TableCell>
    </TableRow>
  )
}

type SparklineProps = {
  points: { at: number; available: number; average: number }[]
}

/** 24-hour trend of the pool: higher means more accounts still had quota. */
function Sparkline({ points }: SparklineProps) {
  if (points.length < 2) {
    return null
  }
  const width = 96
  const height = 22
  const max = Math.max(1, ...points.map((point) => point.available))
  const step = width / (points.length - 1)
  const d = points
    .map((point, index) => {
      const x = index * step
      const y = height - (point.available / max) * (height - 2) - 1
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className='text-primary'
      aria-hidden='true'
    >
      <path d={d} fill='none' stroke='currentColor' strokeWidth='1.5' />
    </svg>
  )
}