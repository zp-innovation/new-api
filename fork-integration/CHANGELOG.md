# Antigravity 集成 — 变更记录

记录每次**上游升级后**对本集成所做的适配动作，以及集成自身的里程碑。
配套文件：`UPSTREAM_PATCHES.md`（补丁锚点清单）、`upgrade-check.ps1`（升级自检脚本）。

---

## 2026-10-26 — 收尾批次 D（P3 清理与上游死代码收敛）

### 范围

**relay**

- `relay/channel/opencode`（D1）：会话 id 只取 `metadata` 里的 `session_id` / `user_id` 标量字符串，`rawString` 对 object / array 直接返回空。此前对象形态的 `metadata` 会把整段 JSON 塞进 `X-OpenCode-Session` 头。9 处客户端错误前缀改用 `ChannelName` 常量（值仍是 `opencode`，它是模型列表的 owner 名）。
- `relay/channel/commandcode`（D2）：`ChannelName` 由 `"command_code"` 改为 `"Command Code"`，与渠道类型展示名一致，9 处客户端错误前缀改用该常量。`pkg/commandcode` 的 `CredentialType` 仍为 `command_code`（凭据契约不变）；`/v1/models` 的 `owned_by` 随之变为展示名。

**controller / middleware / service**

- `controller/channel_upstream_update.go`（D3）：上游模型目录拉取加 20s 超时（`channelModelFetchTimeout`），四处 `Get*ChannelModels` 由 `context.Background()` 改传带超时的 ctx，上游卡死不再拖住管理端请求。
- `controller/workbuddy_usage.go`（D4）：四个失败分支改为 `common.SysError(common.LogText(...))` + `common.ApiErrorT`，复用既有键 `Invalid parameters` / `Failed to fetch usage`，内部错误文案不再直出客户端。
- `controller/channel.go`（D5）：Gemini Web 建渠道的凭据校验拆成 `len == 0`（缺凭据）与 `len > 1`（不支持批量）两条分支，不再把重复凭据报成缺凭据。
- `middleware/cache.go`（D14b）：新增 `isCacheRevalidationRequest`，`Cache-Control` 里任一条目为 `no-cache` / `max-age=0`（逗号切分、大小写不敏感）或 `Pragma: no-cache` 都触发重新校验。此前只认精确的 `no-cache`，浏览器普通刷新的 `max-age=0` 会让页面继续用缓存里的旧 chunk。
- `service/workbuddy_task_actions.go`、`workbuddy_usage.go`、`workbuddy_task_scheduler.go`、`workbuddy_tasks.go`（D14a）：7 处 fmt 形态 `LogWarn` 改走 `common.LogText`；复核后 `service/` 的 82 处 `LogWarn` 已全部走 `LogText`。

**pkg**

- `pkg/workbuddy/activity.go`、`growth.go`（D11）：活动打卡报文的模型 id / 名称提为 `activityReportModelID` / `activityReportModelName` 常量，注释与测试都写明它与 `dailyChatModel` 刻意不同（前者镜像桌面端报文，后者是国际每日活动真正发送的模型）。
- `pkg/workbuddy/activity.go`（D12）：`asAPIError` 改用 `errors.As`，`fmt.Errorf("%w")` 包装后仍能取出 `*APIError`（此前裸断言会漏判，`isHTTPStatus` 随之失效）。
- `pkg/workbuddy/quota.go`（D13）：12 个容量字段改 `*numberValue`，`firstValue` 取第一个非 nil 拼写 —— 存在但为 0 的容量不再被当成缺失而回落到下一个字段；`PackageEndTimeRangeEnd` 由固定的 `2036-01-01` 改为 `now + 10 年`，不再随年份推进失效。

**前端**

- D6：`channels-columns.tsx` 的 `row as any` 去掉，`DataTableTagRowActions` 收真实的 `Channel['tag']` 类型。
- D7：三个面板手写的剪贴板实现收敛到既有的 `useCopyToClipboard`；失败文案由误导的 `Unable to read clipboard` 改为既有的 `Failed to copy to clipboard`，并顺带获得非 HTTPS 环境下的 `execCommand` 回退。
- D8：`channel-schedule/rules-view.tsx` 的草稿规则 id 改在 `openCreate` 里生成，渲染期不再读 `Date.now()`（同一规则重复渲染会拿到不同 id）；补回归测试。
- D9：`channel-schedule` 与 `general/channel-affinity` 的规则编辑器**判定不合并** —— 规则模型（`weekdays/start/end/actions` vs `model_regex/key_sources/ttl_seconds/session_mode`）、校验方式（zodResolver + useFieldArray vs useState + 手写 JSON 校验）都不同，共享面不足 20%。
- D10：新增 `OAuthRefreshButton`（4 个面板接入，`opencode` 传 `showRefreshingLabel={false}` 保持原视觉）与 `hooks/use-authorization-code-input.ts`（antigravity / codex 共用）；`workbuddy-tasks-panel` 的格式化函数与 `@/lib/format` 在 0 值与小数位上不等价，保留原实现。
- 上游死代码收敛：删掉本 PR 未删除任何引用、但全仓零调用的导出 —— 38 个导出 + `ChannelAdvancedSection` 整个组件 + `constants.ts` 7 项 + `lib/` 层 helper 与 3 个类型（14 文件，−905 行）；随之孤立的 1 个语言键从 7 个语言包删除（7776 → 7775）。上游代码仍在使用的导出未动。

**计费**

- `setting/billing_setting/builtin_billing.go`（D15）：模型族提为常量（`claudeOpus55Model` / `claudeSonnet55Model` / `gemini4ArgonModel`，变体键由基名拼接）；Gemini 4 Argon 的促销窗口从「月」粒度改为「日」粒度（`month < 11 || (month == 11 && day <= 30)`），月中截止日因此可表达，计费表达式与价格不变。

**尾项（2026-10-27）**

- 4 个用量对话框（antigravity / workbuddy / opencode / command-code）的「Account Info」卡外壳、标题、可选的刷新按钮以及各自一份的 `InfoField` 收敛到 `dialogs/account-info-card.tsx`（`AccountInfoCard` + `InfoField`）。各渠道的徽标与字段仍留在各自文件里，可见样式不变；4 个对话框合计减少约 4.5 KB 重复代码。
- `general/channel-affinity/constants.ts` 的 `makeUniqueName` 去掉 `Date.now()` 兜底，改为继续递增后缀：纯函数不再依赖时钟，也不会在同一毫秒内生成同一个名字（此前 1000 次以内没空位才会走到该分支）。
- `UPSTREAM_PATCHES.md` 的调度判定示例更新为当前实现（`channelModelCooldownNames` 名单 + `ChannelModelQuotaExhausted`），并补记 `ChannelModelUnavailableUntil` 共用名单的修正原因。

### 验证

```text
go build ./... / go vet ./...                        → exit 0
gofmt -l（改动/新增 19 个 .go）                        → 无输出
go test ./pkg/... ./model/ ./service/ ./router/ ./middleware/ ./setting/... ./common/... ./i18n/... → 全部 ok
go test ./relay/...                                   → 仅 relay/channel 根包 2 个 HTTP2 用例（Windows localhost，基线同现象；单跑 -run TestUpstreamGetBody_HTTP2 通过）
web: tsgo -b → 0；oxlint 25 个改动/新增文件 → 0 errors（仅 1 个 HEAD 既有的 prefer-structured-clone warning）
web: vitest run → 222 文件 / 2598 用例全部通过；bun run build → exit 0
web: 入口 index.1d009a836a.js = 1,084,414 B raw / 277,919 B gzip（预算 ≤300 KB）
web: routeTree.gen.ts 构建前后 SHA256 一致（433EBB71…C211A）
i18n: 7 语言各 7775 键，missing / extra / empty 0/0/0，键序一致
负向对照：撤 D12/D13/`max-age=0` 修复后对应测试分别失败，恢复后通过
```

