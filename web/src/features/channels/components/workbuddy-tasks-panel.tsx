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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Coins, PlayCircle, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  checkinWorkBuddyChannel,
  getWorkBuddyAccounts,
  getWorkBuddyTaskRuns,
  getWorkBuddyUsage,
  getWorkBuddyUsageHistory,
  runWorkBuddyTasks,
  setWorkBuddyAccountTasks,
} from '../api'
import { workBuddyRunMessage } from '../lib/workbuddy-run-message'
import type {
  WorkBuddyAccountView,
  WorkBuddyModelUsage,
  WorkBuddyRealmSummary,
  WorkBuddyTaskRun,
  WorkBuddyTaskSettings,
  WorkBuddyUsageDailyRow,
  WorkBuddyUsageSummary,
} from '../types'

const ACCOUNTS_QUERY_KEY = ['workbuddy-accounts']
const RUNS_QUERY_KEY = ['workbuddy-task-runs']
const USAGE_QUERY_KEY = ['workbuddy-usage']
const HISTORY_QUERY_KEY = ['workbuddy-usage-history']
const HISTORY_DAYS = 14

// The token window is per upstream definition: the gateway only sees a rolling
// day until a cap names the reset clock, so the panel lets the operator pick.
const USAGE_WINDOWS = [
  { value: '24h', label: '24h' },
  { value: 'today', label: 'Today' },
  { value: 'current', label: 'Current window' },
] as const

const REALM_CN = 'cn'
const REALM_INTL = 'intl'
const REALM_ALL = 'all'

// The task and trigger names travel as identifiers, so the display labels are
// resolved here instead of calling t() with a dynamic key.
const TASK_LABELS: Record<string, string> = {
  checkin: 'Check-in',
  activity_report: 'Activity report',
  travel: 'Cat travel',
  streak_bonus: 'Streak housekeeping',
  growth: 'Growth tasks',
  night_cat: 'Night-owl task',
  daily_chat: 'Daily activity chat',
  refresh_credits: 'Refresh credits',
  manual: 'Manual',
  scheduler: 'Scheduled',
}

function taskLabel(t: (key: string) => string, value: string): string {
  return value ? t(TASK_LABELS[value] ?? value) : '-'
}

function formatTime(unix: number): string {
  if (!unix) return '-'
  return new Date(unix * 1000).toLocaleString()
}

function formatNumber(value: number): string {
  if (!value) return '0'
  return Number.isInteger(value) ? String(value) : value.toFixed(2)
}

