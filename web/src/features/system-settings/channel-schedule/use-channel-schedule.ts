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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'

import {
  getChannelScheduleConfig,
  getChannelScheduleRetryTimes,
  getScheduleChannelOptions,
  runChannelScheduleNow,
  saveChannelScheduleConfig,
} from './api'

export const channelScheduleQueryKey = ['channel-schedule'] as const

export function useChannelScheduleConfig() {
  return useQuery({
    queryKey: channelScheduleQueryKey,
    queryFn: getChannelScheduleConfig,
    staleTime: Infinity,
  })
}

export function useChannelScheduleRun() {
  const client = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: runChannelScheduleNow,
    onSuccess: (data) => {
      client.setQueryData(channelScheduleQueryKey, (previous: unknown) => {
        if (
          typeof previous === 'object' &&
          previous !== null &&
          'options' in previous
        ) {
          return { ...previous, status: data.status }
        }
        return previous
      })
      toast.success(t('Check finished'))
    },
    onError: (error) => handleServerError(error),
    meta: { errorToast: false },
  })
}

export function useSaveChannelSchedule() {
  const client = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: saveChannelScheduleConfig,
    onSuccess: (data) => {
      client.setQueryData(channelScheduleQueryKey, data)
      void client.invalidateQueries({ queryKey: ['system-options'] })
      void client.invalidateQueries({ queryKey: ['channel-ops'] })
      toast.success(t('Saved successfully'))
    },
    onError: (error) => handleServerError(error),
    meta: { errorToast: false },
  })
}

export function useChannelScheduleRetryTimes() {
  return useQuery({
    queryKey: [...channelScheduleQueryKey, 'retry-times'],
    queryFn: getChannelScheduleRetryTimes,
    staleTime: Infinity,
  })
}

export function useScheduleChannelOptions() {
  return useQuery({
    queryKey: [...channelScheduleQueryKey, 'channels'],
    queryFn: getScheduleChannelOptions,
    staleTime: 5 * 60 * 1000,
  })
}