---

## 2026-10-26 — 系统任务调度：空闲期不再建空行

### 范围

- `service/system_task.go`：新增可选接口 `PendingSystemTaskHandler`（`HasPendingWork() bool`）。`runSystemTaskScheduler` 在「已启用 + 已到期」判定之后、`CreateSystemTask` 之前询问它；报告无活可做就跳过本轮到下一轮，不再每个 interval 记一条空跑行。未实现该接口的 handler 行为完全不变，`Enabled()` 与 `Interval()` 也没动，所以到期节奏不变。
- 四个同步 handler 实现 `HasPendingWork`：`antigravityQuotaSyncHandler`（存在启用的单 key Antigravity 渠道 —— pass 本身跳过 multi-key 渠道）、`channelAccountQuotaSyncHandler`（存在启用的 WorkBuddy/OpenCode/CommandCode 渠道）、`workBuddyUsageSyncHandler`（存在启用的 WorkBuddy 渠道）、`workBuddyTaskHandler`（同上，其 `Enabled()` 已按配置小时判定）。判定查询失败时 fail-open：照常建行，由 `Run` 记录真实错误，避免调度被静默停摆。
- `model/channel.go`：新增 `ListEnabledChannelsOfTypes`，只取 `id` / `type` / `channel_info` 三列，供上面的判定使用。
- 对照：`controller/system_task_handlers.go` 的 Midjourney / 异步任务轮询本就把「有未完成任务」折进 `Enabled()`，本次不涉及。

### 验证

```text
go build ./... / go vet ./...                        → exit 0
gofmt -l（改动 .go）                                  → 无输出
go test ./model/ ./service/ ./router/ ./middleware/   → 全部 ok
新增测试：TestSystemTaskSchedulerSkipsHandlersWithoutPendingWork、
         TestScheduledSyncHandlersOnlyReportPendingWorkForEnabledChannels
负向对照：移除调度器里的 gate → 上述调度测试失败（expected 0, actual 1）
```

---

## 2026-10-26 — 收尾批次 C2（可确认的 8 条）

### 范围

**relay**

- `relay/channel/geminiweb`：账号结果写库加节流（C1）。`accountResultAlreadyRecorded` 只在「成功 + 无失败状态 + last_ok_at 在 10 分钟心跳窗口内」时跳过写库（常量 `geminiWebAccountHeartbeatSeconds`），失败路径与状态变化一律写；`last_ok_at` 因此最多滞后 10 分钟而不是永久陈旧。同时修掉 tool 路径失败被记两次（`toolCallResponse` 与账号池各写一次 401，`fail_count` 单次 +2 会提前禁用账号），并给 buffered 成功路径补上记录（此前成功从不清 `fail_count`）。
- 新增 `relay/channel/protocolshim`（C2）：`BuildURL` / `RelayFormatForProtocol` / `OpenAIDelegate` / `ClaudeDelegate` 收拢 `relay/channel/opencode` 与 `relay/channel/commandcode` 里逐行同构的两份实现（协议标识按字符串传入，两包互不 import）。`inboundProtocol` 留在 commandcode（opencode 无对应物，抽走会成单调用方 helper）。

**pkg / relaykit**

- `pkg/workbuddy/identity.go`（C12）：删除生产从不赋值的 `TenantID` / `Department` / `DeviceToken` 字段与对应 `X-Department-Info`、`X-Device-Token` 分支；`X-Tenant-Id` 仍由 `EnterpriseID` 发出（与原「TenantID 为空回落 EnterpriseID」等价）。
- `relaykit/relayconvert/internal/shared/schemacompat`（C14①）：`mergeBranches` 不再把任何 `anyOf`/`oneOf`/`allOf` 的约束丢光 —— 保留各分支同值关键字（含一致的 `required`），合并分支 `type` 与 `enum`/`const` 为并集；文档写明仍是 widening（仅部分分支携带的约束无法在去掉组合子后表达，但结果对原 union 始终是超集）。
- `relaykit/relayconvert/internal/shared/claude`（C14②）：`normalizeNullableUnions` 保持默认生效，补作用范围注释与单测；不加渠道开关（上游是否接受 type 数组无法在本机验证，改默认是拿可用换未验证风险）。

**model / service**

- `model/channel_model_cooldown.go`（C17）：新增 `channelModelCooldownNames`，可用性判定与 `ChannelModelUnavailableUntil` 共用同一组名字（forwarded id + origin）。此前 deadline 只查 origin，别名模型被 429 停放后查不到恢复时间，429 + Retry-After 退化成裸 503。
- `model/system_task.go` + `service/system_task.go`（C22）：新增 `DeleteFinishedSystemTasksBefore`，只删终态行且保留每个 type 的最新行（调度器 last-run 判据）；service 侧 7 天保留期、1 小时节流，master 节点执行。此前 `system_tasks` 只增不减（outbox 一天约 5760 行）。

**前端**

- 新增 `web/src/features/channels/components/oauth-panel-parts.tsx`（C27）：抽出五个 OAuth 面板行为完全一致的展示层（外壳、生成按钮、授权链接、兑换区、凭据声明、厂商选择），五面板 1777 → 1520 行。workbuddy 的 3s 轮询、command-code 的凭据网格结构有语义差异，保留原实现。
- C29：本次 PR 新增的零引用导出收敛到 0（6 处去 `export`，删死函数 `workBuddyCredentialLabel` 与其单测）；上游既有的 38 个零引用导出未动（本 PR 未删除它们的任何引用），清单见审核报告建议单独立项。

### 验证

```text
go build ./... / go vet ./...                        → exit 0
gofmt -l（全部改动/新增 .go）                         → 无输出
go test ./pkg/... ./model/ ./service/ ./router/ ./middleware/ ./setting/... ./common/... ./i18n/... → 全部 ok
go test ./relay/...                                  → 仅 relay/channel 根包两个既有 Windows HTTP2 用例偶发失败（单跑通过，基线同现象）
relaykit: GOWORK=off go build ./... / go test ./...   → exit 0 / 全部 ok（模块独立性保持）
web: tsgo -b 0 / oxlint 10 文件 0 warning 0 error
web: vitest src/features/channels 50 文件 488 用例 → 全过
web: bun run build 入口 index.497e040d12.js 1084.4 kB raw / 280.2 kB gzip（预算 ≤300 kB）
web: i18n 7 语言 7776 键 missing/extra/empty 0/0/0
pwsh fork-integration/upgrade-check.ps1 -SkipTests → exit 0（77 锚点）
```

---

## 2026-10-26 — 收尾批次 C（P2 快修 21 条）

### 范围

**relay / controller**