// Token volumes run into the hundreds of millions, so the panel shows them in
// compact millions and keeps the exact figure in the tooltip.
function formatTokens(value: number): string {
  if (!value) return '0'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

function budgetSourceLabel(t: (key: string) => string, source: string): string {
  switch (source) {
    case 'cap':
      return t('Measured from a cap')
    case 'seed':
      return t('Published seed')
    default:
      return t('Unknown budget')
  }
}

function remainingPercent(row: WorkBuddyModelUsage): number | null {
  if (!row.budget_tokens) return null
  return Math.max(0, Math.min(100, row.remaining_ratio * 100))
}

export function WorkBuddyTasksPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [realm, setRealm] = useState<string>(REALM_ALL)
  const [usageWindow, setUsageWindow] = useState<string>('24h')

  const accountsQuery = useQuery({
    queryKey: [...ACCOUNTS_QUERY_KEY, realm],
    queryFn: async () =>
      requireServerSuccess(await getWorkBuddyAccounts({ realm })),
    refetchInterval: 30000,
  })
  const runsQuery = useQuery({
    queryKey: RUNS_QUERY_KEY,
    queryFn: async () =>
      requireServerSuccess(await getWorkBuddyTaskRuns({ limit: 20 })),
    refetchInterval: 30000,
  })
  // Usage is passive: it is folded from the gateway's own consume logs, so the
  // query never touches the upstream and can refresh freely.
  const usageQuery = useQuery({
    queryKey: [...USAGE_QUERY_KEY, realm, usageWindow],
    queryFn: async () =>
      requireServerSuccess(await getWorkBuddyUsage({ window: usageWindow })),
    refetchInterval: 60000,
  })
  // The daily rollup is stored history, so it survives the live window and log
  // retention; it refreshes rarely because a past day cannot change.
  const historyQuery = useQuery({
    queryKey: HISTORY_QUERY_KEY,
    queryFn: async () =>
      requireServerSuccess(await getWorkBuddyUsageHistory({ days: HISTORY_DAYS })),
    refetchInterval: 300000,
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ACCOUNTS_QUERY_KEY })
    void queryClient.invalidateQueries({ queryKey: RUNS_QUERY_KEY })
    void queryClient.invalidateQueries({ queryKey: USAGE_QUERY_KEY })
  }

  const runMutation = useMutation({
    mutationFn: runWorkBuddyTasks,
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Request failed'))
        return
      }
      if (response.data?.queued) {
        toast.success(t('Queued as a background task'))
      } else {
        const summary = response.data?.summary
        toast.success(
          t('Ran {{actions}} action(s) for {{accounts}} account(s)', {
            actions: summary?.actions ?? 0,
            accounts: summary?.accounts ?? 0,
          })
        )
      }
      invalidate()
    },
    onError: () => toast.error(t('Request failed')),
  })

  const taskToggleMutation = useMutation({
    mutationFn: setWorkBuddyAccountTasks,
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Request failed'))
        return
      }
      invalidate()
    },
    onError: () => toast.error(t('Request failed')),
  })

  const checkinMutation = useMutation({
    mutationFn: checkinWorkBuddyChannel,
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Request failed'))
        return
      }
      toast.success(t('Check-in finished'))
      invalidate()
    },
    onError: () => toast.error(t('Request failed')),
  })

  const payload = accountsQuery.data?.data
  const accounts = payload?.accounts ?? []
  const settings = payload?.settings
  const summaries = payload?.summaries
  const runs = runsQuery.data?.data?.runs ?? []
  const busy = runMutation.isPending || checkinMutation.isPending

  // The usage endpoint always returns both realms so the headline cards stay
  // complete; the table shows whatever the segment control selected.
  const usage = usageQuery.data?.data
  const usageRows = usage?.rows ?? []
  // The endpoint always returns both realms, like the usage cards above, so the
  // realm segment filters the stored history on the client.
  const historyRows = (historyQuery.data?.data?.rows ?? []).filter(
    (row) => realm === REALM_ALL || row.realm === realm
  )
  const visibleUsageRows =
    realm === REALM_ALL
      ? usageRows
      : usageRows.filter((row) => row.realm === realm)
  const usageByCell = new Map<string, WorkBuddyModelUsage>()
  for (const row of visibleUsageRows) {
    const key = `${row.channel_id}|${row.account_uid}`
    const existing = usageByCell.get(key)
    // The headline cell per account is the model with the largest usage, which
    // is what the account-row progress bar summarizes.
    if (!existing || row.total_tokens > existing.total_tokens) {
      usageByCell.set(key, row)
    }
  }

  const runFor = (actions: string[], accountUid?: string) => {
    runMutation.mutate({ actions, accountUid, realm })
  }

  const realms: Array<{ value: string; label: string; count: number }> = [
    {
      value: REALM_ALL,
      label: t('All realms'),
      count:
        (summaries?.[REALM_INTL]?.accounts ?? 0) +
        (summaries?.[REALM_CN]?.accounts ?? 0),
    },
    {
      value: REALM_INTL,
      label: t('International'),
      count: summaries?.[REALM_INTL]?.accounts ?? 0,
    },
    {
      value: REALM_CN,
      label: t('China'),
      count: summaries?.[REALM_CN]?.accounts ?? 0,
    },
  ]
  const showStreak = realm !== REALM_INTL
  const showRealmColumn = realm === REALM_ALL

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center gap-2'>
        {realms.map((item) => (
          <Button
            key={item.value}
            size='sm'
            variant={realm === item.value ? 'default' : 'outline'}
            aria-pressed={realm === item.value}
            onClick={() => setRealm(item.value)}
          >
            {item.label}
            <Badge variant='secondary' className='ml-1'>
              {item.count}
            </Badge>
          </Button>
        ))}
      </div>

      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-muted-foreground text-xs font-medium'>
          {t('Token window')}
        </span>
        {USAGE_WINDOWS.map((item) => (
          <Button
            key={item.value}
            size='sm'
            variant={usageWindow === item.value ? 'default' : 'outline'}
            aria-pressed={usageWindow === item.value}
            onClick={() => setUsageWindow(item.value)}
          >
            {t(item.label)}
          </Button>
        ))}
      </div>

      {usage ? (
        <div className='grid gap-3 md:grid-cols-2'>
          <WorkBuddyUsageCard
            realm={REALM_INTL}
            summary={usage.summaries?.[REALM_INTL]}
          />
          <WorkBuddyUsageCard
            realm={REALM_CN}
            summary={usage.summaries?.[REALM_CN]}
          />
        </div>
      ) : null}

      <WorkBuddyUsageHistoryCard rows={historyRows} />

      {summaries ? (
        <div className='grid gap-3 md:grid-cols-2'>
          <WorkBuddyRealmCard
            realm={REALM_INTL}
            summary={summaries[REALM_INTL]}
            active={realm === REALM_INTL}
            onSelect={() => setRealm(REALM_INTL)}
          />
          <WorkBuddyRealmCard
            realm={REALM_CN}
            summary={summaries[REALM_CN]}
            active={realm === REALM_CN}
            onSelect={() => setRealm(REALM_CN)}
          />
        </div>
      ) : null}

      <Card>
        <CardHeader className='flex flex-row items-center justify-between gap-4'>
          <div className='min-w-0'>
            <CardTitle>{t('WorkBuddy accounts')}</CardTitle>
            <CardDescription>
              {t(
                'Rows come from the WorkBuddy channels, so an imported account appears here automatically'
              )}{' '}
              {t(
                'Credits and streak show the last query result; refresh them from upstream when needed'
              )}
            </CardDescription>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={busy}
              onClick={() => invalidate()}
            >
              <RefreshCw data-icon='inline-start' />
              {t('Refresh')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              disabled={busy}
              onClick={() => runFor(['refresh_credits'])}
            >
              <Coins data-icon='inline-start' />
              {t('Refresh credits')}
            </Button>
            {realm !== REALM_INTL ? (
              <Button
                variant='outline'
                size='sm'
                disabled={busy}
                onClick={() => runFor(['checkin'])}
              >
                <Coins data-icon='inline-start' />
                {t('Check in all')}
              </Button>
            ) : null}
            <Button size='sm' disabled={busy} onClick={() => runFor([])}>
              <PlayCircle data-icon='inline-start' />
              {t('Run all enabled')}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Account')}</TableHead>
                <TableHead>{t('Channel')}</TableHead>
                {showRealmColumn ? <TableHead>{t('Realm')}</TableHead> : null}
                <TableHead>{t('Today')}</TableHead>
                <TableHead>{t('Tasks')}</TableHead>
                {showStreak ? <TableHead>{t('Streak')}</TableHead> : null}
                <TableHead>{t('Remaining credits')}</TableHead>
                <TableHead>{t('Token usage')}</TableHead>
                <TableHead>{t('Last run')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {accounts.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={
                      8 + (showRealmColumn ? 1 : 0) + (showStreak ? 1 : 0)
                    }
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No WorkBuddy account yet')}
                  </TableCell>
                </TableRow>
              ) : (
                accounts.map((account) => (
                  <WorkBuddyAccountRow
                    key={`${account.channel_id}-${account.account_uid}`}
                    account={account}
                    usage={usageByCell.get(
                      `${account.channel_id}|${account.account_uid}`
                    )}
                    showRealm={showRealmColumn}
                    showStreak={showStreak}
                    busy={busy}
                    onCheckin={() => checkinMutation.mutate(account.channel_id)}
                    onRun={(actions) => runFor(actions, account.account_uid)}
                    onToggleTasks={(enabled) =>
                      taskToggleMutation.mutate({
                        channelId: account.channel_id,
                        accountUid: account.account_uid,
                        enabled,
                      })
                    }
                  />
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {settings ? <WorkBuddyTaskSettingsCards settings={settings} /> : null}

      <Card>
        <CardHeader>
          <CardTitle>{t('Recent runs')}</CardTitle>
          <CardDescription>
            {t('Every attempt is recorded, newest first')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {runs.length === 0 ? (
            <div className='text-muted-foreground py-6 text-center text-sm'>
              {t('No runs yet')}
            </div>
          ) : (
            <div className='flex flex-col divide-y'>
              {runs.map((run) => (
                <WorkBuddyRunRow key={run.id} run={run} />
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// WorkBuddyRealmCard is one realm's headline. Selecting it filters the table, so
// the two cards double as the realm switch.
function WorkBuddyRealmCard(props: {
  realm: string
  summary?: WorkBuddyRealmSummary
  active: boolean
  onSelect: () => void
}) {
  const { t } = useTranslation()
  const summary = props.summary
  const isCN = props.realm === REALM_CN

  return (
    <Card
      size='sm'
      className={cn(
        'cursor-pointer transition-colors',
        props.active ? 'border-primary' : 'hover:border-border'
      )}
      onClick={props.onSelect}
    >
      <CardHeader className='pb-2'>
        <CardTitle className='flex items-center gap-2 text-sm'>
          <Badge variant={isCN ? 'destructive' : 'default'}>
            {isCN ? t('China') : t('International')}
          </Badge>
          <span className='text-muted-foreground text-xs font-normal'>
            {isCN ? t('China realm routine') : t('International realm routine')}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <InfoField
          label={t('Accounts')}
          value={String(summary?.accounts ?? 0)}
        />
        <InfoField
          label={t('Done today')}
          value={String(summary?.done_today ?? 0)}
        />
        {isCN ? (
          <InfoField
            label={t('Streak average')}
            value={formatNumber(summary?.streak_average ?? 0)}
          />
        ) : null}
        <InfoField
          label={t('Cached balance')}
          value={formatNumber(summary?.remain_credit ?? 0)}
        />
        <InfoField
          label={t('Last run')}
          value={formatTime(summary?.last_run_at ?? 0)}
        />
        {summary && summary.failed_last_run > 0 ? (
          <InfoField
            label={t('Failed last run')}
            value={String(summary.failed_last_run)}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

// The two realms call today's state differently: the China routine signs in,
// the international one holds an activity conversation.
function todayStateLabel(
  t: (key: string) => string,
  isCN: boolean,
  doneToday: boolean
): string {
  if (isCN) {
    return doneToday ? t('Checked in') : t('Not checked in')
  }
  return doneToday ? t('Active today') : t('Not active today')
}

// The daily rollup is the only place the panel can show past days: the live
// window cards describe the current fold and keep no history.
function WorkBuddyUsageHistoryCard(props: { rows: WorkBuddyUsageDailyRow[] }) {
  const { t } = useTranslation()
  const byDay = new Map<string, number>()
  for (const row of props.rows) {
    byDay.set(row.day, (byDay.get(row.day) ?? 0) + row.total_tokens)
  }
  const days = [...byDay.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  // Bars are relative to the busiest day so a quiet fortnight still reads.
  const peak = days.reduce((max, [, total]) => Math.max(max, total), 0)

  return (
    <Card size='sm'>
      <CardHeader className='pb-2'>
        <CardTitle className='text-sm'>{t('Usage history')}</CardTitle>
        <CardDescription>{t('Last 14 days')}</CardDescription>
      </CardHeader>
      <CardContent>
        {days.length === 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('No usage history yet')}
          </p>
        ) : (
          <ul className='flex flex-col gap-1.5'>
            {days.map(([day, total]) => (
              <li key={day} className='flex items-center gap-3 text-xs'>
                <span className='text-muted-foreground w-12 shrink-0 tabular-nums'>
                  {day.slice(5)}
                </span>
                <Progress
                  value={peak ? (total / peak) * 100 : 0}
                  className='flex-1'
                />
                <span className='w-16 shrink-0 text-right tabular-nums'>
                  {formatTokens(total)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

// WorkBuddyUsageCard is one realm's token headline. The numbers come from the
// passive log fold, so they are available even before the first upstream probe.
function WorkBuddyUsageCard(props: {
  realm: string
  summary?: WorkBuddyUsageSummary
}) {
  const { t } = useTranslation()
  const summary = props.summary
  const isCN = props.realm === REALM_CN

  return (
    <Card size='sm'>
      <CardHeader className='pb-2'>
        <CardTitle className='flex items-center gap-2 text-sm'>
          <Badge variant={isCN ? 'destructive' : 'default'}>
            {isCN ? t('China') : t('International')}
          </Badge>
          <span className='text-muted-foreground text-xs font-normal'>
            {t('Token usage')}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <InfoField
          label={t('Accounts')}
          value={String(summary?.accounts ?? 0)}
        />
        <InfoField
          label={t('Total tokens')}
          value={formatTokens(summary?.total_tokens ?? 0)}
        />
        <InfoField
          label={t('Cooling')}
          value={String(summary?.cooling_accounts ?? 0)}
        />
        <InfoField
          label={t('Near the limit')}
          value={String(summary?.near_limit_accounts ?? 0)}
        />
      </CardContent>
    </Card>
  )
}

function WorkBuddyAccountRow(props: {
  account: WorkBuddyAccountView
  usage?: WorkBuddyModelUsage
  showRealm: boolean
  showStreak: boolean
  busy: boolean
  onCheckin: () => void
  onRun: (actions: string[]) => void
  onToggleTasks: (enabled: boolean) => void
}) {
  const { t } = useTranslation()
  const account = props.account
  const isCN = account.realm === REALM_CN
  const doneToday = isCN ? account.checked_in_today : account.daily_chat_today

  return (
    <TableRow>
      <TableCell className='max-w-56 truncate'>
        {account.nickname || account.account_uid}
      </TableCell>
      <TableCell className='max-w-56 truncate'>
        {account.channel_name} (#{account.channel_id})
      </TableCell>
      {props.showRealm ? (
        <TableCell>
          <Badge variant={isCN ? 'destructive' : 'default'}>
            {isCN ? t('China') : t('International')}
          </Badge>
        </TableCell>
      ) : null}
      <TableCell>
        <Badge variant={doneToday ? 'default' : 'outline'}>
          {todayStateLabel(t, isCN, doneToday)}
        </Badge>
      </TableCell>
      <TableCell>
        <Switch
          checked={account.tasks_enabled}
          disabled={props.busy}
          onCheckedChange={props.onToggleTasks}
          aria-label={t('Tasks enabled')}
        />
      </TableCell>
      {props.showStreak ? (
        <TableCell>{account.streak_days || '-'}</TableCell>
      ) : null}
      <TableCell>{formatNumber(account.remain_credit)}</TableCell>
      <TableCell>
        <WorkBuddyUsageCell usage={props.usage} />
      </TableCell>
      <TableCell className='max-w-72'>
        <div className='truncate text-sm'>
          {taskLabel(t, account.last_task)}
        </div>
        <div
          className='text-muted-foreground truncate text-xs'
          title={account.last_message}
        >
          {formatTime(account.last_run_at)} ·{' '}
          {account.last_message
            ? workBuddyRunMessage(t, account.last_message)
            : '-'}
        </div>
      </TableCell>
      <TableCell className='text-right'>
        <div className='flex justify-end gap-2'>
          {isCN ? (
            <Button
              variant='outline'
              size='sm'
              disabled={props.busy}
              onClick={props.onCheckin}
            >
              {t('Check in')}
            </Button>
          ) : null}
          <Button
            variant='outline'
            size='sm'
            disabled={props.busy}
            onClick={() => props.onRun(isCN ? ['checkin'] : ['daily_chat'])}
          >
            {t('Run')}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

// WorkBuddyUsageCell renders one account's token window: the used amount over
// the estimated budget, a progress bar, and the cooling state when the window is
// spent. A model with no published budget shows absolute usage only.
function WorkBuddyUsageCell(props: { usage?: WorkBuddyModelUsage }) {
  const { t } = useTranslation()
  const usage = props.usage
  if (!usage) {
    return (
      <span className='text-muted-foreground text-xs'>{t('No usage yet')}</span>
    )
  }
  const percent = remainingPercent(usage)
  const usedPercent = percent === null ? null : 100 - percent
  const overThreshold = usedPercent !== null && usedPercent >= 80
  const barTone =
    usage.cooling || overThreshold ? 'bg-destructive' : 'bg-primary'
  return (
    <div className='flex min-w-32 flex-col gap-1'>
      <div className='flex items-center gap-2'>
        <span className='text-sm font-medium'>
          {formatTokens(usage.total_tokens)}
        </span>
        {usage.budget_tokens > 0 ? (
          <span className='text-muted-foreground text-xs'>
            / {formatTokens(usage.budget_tokens)}
          </span>
        ) : null}
        {usage.cooling ? (
          <Badge variant='destructive'>{t('Cooling')}</Badge>
        ) : null}
      </div>
      {usedPercent !== null ? (
        <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
          <div
            className={cn('h-full rounded-full', barTone)}
            style={{ width: `${Math.max(2, usedPercent)}%` }}
          />
        </div>
      ) : null}
      <div
        className='text-muted-foreground truncate text-xs'
        title={`${usage.model} · ${budgetSourceLabel(t, usage.budget_source)}`}
      >
        {usage.model}
        {usage.window_reset_at
          ? ` · ${t('Resets')} ${formatTime(usage.window_reset_at)}`
          : ''}
      </div>
    </div>
  )
}

function WorkBuddyRunRow(props: { run: WorkBuddyTaskRun }) {
  const { t } = useTranslation()
  const run = props.run
  return (
    <div className='flex items-center justify-between gap-4 py-2 text-sm'>
      <div className='min-w-0'>
        <span className='font-medium'>{taskLabel(t, run.task)}</span>
        <span className='text-muted-foreground'>
          {' '}
          · {run.account_uid.slice(0, 8)}
        </span>
        <span className='text-muted-foreground'>
          {' '}
          · {taskLabel(t, run.trigger)}
        </span>
      </div>
      <div
        className='text-muted-foreground max-w-96 truncate'
        title={run.message}
      >
        {workBuddyRunMessage(t, run.message)}
      </div>
      <div className='flex items-center gap-2'>
        {run.credit_delta > 0 ? (
          <Badge variant='outline'>+{formatNumber(run.credit_delta)}</Badge>
        ) : null}
        <Badge variant={run.ok ? 'default' : 'destructive'}>
          {run.ok ? t('Success') : t('Failed')}
        </Badge>
        <span className='text-muted-foreground text-xs'>
          {formatTime(run.created_at)}
        </span>
      </div>
    </div>
  )
}

const SETTING_PREFIX = 'workbuddy_task_setting.'

type ToggleKey =
  | 'auto_daily_chat'
  | 'daily_chat_web'
  | 'auto_checkin'
  | 'auto_activity_report'
  | 'auto_travel'
  | 'auto_streak_bonus'
  | 'auto_growth_tasks'
  | 'auto_night_cat'

// The two realms are configured apart: their routines, their call volume and
// therefore their schedule and budget have nothing in common.
function WorkBuddyTaskSettingsCards(props: {
  settings: WorkBuddyTaskSettings
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const settings = props.settings

  const save = (key: string, value: string | number | boolean) => {
    updateOption.mutate(
      { key: SETTING_PREFIX + key, value },
      {
        onSuccess: () => {
          void queryClient.invalidateQueries({ queryKey: ACCOUNTS_QUERY_KEY })
        },
      }
    )
  }

  const toggles = (
    realm: 'intl' | 'cn'
  ): Array<{ key: ToggleKey; label: string }> => {
    if (realm === 'intl') {
      return [
        {
          key: 'auto_daily_chat',
          label: t('Daily activity chat (International)'),
        },
        {
          key: 'daily_chat_web',
          label: t('Use the web channel (International)'),
        },
      ]
    }
    return [
      { key: 'auto_checkin', label: t('Daily check-in (China)') },
      { key: 'auto_activity_report', label: t('Activity report (China)') },
      { key: 'auto_travel', label: t('Cat travel (China)') },
      { key: 'auto_streak_bonus', label: t('Streak housekeeping (China)') },
      { key: 'auto_growth_tasks', label: t('Growth tasks (China)') },
      { key: 'auto_night_cat', label: t('Night-owl task (China)') },
    ]
  }

  return (
    <div className='grid gap-3 lg:grid-cols-2'>
      <Card>
        <CardHeader>
          <CardTitle className='text-sm'>{t('International realm')}</CardTitle>
          <CardDescription>
            {t('One daily activity conversation per account')}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-4'>
          <div className='flex flex-col gap-3'>
            {toggles('intl').map((toggle) => (
              <div
                key={toggle.key}
                className='flex items-center justify-between gap-4'
              >
                <Label htmlFor={`wb-task-${toggle.key}`}>{toggle.label}</Label>
                <Switch
                  id={`wb-task-${toggle.key}`}
                  checked={settings[toggle.key] === true}
                  disabled={updateOption.isPending}
                  onCheckedChange={(checked) => save(toggle.key, checked)}
                />
              </div>
            ))}
          </div>
          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='wb-task-hours-intl'>
                {t('Run at (local hours)')}
              </Label>
              <Input
                id='wb-task-hours-intl'
                defaultValue={settings.schedule_hours_intl}
                onBlur={(event) =>
                  save('schedule_hours_intl', event.target.value)
                }
              />
            </div>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='wb-task-limit-intl'>
                {t('Daily request limit')}
              </Label>
              <Input
                id='wb-task-limit-intl'
                type='number'
                defaultValue={settings.daily_request_limit_intl}
                onBlur={(event) =>
                  save('daily_request_limit_intl', Number(event.target.value))
                }
              />
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='text-sm'>{t('China realm')}</CardTitle>
          <CardDescription>
            {t(
              'Sign-in, activity report, cat travel, streak housekeeping and the growth centre'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-4'>
          <div className='grid gap-3 sm:grid-cols-2'>
            {toggles('cn').map((toggle) => (
              <div
                key={toggle.key}
                className='flex items-center justify-between gap-4'
              >
                <Label htmlFor={`wb-task-${toggle.key}`}>{toggle.label}</Label>
                <Switch
                  id={`wb-task-${toggle.key}`}
                  checked={settings[toggle.key] === true}
                  disabled={updateOption.isPending}
                  onCheckedChange={(checked) => save(toggle.key, checked)}
                />
              </div>
            ))}
          </div>
          <div className='grid gap-3 sm:grid-cols-3'>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='wb-task-hours-cn'>
                {t('Run at (local hours)')}
              </Label>
              <Input
                id='wb-task-hours-cn'
                defaultValue={settings.schedule_hours_cn}
                onBlur={(event) =>
                  save('schedule_hours_cn', event.target.value)
                }
              />
            </div>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='wb-task-limit-cn'>
                {t('Daily request limit')}
              </Label>
              <Input
                id='wb-task-limit-cn'
                type='number'
                defaultValue={settings.daily_request_limit_cn}
                onBlur={(event) =>
                  save('daily_request_limit_cn', Number(event.target.value))
                }
              />
            </div>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='wb-task-growth-limit'>
                {t('Daily request limit with growth tasks')}
              </Label>
              <Input
                id='wb-task-growth-limit'
                type='number'
                defaultValue={settings.growth_request_limit}
                onBlur={(event) =>
                  save('growth_request_limit', Number(event.target.value))
                }
              />
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function InfoField(props: { label: string; value?: string }) {
  const value = props.value?.trim()
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 truncate text-sm font-medium' title={value || '-'}>
        {value || '-'}
      </div>
    </div>
  )
}
