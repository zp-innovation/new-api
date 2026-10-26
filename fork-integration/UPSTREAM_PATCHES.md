# Antigravity + WorkBuddy + OpenCode 集成 — 上游补丁清单

> 本文件记录**对 new-api 既有文件所做的全部改动**。上游升级（merge / rebase）时按本清单逐条核对；
> 自动化核对请运行 `fork-integration/upgrade-check.ps1`。
>
> 各渠道的协议分析与架构决策取自开发期设计稿；按项目约定（AGENTS.md：未经明确要求不得新增
> `docs/` 文件）这些文稿不纳入版本库，结论已落到下方 P 编号条目、代码注释与提交历史中。
> OpenCode 的设备授权协议来自 `sst/opencode` 的 Console 客户端，中继语义对齐 sub2api 的 OpenCode Zen/Go 实现。
> 新增文件（`pkg/antigravity/`、`relay/channel/antigravity/`、`service/antigravity_*.go`、
> `controller/antigravity_oauth.go`、`web/.../antigravity-oauth-panel.tsx` 等）不在本清单内 ——
> **新目录/新文件在 git merge 中不会冲突**，只有下面这些"对既有文件的追加"才需要人工核对。

---

## 核心改动总览（Go）

| ID | 文件 | 行数 | 目的 | 冲突风险 |
| --- | --- | --- | --- | --- |
| P1 | `constant/channel.go` | +1 | 渠道类型号 | 中（上游新增渠道会动这里） |
| P2 | `constant/channel.go` | +1 | 渠道显示名 | 低 |
| P3 | `constant/channel.go` | +1 | 默认 Base URL | 低 |
| P4 | `constant/api_type.go` | +1 | API 类型号 | 中 |
| P5 | `common/api_type.go` | +2 | 渠道类型 → API 类型 | 中 |
| P6 | `common/endpoint_type.go` | +7 | 支持的端点类型 | 低 |
| P7 | `relay/relay_adaptor.go` | +3 | 适配器工厂 + import | 高（上游 churn 高） |
| P8 | `relay/common/relay_info.go` | +1 | 流式支持白名单 | 极高（上游 churn 最高） |
| P9 | `router/channel-router.go` | +3 | OAuth 登录路由 | 低 |
| P10 | `router/channel-router.go` | +2 | 渠道操作路由 | 低 |
| P11 | `main.go` | +1 | 启动自动续期任务 | 低 |

**全部为"纯追加"，无删除、无改动既有逻辑。** 因此冲突解决的一般原则是 **「两边都保留」**。

---

## P1 — 渠道类型号

- **文件**：`constant/channel.go`
- **锚点**：`ChannelTypeSGLang         = 63`
- **插入**：`ChannelTypeAntigravity    = 64`
- **位置**：必须在 `ChannelTypeDummy` **之前**

```go
	ChannelTypeSGLang         = 63
	ChannelTypeAntigravity    = 64      // ← 我们新增
	ChannelTypeDummy          // this one is only for count, do not add any channel after this
```

> **注意 `ChannelTypeDummy` 的语义**：它没有写赋值表达式，Go 会"文本替换"前一个表达式，因此它**恒等于最大的渠道号**
> （Antigravity 单独时 = 64，加入 WorkBuddy 65 后 = 65）。`controller/model.go` 用
> `for i := 1; i <= ChannelTypeDummy; i++` 枚举，所以新类型必须 ≤ 它。

**冲突处理**：若上游占用了 64，改为下一个空号，并**同步更新** P3/P4/P5/P6/P7/P8、前端 `CHANNEL_TYPE_ANTIGRAVITY`（P12）与
`relay/channel/antigravity/wiring_test.go` 的断言。禁止使用历史空号 **28 / 29 / 30 / 32**（上游可能回收）。

---

## P2 — 渠道显示名

- **文件**：`constant/channel.go`，`ChannelTypeNames` map
- **锚点**：`ChannelTypeSGLang:         "SGLang",`
- **插入**：`ChannelTypeAntigravity:    "Google Antigravity",`

---

## P3 — 默认 Base URL

- **文件**：`constant/channel.go`，`ChannelBaseURLs` 切片（按渠道类型号索引）
- **锚点**：`"",                                          //63`
- **插入**：`"https://daily-cloudcode-pa.googleapis.com", //64`
- **注意**：切片是**按索引**使用的，插入位置必须与 P1 的号一致。
- **主机选择**：必须用 `daily-` 前缀。生产主机 `https://cloudcode-pa.googleapis.com` 对免费额度账号直接返回
  `429 RESOURCE_EXHAUSTED`（commit 7db237d3a 修正）。

---

## P4 — API 类型号

- **文件**：`constant/api_type.go`（iota 块）
- **锚点**：`APITypeNewAPI`
- **插入**：`APITypeAntigravity`（必须在 `APITypeDummy` 之前）

---

## P5 — 渠道类型 → API 类型

- **文件**：`common/api_type.go`，`ChannelType2APIType`
- **锚点**：`case constant.ChannelTypeNewAPI:` 块
- **插入**：

```go
	case constant.ChannelTypeAntigravity:
		apiType = constant.APITypeAntigravity
```