- `relay/channel/geminiweb`：删除三处 `var _ = …` 占位符，删掉自造的 `newByteReader` / `sliceReader`，改 `io.NopCloser(bytes.NewReader(payload))`（原实现同样零拷贝，`ContentLength` 不变）。
- `relay/channel/antigravity/adaptor_integration_test.go`、`relay/channel/workbuddy/usage_test.go`：删除残留占位符，`relay/` 下已无 `var _ = `。
- `controller/antigravity_oauth.go`：三处 bind 失败与授权码交换失败不再回显 `{{error}}` 原文，原文进 `common.SysError`，对外复用既有键 `Invalid request` / `Failed to exchange the Antigravity authorization code`；配额历史查询传 `limit=0`（走默认上限）。
- `controller/channel_schedule.go`：五处裸 `gin.H{"success":false}+400` 改为 `common.ApiErrorStatus` / `common.ApiError` + `common.ApiSuccess`，错误原文进服务日志。

**pkg**

- `pkg/commandcode/client.go`：响应体读 `MaxResponseBytes+1` 并显式报超限（`command_code: response exceeds the 4194304-byte limit`）；错误体改 512 rune 截断（原按字节截断会把中文切成非法 UTF-8）。
- `pkg/workbuddy/client.go`：`truncateBody` 改 rune 截断。
- `pkg/workbuddy/quota.go`：CN realm 的 credits 回落只在 404/501（`isMissingCreditsEndpoint`）时发生，其余错误直接返回，回落失败用 `errors.Join` 保留原始错误 —— 此前 5xx 也会静默回落到粗粒度套餐接口。
- `pkg/opencode/client.go`：删除静默回落 `DefaultServer` 的 `NewClient`（生产零调用，`NewClientWithError` 已覆盖该职责）；`pkg/opencode/credential.go`：切换组织时清空 `OrgName`（此前把新 org id 当显示名写进去）。
- 删除零引用符号：`antigravity.Client.Availability`、`antigravity.IsConnectionError`、`antigravity.IsPaidPlanType`、`workbuddy.ParseCredentialLine`、`workbuddy.PlanOnlyModels` / `IsPlanOnlyModel`。

**model**

- `model/option.go`：两段逐行重复的「锁内快照 → 事务写库」改为泛型 helper `buildOptionFamilySnapshot`，锁只在快照构建期间持有（原先 `defer Unlock` 落在事务里）。
- `model/ability.go` / `model/channel_constraint.go`：资格门槛复用 `WorkBuddyChannelHasUsableCredential`（对 nil 与非 WorkBuddy 渠道恒真，语义等价）。
- 删除零引用 `model/ability.go` 的 `DeleteAbilities`、`model/workbuddy_usage_query.go` 的 `WorkBuddyDailyUpsertAt`、`model/channel_model_cooldown.go` 的 `ModelQuotaExhaustedError.ChannelIDs`。
- `model/workbuddy_account_state.go`、`model/gemini_web_account.go`：存库消息改 rune 截断（utf8mb4 列拒收半字符）。
- `model/channel_model_quota_history.go`：`ListChannelModelQuotaHistory` 增加 `limit`（默认上限 50000 行），保留 oldest-first 契约。

**service**

- `service/workbuddy_task_scheduler.go`：删除零调用的 `StartWorkBuddyTaskNow`。
- `service/workbuddy_task_actions.go`：进度轮询的死上界 `range 6`（实际最多 3 次）改为 `range 3`，`time.Sleep` 改可取消的 `time.Timer` + `ctx`。
- `service/channel_schedule.go`：runner id 由秒级时间戳改为纳秒 + 8 位随机（同秒并发会互踩 `FinishSystemTask`）；租约 300s 常量 + 90s 心跳续租（`model.RenewSystemTaskLock` 的唯一调用点），`FinishSystemTask` 前必停。
- `service/channel_account_quota_sync.go`：轮询失败不再把上一次的 `Remain` / `Total` / `ResetTime` 冲成 0，只置 `status=error`。
- 新增 `service/oauth_http_client.go` 的 `oauthHTTPClient(base, timeout)`，五处「复制共享 proxy client 再改超时」收敛为一次调用（共享缓存 client 不被写坏）。

**前端**

- `codex-oauth-panel.tsx`：三个 `catch` 与剪贴板失败统一走 `handleServerError`（此前直接 `toast.error(String(error))`，绕过服务端 message_key 映射）。

**测试**

- 新增：`pkg/commandcode`（超限 / rune 截断）、`pkg/workbuddy`（rune 截断、credits 回落三条）、`pkg/opencode`（切组织、构造 fixture）、`model/message_truncation_test.go`、`model/channel_model_quota_test.go`（历史行上限）、`service/channel_account_quota_sync_test.go`（失败保留余量）、`service/opencode_oauth_test.go`（client 拷贝）。

### 验证

```text
go build ./... / go vet ./...                       → exit 0
gofmt -l（43 个改动/新增 .go）                        → 无输出
go test ./pkg/... ./model/ ./service/ ./router/ ./middleware/ ./setting/... ./common/... ./i18n/... → ok
go test ./relay/...                                 → 仅 relay/channel 根包两个既有 Windows HTTP2 用例偶发失败（基线同现象）
go test ./controller/ -run 'Antigravity|ChannelSchedule|WorkBuddy|OAuth|GeminiWeb' → 断言全过，失败仅 Windows SQLite TempDir 清理噪声
web: tsgo -b 0 / vitest src/features/channels 50 文件 489 用例 / oxlint 0 error
web: bun run build 入口 index.53bf3c1ad8.js 1084.4 kB raw / 280.2 kB gzip（预算 ≤300 kB）
web: i18n 7 语言 7776 键 missing/extra/empty 0/0/0
pwsh fork-integration/upgrade-check.ps1 -SkipTests → exit 0
```

---

## 2026-10-26 — 收尾批次 B（JSON 分层例外 / 治理规则登记）

### 范围

- `pkg/antigravity/json.go`（新增）：该包是协议核心，`architecture_test.go` 用 `go list -deps` 断言它不得依赖任何 `github.com/QuantumNous/new-api/*` 包，因此不能走 `common.*`。新增 `marshalJSON` / `unmarshalJSON` 两个薄包装，包内 13 处编解码（`client.go` 8 处、`envelope.go` 4 处、`credential.go` 1 处）全部收敛到这里，`encoding/json` 只在该文件出现；`credential.go` 随之去掉未使用的 import。协议 DTO 类型（`json.RawMessage`）与 `*_test.go` 保留标准库调用，用于独立校验线上报文形状。
- `fork-integration/UPSTREAM_PATCHES.md`：新增 P66（治理文件：`AGENTS.md` +2 行、`web/AGENTS.md` +23 行）与 P67（`pkg/antigravity` 的分层例外与 JSON 入口），写明冲突处理与约束。
- `fork-integration/upgrade-check.ps1`：新增 6 个锚点（P67、P66a–P66e），锚点总数 71 → 77。

### 验证

```text
go build ./... / go vet ./...                      → exit 0
gofmt -l pkg/antigravity                           → 无输出
go test ./pkg/antigravity/ -count=1                → ok（含 TestPackageHasNoHostImports）
pwsh fork-integration/upgrade-check.ps1 -SkipTests → exit 0（77 锚点 + 7 语言各 7776 键）
```

---

## 2026-10-26 — 收尾批次 A（截断流判定 / 预读超时 / 拒绝体脱敏 / 死端点 / 面板去重）

### 范围

**后端**

