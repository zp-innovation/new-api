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
import { z } from 'zod'

// ============================================================================
// Channel Schema & Types
// ============================================================================

export const channelInfoSchema = z.object({
  is_multi_key: z.boolean().default(false),
  multi_key_size: z.number().default(0),
  multi_key_status_list: z.record(z.string(), z.number()).optional(),
  multi_key_disabled_reason: z.record(z.string(), z.string()).optional(),
  multi_key_disabled_time: z.record(z.string(), z.number()).optional(),
  multi_key_polling_index: z.number().default(0),
  multi_key_mode: z.enum(['random', 'polling']).default('random'),
})

export const channelSchema = z.object({
  id: z.number(),
  type: z.number(),
  key: z.string(),
  openai_organization: z.string().nullish(),
  test_model: z.string().nullish(),
  status: z.number(), // 1: enabled, 0: manual disabled, 2: auto disabled
  name: z.string(),
  weight: z.number().nullish(),
  created_time: z.number(),
  test_time: z.number(),
  response_time: z.number(), // in milliseconds
  base_url: z.string().nullish(),
  other: z.string().default(''),
  balance: z.number().default(0), // in USD
  balance_updated_time: z.number(),
  models: z.string().default(''),
  group: z.string().default('default'),
  used_quota: z.number().default(0),
  model_mapping: z.string().nullish(),
  status_code_mapping: z.string().nullish(),
  priority: z.number().nullish(),
  auto_ban: z.number().nullish(),
  other_info: z.string().default(''),
  tag: z.string().nullish(),
  setting: z.string().nullish(),
  param_override: z.string().nullish(),
  header_override: z.string().nullish(),
  remark: z.string().default(''),
  max_input_tokens: z.number().default(0),
  channel_info: channelInfoSchema.default({
    is_multi_key: false,
    multi_key_size: 0,
    multi_key_polling_index: 0,
    multi_key_mode: 'random',
  }),
  settings: z.string().default('{}'), // other_settings JSON
})

export type Channel = z.infer<typeof channelSchema>

// ============================================================================
// Channel Settings Types
// ============================================================================

export interface ChannelSettings {
  task_plugin_key?: string
  task_extend_plugin_keys?: string[]
  force_format?: boolean
  thinking_to_content?: boolean
  proxy?: string
  pass_through_body_enabled?: boolean
  responses_websocket_enabled?: boolean
  workbuddy_default_thinking?: boolean
  system_prompt?: string
  system_prompt_override?: boolean
  http_protocol?: 'auto' | 'http1' | string
  http2_connection_shards?: number
  tls_insecure_skip_verify?: boolean
  max_input_tokens?: number
}

export interface ChannelOtherSettings {
  azure_responses_version?: string
  vertex_key_type?: 'json' | 'api_key'
  vertex_project_id?: string
  openrouter_enterprise?: boolean
  aws_key_type?: 'ak_sk' | 'api_key'
  aws_api_type?: 'invoke_model' | 'mantle'
  allow_service_tier?: boolean
  disable_store?: boolean
  allow_safety_identifier?: boolean
  allow_include_obfuscation?: boolean
  allow_inference_geo?: boolean
  allow_speed?: boolean
  claude_beta_query?: boolean
  ollama_openai_chat?: boolean
  disable_task_polling_sleep?: boolean
  upstream_model_update_check_enabled?: boolean
  upstream_model_update_auto_sync_enabled?: boolean
  upstream_model_update_ignored_models?: string[]
  upstream_model_update_last_check_time?: number
  upstream_model_update_last_detected_models?: string[]
  advanced_custom?: AdvancedCustomConfig
}

export interface AdvancedCustomConfig {
  advanced_routes?: AdvancedCustomRoute[]
}

export interface AdvancedCustomRoute {
  incoming_path?: string
  upstream_path?: string
  converter?: AdvancedCustomConverter
  models?: string[]
  auth?: AdvancedCustomRouteAuth
  pass_through_body_enabled?: boolean
}

