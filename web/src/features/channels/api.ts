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
import { getGroups as getUserGroups } from '@/features/users/api'
import { api, type ApiRequestConfig } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { PriorityMove, PriorityOrder } from './lib/channel-priority-order'
import type { InferenceStatus } from './lib/inference-status'
import type {
  AddChannelRequest,
  WorkBuddyAccountView,
  WorkBuddyAccountsResponse,
  WorkBuddyRunResponse,
  WorkBuddyTaskRunsResponse,
  WorkBuddyTaskSummary,
  BatchDeleteParams,
  BatchSetTagParams,
  Channel,
  ChannelBalanceResponse,
  ChannelOpsResponse,
  ChannelTestResponse,
  CopyChannelParams,
  CopyChannelResponse,
  FetchModelsResponse,
  GetChannelResponse,
  GetChannelsParams,
  GetChannelsResponse,
  MultiKeyManageParams,
  MultiKeyStatusResponse,
  SearchChannelsParams,
  SearchChannelsResponse,
  TagOperationParams,
  WorkBuddyUsageDailyRow,
  WorkBuddyUsageResponse,
} from './types'

const channelActionConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

export async function getChannelPriorityOrder(): Promise<PriorityOrder> {
  const response = await api.get<{ success: boolean; data: PriorityOrder }>(
    '/api/channel/priority/order',
    { disableDuplicate: true }
  )
  return requireServerSuccess(response.data).data
}

export async function reorderChannelPriority(
  move: PriorityMove
): Promise<PriorityOrder> {
  const response = await api.post<{ success: boolean; data: PriorityOrder }>(
    '/api/channel/priority/reorder',
    move
  )
  return requireServerSuccess(response.data).data
}

export async function getInferenceStatus(
  channelId: number,
  provider: 'vllm' | 'sglang',
  signal?: AbortSignal
): Promise<InferenceStatus> {
  const response = await api.get<{ success: boolean; data: InferenceStatus }>(
    `/api/channel/${channelId}/${provider}/status`,
    { signal, disableDuplicate: true }
  )
  return requireServerSuccess(response.data).data
}

export type TaskPluginOption = {
  sortPriority?: number
  website?: string
  key: string
  name: string
  description?: Record<string, string> | null
  icon?: string
  hasIcon?: boolean
  baseUrl?: string
  models: string[]
  channelTypes?: number[] | null
  upstreams?: string[] | null
}

export async function getTaskPluginOptions(): Promise<TaskPluginOption[]> {
  const response = await api.get<{
    success: boolean
    data: TaskPluginOption[]
  }>('/api/task_plugin_options')
  return requireServerSuccess(response.data).data
}

export type CodexUsageResponse = {
  success: boolean
  message?: string
  upstream_status?: number
  data?: Record<string, unknown>
}

export type CodexResetCreditsResponse = CodexUsageResponse

export type CodexUsageResetResponse = CodexUsageResponse

export type CodexCredentialRefreshResponse = {
  success: boolean
  message?: string
  data?: {
    expires_at?: string
    last_refresh?: string
    account_id?: string
    email?: string
    channel_id?: number
    channel_type?: number
    channel_name?: string
  }
}

// ============================================================================
// Base Channel CRUD Operations
// ============================================================================

/**
 * Get paginated list of channels
 */
export async function getChannels(
  params: GetChannelsParams = {}
): Promise<GetChannelsResponse> {
  const res = await api.get('/api/channel', { params })
  return res.data
}

/**
 * Search channels with filters
 */
export async function searchChannels(
  params: SearchChannelsParams
): Promise<SearchChannelsResponse> {
  const res = await api.get('/api/channel/search', { params })
  return res.data
}

/**
 * Get single channel by ID
 */
export async function getChannel(id: number): Promise<GetChannelResponse> {
  const res = await api.get(`/api/channel/${id}`)
  return res.data
}

/**
 * Get channel operations summary for administrators
 */
export async function getChannelOps(
  autoBan?: boolean
): Promise<ChannelOpsResponse> {
  const res = await api.get(
    '/api/channel/ops',
    channelActionConfig({
      params: autoBan === undefined ? undefined : { auto_ban: autoBan },
    })
  )
  return res.data
}

export async function getChannelDefaultBaseURLs(): Promise<
  Partial<Record<number, string>>