- `relay/helper/stream_scanner.go:381`：截断判定从「仅 OpenAI」推广到所有带终止事件的客户端协议。新增 `streamFormatExpectsTerminal`（OpenAI / Claude / Gemini / OpenAI Responses）；读失败仍对所有格式标 `incomplete`，干净 EOF 且缺 `[DONE]` 时上述四种格式标 `incomplete("upstream_stream_error")`，Embedding 一类无终止态协议保持原有 EOF 正常结束。终端错误事件仍只写给 OpenAI 客户端（`writeUpstreamStreamError`）。影响链：`service/request_policy.go:135` 的成功判定、`pkg/perf_metrics/outcome.go`、`service/log_info_generate.go`。
- `relay/channel/antigravity/stream_prebuffer.go`：预读探测加 30s 上限（`maxStreamPrebufferWait`），签名改为接收 `context.Context`，探测跑在 goroutine、主逻辑 `select` 结果 / `ctx.Done()` / 超时，`body.Close` 只由主 goroutine 执行；`adaptor.go` 只在 `info.IsStream` 时传请求上下文，客户端断开返回 `BadResponse` + `SkipRetry`，无首帧返回 504 `ChannelResponseTimeExceeded`。
- `relay/channel/workbuddy/entitlement.go:172-196`：模型级拒绝不再回传上游原始响应体，message 取 `decoded.ToMessage()` → 折单行 → `common.MaskSensitiveInfo` → 300 rune 截断；错误码与 `IsModelScoped` 不变。
- `model/channel.go:221-231`：只配一个 key 的渠道，其唯一凭据被模型级排除时不再「跳过并静默成功」，改为 `ErrorCodeChannelNoAvailableKey`（503）+ `SkipRetry` + 模型级范围；`middleware/distributor.go:134-141` 不再丢弃 `SetupContextForSelectedChannel` 的错误（此前 channel 非空也原样放行）；`model/workbuddy_model_entitlement.go` 空入参返回 `nil, nil`。
- `middleware/logger.go`：`/api/channel/oauth/` 整个路径的 query 从访问日志剥离（OAuth `code` / `state` 不再落盘），补 `middleware/logger_test.go` 用例。
- `service/gemini_web_rotation_task.go` 与 `relay/channel/geminiweb`：账号结果 / 代理绑定写库失败不再被丢弃（新增未导出 `markAccountResult`，失败走 `common.SysError`）。
- 删除两个零调用的模型发现端点 `GET /api/channel/:id/opencode/models`、`GET /api/channel/:id/command-code/models`（与通用 `getChannelModels` 语义重复）：路由、`controller` 处理器、`service` 包装函数与路由测试断言一并删除；内部 `GetOpenCodeChannelModelsForChannel` / `GetCommandCodeChannelModelsForChannel` 保留（上游模型同步仍在用，P44/P53 锚点不变）。
- `controller/request_bind.go` 新增 `bindOptionalRequestJSON`，删除 5 个逐字相同的 `bind*Request` 私有函数，12 个调用点改共享函数。

**前端**

- 五个原本「后端有、控制台无」的端点接线：三个渠道面板的「刷新凭据」按钮（`POST /api/channel/oauth/{antigravity,workbuddy,opencode}/refresh-token`）、WorkBuddy 任务面板的 14 天用量历史卡（`GET /api/channel/workbuddy/usage/history`）、Gemini Web 账号池的启用 / 停用开关（`POST /api/channel/:id/gemini-web/accounts/:account_id/status`）。
- 新增 `features/channels/lib/oauth-credential.ts`（`readCredentialField`）与 `lib/antigravity-credential.ts`（`mergeAntigravityCredential`：刷新响应只带 token 字段，合并回存量的 `project_id` / `email` / `plan_type`，避免刷新后 key 缺必填字段）。
- 新增 `features/channels/components/hooks/use-device-login-polling.ts` 与 `device-login-status.tsx`：opencode、command-code 面板迁移到共享轮询（递归 `setTimeout`、上游 interval 下限 1s、卸载即停）；WorkBuddy 面板保留自建轮询，因为其 start 失败告警与 ready 回填（realm / product / baseURL）语义不同，迁移会改变可见行为。
- i18n：七语言各 7766 → 7776 键（+10），missing / extra / empty 0/0/0，键序一致。

### 验证

```text
go build ./... / go vet ./...                                  → exit 0
gofmt -l（253 个改动 .go）                                      → 无输出
go test ./model/ ./middleware/ ./router/ ./service/ ./setting/... ./common/... ./i18n/... ./pkg/... → ok
go test ./relay/...                                            → ok（relay/channel 根包两个既有 Windows HTTP2 用例仍偶发）
web: tsgo -b 0 / vitest 224 文件 2605 用例 / oxlint 改动文件 0 error
web: bun run build 入口 index.b89344f55c.js 1084.4 kB raw / 280.2 kB gzip（预算 ≤300 kB）
web: i18n 7 语言 7776 键 missing/extra/empty 0/0/0
pwsh fork-integration/upgrade-check.ps1 -SkipTests              → exit 0
```

---

## 2026-10-26 — 移除 Gemini Web 浏览器登录（未接线）

### 范围

- 删除 `controller/geminiweb_login.go` 与 `controller/geminiweb_login_test.go`：这两个处理器只把"浏览器抓取会话"转发给外部登录服务（`GEMINI_WEB_LOGIN_URL` / `GEMINI_WEB_LOGIN_SECRET`），再把抓到的 cookie 导入 `gemini-web2api-go` 边车（`GEMINI_WEB2API_ADMIN_URL` / `GEMINI_WEB2API_ADMIN_TOKEN`）。前端从未调用这两个接口，控制台面板只能手工粘贴 Cookie。
- 撤回 `router/channel-router.go` 的两条路由：`POST /api/channel/oauth/gemini-web/session`、`GET /api/channel/oauth/gemini-web/session/:id`。
- 移除仓库根未纳入版本库的 `browser-login/` 抓取边车（11 个文件：Node 服务 `server.js` + 抓取扩展 + 静态页），归档到 `.tmp/removed/browser-login/`（`.tmp/` 已被 `.gitignore` 忽略，需要时可原样取回）。
- Gemini Web 渠道（类型 68）能力不变：渠道 key 与渠道账号池都只接受粘贴的 Cookie 全文或带 `cookie` 字段的 JSON（`parseGeminiWebCredential` 校验 `__Secure-1PSID` / `__Secure-1PSIDTS` / `SAPISID`），账号池仍支持新增/删除/列示。

### 验证

```text
go build ./... / go vet ./...                      → exit 0
go test ./router/ -count=1                         → ok
go test ./controller/ -run GeminiWeb -count=1      → ok
gofmt -l controller router                         → 无输出
pwsh fork-integration/upgrade-check.ps1 -SkipTests → exit 0（65 锚点 / 7 语言 7766 键）
```

---

## 2026-10-26 — 移除 Telegram 治理集成

### 范围

- 删除 fork 自建的 Telegram **治理机器人**（Bot webhook 接收 update、`/help` `/start` `/bind` `/whoami` 命令、渠道启停、内联按钮二次确认、告警 outbox、操作审计）：9 个后端文件（`controller/telegram_governance.go`、`model/telegram_governance.go`、`service/telegram_governance{,_alerts,_bot,_commands,_status,_test}.go`、`setting/system_setting/telegram_governance.go`）与 10 个前端文件（`web/src/features/system-settings/telegram/**`、`web/src/routes/_authenticated/system-settings/telegram/**`）。
- 保留上游原生 Telegram **OAuth 登录 / 账号绑定**：`controller/telegram.go`、`oauth/telegram.go`、`setting/system_setting/telegram.go` 及前端登录页、绑定弹窗均未改动。
- 撤回 `UPSTREAM_PATCHES.md` 的 P100–P104（公开 webhook 路由、治理控制台路由组、6 张治理表 `AutoMigrate`、`controller/option.go` 的 `token` 后缀过滤）。
- 前端：`system-settings.config.ts` 移除「Telegram governance」侧边栏分组，`routeTree.gen.ts` 由构建重新生成；7 个语言包删除 110 个仅属于该治理面板的键（7876 → 7766，键序一致、parity 0/0/0）。

