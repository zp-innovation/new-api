#Requires -Version 7.0
<#
.SYNOPSIS
    Verifies that the Antigravity, WorkBuddy, OpenCode, and Command Code integrations survived an upstream new-api merge.

.DESCRIPTION
    Runs four layers of checks:

      1. Anchor check   - every line listed in fork-integration/UPSTREAM_PATCHES.md
                          is still present in the patched upstream files.
      2. Guard tests    - the Go guard tests (wiring, permissions, architecture).
      3. i18n check     - the locale set still has no missing keys.
      4. Version stamp  - VERSION still carries the merged upstream tag plus the
                          +fork build marker that the console displays.

    Exit code 0 means the integration is intact; 1 lists every failed check with
    the repair hint from UPSTREAM_PATCHES.md.

.PARAMETER GoExe
    Go executable to use. Defaults to "go" on PATH.

.PARAMETER SkipTests
    Only run the anchor and i18n checks (fast path while resolving conflicts).

.EXAMPLE
    pwsh fork-integration/upgrade-check.ps1
#>
[CmdletBinding()]
param(
    [string]$GoExe = 'go',
    [switch]$SkipTests
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
# Native commands must not abort the script on a non-zero exit code; the exit
# code is inspected explicitly instead.
if (Test-Path variable:PSNativeCommandUseErrorActionPreference) {
    $PSNativeCommandUseErrorActionPreference = $false
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot

$failures = [System.Collections.Generic.List[string]]::new()

function Test-Anchor {
    param(
        [Parameter(Mandatory)][string]$Id,
        [Parameter(Mandatory)][string]$File,
        [Parameter(Mandatory)][string]$Pattern,
        [Parameter(Mandatory)][string]$Fix
    )

    $path = Join-Path $repoRoot $File
    if (-not (Test-Path -LiteralPath $path)) {
        Write-Host ("FAIL  {0}  {1}  (file not found)" -f $Id, $File) -ForegroundColor Red
        $failures.Add("$Id $File -> $Fix")
        return
    }

    if (Select-String -Path $path -SimpleMatch -Pattern $Pattern -Quiet) {
        Write-Host ("ok    {0}  {1}" -f $Id, $File) -ForegroundColor Green
        return
    }

    Write-Host ("FAIL  {0}  {1}" -f $Id, $File) -ForegroundColor Red
    Write-Host ("      -> {0}" -f $Fix) -ForegroundColor Yellow
    $failures.Add("$Id $File -> $Fix")
}

Write-Host "== 1/4 anchor check ==" -ForegroundColor Cyan

Test-Anchor -Id 'P1'  -File 'constant/channel.go'        -Pattern 'ChannelTypeAntigravity    = 64' -Fix 'UPSTREAM_PATCHES P1: re-add the channel type number before ChannelTypeDummy (renumber if upstream claimed it)'
Test-Anchor -Id 'P2'  -File 'constant/channel.go'        -Pattern 'ChannelTypeAntigravity:    "Google Antigravity"' -Fix 'UPSTREAM_PATCHES P2: re-add the ChannelTypeNames entry'
Test-Anchor -Id 'P3'  -File 'constant/channel.go'        -Pattern '"https://daily-cloudcode-pa.googleapis.com"' -Fix 'UPSTREAM_PATCHES P3: re-add the ChannelBaseURLs entry at the index matching P1 (the daily- host, not cloudcode-pa: the production host answers 429 RESOURCE_EXHAUSTED for free-tier accounts)'
Test-Anchor -Id 'P4'  -File 'constant/api_type.go'       -Pattern 'APITypeAntigravity' -Fix 'UPSTREAM_PATCHES P4: re-add APITypeAntigravity before APITypeDummy'
Test-Anchor -Id 'P5'  -File 'common/api_type.go'         -Pattern 'apiType = constant.APITypeAntigravity' -Fix 'UPSTREAM_PATCHES P5: re-add the ChannelType2APIType case (missing it silently routes traffic to the OpenAI adaptor)'
Test-Anchor -Id 'P6'  -File 'common/endpoint_type.go'    -Pattern 'case constant.ChannelTypeAntigravity:' -Fix 'UPSTREAM_PATCHES P6: re-add the GetEndpointTypesByChannelType case'
Test-Anchor -Id 'P7a' -File 'relay/relay_adaptor.go'     -Pattern 'relay/channel/antigravity' -Fix 'UPSTREAM_PATCHES P7: re-add the antigravity import'
Test-Anchor -Id 'P7b' -File 'relay/relay_adaptor.go'     -Pattern 'return &antigravity.Adaptor{}' -Fix 'UPSTREAM_PATCHES P7: re-add the GetAdaptor case'
Test-Anchor -Id 'P8'  -File 'relay/common/relay_info.go' -Pattern 'constant.ChannelTypeAntigravity:    true,' -Fix 'UPSTREAM_PATCHES P8: re-add the streamSupportedChannels entry (streaming usage breaks without it)'
Test-Anchor -Id 'P9'  -File 'router/channel-router.go'   -Pattern '/oauth/antigravity/auth-url' -Fix 'UPSTREAM_PATCHES P9: re-add the three OAuth routes'
Test-Anchor -Id 'P10' -File 'router/channel-router.go'   -Pattern '/:id/antigravity/refresh' -Fix 'UPSTREAM_PATCHES P10: re-add the two channel operation routes'
Test-Anchor -Id 'P11' -File 'main.go'                    -Pattern 'service.StartAntigravityCredentialAutoRefreshTask()' -Fix 'UPSTREAM_PATCHES P11: re-add the auto-refresh task start'
Test-Anchor -Id 'P12' -File 'web/src/features/channels/constants.ts' -Pattern 'export const CHANNEL_TYPE_ANTIGRAVITY = 64' -Fix 'UPSTREAM_PATCHES P12: re-add the frontend channel type constant and its three map entries'
Test-Anchor -Id 'P13' -File 'web/src/features/channels/lib/channel-form.ts' -Pattern 'function isAntigravityCredential' -Fix 'UPSTREAM_PATCHES P13: re-add the credential validator and validation branch'
Test-Anchor -Id 'P14' -File 'web/src/features/channels/api.ts' -Pattern 'refreshAntigravityChannelCredential' -Fix 'UPSTREAM_PATCHES P14: re-add the Antigravity API wrappers'
Test-Anchor -Id 'P15' -File 'web/src/features/channels/components/drawers/channel-mutate-drawer.tsx' -Pattern 'AntigravityOAuthPanel' -Fix 'UPSTREAM_PATCHES P15: re-add the drawer import and panel block'

Test-Anchor -Id 'P17' -File 'constant/channel.go'        -Pattern 'ChannelTypeWorkBuddy      = 65' -Fix 'UPSTREAM_PATCHES P17: re-add the WorkBuddy channel type number before ChannelTypeDummy (renumber every P17-P31 reference if upstream claimed 65)'
Test-Anchor -Id 'P18' -File 'constant/channel.go'        -Pattern 'ChannelTypeWorkBuddy:      "WorkBuddy"' -Fix 'UPSTREAM_PATCHES P18: re-add the ChannelTypeNames entry'
Test-Anchor -Id 'P19' -File 'constant/channel.go'        -Pattern '"https://www.workbuddy.ai"' -Fix 'UPSTREAM_PATCHES P19: re-add the ChannelBaseURLs entry at the index matching P17'
Test-Anchor -Id 'P20' -File 'constant/api_type.go'       -Pattern 'APITypeWorkBuddy' -Fix 'UPSTREAM_PATCHES P20: re-add APITypeWorkBuddy before APITypeDummy'
Test-Anchor -Id 'P21' -File 'common/api_type.go'         -Pattern 'apiType = constant.APITypeWorkBuddy' -Fix 'UPSTREAM_PATCHES P21: re-add the ChannelType2APIType case (missing it silently routes traffic to the OpenAI adaptor)'
Test-Anchor -Id 'P22' -File 'common/endpoint_type.go'    -Pattern 'case constant.ChannelTypeWorkBuddy:' -Fix 'UPSTREAM_PATCHES P22: re-add the GetEndpointTypesByChannelType case'
Test-Anchor -Id 'P23a' -File 'relay/relay_adaptor.go'    -Pattern 'relay/channel/workbuddy' -Fix 'UPSTREAM_PATCHES P23: re-add the workbuddy import'
Test-Anchor -Id 'P23b' -File 'relay/relay_adaptor.go'    -Pattern 'return &workbuddy.Adaptor{}' -Fix 'UPSTREAM_PATCHES P23: re-add the GetAdaptor case'
Test-Anchor -Id 'P24' -File 'relay/common/relay_info.go' -Pattern 'constant.ChannelTypeWorkBuddy:      true,' -Fix 'UPSTREAM_PATCHES P24: re-add the streamSupportedChannels entry (streaming usage breaks without it)'
Test-Anchor -Id 'P25' -File 'router/channel-router.go'   -Pattern '/oauth/workbuddy/auth-url' -Fix 'UPSTREAM_PATCHES P25: re-add the three device-login routes'
Test-Anchor -Id 'P26' -File 'web/src/features/channels/constants.ts' -Pattern 'export const CHANNEL_TYPE_WORKBUDDY = 65' -Fix 'UPSTREAM_PATCHES P26: re-add the frontend channel type constant and its four map entries'
Test-Anchor -Id 'P27' -File 'web/src/features/channels/lib/channel-utils.ts' -Pattern "65: 'CodeBuddy'" -Fix 'UPSTREAM_PATCHES P27: re-add the icon mapping'
Test-Anchor -Id 'P28' -File 'web/src/features/channels/lib/channel-type-config.ts' -Pattern '[CHANNEL_TYPE_WORKBUDDY]: {' -Fix 'UPSTREAM_PATCHES P28: re-add the channel type config entry'
Test-Anchor -Id 'P29' -File 'web/src/features/channels/api.ts' -Pattern 'startWorkBuddyLogin' -Fix 'UPSTREAM_PATCHES P29: re-add the WorkBuddy device-login API wrappers'
Test-Anchor -Id 'P30' -File 'web/src/features/channels/components/drawers/channel-mutate-drawer.tsx' -Pattern 'WorkBuddyOAuthPanel' -Fix 'UPSTREAM_PATCHES P30: re-add the drawer import and panel block'
Test-Anchor -Id 'P32' -File 'web/src/features/channels/components/drawers/channel-mutate-drawer.tsx' -Pattern "const drawerSide = 'right'" -Fix 'UPSTREAM_PATCHES P32: re-pin the channel sheet to the right for edits as well as creates'

Test-Anchor -Id 'P33' -File 'constant/channel.go'        -Pattern 'ChannelTypeOpenCode       = 66' -Fix 'UPSTREAM_PATCHES P33: re-add the OpenCode channel type number before ChannelTypeDummy (renumber if upstream claimed 66)'
Test-Anchor -Id 'P34' -File 'constant/channel.go'        -Pattern 'ChannelTypeOpenCode:       "OpenCode"' -Fix 'UPSTREAM_PATCHES P34: re-add the ChannelTypeNames entry'
Test-Anchor -Id 'P35' -File 'constant/channel.go'        -Pattern '"https://opencode.ai/zen/v1"' -Fix 'UPSTREAM_PATCHES P35: re-add the ChannelBaseURLs entry at the index matching P33'
Test-Anchor -Id 'P36' -File 'constant/api_type.go'       -Pattern 'APITypeOpenCode' -Fix 'UPSTREAM_PATCHES P36: re-add APITypeOpenCode before APITypeDummy'
Test-Anchor -Id 'P37' -File 'common/api_type.go'         -Pattern 'apiType = constant.APITypeOpenCode' -Fix 'UPSTREAM_PATCHES P37: re-add the ChannelType2APIType case (missing it silently routes traffic to the OpenAI adaptor)'
Test-Anchor -Id 'P38' -File 'common/endpoint_type.go'    -Pattern 'case constant.ChannelTypeOpenCode:' -Fix 'UPSTREAM_PATCHES P38: re-add the GetEndpointTypesByChannelType case'
Test-Anchor -Id 'P39a' -File 'relay/relay_adaptor.go'    -Pattern 'relay/channel/opencode' -Fix 'UPSTREAM_PATCHES P39: re-add the opencode import'
Test-Anchor -Id 'P39b' -File 'relay/relay_adaptor.go'    -Pattern 'return &opencode.Adaptor{}' -Fix 'UPSTREAM_PATCHES P39: re-add the GetAdaptor case'
Test-Anchor -Id 'P40' -File 'relay/common/relay_info.go' -Pattern 'constant.ChannelTypeOpenCode:       true,' -Fix 'UPSTREAM_PATCHES P40: re-add the streamSupportedChannels entry (streaming usage breaks without it)'
Test-Anchor -Id 'P41' -File 'router/channel-router.go'   -Pattern '/oauth/opencode/auth-url' -Fix 'UPSTREAM_PATCHES P41: re-add the three device-login routes'
Test-Anchor -Id 'P41b' -File 'router/channel-router.go'  -Pattern '/:id/opencode/usage' -Fix 'UPSTREAM_PATCHES P41: re-add the channel refresh/model/usage routes'
Test-Anchor -Id 'P42' -File 'main.go'                    -Pattern 'service.StartOpenCodeCredentialAutoRefreshTask()' -Fix 'UPSTREAM_PATCHES P42: re-add the auto-refresh task start'
Test-Anchor -Id 'P43' -File 'controller/channel.go'      -Pattern 'occore.ImportAuthExport' -Fix 'UPSTREAM_PATCHES P43: re-add the OpenCode channel key validation branch'
Test-Anchor -Id 'P44' -File 'controller/channel_upstream_update.go' -Pattern 'GetOpenCodeChannelModelsForChannel' -Fix 'UPSTREAM_PATCHES P44: re-add the OpenCode upstream model discovery branch'
Test-Anchor -Id 'P45' -File 'service/error.go'        -Pattern 'OpenCode free tier is restricted to the official OpenCode client' -Fix 'UPSTREAM_PATCHES P45: re-add the FreeTierError classification so the channel is not retried or auto-disabled'

Test-Anchor -Id 'P46' -File 'constant/channel.go'        -Pattern 'ChannelTypeCommandCode    = 67' -Fix 'UPSTREAM_PATCHES P46: re-add the Command Code channel type number before ChannelTypeDummy (renumber if upstream claimed 67)'
Test-Anchor -Id 'P47' -File 'constant/channel.go'        -Pattern 'ChannelTypeCommandCode:    "Command Code"' -Fix 'UPSTREAM_PATCHES P47: re-add the ChannelTypeNames entry'
Test-Anchor -Id 'P48' -File 'constant/channel.go'        -Pattern '"https://api.commandcode.ai/provider/v1"' -Fix 'UPSTREAM_PATCHES P48: re-add the ChannelBaseURLs entry at the index matching P46'
Test-Anchor -Id 'P49a' -File 'constant/api_type.go'      -Pattern 'APITypeCommandCode' -Fix 'UPSTREAM_PATCHES P49: re-add APITypeCommandCode before APITypeDummy'
Test-Anchor -Id 'P49b' -File 'common/api_type.go'        -Pattern 'apiType = constant.APITypeCommandCode' -Fix 'UPSTREAM_PATCHES P49: re-add the ChannelType2APIType case (missing it silently routes traffic to the OpenAI adaptor)'
Test-Anchor -Id 'P50' -File 'common/endpoint_type.go'    -Pattern 'case constant.ChannelTypeCommandCode:' -Fix 'UPSTREAM_PATCHES P50: re-add the GetEndpointTypesByChannelType case'
Test-Anchor -Id 'P51a' -File 'relay/relay_adaptor.go'    -Pattern 'relay/channel/commandcode' -Fix 'UPSTREAM_PATCHES P51: re-add the commandcode import'
Test-Anchor -Id 'P51b' -File 'relay/relay_adaptor.go'    -Pattern 'return &commandcode.Adaptor{}' -Fix 'UPSTREAM_PATCHES P51: re-add the GetAdaptor case'
Test-Anchor -Id 'P51c' -File 'relay/common/relay_info.go' -Pattern 'constant.ChannelTypeCommandCode:    true,' -Fix 'UPSTREAM_PATCHES P51: re-add the streamSupportedChannels entry (streaming usage breaks without it)'
Test-Anchor -Id 'P52a' -File 'router/channel-router.go'  -Pattern '/oauth/command-code/callback' -Fix 'UPSTREAM_PATCHES P52: re-add the public Studio callback route before AdminAuth'
Test-Anchor -Id 'P52b' -File 'router/channel-router.go'  -Pattern '/oauth/command-code/auth-url' -Fix 'UPSTREAM_PATCHES P52: re-add the Command Code OAuth and channel operation routes'
Test-Anchor -Id 'P53' -File 'controller/channel_upstream_update.go' -Pattern 'GetCommandCodeChannelModelsForChannel' -Fix 'UPSTREAM_PATCHES P53: re-add the Command Code upstream model discovery branch'

Test-Anchor -Id 'P54' -File 'web/src/features/channels/constants.ts' -Pattern 'CHANNEL_TYPE_COMMAND_CODE = 67' -Fix 'UPSTREAM_PATCHES frontend: re-add the Command Code channel constant'
Test-Anchor -Id 'P55' -File 'web/src/features/channels/lib/channel-utils.ts' -Pattern "67: 'CommandCode'" -Fix 'UPSTREAM_PATCHES frontend: re-add the Command Code icon mapping'
Test-Anchor -Id 'P56' -File 'web/src/features/channels/lib/channel-type-config.ts' -Pattern '[CHANNEL_TYPE_COMMAND_CODE]: {' -Fix 'UPSTREAM_PATCHES frontend: re-add the Command Code channel type config'
Test-Anchor -Id 'P57' -File 'web/src/features/channels/api.ts' -Pattern 'startCommandCodeLogin' -Fix 'UPSTREAM_PATCHES frontend: re-add the Command Code API wrappers'
Test-Anchor -Id 'P58' -File 'web/src/features/channels/components/drawers/channel-mutate-drawer.tsx' -Pattern 'CommandCodeOAuthPanel' -Fix 'UPSTREAM_PATCHES frontend: re-add the Command Code panel and import-account condition'

Test-Anchor -Id 'P59' -File 'service/relay_error.go' -Pattern 'model.MarkChannelModelCooldownFromError' -Fix 'UPSTREAM_PATCHES P59: re-add the per-(channel, model) quota cooldown mark in ProcessChannelError'
Test-Anchor -Id 'P60' -File 'model/ability.go'       -Pattern 'dropCoolingDownAbilities(abilities, model)' -Fix 'UPSTREAM_PATCHES P60: re-add the cooldown filter in GetChannel (database selection path)'
Test-Anchor -Id 'P61' -File 'model/channel_cache.go' -Pattern 'dropCoolingDownChannels(channels, model)' -Fix 'UPSTREAM_PATCHES P61: re-add the cooldown filter in GetRandomSatisfiedChannel (memory-cache selection path)'

Test-Anchor -Id 'P62' -File 'model/main.go'          -Pattern '&ChannelModelQuota{},' -Fix 'UPSTREAM_PATCHES P62: re-add the ChannelModelQuota migration'
Test-Anchor -Id 'P63' -File 'model/system_task.go'   -Pattern 'SystemTaskTypeChannelModelQuotaSync' -Fix 'UPSTREAM_PATCHES P63: re-add the quota sync system task type'
Test-Anchor -Id 'P64' -File 'main.go'                -Pattern 'model.LoadChannelModelQuotaCache()' -Fix 'UPSTREAM_PATCHES P64: re-add the quota cache warm-up at start-up'
Test-Anchor -Id 'P65' -File 'router/channel-router.go' -Pattern '/antigravity/quotas' -Fix 'UPSTREAM_PATCHES P65: re-add the Antigravity quota overview route'

Test-Anchor -Id 'P67' -File 'pkg/antigravity/json.go' -Pattern 'func marshalJSON(value any) ([]byte, error)' -Fix 'UPSTREAM_PATCHES P67: keep the protocol core free of host imports by routing JSON through pkg/antigravity/json.go instead of common'

Test-Anchor -Id 'P66a' -File 'AGENTS.md'     -Pattern '- **First-paint weight (mandatory):**' -Fix 'UPSTREAM_PATCHES P66: re-add the first-paint weight rule (no static locale JSON in the entry chunk; record the entry chunk size after a production build)'
Test-Anchor -Id 'P66b' -File 'AGENTS.md'     -Pattern 'is embedded into the Go binary' -Fix 'UPSTREAM_PATCHES P66: re-add the note that web/dist ships inside the Go binary'
Test-Anchor -Id 'P66c' -File 'web/AGENTS.md' -Pattern '**语言包按需加载（强制）**' -Fix 'UPSTREAM_PATCHES P66: re-add section 3.1 lazy locale loading rules (LOCALE_BUNDLES / loadLanguageBundle / changeInterfaceLanguage / i18nReady)'
Test-Anchor -Id 'P66d' -File 'web/AGENTS.md' -Pattern '**首屏体积预算（强制）**' -Fix 'UPSTREAM_PATCHES P66: re-add section 3.4 first-paint budget (entry chunk <= 300 KB gzip, first visit JS <= 1.2 MB)'
Test-Anchor -Id 'P66e' -File 'web/AGENTS.md' -Pattern '**前端产物随二进制发布**' -Fix 'UPSTREAM_PATCHES P66: re-add section 3.16 release and verification steps for the embedded web/dist'

if (-not $SkipTests) {
    Write-Host "`n== 2/4 guard tests ==" -ForegroundColor Cyan
    & $GoExe test -count=1 ./pkg/antigravity/ ./relay/channel/antigravity/ ./pkg/workbuddy/ ./relay/channel/workbuddy/ ./pkg/opencode/ ./relay/channel/opencode/ ./relay/ ./router/ ./model/
    if ($LASTEXITCODE -ne 0) {
        Write-Host 'FAIL  guard tests' -ForegroundColor Red
        $failures.Add('guard tests -> run "go test ./pkg/antigravity/ ./relay/channel/antigravity/ ./pkg/workbuddy/ ./relay/channel/workbuddy/ ./pkg/opencode/ ./relay/channel/opencode/ ./relay/ ./router/ ./model/" and inspect the first compile error; the adaptor layers are relay/channel/antigravity/adaptor.go, relay/channel/workbuddy/adaptor.go, and relay/channel/opencode/adaptor.go')
    } else {
        Write-Host 'ok    guard tests' -ForegroundColor Green
    }

    Write-Host "`n== 2b/4 vet + format ==" -ForegroundColor Cyan
    & $GoExe vet ./pkg/antigravity/ ./relay/channel/antigravity/ ./pkg/workbuddy/ ./relay/channel/workbuddy/ ./pkg/opencode/ ./relay/channel/opencode/
    if ($LASTEXITCODE -ne 0) {
        $failures.Add('go vet -> fix the reported diagnostics in the antigravity packages')
    }
    $unformatted = & $GoExe fmt ./pkg/antigravity/ ./relay/channel/antigravity/ ./pkg/workbuddy/ ./relay/channel/workbuddy/ ./pkg/opencode/ ./relay/channel/opencode/ 2>&1
    if ($unformatted) {
        Write-Host 'note  go fmt rewrote files (review the diff)' -ForegroundColor Yellow
    }
} else {
    Write-Host "`n== 2/4 guard tests (skipped) ==" -ForegroundColor DarkGray
}

Write-Host "`n== 3/4 i18n check ==" -ForegroundColor Cyan
$localeDir = Join-Path $repoRoot 'web/src/i18n/locales'
$requiredKeys = @(
    'Google Antigravity',
    'Antigravity credentials created',
    'Antigravity credentials refreshed',
    'Antigravity quota fetched',
    'WorkBuddy',
    'WorkBuddy credentials created',
    'OpenCode',
    'OpenCode credentials created',
    'OpenCode usage'
)
$enPath = Join-Path $localeDir 'en.json'
if (-not (Test-Path -LiteralPath $enPath)) {
    $failures.Add('i18n -> web/src/i18n/locales/en.json is missing')
} else {
    $en = Get-Content -LiteralPath $enPath -Raw
    foreach ($key in $requiredKeys) {
        if ($en.Contains('"' + $key + '"')) {
            Write-Host ("ok    key: {0}" -f $key) -ForegroundColor Green
        } else {
            Write-Host ("FAIL  key: {0}" -f $key) -ForegroundColor Red
            $failures.Add("i18n $key -> re-apply the locale keys via web/scripts/add-missing-keys.mjs (append-only); do NOT run node scripts/sync-i18n.mjs, it reorders the whole locale file")
        }
    }

    # The locale files are append-ordered, so sync-i18n.mjs is deliberately NOT run here:
    # it rewrites every locale in base-key order (web/scripts/sync-i18n.mjs, the writeFile
    # right after reorderLikeBase) and would turn a clean tree into a thousand-line diff.
    # The missing-key check is done in-process instead, and never writes a file.
    # -AsHashtable: the locale files contain keys that differ only by case
    # ('1 hour' vs '1 Hour'), which ConvertFrom-Json's case-insensitive object mode rejects.
    $enJson = Get-Content -LiteralPath $enPath -Raw | ConvertFrom-Json -AsHashtable
    $enKeys = @($enJson['translation'].Keys)
    foreach ($locale in @('zh', 'zh-TW', 'ja', 'fr', 'ru', 'vi')) {
        $localePath = Join-Path $localeDir "$locale.json"
        if (-not (Test-Path -LiteralPath $localePath)) {
            Write-Host ("FAIL  {0} locale file not found" -f $locale) -ForegroundColor Red
            $failures.Add("i18n $locale -> restore web/src/i18n/locales/$locale.json")
            continue
        }
        $localeJson = Get-Content -LiteralPath $localePath -Raw | ConvertFrom-Json -AsHashtable
        $localeTranslation = $localeJson['translation']
        $missing = @($enKeys | Where-Object { -not $localeTranslation.ContainsKey($_) })
        if ($missing.Count -eq 0) {
            Write-Host ("ok    {0} has all {1} keys" -f $locale, $enKeys.Count) -ForegroundColor Green
        } else {
            Write-Host ("FAIL  {0} is missing {1} key(s), first: {2}" -f $locale, $missing.Count, $missing[0]) -ForegroundColor Red
            $failures.Add("i18n $locale missing=$($missing.Count) -> append the missing keys with web/scripts/add-missing-keys.mjs (append-only); do NOT run sync-i18n.mjs")
        }
    }
}

Write-Host "`n== 4/4 release version ==" -ForegroundColor Cyan
# /api/status 的 data.version 取自 common.Version，构建时由 Dockerfile:29 / Makefile:18 从
# VERSION 用 -X 注入；文件为空时回落 "v0.0.0"，控制台会渲染成 Unknown version。
$versionPath = Join-Path $repoRoot 'VERSION'
# 空文件时 Get-Content -Raw 返回 $null，StrictMode 下不能直接 .Trim()。
$versionRaw = if (Test-Path -LiteralPath $versionPath) { Get-Content -LiteralPath $versionPath -Raw } else { '' }
$version = if ($versionRaw) { $versionRaw.Trim() } else { '' }
if ($version -notmatch '^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?\+[0-9A-Za-z.-]+$') {
    Write-Host ("FAIL  VERSION: '{0}'" -f $version) -ForegroundColor Red
    $failures.Add('VERSION -> set it to "<merged upstream tag>+fork" (e.g. v1.0.0-rc.47+fork); an empty file ships v0.0.0 and the console shows Unknown version')
} else {
    $versionTag = ($version -split '\+')[0]
    $git = Get-Command git -ErrorAction SilentlyContinue
    # 最近的 tag 就是上一次合并进来的上游版本，前缀必须与它一致，否则页面显示的"正常版本"是过期值。
    $nearestTag = if ($git) { @(& git describe --tags --abbrev=0 2>$null)[0] } else { $null }
    if ($git -and $nearestTag -and $nearestTag -ne $versionTag) {
        Write-Host ("FAIL  VERSION: {0} (nearest tag {1})" -f $version, $nearestTag) -ForegroundColor Red
        $failures.Add("VERSION -> bump the prefix to the merged upstream tag ($nearestTag) before releasing")
    } else {
        Write-Host ("ok    VERSION: {0}" -f $version) -ForegroundColor Green
    }
}

Pop-Location

Write-Host ''
if ($failures.Count -gt 0) {
    Write-Host ("{0} check(s) failed:" -f $failures.Count) -ForegroundColor Red
    foreach ($failure in $failures) {
        Write-Host ("  - {0}" -f $failure) -ForegroundColor Red
    }
    exit 1
}

Write-Host 'All Antigravity + WorkBuddy + OpenCode + Command Code integration checks passed.' -ForegroundColor Green
exit 0