> {
  const response = await api.get<{
    success: boolean
    data: Partial<Record<number, string>>
  }>('/api/channel/default_base_urls')
  return requireServerSuccess(response.data).data
}

/**
 * Create new channel(s)
 * Supports single, batch, and multi-key modes
 */
export async function createChannel(
  data: AddChannelRequest
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post('/api/channel', data, channelActionConfig())
  return res.data
}

/**
 * Update existing channel
 */
export async function updateChannel(
  id: number,
  data: Partial<Channel>
): Promise<{ success: boolean; message?: string; data?: Channel }> {
  const res = await api.put(
    '/api/channel/',
    { id, ...data },
    channelActionConfig()
  )
  return res.data
}

/**
 * Update channel enabled/disabled status.
 */
export async function updateChannelStatus(
  id: number,
  status: number
): Promise<{ success: boolean; message?: string; data?: boolean }> {
  const res = await api.post(
    `/api/channel/${id}/status`,
    { status },
    channelActionConfig()
  )
  return res.data
}

/**
 * Batch update channel enabled/disabled status.
 */
export async function batchUpdateChannelStatus(
  ids: number[],
  status: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/status/batch',
    { ids, status },
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete single channel
 */
export async function deleteChannel(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/channel/${id}`, channelActionConfig())
  return res.data
}

/**
 * Batch delete channels
 */
export async function batchDeleteChannels(
  data: BatchDeleteParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post('/api/channel/batch', data, channelActionConfig())
  return res.data
}

/**
 * Batch set tag for channels
 */
export async function batchSetChannelTag(
  data: BatchSetTagParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/batch/tag',
    data,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Channel Operations
// ============================================================================

/**
 * Test channel connectivity
 */
export async function testChannel(
  id: number,
  params?: { model?: string; endpoint_type?: string; stream?: boolean }
): Promise<ChannelTestResponse> {
  const res = await api.get(
    `/api/channel/test/${id}`,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Update channel balance
 */
export async function updateChannelBalance(
  id: number
): Promise<ChannelBalanceResponse> {
  const res = await api.get(
    `/api/channel/update_balance/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Fetch available models from upstream provider
 */
export async function fetchUpstreamModels(
  id: number
): Promise<FetchModelsResponse> {
  const res = await api.get(
    `/api/channel/fetch_models/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Copy/clone a channel
 */
export async function copyChannel(
  id: number,
  params: CopyChannelParams = {}
): Promise<CopyChannelResponse> {
  const res = await api.post(
    `/api/channel/copy/${id}`,
    null,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Fix channel abilities
 */
export async function fixChannelAbilities(): Promise<{
  success: boolean
  message?: string
  data?: { success: number; fails: number }
}> {
  const res = await api.post(
    '/api/channel/fix',
    undefined,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete all disabled channels
 */
export async function deleteDisabledChannels(): Promise<{
  success: boolean
  message?: string
  data?: number
}> {
  const res = await api.delete('/api/channel/disabled', channelActionConfig())
  return res.data
}

/**
 * Get channel key (requires 2FA verification)
 */
export async function getChannelKey(
  id: number,
  proofToken: string,
  signal?: AbortSignal
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const res = await api.post(
    `/api/channel/${id}/key`,
    undefined,
    channelActionConfig({
      headers: { 'X-Security-Proof': proofToken },
      signal,
    })
  )
  return res.data
}

// ============================================================================
// Codex Channel Operations
// ============================================================================

export async function refreshCodexCredential(
  channelId: number
): Promise<CodexCredentialRefreshResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

export async function getCodexUsage(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function getCodexResetCredits(
  channelId: number
): Promise<CodexResetCreditsResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage/reset-credits`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function resetCodexUsage(
  channelId: number
): Promise<CodexUsageResetResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/usage/reset`,
    {},
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

// ============================================================================
// Codex OAuth
// ============================================================================

export type CodexAuthURLResponse = {
  success: boolean
  message?: string
  data?: {
    auth_url: string
    session_id: string
    state: string
    redirect_uri: string
  }
}

export type CodexOAuthCredentialResponse = {
  success: boolean
  message?: string
  data?: {
    type?: string
    id_token?: string
    access_token?: string
    refresh_token?: string
    account_id?: string
    email?: string
    last_refresh?: string
    expired?: string
  }
}

export async function generateCodexAuthURL(
  params: { proxy?: string } = {}
): Promise<CodexAuthURLResponse> {
  const res = await api.post(
    '/api/channel/oauth/codex/auth-url',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function exchangeCodexCode(params: {
  session_id: string
  state: string
  code: string
  proxy?: string
}): Promise<CodexOAuthCredentialResponse> {
  const res = await api.post(
    '/api/channel/oauth/codex/exchange-code',
    params,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Antigravity OAuth
// ============================================================================

export type AntigravityAuthURLResponse = {
  success: boolean
  message?: string
  data?: {
    auth_url: string
    session_id: string
    state: string
    redirect_uri: string
  }
}

export type AntigravityCredentialResponse = {
  success: boolean
  message?: string
  data?: {
    type?: string
    access_token: string
    refresh_token?: string
    expires_at?: string
    last_refresh?: string
    email?: string
    project_id: string
    plan_type?: string
  }
}

export async function generateAntigravityAuthURL(
  params: { proxy?: string } = {}
): Promise<AntigravityAuthURLResponse> {
  const res = await api.post(
    '/api/channel/oauth/antigravity/auth-url',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function exchangeAntigravityCode(params: {
  session_id: string
  state: string
  code: string
  proxy?: string
}): Promise<AntigravityCredentialResponse> {
  const res = await api.post(
    '/api/channel/oauth/antigravity/exchange-code',
    params,
    channelActionConfig()
  )
  return res.data
}

export type AntigravityQuotaModel = {
  model: string
  display_name?: string
  remaining_fraction: number
  reset_time?: string
  recommended?: boolean
  supports_images?: boolean
  supports_thinking?: boolean
}

export type AntigravityQuotaResponse = {
  success: boolean
  message?: string
  data?: {
    channel_id: number
    channel_name: string
    email?: string
    plan_type?: string
    project_id?: string
    models: AntigravityQuotaModel[]
  }
}

export async function getAntigravityChannelQuota(
  channelId: number
): Promise<AntigravityQuotaResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/antigravity/quota`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export type AntigravityQuotaOverviewAccount = {
  channel_id: number
  channel_name: string
  remaining_fraction: number
  reset_time: number
  status: string
  fetched_at: number
  exhausted: boolean
  remaining_change_per_hour?: number
  exhausted_at?: number
}

export type AntigravityQuotaOverviewModel = {
  model: string
  total_accounts: number
  available_accounts: number
  earliest_reset: number
  in_use: boolean
  accounts: AntigravityQuotaOverviewAccount[]
}

export type AntigravityQuotaOverviewResponse = {
  success: boolean
  message?: string
  data?: {
    models: AntigravityQuotaOverviewModel[]
    hidden_models: number
    in_use_count: number
    generated_at: number
  }
}

/**
 * Reads the stored per-(account, model) quota snapshot used by the routing
 * layer. It never calls the upstream provider, so the board can refresh freely
 * and still render while an account is unreachable.
 */
export async function getAntigravityQuotaOverview(
  all = false
): Promise<AntigravityQuotaOverviewResponse> {
  const res = await api.get(
    all
      ? '/api/channel/antigravity/quotas?all=1'
      : '/api/channel/antigravity/quotas',
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export type WorkBuddyCreditPackage = {
  name: string
  package_code?: string
  product_name?: string
  sub_product_name?: string
  grant_reason?: string
  resource_id?: string
  deal_name?: string
  create_time?: string
  remain: number
  used: number
  size: number
  unit?: string
  in_usage: boolean
  auto_renew: boolean
  no_expiry: boolean
  is_expired: boolean
  cycle_start_time?: string
  cycle_end_time?: string
  expire_time?: string
  days_left?: number
  status: number
}

export type WorkBuddyCheckinInfo = {
  today_checked_in: boolean
  streak_days: number
  daily_credit: number
  today_credit: number
  active: boolean
}

export type WorkBuddyExpiringPackage = {
  name: string
  package_code?: string
  remain: number
  cycle_end_time?: string
  days_left: number
}

export type WorkBuddyCredits = {
  remain: number
  used: number
  size: number
  used_percent: string
  remain_percent: string
  is_paid_user: boolean
  is_enterprise: boolean
  checkin?: WorkBuddyCheckinInfo
  earliest_expiring?: WorkBuddyExpiringPackage
  packages: WorkBuddyCreditPackage[]
  updated_at: string
}

export type WorkBuddyCreditsResponse = {
  success: boolean
  message?: string
  data?: {
    channel_id: number
    channel_name: string
    account_uid?: string
    account_name?: string
    account_count?: number
    credits: WorkBuddyCredits
  }
}

export async function getWorkBuddyChannelCredits(
  channelId: number
): Promise<WorkBuddyCreditsResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/workbuddy/credits`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function refreshAntigravityChannelCredential(
  channelId: number
): Promise<AntigravityCredentialResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/antigravity/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

/**
 * Renews one OAuth credential before it is saved as a channel key. The response
 * omits the account fields (project_id, email, plan_type), so the caller merges
 * them over the stored credential; see mergeAntigravityCredential.
 */
export async function refreshAntigravityCredential(params: {
  refresh_token: string
  proxy?: string
}): Promise<AntigravityCredentialResponse> {
  const res = await api.post(
    '/api/channel/oauth/antigravity/refresh-token',
    params,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Gemini Web account pool
// ============================================================================

export type GeminiWebAccount = {
  id: number
  channel_id: number
  label: string
  status: string
  note: string
  fail_count: number
  last_used_at: number
  last_ok_at: number
  last_error: string
  created_at: number
  cookie_size: number
}

export type GeminiWebAccountListResponse = {
  success: boolean
  message?: string
  data?: GeminiWebAccount[]
}

export async function listGeminiWebAccounts(
  channelId: number
): Promise<GeminiWebAccountListResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/gemini-web/accounts`,
    channelActionConfig()
  )
  return res.data
}

export async function addGeminiWebAccount(
  channelId: number,
  payload: { label: string; cookie: string; note?: string; proxy?: string }
): Promise<{ success: boolean; message?: string; data?: { id: number } }> {
  const res = await api.post(
    `/api/channel/${channelId}/gemini-web/accounts`,
    payload,
    channelActionConfig()
  )
  return res.data
}

export async function deleteGeminiWebAccount(
  channelId: number,
  accountId: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(
    `/api/channel/${channelId}/gemini-web/accounts/${accountId}`,
    channelActionConfig()
  )
  return res.data
}

/** Disables an account without deleting it, so its cookie can be reused later. */
export async function setGeminiWebAccountStatus(
  channelId: number,
  accountId: number,
  status: 'enabled' | 'disabled'
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    `/api/channel/${channelId}/gemini-web/accounts/${accountId}/status`,
    { status },
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// WorkBuddy OAuth (device authorization)
// ============================================================================

export type WorkBuddyStartLoginResponse = {
  success: boolean
  message?: string
  data?: {
    state: string
    auth_url: string
    realm: string
    product: string
    expires_in: number
  }
}

export type WorkBuddyLoginStatusResponse = {
  success: boolean
  message?: string
  data?: {
    status: 'pending' | 'ready' | 'expired'
    credential?: string
  }
}

export async function startWorkBuddyLogin(params: {
  realm?: string
  product?: string
  proxy?: string
}): Promise<WorkBuddyStartLoginResponse> {
  const res = await api.post(
    '/api/channel/oauth/workbuddy/auth-url',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function pollWorkBuddyLogin(params: {
  state: string
  proxy?: string
}): Promise<WorkBuddyLoginStatusResponse> {
  const res = await api.post(
    '/api/channel/oauth/workbuddy/poll-token',
    params,
    channelActionConfig()
  )
  return res.data
}

export type WorkBuddyCredentialResponse = {
  success: boolean
  message?: string
  data?: {
    credential: string
  }
}

/**
 * Renews one credential before it is saved as a channel key. The upstream
 * rotates the refresh token, so the returned value replaces the stored one.
 */
export async function refreshWorkBuddyCredential(params: {
  credential: string
  proxy?: string
}): Promise<WorkBuddyCredentialResponse> {
  const res = await api.post(
    '/api/channel/oauth/workbuddy/refresh-token',
    params,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// OpenCode OAuth (device authorization)
// ============================================================================

export type OpenCodeStartLoginResponse = {
  success: boolean
  message?: string
  data?: {
    session_id: string
    auth_url: string
    user_code: string
    expires_in: number
    interval: number
    mode: 'zen' | 'go'
  }
}

export type OpenCodeLoginStatusResponse = {
  success: boolean
  message?: string
  data?: {
    status: 'pending' | 'slow_down' | 'ready' | 'expired' | 'denied'
    interval?: number
    credential?: string
  }
}

export type OpenCodeCredentialResponse = {
  success: boolean
  message?: string
  data?: {
    credential: string
  }
}

export type OpenCodeUsageWindow = {
  status?: string
  percent: number
  resets_at?: string
}

export type OpenCodeUsageEntry = {
  account_id?: string
  email?: string
  org_id?: string
  org_name?: string
  rolling: OpenCodeUsageWindow
  weekly: OpenCodeUsageWindow
  monthly: OpenCodeUsageWindow
}

export type OpenCodeUsageResponse = {
  success: boolean
  message?: string
  data?: {
    mode: 'zen' | 'go'
    entries: OpenCodeUsageEntry[]
  }
}

export async function startOpenCodeLogin(params: {
  mode?: 'zen' | 'go'
  server?: string
  proxy?: string
}): Promise<OpenCodeStartLoginResponse> {
  const res = await api.post(
    '/api/channel/oauth/opencode/auth-url',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function pollOpenCodeLogin(params: {
  session_id: string
  proxy?: string
}): Promise<OpenCodeLoginStatusResponse> {
  const res = await api.post(
    '/api/channel/oauth/opencode/poll-token',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function refreshOpenCodeChannelCredential(
  channelId: number
): Promise<OpenCodeCredentialResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/opencode/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

/**
 * Renews one credential before it is saved as a channel key. The returned
 * compact JSON already carries every field the key needs.
 */
export async function refreshOpenCodeCredential(params: {
  credential: string
  proxy?: string
}): Promise<OpenCodeCredentialResponse> {
  const res = await api.post(
    '/api/channel/oauth/opencode/refresh-token',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function getOpenCodeChannelUsage(
  channelId: number
): Promise<OpenCodeUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/opencode/usage`,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Command Code (Studio API key handoff)
// ============================================================================

export type CommandCodeLoginStartResponse = {
  success: boolean
  message?: string
  data?: {
    session_id: string
    state: string
    auth_url: string
    callback_url: string
    expires_in: number
  }
}

export type CommandCodeLoginStatusResponse = {
  success: boolean
  message?: string
  data?: {
    status: 'pending' | 'ready' | 'expired' | 'denied' | 'error'
    credential?: string
  }
}

export type CommandCodeCredentialResponse = {
  success: boolean
  message?: string
  data?: {
    credential: string
    user_id?: string
    user_name?: string
    key_name?: string
    org_id?: string
    org_name?: string
  }
}

export type CommandCodeUsageWindow = {
  used: number
  cap: number
  percent: number
  resets_at?: string
}

export type CommandCodeUsageEntry = {
  user_id?: string
  user_name?: string
  org_id?: string
  org_name?: string
  plan_id?: string
  plan_name?: string
  monthly_credits: number
  purchased_credits: number
  free_credits: number
  monthly_used: number
  monthly_cap: number
  five_hour?: CommandCodeUsageWindow
  weekly?: CommandCodeUsageWindow
  period_start?: string
  period_end?: string
}

export type CommandCodeUsageResponse = {
  success: boolean
  message?: string
  data?: {
    entries: CommandCodeUsageEntry[]
  }
}

export async function startCommandCodeLogin(params: {
  callback_url: string
  proxy?: string
}): Promise<CommandCodeLoginStartResponse> {
  const res = await api.post(
    '/api/channel/oauth/command-code/auth-url',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function pollCommandCodeLogin(params: {
  session_id: string
  proxy?: string
}): Promise<CommandCodeLoginStatusResponse> {
  const res = await api.post(
    '/api/channel/oauth/command-code/poll-token',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function validateCommandCodeCredential(params: {
  credential: string
  proxy?: string
}): Promise<CommandCodeCredentialResponse> {
  const res = await api.post(
    '/api/channel/oauth/command-code/validate',
    params,
    channelActionConfig()
  )
  return res.data
}

export async function validateCommandCodeChannelCredential(
  channelId: number
): Promise<CommandCodeCredentialResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/command-code/validate`,
    {},
    channelActionConfig()
  )
  return res.data
}

export async function getCommandCodeChannelUsage(
  channelId: number
): Promise<CommandCodeUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/command-code/usage`,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Multi-Key Management
// ============================================================================

/**
 * Manage multi-key channel operations
 */
export async function manageMultiKeys(
  params: MultiKeyManageParams
): Promise<MultiKeyStatusResponse | { success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/multi_key/manage',
    params,
    channelActionConfig()
  )
  return res.data
}

/**
 * Get key status for multi-key channel
 */
export async function getMultiKeyStatus(
  channelId: number,
  page = 1,
  pageSize = 50,
  status?: number
): Promise<MultiKeyStatusResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'get_key_status',
    page,
    page_size: pageSize,
    status,
  }) as Promise<MultiKeyStatusResponse>
}

/**
 * Enable a specific key in multi-key channel
 */
export async function enableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable a specific key in multi-key channel
 */
export async function disableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete a specific key in multi-key channel
 */
export async function deleteMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Enable all keys in multi-key channel
 */
export async function enableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable all keys in multi-key channel
 */
export async function disableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete all disabled keys in multi-key channel
 */
export async function deleteDisabledMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_disabled_keys',
  }) as Promise<{ success: boolean; message?: string; data?: number }>
}

// ============================================================================
// Tag Operations
// ============================================================================

/**
 * Enable all channels with a specific tag
 */
export async function enableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/enabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Disable all channels with a specific tag
 */
export async function disableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/disabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Edit all channels with a specific tag
 */
export async function editTagChannels(
  params: TagOperationParams
): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/channel/tag', params, channelActionConfig())
  return res.data
}

/**
 * Get models for a specific tag
 */
export async function getTagModels(
  tag: string
): Promise<{ success: boolean; message?: string; data?: string }> {
  const res = await api.get('/api/channel/tag/models', { params: { tag } })
  return res.data
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Fetch models from the current unsaved channel form configuration.
 */
export async function fetchModels(data: {
  base_url: string
  type: number
  key?: string
  channel_id?: number
  advanced_custom?: string
  header_override?: string
  proxy?: string
}): Promise<FetchModelsResponse> {
  const res = await api.post(
    '/api/channel/fetch_models',
    data,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete an Ollama model from a channel
 */
export async function deleteOllamaModel(params: {
  channel_id: number
  model_name: string
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(
    '/api/channel/ollama/delete',
    channelActionConfig({ data: params })
  )
  return res.data
}

/**
 * Test all enabled channels
 */
export async function testAllChannels(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get('/api/channel/test', channelActionConfig())
  return res.data
}

/**
 * Update balance for all enabled channels
 */
export async function updateAllChannelsBalance(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get(
    '/api/channel/update_balance',
    channelActionConfig()
  )
  return res.data
}

/**
 * Get all available models
 */
export async function getAllModels(): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: string; [key: string]: unknown }>
}> {
  const res = await api.get('/api/channel/models')
  return res.data
}

/**
 * Get all enabled models
 */
export async function getEnabledModels(): Promise<{
  success: boolean
  message?: string
  data?: string[]
}> {
  const res = await api.get('/api/channel/models_enabled')
  return res.data
}

// ============================================================================
// Group Management
// ============================================================================

/**
 * Get all available groups (re-exported from users API for convenience)
 */
export const getGroups = getUserGroups

// ============================================================================
// Prefill Groups (Model Groups)
// ============================================================================

/**
 * Get prefill groups for quick model selection
 */
export async function getPrefillGroups(
  type: 'model' | 'group' = 'model'
): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: number; name: string; items: string | string[] }>
}> {
  const res = await api.get('/api/prefill_group', { params: { type } })
  return res.data
}

// ============================================================================
// WorkBuddy tasks and welfare
// ============================================================================

/**
 * List every WorkBuddy account with its task state. The rows are derived from
 * the channels, so an imported channel shows up without any registration step.
 */
export async function getWorkBuddyUsage(
  params: {
    channelIds?: number[]
    models?: string[]
    window?: string
  } = {}
): Promise<{
  success: boolean
  message?: string
  data?: WorkBuddyUsageResponse
}> {
  const query: Record<string, string> = {}
  if (params.channelIds?.length) query.channel_ids = params.channelIds.join(',')
  if (params.models?.length) query.models = params.models.join(',')
  if (params.window) query.window = params.window
  const res = await api.get('/api/channel/workbuddy/usage', {
    params: Object.keys(query).length ? query : undefined,
  })
  return res.data
}

/**
 * Daily rollup written by the usage fold. It is stored history, so it survives
 * log retention and answers what a window did on each past day.
 */
export async function getWorkBuddyUsageHistory(
  params: { channelIds?: number[]; days?: number } = {}
): Promise<{
  success: boolean
  message?: string
  data?: { days: number; rows: WorkBuddyUsageDailyRow[] }
}> {
  const query: Record<string, string> = {}
  if (params.channelIds?.length) query.channel_ids = params.channelIds.join(',')
  if (params.days) query.days = String(params.days)
  const res = await api.get('/api/channel/workbuddy/usage/history', {
    params: Object.keys(query).length ? query : undefined,
  })
  return res.data
}

export async function getWorkBuddyAccounts(
  params: { channelIds?: number[]; realm?: string } = {}
): Promise<{
  success: boolean
  message?: string
  data?: WorkBuddyAccountsResponse
}> {
  const query: Record<string, string> = {}
  if (params.channelIds?.length) query.channel_ids = params.channelIds.join(',')
  if (params.realm && params.realm !== 'all') query.realm = params.realm
  const res = await api.get('/api/channel/workbuddy/accounts', {
    params: Object.keys(query).length ? query : undefined,
  })
  return res.data
}

export async function setWorkBuddyAccountTasks(request: {
  channelId: number
  accountUid: string
  enabled: boolean
}): Promise<{
  success: boolean
  message?: string
  data?: { account: WorkBuddyAccountView }
}> {
  const res = await api.post('/api/channel/workbuddy/accounts/tasks', {
    channel_id: request.channelId,
    account_uid: request.accountUid,
    enabled: request.enabled,
  })
  return res.data
}

/**
 * Run the selected actions. A single account runs inline; anything wider is
 * queued as a system task and reported with its task id.
 */
export async function runWorkBuddyTasks(request: {
  channelIds?: number[]
  accountUid?: string
  realm?: string
  actions?: string[]
}): Promise<{
  success: boolean
  message?: string
  data?: WorkBuddyRunResponse
}> {
  const res = await api.post('/api/channel/workbuddy/run', {
    channel_ids: request.channelIds ?? [],
    account_uid: request.accountUid ?? '',
    realm: request.realm ?? '',
    actions: request.actions ?? [],
  })
  return res.data
}

/** Read the run history, newest first. */
export async function getWorkBuddyTaskRuns(
  params: { channelId?: number; accountUid?: string; limit?: number } = {}
): Promise<{
  success: boolean
  message?: string
  data?: WorkBuddyTaskRunsResponse
}> {
  const res = await api.get('/api/channel/workbuddy/runs', {
    params: {
      channel_id: params.channelId,
      account_uid: params.accountUid,
      limit: params.limit,
    },
  })
  return res.data
}

/** Sign every account of one channel in right now. */
export async function checkinWorkBuddyChannel(channelId: number): Promise<{
  success: boolean
  message?: string
  data?: WorkBuddyTaskSummary
}> {
  const res = await api.post(`/api/channel/${channelId}/workbuddy/checkin`)
  return res.data
}

export type AntigravityQuotaHistoryPoint = {
  at: number
  available: number
  average: number
}

export type AntigravityQuotaHistorySeries = {
  model: string
  points: AntigravityQuotaHistoryPoint[]
}

export type AntigravityQuotaHistoryResponse = {
  success: boolean
  message?: string
  data?: {
    models: AntigravityQuotaHistorySeries[]
    hours: number
    generated_at: number
  }
}

export async function getAntigravityQuotaHistory(
  hours = 24
): Promise<AntigravityQuotaHistoryResponse> {
  const res = await api.get(
    `/api/channel/antigravity/quotas/history?hours=${hours}`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}
export type ChannelAccountQuotaRow = {
  channel_id: number
  channel_name: string
  channel_type: number
  remain: number
  total: number
  reset_time: number
  status: string
  error?: string
  fetched_at: number
}

export type ChannelAccountQuotaResponse = {
  success: boolean
  message?: string
  data?: {
    accounts: ChannelAccountQuotaRow[]
    generated_at: number
  }
}

export async function getChannelAccountQuotas(
  channelType = 0
): Promise<ChannelAccountQuotaResponse> {
  const url =
    channelType > 0
      ? `/api/channel/account-quotas?type=${channelType}`
      : '/api/channel/account-quotas'
  const res = await api.get(
    url,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}