### 验证

```text
go build ./... / go vet ./...                              → exit 0
go test ./router/ ./model/ ./service/ ./middleware/        → ok
web: tsgo -b / vitest run / oxlint（改动文件）              → exit 0
web: i18n 7 语言 missing/extra/empty                        → 0/0/0
```

---

## 2026-10-26 — 合并上游 `v1.0.0-rc.47`（`e835b1bfc`）

### 范围

| 项 | 值 |
| --- | --- |
| 起点 | `v1.0.0-rc.46`（`7d8b91268`）+ 本地 64 个提交（`516d98bf8`） |
| 终点 | `v1.0.0-rc.47`（`e835b1bfc`，22 个上游提交） |
| 合并结果 | `7f3fdbd1c`（`feat/antigravity`），另有 `874e6108d` 只做 gofmt |
| 合并规模 | 相对 `v1.0.0-rc.46`：530 个文件、+73,288 / −4,605 行 |
| 上游主要功能 | 流式写入预算 `StreamWriteBudget`（`7e6596728` 大事件不再截断慢客户端、`0c2af9780` 已交付的 Responses 流不再报假断连）、访问日志隐藏查询凭据（`30748c655`）、任务插件自行校验请求 / 透传客户端头 / multipart 原生路由（`db80ac8e0`、`dbe97beb9`、`556517fa5`）、任务超时放宽到 72 小时（`0034b2491`）、Claude 缓存写入独立计费（`94cc3ff09`、`e9d4d6353`）、上游价格按站点汇率换算（`eb169e74d`）、保持 JSON Schema 属性顺序（`d0b840e75`）、Gemini 顶层 `imageConfig`、渠道高级自定义请求体透传、Web 构建改用 Bun 1.4.4 + `bun check`、依赖 bump |

### 冲突与融合

- `relaykit/relayconvert/internal/shared/claude/schema.go`：上游把 `FunctionParametersToInputSchema` 的入参改成 `json.RawMessage` 以保持属性顺序；本地可空 union 重写必须保留 → 新增 `normalizeNullableUnions(schema json.RawMessage) json.RawMessage`，`schemacompat.ForUpstream` 判定未改动时原样返回（保序），改动时才重新序列化。
- `relaykit/relayconvert/internal/toolconv/encode.go`：删除本地 `functionParametersMap` 死代码（上游调用方已直接传 raw），并把自动合并且切坏的 flatten 块变量（`undefined: cloned`）改为 `schema`。
- `relaykit/relayconvert/internal/oai_chat/to_claude_messages_req_test.go`、`relaykit/relayconvert/request_registry_test.go`：`Parameters` / `InputSchema` 从 map 字面量改为 `json.RawMessage`，断言改用 `assert.JSONEq`。
- `relay/helper/stream_scanner.go`：上游 rc.47 用 `StreamWriteBudget` + `streamResponseWriter`（在 `relay/helper/common.go` 的 `SetEventStreamHeaders` 挂载）取代本地 `ExtendWriteDeadline`，本地对它的调用点随上游删除；本地"截断流不能冒充正常结束"的 `MarkIncomplete("upstream_stream_error")` 从 scanner goroutine 移到全部 goroutine join 之后（`sawDone` / `readFailed` / `endedWithoutDone` 三个闭包外变量，`cleanup()` 之后统一判定），否则与上游 `response.completed` 处理存在竞态，`relay/channel/openai` 计费用例会随机失败。语义差异保留：读错误时所有协议标记 incomplete，干净 EOF 但缺 `[DONE]` 只对 OpenAI 标记（2026-10-26 起推广到全部带终止事件的协议，见上方「收尾批次 A」）。
- `common/limiter/limiter_test.go`：上游测试仍用 `redisLimiter.limitScriptSHA`，本地实现早已是 `redis.Script` → 改为 `redisLimiter.script.Hash()`。
- `web/src/features/system-settings/models/upstream-ratio-sync-helpers.ts`：整体取上游（新增汇率/币种感知的 `formatSyncPriceAmount`、`USD_PRICING_CURRENCY` 参数）。
- `web/src/features/channels/components/__tests__/channel-quick-options.test.tsx`：取上游 import（补 `transformFormDataToCreatePayload` / `transformFormDataToUpdatePayload`）。
- `web/src/i18n/locales/*.json`（7 个）：按 base/ours/theirs 三方并集合并，本地 7895 键 + 上游 22 个新键 = 7917 键，上游未改既有译文。`web/src/i18n/config.ts` 的语言包按需加载实现未被合并触碰。

### 验证

```text
go build ./... / go vet ./...（根模块）                                          → exit 0
GOWORK=off go build ./... / go vet ./... / go test ./relayconvert/...（relaykit） → exit 0
tokenkit: GOWORK=off go build ./...                                             → exit 0
go test ./relay/... ./common/... ./middleware/... ./pkg/... ./setting/...        → exit 0
go test ./service/... ./model/... ./i18n/... ./types/...                         → 全部 ok
go test ./controller/... ./router/...                                            → 113 个用例 FAIL，与合并前基线完全一致（见"已知遗留"）
gofmt -l（本次改动的 84 个 Go 文件）                                              → 无输出
fork-integration/upgrade-check.ps1 -SkipTests                                    → 65 个锚点、7 语言 7917 键全过
web: tsgo -b（本机无 bun，直接调用 node_modules/.bin/tsgo.CMD）                   → exit 0
web: oxlint 全仓 390 diags                                                       → 合并前基线 393，未新增
web: vitest                                                                      → 222 文件 / 2590 用例全过
web: rsbuild build                                                               → 入口 index.2bbda509c2.js 1,116,522B / 285,414B gzip（满足 web/AGENTS.md 3.4 的 ≤300KB gzip 预算）
```

### 已知遗留 / 环境限制

- `go test ./controller/... ./router/...` 在本机 Windows 上有 113 个用例失败；这些用例在合并前基线 `516d98bf8` 上同样失败，失败集合逐名比对完全一致（0 个新增、0 个消失），不是本次合并引入的。原因分三类：
  - SQLite 临时文件在 Windows 上 `TempDir RemoveAll cleanup` 因文件仍被占用而失败（rc.45 条目已记录同一问题）。
  - 本地新增的 telegram governance 路由没有在 `middleware/access_token_routes.go` 的规则表里声明，`TestAccessTokenRouteRulesCoverEveryDashboardRoute` 报 "must be declared exactly once"。该约束是 fail closed 的：普通（非 legacy）个人访问令牌调用这些端点会被 403 `ACCESS_TOKEN_ROUTE_UNDECLARED` 拒绝，属于待补的功能缺口，不是越权通道。补声明会改变访问令牌对这些端点的授权语义，单独处理。（2026-10-26 该治理集成已整体移除，本项随之消失。）
  - `controller` 的渠道删除用例所用测试库未迁移 `gemini_web_accounts`，删除渠道时报 `no such table`。
- 本机没有 MySQL / PostgreSQL 引擎，三库矩阵未执行（SQLite 由上述 `go test` 覆盖；线上 gk / tx101 都是 PostgreSQL 15，实际运行覆盖了它）。合并涉及计费、审计与迁移路径，MySQL 方向仍需在有 MySQL 8.0 的环境补跑。
- 工作树里 `$null`、`.tmp/`、`browser-login/`、`tools/`、`web/pnpm-lock.yaml` 仍未跟踪，未纳入提交。