> **缺这一条会静默降级**：`ChannelType2APIType` 未命中时返回 `(APITypeOpenAI, false)`，
> Antigravity 流量会被错误地交给 OpenAI 适配器。守卫测试会捕获。

---

## P6 — 端点类型

- **文件**：`common/endpoint_type.go`，`GetEndpointTypesByChannelType`
- **锚点**：`case constant.ChannelTypeCodex:` 块之后
- **插入**：

```go
	case constant.ChannelTypeAntigravity:
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeGemini,
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
		}
```

（Antigravity 同时承载 Claude 与 Gemini 两族模型，因此同时声明 Anthropic 与 Gemini 端点。）

---

## P7 — 适配器工厂

- **文件**：`relay/relay_adaptor.go`
- **锚点 1（import）**：`"github.com/QuantumNous/new-api/relay/channel/advancedcustom"` 之后
  插入 `"github.com/QuantumNous/new-api/relay/channel/antigravity"`（gofmt 会按字母序排到 `ali` 之后）
- **锚点 2（switch）**：`case constant.APITypeNewAPI:` 块之后
- **插入**：

```go
	case constant.APITypeAntigravity:
		return &antigravity.Adaptor{}
```

> 上游 churn 较高（500 次提交内改动 54 次）。**只追加 case，不要改动其它分支。**

---

## P8 — 流式支持白名单

- **文件**：`relay/common/relay_info.go`，`streamSupportedChannels` map（**包级私有**）
- **锚点**：`constant.ChannelTypeTencent:        true,`
- **插入**：`constant.ChannelTypeAntigravity:    true,`
- **影响**：未登记则 `InitChannelMeta` 不会设置 `SupportStreamOptions`，流式请求的 usage 统计会异常。
- **注意**：该 map 无导出访问器，无法在守卫测试里直接断言，因此本项由**集成测试**兜底（见 `docs` §6）。

---

## P9 — OAuth 登录路由

- **文件**：`router/channel-router.go`，`channelPermissionRoutes` 表末尾
- **锚点**：`.../upstream_updates/detect_all` 那一行
- **插入**：

```go
	{method: http.MethodPost, path: "/oauth/antigravity/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.GenerateAntigravityAuthURL},
	{method: http.MethodPost, path: "/oauth/antigravity/exchange-code", permission: authz.ChannelSensitiveWrite, handler: controller.ExchangeAntigravityCode},
	{method: http.MethodPost, path: "/oauth/antigravity/refresh-token", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshAntigravityCredential},
```

> 权限必须是 `ChannelSensitiveWrite`：这些接口会签发账号凭证。`router/antigravity_oauth_router_test.go` 有断言。

---

## P10 — 渠道操作路由

- **文件**：`router/channel-router.go`（同上，紧随 P9）
- **插入**：

```go
	{method: http.MethodPost, path: "/:id/antigravity/refresh", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshAntigravityChannelCredential},
	{method: http.MethodGet, path: "/:id/antigravity/quota", permission: authz.ChannelRead, handler: controller.GetAntigravityChannelQuota},
```

---

## P11 — 启动自动续期任务

- **文件**：`main.go`
- **锚点**：`service.StartCodexCredentialAutoRefreshTask()`
- **插入**：`service.StartAntigravityCredentialAutoRefreshTask()`
- **注意**：任务内部已判断 `common.IsMasterNode`，与 Codex 任务一致。

---

## 前端改动

