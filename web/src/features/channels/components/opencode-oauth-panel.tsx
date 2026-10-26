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
import { Activity, Loader2 } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { SelectItem } from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  getOpenCodeChannelUsage,
  pollOpenCodeLogin,
  refreshOpenCodeChannelCredential,
  refreshOpenCodeCredential,
  startOpenCodeLogin,
  type OpenCodeUsageEntry,
} from '../api'
import { readCredentialField } from '../lib/oauth-credential'
import { DeviceLoginAuthorization, DeviceLoginOutcome } from './device-login-status'
import { useDeviceLoginPolling } from './hooks/use-device-login-polling'
import {
  OAuthGenerateButton,
  OAuthPanelSelect,
  OAuthPanelShell,
  OAuthRefreshButton,
} from './oauth-panel-parts'

type OpenCodeMode = 'zen' | 'go'

type OpenCodeOAuthPanelProps = {
  channelId?: number
  /** Current key draft; a refresh token inside it enables a pre-save renewal. */
  credential?: string
  proxy?: string
  disabled?: boolean
  onCredential: (credential: string) => void
  onRefreshed?: () => void
}

export function OpenCodeOAuthPanel({
  channelId,
  credential,
  proxy,
  disabled,
  onCredential,
  onRefreshed,
}: OpenCodeOAuthPanelProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<OpenCodeMode>('zen')
  const [refreshing, setRefreshing] = useState(false)
  const [loadingUsage, setLoadingUsage] = useState(false)
  const [usage, setUsage] = useState<OpenCodeUsageEntry[]>([])
  const refreshToken = readCredentialField(credential, 'refresh_token')
  // The device flow, its polling cadence and its cleanup are shared with the
  // other device-login panels; only the endpoints and the copy differ.
  const { phase, authUrl, userCode, starting, start } = useDeviceLoginPolling({
    startLogin: async () => {
      const started = await startOpenCodeLogin({
        mode,
        ...(proxy ? { proxy } : {}),
      })
      if (!started.success || !started.data) {
        throw new Error(
          started.message || t('Failed to start the OpenCode login')
        )
      }
      return {
        authUrl: started.data.auth_url,
        sessionId: started.data.session_id,
        userCode: started.data.user_code,
        intervalMs: (started.data.interval || 2) * 1000,
      }
    },
    pollLogin: async (sessionId) => {
      const polled = await pollOpenCodeLogin({
        session_id: sessionId,
        ...(proxy ? { proxy } : {}),
      })
      return {
        status: polled.data?.status ?? 'expired',
        credential: polled.data?.credential,
        ...(polled.data?.interval === undefined
          ? {}
          : { intervalMs: polled.data.interval * 1000 }),
      }
    },
    onCredential,
    messages: {
      startFailed: t('Failed to start the OpenCode login'),
      pollFailed: t('Failed to poll the OpenCode login'),
      created: t('OpenCode credentials created'),
      denied: t('The authorization request was denied.'),
      expired: t('The authorization request expired. Generate a new link.'),
    },
  })

  const handleRefresh = useCallback(async () => {
    if (!channelId && !refreshToken) return
    setRefreshing(true)
    try {
      const refreshed = requireServerSuccess(
        channelId
          ? await refreshOpenCodeChannelCredential(channelId)
          : await refreshOpenCodeCredential({
              credential: credential ?? '',
              ...(proxy ? { proxy } : {}),
            })
      )
      if (!refreshed.data) {
        throw new Error(t('Failed to refresh the OpenCode credential'))
      }
      onCredential(refreshed.data.credential)
      if (channelId) onRefreshed?.()
      toast.success(t('OpenCode credentials refreshed'))
    } catch (error) {
      handleServerError(
        error,
        t('Failed to refresh the OpenCode credential')
      )
    } finally {
      setRefreshing(false)
    }
  }, [channelId, credential, onCredential, onRefreshed, proxy, refreshToken, t])

  const handleUsage = useCallback(async () => {
    if (!channelId) return
    setLoadingUsage(true)
    try {
      const response = await getOpenCodeChannelUsage(channelId)
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to fetch the OpenCode usage')
        )
      }
      setUsage(response.data.entries)
    } catch (error) {
      handleServerError(error, t('Failed to fetch the OpenCode usage'))
    } finally {
      setLoadingUsage(false)
    }
  }, [channelId, t])

  return (
    <OAuthPanelShell
      description={t(
        'OpenCode channels use a Console OAuth credential as the key.'
      )}
      actions={
        <OAuthGenerateButton
          busy={starting}
          disabled={disabled}
          extraDisabled={phase === 'pending'}
          onClick={start}
        />
      }
    >

      <Alert>
        <AlertDescription>
          {t(
            'OpenCode free-tier accounts are restricted to the official OpenCode client. Use an OpenCode Zen API key or an OpenCode Go subscription for gateway access.'
          )}
        </AlertDescription>
      </Alert>
      <div className='text-muted-foreground text-xs'>
        {t(
          'An OpenCode Zen API key can be pasted directly into the key field.'
        )}
      </div>

      <div className='grid gap-2 sm:grid-cols-2'>
        <OAuthPanelSelect
          label={t('Mode')}
          value={mode}
          items={[
            { value: 'zen', label: t('OpenCode Zen') },
            { value: 'go', label: t('OpenCode Go') },
          ]}
          disabled={disabled || phase === 'pending'}
          onValueChange={(value) => setMode(value === 'go' ? 'go' : 'zen')}
        >
          <SelectItem value='zen'>{t('OpenCode Zen')}</SelectItem>
          <SelectItem value='go'>{t('OpenCode Go')}</SelectItem>
        </OAuthPanelSelect>
        <div className='flex flex-col justify-end gap-2'>
          {channelId || refreshToken ? (
            <div className='flex flex-wrap items-center gap-2'>
              <OAuthRefreshButton
                refreshing={refreshing}
                disabled={disabled}
                onClick={handleRefresh}
                showRefreshingLabel={false}
              />
              {channelId ? (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={handleUsage}
                  disabled={disabled || loadingUsage}
                >
                  {loadingUsage ? (
                    <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  ) : (
                    <Activity className='mr-2 h-4 w-4' />
                  )}
                  {t('Query usage')}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      {authUrl ? (
        <DeviceLoginAuthorization authUrl={authUrl} userCode={userCode} copyable />
      ) : null}

      <DeviceLoginOutcome phase={phase} />

      {usage.length > 0 ? (
        <div className='flex flex-col gap-2'>
          <Label className='text-xs'>{t('OpenCode usage')}</Label>
          <div className='flex flex-col gap-2'>
            {usage.map((entry) => (
              <div
                key={
                  entry.account_id ?? entry.email ?? entry.org_id ?? 'account'
                }
                className='border-border/60 grid gap-2 rounded-md border px-3 py-2 text-xs sm:grid-cols-4'
              >
                <div className='truncate sm:col-span-1'>
                  {entry.email || entry.account_id || t('Account')}
                </div>
                <div>
                  {t('Rolling')}: {entry.rolling.percent.toFixed(1)}%
                </div>
                <div>
                  {t('Weekly')}: {entry.weekly.percent.toFixed(1)}%
                </div>
                <div>
                  {t('Monthly')}: {entry.monthly.percent.toFixed(1)}%
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </OAuthPanelShell>
  )
}