### 发布（2026-10-26 04:00）

| 项 | 值 |
| --- | --- |
| 二进制 | `new-api-linux-amd64`，138,596,514B，sha256 `20cf98de90418d332c3bd3edfe63fa0934c82537777c96f43cfd448d31215637`（Go 1.26.10 交叉编译，`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOWORK=off`，`-trimpath -pgo=<moejs>/default.pgo -ldflags "-s -w"`） |
| 镜像 | 两台同为 `new-api-fork:rc47-20261026-0400`（以重启前镜像为 base，`COPY` 新二进制，避免从 Docker Hub 拉取） |
| tx101 | `/home/ubuntu/new-api`，compose 备份 `docker-compose.yml.bak-rc47-20261026-0400`，数据库备份 `backup/db-new-api-20261026-0400.sql.gz` |
| gk | `/home/gk/new-api`，compose 备份 `docker-compose.yml.bak-rc47-20261026-0400`，数据库备份 `backup/db-new-api-20261026-0400.sql.gz` |
| 迁移 | 两台启动日志 `database migration started` 后无报错，`audit_logs` 出现新复合索引 `idx_audit_time_event`；gk 重启一次验证 `AutoMigrate` 幂等（无 ALTER 抖动，ready in 3.4s） |
| 回滚 | 把 compose 的 `image:` 改回备份文件里的旧标签，`docker compose up -d --no-deps new-api` |

验证结果：两台 `/api/status`、`/api/pricing`、`/api/notice`、`/api/setup`、`/api/ratio_config` 均 200，未登录端点 `/api/user/self`、`/v1/chat/completions` 均 401（非 500），日志无 panic；公网 `https://onima.chat/` 首屏改为 `index.2bbda509c2.js`，语言包只按需拉一个 async chunk（zh `cu.37875e06b8.js` 186KB、ja `p6.bf62474494.js` 199KB、ru `guy.1fcab69d97.js` 215KB），`/console`、`/pricing`、`/about` 深链均正常挂载且控制台无报错。

### 版本号标注（2026-10-26 04:50）

线上此前显示 “未知版本”：`common/constants.go:14` 的 `var Version = "v0.0.0"` 只在构建时由 `-X` 覆盖，而仓库 `VERSION` 文件一直是 0 字节（上游 `f4450040b` 引入空文件），`Dockerfile:29` 的 `-X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'` 于是注入空串；前端 `web/src/features/system-update/use-system-update.ts:151` 把 `v0.0.0` 视为未发布版本，`system-update-action.tsx:58` 回退渲染 `t('Unknown version')`。

- `VERSION` 改为 `v1.0.0-rc.47+fork`：前缀是上游最新 release tag（“正常版本”），`+fork` 是本 fork 的构建标记，页面上能一眼区分。
- 加号后缀是 semver 构建元数据：`web/src/features/system-update/releases.ts:50` 的 `parseSystemVersion` 接受并忽略它，与上游 `v1.0.0-rc.47` 比较结果为 0，不会误报“有更新”。
- 长度是刻意的：`app-header.tsx` 的版本 pill 是 `max-w-32 truncate font-mono text-xs`（128px），实测 `v1.0.0-rc.47+fork.20261026` 宽 171.5px 会被截断，`v1.0.0-rc.47+fork` 112.2px 才能完整显示；构建时间由镜像 tag 与本文档记录，不再塞进版本串。
- `.gitattributes` 增加 `VERSION text eol=lf`：`$(cat VERSION)` 只去掉结尾换行，若在 Windows 上检出成 CRLF，注入的版本串会带 `\r`。
- 今后 Docker 构建自动生效：`Dockerfile:7-8,29` 与 `Makefile:18` 都从 `VERSION` 取值（shell 命令替换会去掉结尾换行），所以每次升级上游后要同步改这个文件的前缀。

| 项 | 值 |
| --- | --- |
| 二进制 | `new-api-linux-amd64`，138,596,514B，sha256 `24a22f6181b7b109e1df38a3180c635798e61aa82176a400bd10350d2595cb84`（同 rc.47 编译参数，另加 `-X .../common.Version=v1.0.0-rc.47+fork`） |
| tx101 镜像 | `new-api-fork:ver-20261026-045018`（替换 `ver-20261026-044544`） |
| gk 镜像 | `new-api-fork:ver-20261026-045036`（替换 `ver-20261026-044618`） |
| 校验 | 两台 `/api/status` 的 `data.version` 与响应头 `X-New-Api-Version` 均为 `v1.0.0-rc.47+fork`，首页仍引 `index.2bbda509c2.js`；浏览器实测 `/` 与 `/console` 挂载正常、无控制台报错 |

---

## 2026-10-18 — 合并上游 `v1.0.0-rc.45`（`6cf0d90b5`）

### 范围

| 项 | 值 |
| --- | --- |
| 起点 | `v1.0.0-rc.44` + 本地 59 个提交（`a052353c4`） |
| 终点 | `v1.0.0-rc.45`（`6cf0d90b5`，31 个上游提交） |
| 合并结果 | `d117180a8`（`feat/antigravity`） |
| 上游主要功能 | DeepSeek V4.1 Flash / Search / Fetch、通知分类开关、渠道优先级拖拽、Sub2API 余额、AWS Mantle、令牌查询用户额度 |

### 冲突与融合

- `model/ability.go`：先保留上游按 routing name 回退查渠道，再执行本地 `(channel, model)` 429 冷却。
- `service/relay_error.go`：保留上游按渠道地址脱敏，同时把脱敏后的错误交给本地冷却判断。
- `common/model_test.go`：同时保留本地 Codex 端点测试与上游 DeepSeek Tools 端点测试。
- `channel-configuration.test.tsx`：同时保留本地 Antigravity / WorkBuddy / 最大输入 Token 测试与上游 AWS Mantle 测试。
- `web/src/i18n/locales/*.json`：保留本地 7564 个键，再补入上游 64 个新键 × 7 语言（共 448 条）；`i18n:sync` 报告 missing=0。
- 上游删除 `RootUserLanguage` / `NotifyRootUser` 后，Antigravity 配额耗尽通知改用 `NotifyChannelWatchers`，按管理员语言和渠道通知开关发送。
- `model.Token.AllowUserQuotaQuery` 是新增列；新建令牌默认开启，旧令牌升级后保持关闭。补充了 SQLite / MySQL / PostgreSQL 的旧表升级与重复迁移测试。

### 验证

```text
fork-integration/upgrade-check.ps1                 → 65 个锚点、guard tests、vet/gofmt、7 语言 7628 键全过
go build ./...（根模块 / relaykit / tokenkit）      → exit 0
go vet ./...（根模块 / relaykit / tokenkit）        → exit 0
数据库矩阵                                          → SQLite 3.50.4 / MySQL 8.0.46 / PostgreSQL 15.17 全过
web: bun run typecheck                              → exit 0
web: bun run test                                   → 217 文件 / 2504 用例全过
web: bun run build                                  → exit 0
```

### 已知环境限制

- 完整 `go test` 在 Windows 上仍有既有的 SQLite 临时文件清理失败，以及两个 HTTP/2 transport 测试失败；相关包的目标测试和本次新增数据库矩阵均通过。
- 完整 `bun run lint` / `format:check` 仍会命中仓库中与本次合并无关的既有问题；本次涉及文件的 lint 与 format 检查通过。