export interface AdvancedCustomRouteAuth {
  type?: AdvancedCustomAuthType
  name?: string
  value?: string
}

export type AdvancedCustomConverter =
  | 'none'
  | 'anthropic_messages_to_openai_chat_completions'
  | 'openai_chat_completions_to_anthropic_messages'
  | 'openai_chat_completions_to_openai_responses'
  | 'openai_responses_to_openai_chat_completions'
  | 'openai_responses_to_gemini_generate_content'
  | 'gemini_generate_content_to_openai_chat_completions'
  | 'openai_chat_completions_to_gemini_generate_content'
  | 'claude_messages_to_openai_responses'
  | 'gemini_generate_content_to_openai_responses'

export type AdvancedCustomAuthType = 'none' | 'header' | 'query'

// ============================================================================
// API Response Types
// ============================================================================

export interface GetChannelsResponse {
  success: boolean
  message?: string
  data?: {
    items: Channel[]
    total: number
    page: number
    page_size: number
    type_counts?: Record<string, number>
  }
}

export interface SearchChannelsResponse {
  success: boolean
  message?: string
  data?: {
    items: Channel[]
    total: number
    type_counts?: Record<string, number>
  }
}

export interface GetChannelResponse {
  success: boolean
  message?: string
  data?: Channel
}

export interface ChannelOpsResponse {
  success: boolean
  message?: string
  data?: {
    retry_times: number
    request_policy?: {
      automatic_disable: boolean
      source: string
    }
  }
}

export interface ChannelTestResponse {
  success: boolean
  message?: string
  error_code?: string
  time?: number
  data?: {
    response_time?: number
    error?: string
  }
}

export interface ChannelBalanceResponse {
  success: boolean
  message?: string
  balance?: number
  currency?: string
  raw_response?: string
}

export interface FetchModelsResponse {
  success: boolean
  message?: string
  data?: string[]
}

export interface CopyChannelResponse {
  success: boolean
  message?: string
  data?: {
    id: number
  }
}

// ============================================================================
// Multi-Key Management Types
// ============================================================================

export interface KeyStatus {
  index: number
  status: number // 1: enabled, 2: manual disabled, 3: auto disabled
  disabled_time?: number
  reason?: string
  key_preview?: string
}

export type MultiKeyConfirmAction = {
  type:
    | 'enable'
    | 'disable'
    | 'delete'
    | 'enable-all'
    | 'disable-all'
    | 'delete-disabled'
  keyIndex?: number
}

export interface MultiKeyStatusResponse {
  success: boolean
  message?: string
  data?: {
    keys: KeyStatus[]
    total: number
    page: number
    page_size: number
    total_pages: number
    enabled_count: number
    manual_disabled_count: number
    auto_disabled_count: number
  }
}

// ============================================================================
// API Request Parameters
// ============================================================================

export type ChannelSortBy =
  | 'id'
  | 'name'
  | 'priority'
  | 'balance'
  | 'response_time'
  | 'test_time'

export type ChannelSortOrder = 'asc' | 'desc'

export interface GetChannelsParams {
  p?: number
  page_size?: number
  status?: string // 'enabled', 'disabled', or empty for all
  type?: number
  group?: string
  id_sort?: boolean
  tag_mode?: boolean
  sort_by?: ChannelSortBy
  sort_order?: ChannelSortOrder
}

export interface SearchChannelsParams {
  keyword?: string
  group?: string
  model?: string
  status?: string
  type?: number
  id_sort?: boolean
  tag_mode?: boolean
  sort_by?: ChannelSortBy
  sort_order?: ChannelSortOrder
  p?: number
  page_size?: number
}

export interface CopyChannelParams {
  suffix?: string
  reset_balance?: boolean
}

export interface MultiKeyManageParams {
  channel_id: number
  action:
    | 'get_key_status'
    | 'disable_key'
    | 'enable_key'
    | 'enable_all_keys'
    | 'disable_all_keys'
    | 'delete_key'
    | 'delete_disabled_keys'
  key_index?: number
  page?: number
  page_size?: number
  status?: number // 1=enabled, 2=manual_disabled, 3=auto_disabled
}

