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
import {
  Activity,
  ExternalLink,
  KeyRound,
  Loader2,
  RefreshCw,
} from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatNumber, formatPercent } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getCommandCodeChannelUsage,
  pollCommandCodeLogin,
  startCommandCodeLogin,
  validateCommandCodeChannelCredential,
  validateCommandCodeCredential,
  type CommandCodeUsageEntry,
} from '../api'
import { DeviceLoginAuthorization, DeviceLoginOutcome } from './device-login-status'
import { useDeviceLoginPolling } from './hooks/use-device-login-polling'
import { OAuthPanelShell } from './oauth-panel-parts'

const COMMAND_CODE_STUDIO_URL = 'https://commandcode.ai/studio'
const POLL_INTERVAL_MS = 2000

type CommandCodeOAuthPanelProps = {
  channelId?: number
  proxy?: string
  disabled?: boolean
  onCredential: (credential: string) => void
  onRefreshed?: () => void
}

function isLoopbackConsole(): boolean {
  if (typeof window === 'undefined') return false
  const hostname = window.location.hostname.toLowerCase()
  return (
    hostname === 'localhost' ||
    hostname === '127.0.0.1' ||
    hostname === '::1' ||
    hostname === '[::1]'
  )
}

export function CommandCodeOAuthPanel(
  props: CommandCodeOAuthPanelProps
) {
  const { t } = useTranslation()
  const [credentialInput, setCredentialInput] = useState('')
  const [validating, setValidating] = useState(false)
  const [accountLabel, setAccountLabel] = useState('')
  const [planLabel, setPlanLabel] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const [loadingUsage, setLoadingUsage] = useState(false)
  const [usage, setUsage] = useState<CommandCodeUsageEntry[]>([])
  const loopback = isLoopbackConsole()
  // The browser sign-in is a device flow like OpenCode's, so the polling loop,
  // its expiry handling and its unmount cleanup come from the shared hook.
  const {
    phase,
    authUrl,
    starting: browserStarting,
    start,
  } = useDeviceLoginPolling({
    startLogin: async () => {
      const started = await startCommandCodeLogin({
        callback_url: `${window.location.origin}/api/channel/oauth/command-code/callback`,
        ...(props.proxy ? { proxy: props.proxy } : {}),
      })
      if (!started.success || !started.data) {
        throw new Error(
          started.message || t('Failed to start the Command Code login')
        )
      }
      return {
        authUrl: started.data.auth_url,
        sessionId: started.data.session_id,
      }
    },
    pollLogin: async (sessionId) => {
      const polled = await pollCommandCodeLogin({
        session_id: sessionId,
        ...(props.proxy ? { proxy: props.proxy } : {}),
      })
      return {
        status: polled.data?.status ?? 'expired',
        credential: polled.data?.credential,
      }
    },
    onCredential: props.onCredential,
    defaultIntervalMs: POLL_INTERVAL_MS,
    messages: {
      startFailed: t('Failed to start the Command Code login'),
      pollFailed: t('Failed to poll the Command Code login'),
      created: t('Command Code credentials created'),
      denied: t('The authorization request was denied.'),
      expired: t('The authorization request expired. Generate a new link.'),
    },
  })

  const handleValidate = useCallback(async () => {
    const credential = credentialInput.trim()
    if (!credential) {
      toast.error(t('Paste a Command Code API key first'))
      return
    }
    setValidating(true)
    try {
      const validated = await validateCommandCodeCredential({
        credential,
        ...(props.proxy ? { proxy: props.proxy } : {}),
      })
      if (!validated.success || !validated.data) {
        throw new Error(
          validated.message || t('Failed to validate the Command Code credential')
        )
      }
      props.onCredential(validated.data.credential)
      setCredentialInput('')
      setAccountLabel(
        validated.data.user_name || validated.data.user_id || ''
      )
      toast.success(t('Command Code credential validated'))
    } catch (error) {
      handleServerError(error, t('Failed to validate the Command Code credential'))
    } finally {
      setValidating(false)
    }
  }, [credentialInput, props, t])

  const handleRevalidate = useCallback(async () => {
    if (!props.channelId) return
    setRefreshing(true)
    try {
      const validated = await validateCommandCodeChannelCredential(
        props.channelId
      )
      if (!validated.success || !validated.data) {
        throw new Error(
          validated.message || t('Failed to validate the Command Code credential')
        )
      }
      props.onCredential(validated.data.credential)
      props.onRefreshed?.()
      toast.success(t('Command Code credential validated'))
    } catch (error) {
      handleServerError(error, t('Failed to validate the Command Code credential'))
    } finally {
      setRefreshing(false)
    }
  }, [props, t])

  const handleUsage = useCallback(async () => {
    if (!props.channelId) return
    setLoadingUsage(true)
    try {
      const response = await getCommandCodeChannelUsage(props.channelId)
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to fetch the Command Code usage')
        )
      }
      setUsage(response.data.entries)
      const first = response.data.entries[0]
      if (first) {
        setAccountLabel(first.user_name || first.user_id || '')
        setPlanLabel(first.plan_name || first.plan_id || '')
      }
    } catch (error) {
      handleServerError(error, t('Failed to fetch the Command Code usage'))
    } finally {
      setLoadingUsage(false)
    }
  }, [props.channelId, t])

  return (
    <OAuthPanelShell
      description={t('Command Code uses a Studio API key as the channel key.')}
      actions={
        <>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() =>
              window.open(COMMAND_CODE_STUDIO_URL, '_blank', 'noopener,noreferrer')
            }
          >
            <ExternalLink className='mr-2 h-4 w-4' />
            {t('Open Command Code Studio')}
          </Button>
          {loopback ? (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={start}
              disabled={props.disabled || browserStarting || phase === 'pending'}
            >
              {browserStarting ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : (
                <ExternalLink className='mr-2 h-4 w-4' />
              )}
              {browserStarting
                ? t('Generating...')
                : t('Sign in with Command Code')}
            </Button>
          ) : null}
        </>
      }
    >
      {!loopback ? (
        <Alert>
          <AlertDescription>
            {t(
              'Browser sign-in requires the console to be opened on localhost. Paste an API key from Command Code Studio instead.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className='grid gap-2'>
        <Label className='text-xs' htmlFor='command-code-credential'>
          {t('Command Code API key or auth.json')}
        </Label>
        <div className='flex flex-col gap-2 sm:flex-row'>
          <Input
            id='command-code-credential'
            value={credentialInput}
            onChange={(event) => setCredentialInput(event.target.value)}
            placeholder={t(
              'Paste user_... or the contents of ~/.commandcode/auth.json'
            )}
            disabled={props.disabled || validating}
            className='font-mono text-xs'
            autoComplete='off'
          />
          <Button
            type='button'
            size='sm'
            onClick={handleValidate}
            disabled={props.disabled || validating}
          >
            {validating ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : (
              <KeyRound className='mr-2 h-4 w-4' />
            )}
            {validating ? t('Validating...') : t('Validate and use')}
          </Button>
        </div>
      </div>

      {authUrl ? <DeviceLoginAuthorization authUrl={authUrl} /> : null}

      <DeviceLoginOutcome phase={phase} />

      {accountLabel || planLabel ? (
        <div className='text-muted-foreground text-xs'>
          {t('Account')}:{' '}
          <span className='text-foreground'>{accountLabel || '-'}</span>
          {planLabel ? (
            <>
              {' · '}
              {t('Plan')}:{' '}
              <span className='text-foreground'>{planLabel}</span>
            </>
          ) : null}
        </div>
      ) : null}

      {props.channelId ? (
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleRevalidate}
            disabled={props.disabled || refreshing}
          >
            {refreshing ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : (
              <RefreshCw className='mr-2 h-4 w-4' />
            )}
            {t('Revalidate credential')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleUsage}
            disabled={props.disabled || loadingUsage}
          >
            {loadingUsage ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : (
              <Activity className='mr-2 h-4 w-4' />
            )}
            {t('Query usage')}
          </Button>
        </div>
      ) : null}

      {usage.length > 0 ? (
        <div className='flex flex-col gap-2'>
          <Label className='text-xs'>{t('Command Code usage')}</Label>
          <div className='flex flex-col gap-2'>
            {usage.map((entry) => (
              <div
                key={
                  entry.user_id ??
                  entry.user_name ??
                  entry.org_id ??
                  'account'
                }
                className='border-border/60 grid gap-2 rounded-md border px-3 py-2 text-xs sm:grid-cols-4'
              >
                <div className='truncate'>
                  {entry.user_name || entry.user_id || t('Account')}
                </div>
                <div>
                  {t('Monthly credits')}:{' '}
                  {formatNumber(entry.monthly_credits)}
                </div>
                <div>
                  {t('Purchased credits')}:{' '}
                  {formatNumber(entry.purchased_credits)}
                </div>
                <div>
                  {entry.five_hour
                    ? `${t('5-hour window')}: ${formatPercent(entry.five_hour.percent)}`
                    : t('No rolling window')}
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </OAuthPanelShell>
  )
}
