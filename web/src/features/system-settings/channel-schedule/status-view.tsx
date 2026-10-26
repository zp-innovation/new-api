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
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { SettingsCard } from '../components/settings-card'
import type { ChannelScheduleRunResult, ChannelScheduleStatus } from './types'

/** The backend reports why a run did nothing in English; map it for display. */
const SKIP_REASON_KEYS: Record<string, string> = {
  'schedule disabled': 'The schedule is off',
  'no rule matches the current time': 'No rule matches the current time',
  'another run holds the lock': 'Another node is already running this check',
}

type StatusViewProps = {
  status: ChannelScheduleStatus
  lastRun: ChannelScheduleRunResult | null
  isChecking: boolean
  onCheck: () => void
}

export function ChannelScheduleStatusView(props: StatusViewProps) {
  const { t } = useTranslation()
  const { status } = props
  const lastRunSummary = () => {
    const run = props.lastRun
    if (!run) return null
    if (run.skipped) {
      return t(SKIP_REASON_KEYS[run.skipped] ?? 'Check finished')
    }
    return t('Changed {{count}} channel(s)', { count: run.changes.length })
  }

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <SettingsCard
        title={t('Applied state')}
        description={t(
          'A manual change counts as drift and is corrected on the next check.'
        )}
      >
        <div className='flex flex-col gap-4'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <dl className='grid gap-x-8 gap-y-2 sm:grid-cols-2'>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Current time')}
                </dt>
                <dd className='text-sm'>
                  {status.now} ({status.timezone})
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Matched rule')}
                </dt>
                <dd className='text-sm'>
                  {status.matched_rule_name ?? t('No rule is active right now')}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Consistency')}
                </dt>
                <dd className='text-sm'>
                  <Badge variant={status.consistent ? 'secondary' : 'warning'}>
                    {status.consistent
                      ? t('Consistent')
                      : t('Pending correction')}
                  </Badge>
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Last run')}
                </dt>
                <dd className='text-sm'>
                  {status.last_run_at > 0
                    ? new Date(status.last_run_at * 1000).toLocaleString()
                    : t('Never run')}
                </dd>
              </div>
            </dl>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={props.onCheck}
              disabled={props.isChecking}
            >
              <RefreshCw data-icon='inline-start' />
              {props.isChecking ? t('Checking...') : t('Check now')}
            </Button>
          </div>

          {lastRunSummary() ? (
            <p className='text-muted-foreground text-sm' role='status'>
              {lastRunSummary()}
            </p>
          ) : null}
        </div>
      </SettingsCard>

      <SettingsCard title={t('Expected')}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='w-[28%]'>{t('Scope')}</TableHead>
              <TableHead>{t('Expected')}</TableHead>
              <TableHead>{t('Actual')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {status.expectations.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className='text-muted-foreground'>
                  {t('No rules yet. Add one to switch channels by time.')}
                </TableCell>
              </TableRow>
            ) : (
              status.expectations.flatMap((expectation) => {
                if (expectation.fields.length === 0) {
                  return [
                    <TableRow key={`${expectation.scope}-none`}>
                      <TableCell>{expectation.scope}</TableCell>
                      <TableCell className='text-muted-foreground'>—</TableCell>
                      <TableCell className='text-muted-foreground'>—</TableCell>
                      <TableCell>
                        <Badge variant='warning'>
                          {t('Not part of the schedule')}
                        </Badge>
                      </TableCell>
                    </TableRow>,
                  ]
                }
                return expectation.fields.map((field) => (
                  <TableRow key={`${expectation.scope}-${field.field}`}>
                    <TableCell>
                      {expectation.scope}
                      {!expectation.participates ? (
                        <Badge variant='warning' className='ml-2'>
                          {t('Not part of the schedule')}
                        </Badge>
                      ) : null}
                    </TableCell>
                    <TableCell>{field.expected}</TableCell>
                    <TableCell>{field.actual}</TableCell>
                    <TableCell>
                      <Badge variant={field.matches ? 'secondary' : 'warning'}>
                        {field.matches
                          ? t('Consistent')
                          : t('Pending correction')}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))
              })
            )}
          </TableBody>
        </Table>
      </SettingsCard>
    </div>
  )
}
