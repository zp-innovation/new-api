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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { parseScheduleRules } from './lib/schedule-rules'
import { ChannelScheduleRulesView } from './rules-view'
import { ChannelScheduleStatusView } from './status-view'
import {
  CHANNEL_SCHEDULE_RULES_KEY,
  type ChannelScheduleRunResult,
} from './types'
import {
  useChannelScheduleConfig,
  useChannelScheduleRetryTimes,
  useChannelScheduleRun,
  useSaveChannelSchedule,
  useScheduleChannelOptions,
} from './use-channel-schedule'

export function ChannelScheduleSection() {
  const { t } = useTranslation()
  const query = useChannelScheduleConfig()
  const retryTimesQuery = useChannelScheduleRetryTimes()
  const channelOptionsQuery = useScheduleChannelOptions()
  const saveMutation = useSaveChannelSchedule()
  const runMutation = useChannelScheduleRun()
  const [lastRun, setLastRun] = useState<ChannelScheduleRunResult | null>(null)

  const options = query.data?.options
  const rulesCount = useMemo(
    () => parseScheduleRules(options?.[CHANNEL_SCHEDULE_RULES_KEY]).length,
    [options]
  )
  // Remount the editor when the persisted options change so its local draft
  // never mixes with a freshly saved state.
  const savedKey = useMemo(() => JSON.stringify(options ?? {}), [options])

  if (query.isPending) return <LoadingState />
  if (query.isError || !query.data) {
    return (
      <ErrorState
        title={t('Failed to load settings')}
        onRetry={() => void query.refetch()}
      />
    )
  }

  return (
    <Tabs defaultValue='rules' className='min-w-0'>
      <TabsList>
        <TabsTrigger value='rules'>
          {t('Rules ({{count}})', { count: rulesCount })}
        </TabsTrigger>
        <TabsTrigger value='status'>{t('Applied state')}</TabsTrigger>
      </TabsList>

      <TabsContent value='rules'>
        <ChannelScheduleRulesView
          key={savedKey}
          options={query.data.options}
          status={query.data.status}
          channelOptions={channelOptionsQuery.data ?? []}
          retryTimes={retryTimesQuery.data ?? 0}
          isSaving={saveMutation.isPending}
          onSave={(changed) => saveMutation.mutate(changed)}
        />
      </TabsContent>

      <TabsContent value='status'>
        <ChannelScheduleStatusView
          status={query.data.status}
          lastRun={lastRun}
          isChecking={runMutation.isPending}
          onCheck={() =>
            runMutation.mutate(undefined, {
              onSuccess: (data) => setLastRun(data.result),
            })
          }
        />
      </TabsContent>
    </Tabs>
  )
}
