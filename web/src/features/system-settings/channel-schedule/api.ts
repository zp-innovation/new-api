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
import { getChannels } from '@/features/channels/api'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { ChannelScheduleConfig, ChannelScheduleRunResponse } from './types'

type Response<T> = { success: boolean; message?: string; data: T }

/** One channel the rule editor can target, plus its single tag. */
export type ScheduleChannelOption = {
  id: number
  name: string
  tag: string
}

export async function getChannelScheduleConfig(): Promise<ChannelScheduleConfig> {
  const response = await api.get<Response<ChannelScheduleConfig>>(
    '/api/option/channel_schedule'
  )
  return requireServerSuccess(response.data).data
}

export async function saveChannelScheduleConfig(
  options: Record<string, string>
): Promise<ChannelScheduleConfig> {
  const response = await api.patch<Response<ChannelScheduleConfig>>(
    '/api/option/channel_schedule',
    { options }
  )
  return requireServerSuccess(response.data).data
}

export async function runChannelScheduleNow(): Promise<ChannelScheduleRunResponse> {
  const response = await api.post<Response<ChannelScheduleRunResponse>>(
    '/api/option/channel_schedule/run'
  )
  return requireServerSuccess(response.data).data
}

/**
 * The retry budget lives in the request policy options. Priority is layered and
 * traffic only drops to the lower layer on a retry, so the schedule view warns
 * when this is 0.
 */
export async function getChannelScheduleRetryTimes(): Promise<number> {
  const response = await api.get<Response<{ options: Record<string, string> }>>(
    '/api/option/request_policy'
  )
  const options = requireServerSuccess(response.data).data.options
  const parsed = Number(options?.RetryTimes ?? 0)
  return Number.isFinite(parsed) ? parsed : 0
}

/** Channels and the distinct tags they carry, for the rule editor targets. */
export async function getScheduleChannelOptions(): Promise<
  ScheduleChannelOption[]
> {
  const response = await getChannels({ p: 1, page_size: 100 })
  const items = response.data?.items ?? []
  return items.map((channel) => ({
    id: channel.id,
    name: channel.name,
    tag: (channel.tag ?? '').trim(),
  }))
}