---

## 2026-10-10 — 首次集成（上游基线 `1d4328e97`）

### 交付范围

| 里程碑 | 内容 |
| --- | --- |
| M0 | 协议内核 `pkg/antigravity/`：OAuth + PKCE + SessionStore、Cloud Code Assist 客户端（token / loadCodeAssist / onboardUser / fetchAvailableModels）、v1internal 信封与 SSE 解包 Reader、模型映射、配额扁平化 |
| M1 | 渠道接入 `relay/channel/antigravity/`：渠道类型 64、适配器（转换全部委托给 `gemini.Adaptor`，仅做信封包装与响应解包）、装配守卫测试 |
| M2 | OAuth 登录：`service/antigravity_oauth.go` + `controller/antigravity_oauth.go` + 3 条路由 + 前端 `antigravity-oauth-panel.tsx` + 抽屉集成 + i18n |
| M3 | 续期与配额：按渠道 ID 刷新、15 分钟提前窗口的后台自动续期任务、配额查询端点、面板「刷新凭证」接线 |

### 规模

- **新增文件 13 个**（新目录在 git merge 中不会冲突）
- **既有文件追加 19 个 / 约 316 行**（其中 Go 核心注册文件 23 行）
- 守卫与单元测试 **61 个用例**

### 验证基线（本次全绿）

```
go build ./relay/... ./common/... ./constant/... ./controller/... ./service/... ./model/... ./pkg/... ./router/...  → exit 0
go vet（antigravity 相关包）                                                                                      → exit 0
go test -count=1 ./pkg/antigravity/ ./relay/channel/antigravity/ ./relay/ ./router/ ./constant/ ./common/         → 全部 ok
go test -count=1 ./service/ -run Antigravity                                                                      → 7/7 PASS
node web/scripts/sync-i18n.mjs                                                                                    → 7 语言 missing=0 extras=0
```

### 已知遗留

- `relay/common/relay_info.go` 的 `streamSupportedChannels` 是包级私有 map，无导出访问器，无法在守卫测试中直接断言（由集成测试兜底）。
- 配额查询端点已可用，前端暂无展示 UI。
- `POST /api/channel/oauth/antigravity/refresh-token`（按 token 刷新）保留为运维 API，前端未接线。
- 隐私设置（`setUserSettings`）未实现，`Credential.privacy_mode` 字段保留但恒为空。

### 环境备注（踩坑记录）

- `relay/channel/antigravity/adaptor.go` 在 Codex 编辑器面板中打开过后，出现过一次**编辑器缓冲回写覆盖磁盘改动**（`OAuthKey` → `agcore.Credential` 被还原，导致编译失败）。
  **改完代码后请以 `git status` + 编译结果复核，不要只依赖编辑器面板显示的内容。**
- 本仓库 locale 文件**不是字母序**且含 `\u` 转义，i18n 必须**纯文本末尾追加**（见 `UPSTREAM_PATCHES.md` 的说明）。

---

## 2026-10-14 — 升级至上游 `v1.0.0-rc.44`（`633b4c5b1`）

### 范围

| 项 | 值 |
| --- | --- |
| 起点 | `v1.0.0-rc.43`（`c6741c36a`） |
| 终点 | `v1.0.0-rc.44`（`633b4c5b1`） |
| 上游提交 | 41 个提交 / 130 个文件（含大量依赖 bump 与 pricing / relayconvert / logs 修复） |
| 变基结果 | fork 51 个提交全部重放在 rc.44 之上，`HEAD = a13fbc6c2` |

### 冲突面（上游改动 ∩ fork 改动）

只有 8 个文件，其余 169 个 fork 改动文件与本次上游改动无交集：

- `relay/common/relay_info.go` —— P8 / P24 流式白名单，上游 churn 最高的一条，两边都保留即可。
- `web/src/i18n/locales/*.json`（7 个语言文件）—— P16 / P31，按「纯文本末尾追加」合并，键序未重排。

**没有任何锚点需要改号或迁移**：31 个锚点原样命中，P1–P32 的插入位置在本版上游里依然成立。

### 本次新增适配

- `web/src/test-setup.ts`：rc.44 的 `@base-ui` 在关闭浮层时先等 `getAnimations()` 的动画结束再卸载。
  jsdom 的 `getAnimations` 桩恒返回 `[]`，卸载被推迟到下一个 rAF，于是「点关闭 → 立刻断言浮层消失」
  的用例会读到残留 DOM（`api-keys-mutate-drawer`）。
  改为条件式 `BASE_UI_ANIMATIONS_DISABLED` getter：默认禁用动画等待；当测试自己往
  `HTMLElement.prototype` 装了 `getAnimations`（`channel-configuration`、`user-binding-dialog`、
  `param-override-editor`）时把控制权交回 base-ui，走真实动画路径。
  这样两类用例各自成立，无需改动上游测试文件。

### 验证基线（本次全绿）

```
fork-integration/upgrade-check.ps1                     → 31 锚点 + 守卫测试 + vet/gofmt + 7 语言 i18n（7423 键）全过
go build ./...（根模块 / relaykit / tokenkit）          → exit 0（relaykit、tokenkit 用 GOWORK=off 独立构建）
go test ./common/limiter/ ./model/ ./pkg/workbuddy/ ./relay/channel/workbuddy/ ./service/  → ok
go test ./controller/                                   → 仅 TestAPITokenAuditDatabaseMatrix/sqlite 在 Windows 上因
                                                          SQLite 临时文件句柄失败；已在干净的 rc.44 worktree 复现，
                                                          属上游既有问题，与本 fork 改动无关
web: bun run typecheck                                 → exit 0
web: bun run test                                      → 201 文件 / 2315 用例全过
web: bun run build                                     → exit 0
```

### 已知遗留 / 环境限制