| ID | 文件 | 行数 | 内容 |
| --- | --- | --- | --- |
| P12 | `web/src/features/channels/constants.ts` | +5 | `CHANNEL_TYPE_ANTIGRAVITY = 64`、`CHANNEL_TYPES[64]`、`CHANNEL_PROVIDER_PRESENTATION[64]`、`TYPE_TO_KEY_PROMPT[64]` |
| P13 | `web/src/features/channels/lib/channel-form.ts` | +24 | `isAntigravityCredential()` + 保存前校验分支 |
| P14 | `web/src/features/channels/api.ts` | +80 | 4 个 API 封装 + 2 个响应类型 |
| P15 | `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | +27 | 常量/组件 import + `currentType === CHANNEL_TYPE_ANTIGRAVITY` 面板块 |
| P16 | `web/src/i18n/locales/*.json` | 每文件 +23/-1 | 21 个键 × 7 语言（**仅追加**） |

> **P12 的 `CHANNEL_PROVIDER_PRESENTATION` 有类型约束**：它是
> `satisfies Record<Exclude<keyof typeof CHANNEL_TYPES, ...>>`，因此**新增 `CHANNEL_TYPES` 条目后必须同时补描述**，否则 `tsgo` 直接报错。

### i18n 的写入方式（重要）

仓库的 locale 文件**不是字母序**（历史按发布顺序追加），且部分键以 `\u0061` 这类转义存储。
因此：

- ✅ 正确：用 `web/scripts/add-missing-keys.mjs` 脚本**纯文本末尾追加**，保留原键序与转义（每文件 +N/-1 行）
- ❌ 错误：`JSON.parse` → `localeCompare` 排序 → `JSON.stringify` 重写整个文件
  （实测会产生**每文件 782 行伪差异**，因为排序变化 + 转义被还原成字符）

`.agents/skills/i18n-translate/SKILL.md` 的模板默认会做后者，本仓库需按上述方式执行。
新增键后必须运行 `node scripts/sync-i18n.mjs`（等价于 `bun run i18n:sync`）并确认报告 `missing=0 extras=0`。

---

## WorkBuddy 集成（P17 起）

> WorkBuddy 与 Antigravity 是**同一套追加模式**：渠道类型 65 + 原生适配器 + 设备码登录端点。
> 新增目录（`pkg/workbuddy/`、`relay/channel/workbuddy/`、`service/workbuddy_*.go`、
> `controller/workbuddy_oauth.go`、`web/.../workbuddy-oauth-panel.tsx`）同样不在本清单内。

| ID | 文件 | 行数 | 目的 | 冲突风险 |
| --- | --- | --- | --- | --- |
| P17 | `constant/channel.go` | +1 | 渠道类型号 65 | 中 |
| P18 | `constant/channel.go` | +1 | 渠道显示名 | 低 |
| P19 | `constant/channel.go` | +1 | 默认 Base URL（仅表单提示与回落） | 低 |
| P20 | `constant/api_type.go` | +1 | API 类型号 43 | 中 |
| P21 | `common/api_type.go` | +2 | 渠道类型 → API 类型 | 中 |
| P22 | `common/endpoint_type.go` | +7 | 支持的端点类型 | 低 |
| P23 | `relay/relay_adaptor.go` | +3 | 适配器工厂 + import | 高 |
| P24 | `relay/common/relay_info.go` | +1 | 流式支持白名单 | 极高 |
| P25 | `router/channel-router.go` | +3 | 设备码登录路由 | 低 |
| P26 | `web/src/features/channels/constants.ts` | +5 | 前端渠道类型 65 与四处映射 | 中 |
| P27 | `web/src/features/channels/lib/channel-utils.ts` | +2 | `65: 'CodeBuddy'` 图标 | 低 |
| P28 | `web/src/features/channels/lib/channel-type-config.ts` | +5 | 渠道类型配置 | 低 |
| P29 | `web/src/features/channels/api.ts` | +60 | 3 个设备码登录封装 + 3 个响应类型 | 中 |
| P30 | `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | +14 | 常量/组件 import + 面板块 + 导入账号条件 | 中 |
| P31 | `web/src/i18n/locales/*.json` | 每文件 +16/-1 | 16 个键 × 7 语言（**仅追加**） | 低 |

### P17–P19 — constant/channel.go

```go
	ChannelTypeAntigravity    = 64
	ChannelTypeWorkBuddy      = 65      // ← 我们新增
	ChannelTypeDummy          // this one is only for count, do not add any channel after this
```

- `ChannelTypeNames`：`ChannelTypeWorkBuddy:      "WorkBuddy",`
- `ChannelBaseURLs`：`"https://www.workbuddy.ai", //65` —— 该值只是**表单提示**与「凭据未写 realm」时的回落；
  真实上游地址由 `pkg/workbuddy/identity.go` 按 realm 决定（intl = `www.workbuddy.ai`，cn = `copilot.tencent.com`）。

> **`ChannelTypeDummy` 语义修正**：本清单早期版本写「当前 = 64」，实际它没有赋值表达式、Go 会文本替换前一个
> 表达式，因此**恒等于最大渠道号**，现在是 **65**（`controller/model.go` 的
> `for i := 1; i <= ChannelTypeDummy; i++` 会枚举到 65）。新增 65 **不需要**（也不能）把 Dummy 写成 66。

### P20–P22 — 类型号与端点

```go
	APITypeAntigravity
	APITypeWorkBuddy      // ← 我们新增（= 43）
	APITypeDummy // this one is only for count, do not add any channel after this
```

```go
	case constant.ChannelTypeWorkBuddy:
		apiType = constant.APITypeWorkBuddy
```

```go
	case constant.ChannelTypeWorkBuddy:
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeGemini,
			constant.EndpointTypeOpenAI,
		}
```

（WorkBuddy 上游是 OpenAI `chat.completions` 形态，但 new-api 侧允许 Claude/Gemini 入站由适配器转换，
因此同时声明三个端点；**刻意不含 `EndpointTypeOpenAIResponse`** —— 适配器对 `/v1/responses` 直接报不支持。）

### P23–P25 — 适配器与路由

```go
	"github.com/QuantumNous/new-api/relay/channel/workbuddy"
```
```go
	case constant.APITypeWorkBuddy:
		return &workbuddy.Adaptor{}
```
```go
	constant.ChannelTypeWorkBuddy:      true,
```
```go
	{method: http.MethodPost, path: "/oauth/workbuddy/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.StartWorkBuddyLogin},
	{method: http.MethodPost, path: "/oauth/workbuddy/poll-token", permission: authz.ChannelSensitiveWrite, handler: controller.PollWorkBuddyLogin},
	{method: http.MethodPost, path: "/oauth/workbuddy/refresh-token", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshWorkBuddyCredential},
```

> WorkBuddy 是**设备授权码**流程（无回调、无 code 粘贴）：`auth-url` 返回 `{state, auth_url}`，
> 前端每 3 秒轮询 `poll-token`，`status=ready` 时返回**紧凑单行**凭据字符串（拿到即删会话，重放返回 `expired`）。
> 权限同样是 `ChannelSensitiveWrite`（`router/workbuddy_oauth_router_test.go` 有断言）。

### P26–P31 — 前端

- `constants.ts`：`CHANNEL_TYPE_WORKBUDDY = 65`、`CHANNEL_TYPES[65] = 'WorkBuddy'`、
  `CHANNEL_PROVIDER_PRESENTATION[65].descriptionKey`、`MODEL_FETCHABLE_TYPES` 追加 65、`TYPE_TO_KEY_PROMPT[65]`。
- `channel-utils.ts`：`65: 'CodeBuddy'`（上游 CN realm 是腾讯 CodeBuddy；`@lobehub/icons` 没有 WorkBuddy 品牌）。
- `channel-type-config.ts`：`[CHANNEL_TYPE_WORKBUDDY]{ id, name: CHANNEL_TYPES[65], icon: 'CodeBuddy' }`。
- `api.ts`：`startWorkBuddyLogin` / `pollWorkBuddyLogin` / `refreshWorkBuddyCredential` + 3 个响应类型。
- `channel-mutate-drawer.tsx`：常量与组件 import、`currentType === CHANNEL_TYPE_WORKBUDDY` 面板块、
  「导入账号」按钮条件追加 65。
- i18n：16 个键 × 7 语言，**仅追加**（写入方式见下节）。

### 多账号 = 多行 key（无需补丁）

`middleware/distributor.go` 调 `channel.GetNextEnabledKey()`（`model/channel.go`，按 `\n` 切分）
并把选中的那一行写进 `info.ApiKey`，同时设置 `ContextKeyChannelIsMultiKey` / `ContextKeyChannelMultiKeyIndex`。
因此 WorkBuddy 的「多账号」= 渠道 key 列每行一个**紧凑单行 JSON 凭据**，适配器与注册层都无需额外改动。

> ⚠️ 凭据**必须**是紧凑单行：`controller/channel.go` 的 `channelKeyForModelDiscovery` 只对
> Codex/Antigravity 保留整段多行，其它类型（含 WorkBuddy）取第一行；若凭据被换行拆开，
> 第一行会变成残缺 JSON，界面报 `workbuddy: invalid credential json`。

## P32 — 渠道编辑抽屉固定在右侧（本 fork 的 UI 约定）

上游 `505805a4c`（「feat(web): unify channel setup and refine model and settings editors」）
把**编辑**渠道的抽屉放在左侧、**新建**放在右侧，并在
`web/src/features/channels/components/__tests__/channel-configuration.test.tsx` 里用
`data-side` / `left-0` 断言钉住了这个行为。本 fork 统一为**始终右侧**：编辑时左侧抽屉会盖住
侧边导航，而且与新建行为不一致。

- `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`：
  删掉 `requestedSide` / `drawerSide` 的左右切换状态机与退出动画方向保持逻辑，直接
  `const drawerSide = 'right'`。
- 连带影响：浮动「模型重定向」面板（`use-redirect-panel-placement.ts`）改为停靠在抽屉的
  左侧空闲区；同文件测试里的 `useWideScreen()` 几何与面板坐标断言同步为右侧抽屉。
- 上游再次整理这个文件时，若自检 P32 报红，说明这条约定被合并覆盖了，需要重新钉住。

---

## OpenCode 集成（P33–P44，2026-10-16）

OpenCode 使用官方 Console **device authorization** 流程，渠道类型为单个 `66`，凭证里的
`mode=zen|go` 决定推理基址。新目录/新文件包括 `pkg/opencode/`、`relay/channel/opencode/`、
`service/opencode_*.go`、`controller/opencode_oauth.go`、`web/.../opencode-oauth-panel.tsx`，
它们不进入补丁清单；以下是既有文件的追加点。

### P33 — 渠道类型号

- 文件：`constant/channel.go`
- 锚点：`ChannelTypeWorkBuddy      = 65`
- 插入：`ChannelTypeOpenCode       = 66`（必须在 `ChannelTypeDummy` 之前）

### P34 — 渠道显示名

- 文件：`constant/channel.go`，`ChannelTypeNames`
- 插入：`ChannelTypeOpenCode: "OpenCode"`

### P35 — 默认 Base URL

- 文件：`constant/channel.go`，`ChannelBaseURLs`
- 插入：`"https://opencode.ai/zen/v1"`（Zen/Go 的实际基址由凭证 mode 覆盖）

### P36–P37 — API 类型

```go
	APITypeOpenCode
	APITypeDummy // this one is only for count, do not add any channel after this
```

```go
	case constant.ChannelTypeOpenCode:
		apiType = constant.APITypeOpenCode
```

### P38 — 端点类型

```go
	case constant.ChannelTypeOpenCode:
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeAnthropic,
		}
```

OpenCode 的 `gemini-*` / `jev-*` 只存在专用端点，适配器会明确拒绝，不声明 Gemini 端点。

### P39–P40 — 适配器工厂与流式白名单

```go
	"github.com/QuantumNous/new-api/relay/channel/opencode"

	case constant.APITypeOpenCode:
		return &opencode.Adaptor{}

	constant.ChannelTypeOpenCode:       true,
```

### P41 — OAuth 与渠道操作路由

```go
	{method: http.MethodPost, path: "/oauth/opencode/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.StartOpenCodeLogin},
	{method: http.MethodPost, path: "/oauth/opencode/poll-token", permission: authz.ChannelSensitiveWrite, handler: controller.PollOpenCodeLogin},
	{method: http.MethodPost, path: "/oauth/opencode/refresh-token", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshOpenCodeCredential},
	{method: http.MethodPost, path: "/:id/opencode/refresh", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshOpenCodeChannelCredential},
	{method: http.MethodGet, path: "/:id/opencode/usage", permission: authz.ChannelRead, handler: controller.GetOpenCodeChannelUsage},
```

### P42 — 后台续期任务

- 文件：`main.go`
- 插入：`service.StartOpenCodeCredentialAutoRefreshTask()`

### P43 — 渠道 key 校验

- 文件：`controller/channel.go`
- 在 Codex key 校验之后追加 OpenCode 分支：每行必须能解析为 `pkg/opencode.Credential`
  或 `opencode auth export` JSON。

### P44 — 上游模型同步

- 文件：`controller/channel_upstream_update.go`
- 在 WorkBuddy 分支之后追加 OpenCode 分支，调用
  `service.GetOpenCodeChannelModelsForChannel`。

### P45 — FreeTierError 归类

- 文件：`service/error.go`
- 在 `RelayErrorHandler` 读取响应体后追加 OpenCode `FreeTierError` 分支：
  HTTP 403 + `FreeTierError` + `free tier can only be used from within OpenCode`
  归类为 `ErrOptionWithSkipRetry()`，避免重试和自动禁用，并把文案改成
  「free tier is restricted to the official OpenCode client」。

### 前端追加点

- `constants.ts`：`CHANNEL_TYPE_OPENCODE = 66`、`CHANNEL_TYPES[66]`、展示文案、
  `MODEL_FETCHABLE_TYPES`、`TYPE_TO_KEY_PROMPT`。
- `channel-utils.ts`：`66: 'OpenCode'`。
- `channel-type-config.ts`：`[CHANNEL_TYPE_OPENCODE]` 配置。
- `channel-form.ts`：OpenCode 凭证校验。
- `channel-form.ts` 同时接受直接粘贴的 `oc_...` API Key；多行时每行一个 Key。
- `channel-credential-import.ts`：`opencode auth export` 数组/单条转换。
- `api.ts`：auth-url / poll-token / refresh-token / models / usage 封装。
- `channel-mutate-drawer.tsx`：`OpenCodeOAuthPanel` 集成与「导入账号」条件。
- i18n：17 个键 × 7 语言，按 `i18n-translate` skill 通过脚本追加。

---

## Command Code 集成（P46–P53，2026-10-17）

Command Code 使用官方 Studio 的 API Key handoff 流程，渠道类型为单个 `67`。官方 CLI
把浏览器登录结果写回本机 loopback 服务，因此 new-api 只在 `localhost` 控制台下提供
浏览器登录；其他部署使用 Studio API Key 粘贴/校验。新目录/新文件包括
`pkg/commandcode/`、`relay/channel/commandcode/`、`service/command_code_oauth.go`、
`controller/command_code_oauth.go`、`web/.../command-code-oauth-panel.tsx`，它们不进入
补丁清单；以下是既有文件的追加点。

### P46 — 渠道类型号

- 文件：`constant/channel.go`
- 锚点：`ChannelTypeOpenCode       = 66`
- 插入：`ChannelTypeCommandCode    = 67`（必须在 `ChannelTypeDummy` 之前）

### P47 — 渠道显示名

- 文件：`constant/channel.go`，`ChannelTypeNames`
- 插入：`ChannelTypeCommandCode:    "Command Code"`

### P48 — 默认 Base URL

- 文件：`constant/channel.go`，`ChannelBaseURLs`
- 插入：`"https://api.commandcode.ai/provider/v1"`

### P49 — API 类型

```go
	APITypeCommandCode
	APITypeDummy // this one is only for count, do not add any channel after this
```

```go
	case constant.ChannelTypeCommandCode:
		apiType = constant.APITypeCommandCode
```

### P50 — 端点类型

```go
	case constant.ChannelTypeCommandCode:
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeAnthropic,
		}
```

Command Code 的 `typesafe/jev` 只存在专用 System One 端点，适配器会明确拒绝，不声明
Gemini 端点。

### P51 — 适配器工厂与流式白名单

```go
	"github.com/QuantumNous/new-api/relay/channel/commandcode"

	case constant.APITypeCommandCode:
		return &commandcode.Adaptor{}

	constant.ChannelTypeCommandCode:    true,
```

### P52 — OAuth 与渠道操作路由

```go
	// 注册在 AdminAuth 之前：Studio 回调只靠一次性 state 鉴权。
	channelRoute.GET("/oauth/command-code/callback", middleware.DisableCache(), controller.CommandCodeCallback)

	{method: http.MethodPost, path: "/oauth/command-code/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.StartCommandCodeLogin},
	{method: http.MethodPost, path: "/oauth/command-code/poll-token", permission: authz.ChannelSensitiveWrite, handler: controller.PollCommandCodeLogin},
	{method: http.MethodPost, path: "/oauth/command-code/validate", permission: authz.ChannelSensitiveWrite, handler: controller.ValidateCommandCodeCredential},
	{method: http.MethodPost, path: "/:id/command-code/validate", permission: authz.ChannelSensitiveWrite, handler: controller.ValidateCommandCodeChannelCredential},
	{method: http.MethodGet, path: "/:id/command-code/usage", permission: authz.ChannelRead, handler: controller.GetCommandCodeChannelUsage},
```

### P53 — 上游模型同步

- 文件：`controller/channel_upstream_update.go`
- 在 OpenCode 分支之后追加 Command Code 分支，调用
  `service.GetCommandCodeChannelModelsForChannel`；该调用同时刷新适配器的动态协议路由缓存。

### 前端追加点

- `constants.ts`：`CHANNEL_TYPE_COMMAND_CODE = 67`、`CHANNEL_TYPES[67]`、展示文案、
  `MODEL_FETCHABLE_TYPES`、`TYPE_TO_KEY_PROMPT`。
- `channel-utils.ts`：`67: 'CommandCode'`。
- `channel-type-config.ts`：`[CHANNEL_TYPE_COMMAND_CODE]` 配置。
- `channel-form.ts`：Command Code API Key / `auth.json` 凭证校验。
- `channel-credential-import.ts`：`~/.commandcode/auth.json` 的 camelCase 转换。
- `api.ts`：auth-url / poll-token / validate / models / usage 封装。
- `channel-mutate-drawer.tsx`：`CommandCodeOAuthPanel` 集成与「导入账号」条件。
- i18n：25 个键 × 7 语言，按 `i18n-translate` skill 通过脚本追加。

---

## 429 配额冷却（P59–P61，2026-10-17）

> 上游把「账号配额耗尽」当普通 429 重试，既不记忆也不冷却：同一个已耗尽账号会
> 被后续请求反复抽中（实测 tx101 出现 `40→40→36` 的重复命中，skechers 账号在 5 小时
> 窗口内被抽中 6 次全部 429）。本组改动把配额拒绝按 **(渠道, 模型)** 粒度停用一段时间。

新增文件（不在锚点清单内）：`model/channel_model_cooldown.go`、`model/channel_model_cooldown_test.go`。

| ID | 文件 | 行数 | 目的 | 冲突风险 |
| --- | --- | --- | --- | --- |
| P59 | `service/relay_error.go` | +7 | 在 `ProcessChannelError` 里记录配额冷却 | 低（该函数改动少） |
| P60 | `model/ability.go` | +4 | 数据库选路 `GetChannel` 跳过冷却中的 (渠道, 模型) | 中 |
| P61 | `model/channel_cache.go` | +5 | 内存缓存选路 `GetRandomSatisfiedChannel` 同样跳过 | 中 |

### P59 — 记录冷却

`ProcessChannelError` 在写完渠道错误日志之后追加：

```go
	if modelName := relayInfo.GetOriginModelName(); modelName != "" {
		if until, parked := model.MarkChannelModelCooldownFromError(channelError.ChannelId, modelName, err.StatusCode, err.MaskSensitiveErrorWithStatusCode()); parked {
			logger.LogWarn(c, common.LogText("channel #%d parked for model %s until %s after a quota rejection", channelError.ChannelId, modelName, until.Format(time.RFC3339)))
		}
	}
```

只在 429 且消息命中配额关键词（`individual quota reached` / `resource has been exhausted` /
`quota exceeded` / `exceeded your current quota` / `insufficient_quota` / `insufficient credits`）时记录；
纯速率限制（`Rate limit reached`）不触发。冷却时长取上游 `Resets in 4h29m56s`，加 30s 缓冲，上限 6 小时；
消息里没有重置时间时用默认 5 分钟。

### P60 / P61 — 选路时跳过

```go
	// model/ability.go
	abilities = dropCoolingDownAbilities(abilities, model)
```

```go
	// model/channel_cache.go
	channels = dropCoolingDownChannels(channels, model)
```

两个入口都要加：`MEMORY_CACHE_ENABLED=false`（tx101 当前配置）走 `ability.go`，
`true` 走 `channel_cache.go`。**注意 `abilities.enabled` 不能替代本机制**——内存缓存
`group2model2channels` 是按 `channels.models` 构建的，根本不读 `abilities.enabled`，
所以在开启内存缓存的部署里改 ability 不生效。

**全部候选都在冷却时保留原列表**，让请求仍然打到上游并返回真实的 429，
而不是变成 `no available channel`。

### 已知边界

- 冷却表是进程内 `sync.Map`，单节点自动过期；多节点部署各自学习（tx101 是单节点）。
- 键用**请求模型名**（`OriginModelName`），与选路使用的模型名一致；同一上游模型的不同别名
  各自独立冷却。
- 重启后冷却丢失，会重新学习一次。

---

## 配额快照与看板（P62–P65，2026-10-17）

> 之前只有"被 429 打过才知道账号耗尽"，管理员看不到"什么时候恢复"。上游的
> `v1internal:fetchAvailableModels` 是**免费的配额查询接口**，返回每个模型的
> `remainingFraction` + `resetTime`。本组改动把它定时落库、进入调度、并在控制台展示。

新增文件（不在锚点清单内）：`model/channel_model_quota.go`、`model/channel_model_quota_test.go`、
`service/antigravity_quota_sync.go`、`service/antigravity_quota_sync_test.go`、
`web/src/features/channels/components/antigravity-quota-board.tsx`。

| ID | 文件 | 行数 | 目的 | 冲突风险 |
| --- | --- | --- | --- | --- |
| P62 | `model/main.go` | +1 | 注册 `ChannelModelQuota` 迁移 | 中（迁移列表 churn） |
| P63 | `model/system_task.go` | +3 | 注册配额同步任务类型 | 低 |
| P64 | `main.go` | +5 | 启动时预热配额镜像 | 低 |
| P65 | `router/channel-router.go` | +1 | 配额总览路由 | 低 |

### 数据模型

`channel_model_quotas` 表按 **(channel_id, model)** 唯一索引存一行快照：

```
remaining_fraction float64   -- quotaInfo.remainingFraction，缺省即 0
reset_time         bigint    -- 绝对 Unix 秒；0 = 上游未给
status             varchar   -- ok | error
fetched_at         bigint    -- 用于判断快照是否过期
```

**关键语义**：账号耗尽时上游仍然返回 `quotaInfo`，只是省掉了 `remainingFraction`。
所以"有 quotaInfo 但无 remainingFraction"必须写成 0，而不是跳过——跳过会让路由
继续把流量打进一个已经打满的账号。模型完全没有 `quotaInfo` 时才跳过（该账号不提供该模型）。

### 调度接入（沿用 P60/P61 的入口）

P60/P61 的两个筛选函数现在同时看两个来源：请求时观察到的 429 冷却，以及这里的定时快照。

```go
// every name the pair may be parked under: the id the request is forwarded as
// after following the channel's model_mapping, plus the name the client asked
// for (both the 429 lot and the quota snapshot key off the forwarded id)
func channelModelCooldownNames(channelID int, model string) []string { ... }

func channelModelUnavailable(channelID int, model string, now time.Time) bool {
	for _, name := range channelModelCooldownNames(channelID, model) {
		if ChannelModelCoolingDown(channelID, name) { return true }
		if exhausted, _ := ChannelModelQuotaExhausted(channelID, name, now); exhausted { return true }
	}
	return false
}
```

`ChannelModelUnavailableUntil` 共用同一份名单（2026-10-26 修正）：此前只有可用性检查解析了别名，
被停放的 pair 虽然会被跳过，但它恢复的 deadline 查不到，调用方拿不到最早的复位时间，
于是本可以带 `Retry-After` 的 429 退化成裸 503。

**全部失败开放（fail-open）**，任一条件不满足就不拦：

- 没有该 (渠道, 模型) 的快照
- 快照 `status != ok`（上次轮询失败）
- 快照超过 `ChannelModelQuotaMaxAge`（45 分钟）
- `reset_time` 为 0（没有复位时钟，不能永久停放）
- `reset_time` 已过期（窗口已滚动）

阈值 `ChannelModelQuotaExhaustedBelow = 0.01`：上游对已打满账号报的是"极小但非零"的
比例（实测 0.0001–0.009），用 `== 0` 判断会漏掉它们。

### 已知边界

- 快照镜像在内存里，重启后由 `LoadChannelModelQuotaCache()` 从表恢复（P64）。
- 轮询 15 分钟一次、单账号 30 秒超时、并发 4，避免大批量账号同时打上游。
- 路由匹配用的是**请求模型名**；别名与上游 id 不一致时查不到快照，按失败开放处理。
- 多渠道（multi-key）账号暂不采集，避免把一行 key 展开成多份配额互相覆盖。

---

---

## 分层例外：pkg/antigravity 的 JSON 包装（P67，2026-10-26）

`AGENTS.md` 要求根模块的 JSON 编解码统一走 `common.*`，而 `pkg/antigravity` 是协议核心，
被 `pkg/antigravity/architecture_test.go`（`go list -deps`）禁止依赖任何
`github.com/QuantumNous/new-api/*` 包，因此不能引用 `common`。

处理方式：在 `pkg/antigravity/json.go` 放两个薄包装 `marshalJSON` / `unmarshalJSON`，
包内其余文件只调用这两个函数，`encoding/json` 只出现在这一个文件里。这样分层不变量与
“编解码集中一处”两条都成立，规则例外只有一个有名字的落点。

| ID | 文件 | 行数 | 目的 | 冲突风险 |
| --- | --- | --- | --- | --- |
| P67 | `pkg/antigravity/json.go` | 新增文件 | 协议核心的 JSON 入口 | 无（新文件，不参与合并冲突） |

**约束**：不得把 `common` 引入 `pkg/antigravity`（架构测试会红）；也不得在其他文件重新出现
`json.Marshal` / `json.Unmarshal` 调用——需要时用 `pkg/antigravity/json.go` 的包装。
测试文件（`*_test.go`）保留标准库调用，用于独立校验线上报文形状。

---

## 治理文件（P66，2026-10-26）

本 fork 收紧了首屏体积与前端发布口径，规则写在仓库根的 `AGENTS.md` 与 `web/AGENTS.md`。
这两个文件是**上游文件**，本 fork 只在其中追加条目，不改写既有条目。

| ID | 文件 | 行数 | 目的 | 冲突风险 |
| --- | --- | --- | --- | --- |
| P66 | `AGENTS.md` | +2 | 首屏体积预算（禁止静态引入语言包/大 JSON 进入口）、`web/dist` 随二进制发布 | 中（上游会改这份文件） |
| P66 | `web/AGENTS.md` | +23 | 3.1 语言包按需加载、3.4 首屏预算与基线数字、3.16 发布与校验步骤 | 中 |

**冲突处理**：上游若改写这两个文件，按**两边都保留**处理——本 fork 只新增列表项与小节，
不替换既有规则。合并后复核三件事：

- `web/src/i18n/config.ts` 仍通过 `LOCALE_BUNDLES` 动态 `import()` 装载语言包，入口链路没有静态语言 JSON。
- 体积核对按 `web/AGENTS.md` 3.4 执行并更新基线数字：`bun run build` 后记录 `dist/static/js/index.*.js` 的原始与 gzip 字节数。
- 发布口径仍是“重编 Go 二进制与镜像”，`main.go` 保留 `//go:embed web/dist` 与 `//go:embed web/dist/index.html`。

**补丁预算**：`AGENTS.md` +2 行、`web/AGENTS.md` +23 行。锚点见 `upgrade-check.ps1` 的 P66a–P66e。

---

## 升级操作流程

```powershell
# 1. 拉取上游并变基
git fetch origin
git rebase origin/main

# 2. 冲突处理：本清单全部是"纯追加"，一般选择"两边都保留"
#    若 P1 的类型号被占用，按 P1 的说明改号并同步全部引用

# 3. 自检
pwsh fork-integration/upgrade-check.ps1

# 4. 若自检失败，按输出里的 "-> 修复指引" 逐条处理，然后重复第 3 步
```

## VERSION 发布标记（每次合并后更新）

`VERSION` 不是补丁锚点，但每次合并上游后必须改：它是 `-X .../common.Version` 的取值来源
（`Dockerfile:7-8,29`、`Makefile:18`），空文件会让 `/api/status` 返回 `v0.0.0`，控制台显示
“Unknown version”。格式固定为 `<已合并的上游 tag>+fork`（例如 `v1.0.0-rc.47+fork`）：
`+fork` 是 semver 构建元数据，前端 `web/src/features/system-update/releases.ts` 的
`parseSystemVersion` 会忽略它，不会误报“有更新”；同时它在版本 pill 的 128px 宽度内能完整显示
（`v1.0.0-rc.47+fork.20261026` 实测 171.5px 会被截断，`v1.0.0-rc.47+fork` 是 112.2px）。
`upgrade-check.ps1` 的第 4 层会拦住空文件和前缀过期两种情况。

## Patch budget 记录（每次升级后更新）

| 分组 | Antigravity（2026-10-10） | WorkBuddy（2026-10-11） | OpenCode（2026-10-16） | Command Code（2026-10-17） |
| --- | --- | --- | --- | --- |
| Go 核心注册文件 | 23 行 | 20 行（9 处注册） | 约 30 行（含校验与模型同步） | 约 34 行（8 处注册） |
| 前端 feature 文件 | 136 行 | 约 91 行（5 个文件） | 约 220 行（含面板与导入） | 约 260 行（含面板、校验与导入） |
| i18n locale 文件 | 每文件 +23/-1 行 | 每文件 +16/-1 行 | 每文件 +17 行 | 每文件 +25 行 |
| 既有文件合计 | 20 个文件、约 330 行插入 | 14 个既有文件、约 127 行插入 | 约 19 个既有文件、约 270 行插入 | 约 18 个既有文件、约 300 行插入 |

### rc.43 → rc.44（2026-10-14）

本版上游改了 130 个文件，但与 fork 改动**只交集 8 个**：`relay/common/relay_info.go`（P8 / P24）
与 7 个 locale 文件（P16 / P31）。**补丁预算不变，锚点数量仍是 31，无一条需要改号。**

唯一新增的适配在测试基建而非业务代码：`web/src/test-setup.ts` 增加条件式
`BASE_UI_ANIMATIONS_DISABLED` getter，抵消 rc.44 `@base-ui` 在 jsdom 下等待动画结束
才卸载浮层的新行为。该文件是上游文件，升级时若报冲突按「保留 getter 语义」处理。