export interface BatchDeleteParams {
  ids: number[]
}

export interface BatchSetTagParams {
  ids: number[]
  tag: string | null
}

export interface TagOperationParams {
  tag: string
  new_tag?: string
  priority?: number
  weight?: number
  model_mapping?: string
  models?: string
  groups?: string
}

// ============================================================================
// Add Channel Request (special structure)
// ============================================================================

export interface AddChannelRequest {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
}

// WorkBuddy task and welfare console types. The account rows are derived from
// the WorkBuddy channels, so a freshly imported channel appears without any
// registration step.
export type WorkBuddyAccountView = {
  channel_id: number
  channel_name: string
  channel_status: number
  account_uid: string
  nickname: string
  realm: string
  product: string
  tasks_enabled: boolean
  checked_in_today: boolean
  daily_chat_today: boolean
  activity_report_today: boolean
  last_checkin_at: number
  last_daily_chat_at: number
  last_activity_report_at: number
  last_run_at: number
  last_task: string
  last_status: string
  last_message: string
  streak_days: number
  daily_credit: number
  today_credit: number
  remain_credit: number
}

export type WorkBuddyTaskSettings = {
  enabled: boolean
  auto_checkin: boolean
  auto_activity_report: boolean
  auto_travel: boolean
  auto_streak_bonus: boolean
  auto_growth_tasks: boolean
  auto_night_cat: boolean
  auto_daily_chat: boolean
  daily_chat_web: boolean
  schedule_hours_intl: string
  daily_request_limit_intl: number
  schedule_hours_cn: string
  daily_request_limit_cn: number
  growth_request_limit: number
  night_hour: number
  account_gap_seconds: number
  run_retention_days: number
}

export type WorkBuddyRealmSummary = {
  realm: string
  accounts: number
  done_today: number
  streak_average: number
  remain_credit: number
  failed_last_run: number
  last_run_at: number
}

export type WorkBuddyModelUsage = {
  channel_id: number
  account_uid: string
  account_name: string
  realm: string
  model: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  last_used_at: number
  window_start: number
  window_reset_at: number
  budget_tokens: number
  remaining_tokens: number
  budget_source: 'cap' | 'seed' | 'passive' | 'unknown' | string
  remaining_ratio: number
  cooling: boolean
}

export type WorkBuddyUsageSummary = {
  realm: string
  accounts: number
  requests: number
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cooling_accounts: number
  near_limit_accounts: number
  unknown_budget: number
}

export type WorkBuddyUsageResponse = {
  window: string
  since: number
  rows: WorkBuddyModelUsage[]
  summaries: Record<string, WorkBuddyUsageSummary>
}

export type WorkBuddyUsageDailyRow = {
  id: number
  channel_id: number
  account_uid: string
  model: string
  day: string
  realm: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  updated_at: number
}

export type WorkBuddyAccountsResponse = {
  accounts: WorkBuddyAccountView[]
  settings: WorkBuddyTaskSettings
  realm: string
  summaries: Record<string, WorkBuddyRealmSummary>
}

export type WorkBuddyTaskRun = {
  id: number
  channel_id: number
  account_uid: string
  task: string
  trigger: string
  ok: boolean
  message: string
  credit_delta: number
  created_at: number
}

export type WorkBuddyTaskRunsResponse = {
  runs: WorkBuddyTaskRun[]
  total: number
}

type WorkBuddyAccountResult = {
  channel_id: number
  account_uid: string
  nickname: string
  realm: string
  action: string
  ok: boolean
  skipped: boolean
  message: string
  credit: number
}

export type WorkBuddyTaskSummary = {
  trigger: string
  accounts: number
  actions: number
  credit_delta: number
  results: WorkBuddyAccountResult[]
  started_at: number
  finished_at: number
}

export type WorkBuddyRunResponse = {
  queued: boolean
  created?: boolean
  task_id?: string
  summary?: WorkBuddyTaskSummary
}