- 本机未安装 MySQL / PostgreSQL（`TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 未配置），
  三数据库矩阵**尚未执行**，不能宣称数据库兼容性已验证。
- 备份 ref `backup/pre-rc43-upgrade`、`backup/pre-rc44-head` 与 `stash@{0}`（升级前 WIP）保留，待新二进制跑通后再清理。

---

## 2026-10-16 — 对齐 sub2api `v0.2.17` 的 Antigravity 能力（P0 + P1）

### 参考与范围

- 对照基线：sub2api `v0.2.17`（`b0a02a33`，2026-10-16）。对比结论见本次改动记录。
- 本轮只做 P0（模型表 / UA / 套餐归一化 / 思考变体）与 P1（错误脱敏 / 图片模型渠道测试 /
  `MODEL_CAPACITY_EXHAUSTED` 与空流 `MALFORMED_FUNCTION_CALL` 重试）。
- 明确不处理 P2：账号级按模型限流、credits overages 重试、composite group 的 `/v1beta` 路由、
  apicompat 最小输出下限。这些与 new-api 的渠道/多 key 架构差异过大，需要单独设计。

### 改动

- `pkg/antigravity/models.go`：补 Claude Opus/Sonnet 5.5（8 项）与 Gemini 4 Argon（4 项）映射；
  新增裸 Gemini 名 (`gemini-3.6/3.7/3.8-flash`) 按 `thinkingConfig` 解析到
  `-high/-medium/-low/-tiered` 变体，显式别名与真实裸模型（`gemini-2.5-flash`、`gemini-3.1-pro`）保持不变。
- `pkg/antigravity/useragent.go`：默认版本升到 `2.23.0`，格式改为 `antigravity/hub/<ver> windows/amd64`。
- `pkg/antigravity/quota.go`：`g1-pro-*-tier` / `g1-ultra-*-tier` 归一到 Pro / Ultra，新增 `IsPaidPlanType`。
  转发端点仍沿用本 fork 的 daily 优先策略（`ForwardBaseURLs`），未改成 sub2api 的
  「付费→daily / 免费→prod」，因为本 fork 已按实测决定 daily 优先（见 `UPSTREAM_PATCHES.md` P3）。
- `relay/channel/antigravity/error_body.go`：非 2xx 上游错误重写为 `{code,message,status}`，
  清除 `projects/<id>`、服务账号邮箱、consumer/project 数字与 Bearer token，丢弃 `details`。
- `relay/channel/antigravity/retry.go`：503/429 且带 `MODEL_CAPACITY_EXHAUSTED` 时用可重放请求体
  原地重试（60 次 × 1s），并对同一模型做 10s 冷却去重，避免并发请求一起压上游。
- `relay/channel/antigravity/stream_prebuffer.go`：流式响应在首个有效内容前先缓冲；若整条流没有内容
  却以 `MALFORMED_FUNCTION_CALL` 结束，则返回可重试错误（此时尚未向前端写入任何 chunk）。
- `relay/channel/antigravity/adaptor.go`：接线以上逻辑；图片模型（`*-image`）的渠道测试自动改写为
  图片提示词 + `responseModalities: [TEXT, IMAGE]` + `imageConfig.aspectRatio=1:1`，
  图片响应沿用 Gemini → OpenAI 的 data-URL 渲染。

### 冲突面

全部改动落在 fork 新建文件（`pkg/antigravity/`、`relay/channel/antigravity/`），未触碰任何上游既有文件，
因此 `UPSTREAM_PATCHES.md` 的 31 个锚点与 P1–P32 清单不变，下次上游升级不需要新增锚点。

### 验证

```
fork-integration/upgrade-check.ps1                                   → 31 锚点 + 守卫测试 + vet/gofmt + 7 语言 i18n 全过
go build ./...                                                       → exit 0
go test ./pkg/antigravity/ ./relay/channel/antigravity/              → ok
go test ./relay/ ./service/ ./common/limiter/                        → ok
go test ./controller/                                                → 仅 Windows SQLite 临时文件句柄清理失败（与 2026-10-14 记录的既有问题一致）
go test ./relay/channel/（TestUpstreamGetBody_HTTP2RetryAfterGracefulGoAway_PassThrough）
                                                                    → 本机确定性失败，本机 HTTP/2 连接被本机网络栈中断，与本次改动无关
```

### 已知限制

- `MODEL_CAPACITY_EXHAUSTED` 重试要求请求体可重放（`common.ReplayableBody`）；渠道测试使用一次性
  `bytes.Buffer`，因此不参与该重试。
- 空流 `MALFORMED_FUNCTION_CALL` 只在整条流没有任何有效内容时重试；一旦已有内容输出（含 thinking），
  按正常完成处理，避免半途切换渠道导致前端拼出两段回答。

---

## 2026-10-16 — OpenCode OAuth / Zen + Go 渠道集成

### 范围

- 新增渠道类型 `66 = OpenCode`，通过 `pkg/opencode` 实现官方 Console device authorization：
  `/auth/device/code`、`/auth/device/token`、`/api/user`、`/api/orgs`。
- 单一渠道类型 + 凭证 `mode=zen|go`：Zen 基址 `https://opencode.ai/zen/v1`，
  Go 基址 `https://opencode.ai/zen/go/v1`，Anthropic 端点分别使用去 `/v1` 的对应基址。
- `relay/channel/opencode` 按模型在 Chat Completions / Responses / Anthropic Messages 之间路由，
  复用 `relayconvert` 做跨协议请求与响应转换；`gemini-*` / `jev-*` 明确拒绝并提示专用端点。
- 登录、按渠道刷新、按行多 key 刷新、后台自动续期、模型发现、Go `/usage` 查询与管理接口。
- 前端 `opencode-oauth-panel.tsx`：模式选择、设备授权链接、用户代码、轮询、手动刷新、Go 用量展示。
- 支持 `opencode auth export` JSON 导入为渠道 key；支持多账号每行一个紧凑凭证。
- 支持直接粘贴 OpenCode Zen API Key（如 `oc_...`）；Zen/Go 模式优先取凭证，其次按渠道 Base URL 推断。

### 改动

- 新增 `pkg/opencode/`：device flow、凭证结构、协议规则、模型列表、Go usage、auth export 导入、会话存储。
- 新增 `service/opencode_oauth.go`、`service/opencode_credential_refresh_task.go`、`service/opencode_usage.go`。
- 新增 `controller/opencode_oauth.go` 与 6 条路由（登录 3 条 + 渠道刷新/模型/用量 3 条）。
- 新增 `relay/channel/opencode/` 适配器；`relay/opencode_wiring_test.go` 守卫注册链路。
- 既有注册文件追加：`constant/channel.go`、`constant/api_type.go`、`common/api_type.go`、
  `common/endpoint_type.go`、`relay/relay_adaptor.go`、`relay/common/relay_info.go`、
  `router/channel-router.go`、`main.go`、`controller/channel.go`、`controller/channel_upstream_update.go`。
- 前端追加：`constants.ts`、`channel-utils.ts`、`channel-type-config.ts`、`channel-form.ts`、
  `channel-credential-import.ts`、`api.ts`、`channel-mutate-drawer.tsx`、i18n 7 语言。

### 验证

```
go build ./...                                                        → exit 0
go vet ./pkg/opencode/... ./relay/channel/opencode/... ./service/... ./controller/... ./router/...
                                                                      → exit 0
go test -count=1 ./pkg/opencode/... ./relay/channel/opencode/... ./relay/ ./router/... ./service/... ./common/... ./constant/...
                                                                      → 全部 ok
go test -count=1 ./controller/ -run TestNonExistent                    → 编译通过
web: node_modules/.bin/tsgo.exe -b                                    → exit 0
web: node_modules/.bin/oxlint.exe（改动文件）                          → exit 0
web: vitest run channel-credential-import / opencode-oauth-panel       → 15/15 PASS
web: node scripts/sync-i18n.mjs                                       → 7 语言 missing=0 extras=0
```

### 已知限制

- 本机未安装 MySQL / PostgreSQL；OpenCode 集成不改变数据库 schema，但仍需在正式升级矩阵中
  复核渠道 key 文本读写。
- 完整 controller 测试集在本机 Windows 因 SQLite 临时文件句柄未释放而失败，与本次改动无关。
- 真实 OpenCode Console 账号冒烟（device flow、Zen/Go 推理、刷新、Go usage）尚未执行；
  建议用 `OPENCODE_SMOKE=1` 或手动授权完成一次端到端验证。
- OpenCode 免费账号被上游明确限制为仅官方客户端可用（HTTP 403 `FreeTierError`）。new-api
  将该错误归类为不可重试、不可自动禁用，并提示使用 OpenCode Zen API Key 或 Go 订阅；
  不实现绕过该限制的指纹/客户端伪造。

---

## 模板（下次上游升级时复制）

```markdown
## YYYY-MM-DD — 升级至上游 <commit>

- 上游变更摘要：
- 冲突文件与处理：
- `upgrade-check.ps1` 结果：
- 需要更新的锚点（P 编号）：
- 新增/调整的测试：
```
