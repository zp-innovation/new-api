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
import { Loader2 } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  exchangeAntigravityCode,
  generateAntigravityAuthURL,
  getAntigravityChannelQuota,
  refreshAntigravityChannelCredential,
  refreshAntigravityCredential,
  type AntigravityQuotaModel,
} from '../api'
import { mergeAntigravityCredential } from '../lib/antigravity-credential'
import { readCredentialField } from '../lib/oauth-credential'
import {
  OAuthAuthorizationLink,
  OAuthCodeExchange,
  OAuthCredentialDisclaimer,
  OAuthGenerateButton,
  OAuthPanelShell,
  OAuthRefreshButton,
} from './oauth-panel-parts'
import { useAuthorizationCodeInput } from './hooks/use-authorization-code-input'

type AntigravityOAuthPanelProps = {
  /** Existing channel id; enables the persisted refresh action when present. */
  channelId?: number
  /**
   * Current key draft. A refresh token inside it enables renewing the
   * credential before the channel is saved.
   */
  credential?: string
  /** Channel proxy, forwarded to the OAuth calls. */
  proxy?: string
  /** Disables every action, e.g. while the editor is read-only. */
  disabled?: boolean
  /** Receives the credential JSON to store as the channel key. */
  onCredential: (credential: string) => void
  /** Called after the stored credential was refreshed server-side. */
  onRefreshed?: () => void
}

/**
 * Drives the Antigravity authorization-code flow from the channel editor:
 * generate the consent URL, paste the returned code, then write the credential
 * into the channel key field.
 */
export function AntigravityOAuthPanel({
  channelId,
  credential,
  proxy,
  disabled,
  onCredential,
  onRefreshed,
}: AntigravityOAuthPanelProps) {
  const { t } = useTranslation()
  const [authUrl, setAuthUrl] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [state, setState] = useState('')
  const { code, codeExtracted, handleCodeChange, resetCode } =
    useAuthorizationCodeInput()
  const [generating, setGenerating] = useState(false)
  const [exchanging, setExchanging] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [quota, setQuota] = useState<AntigravityQuotaModel[] | null>(null)
  const [loadingQuota, setLoadingQuota] = useState(false)
  const refreshToken = readCredentialField(credential, 'refresh_token')

  const handleGenerate = useCallback(async () => {
    setGenerating(true)
    try {
      const res = await generateAntigravityAuthURL(proxy ? { proxy } : {})
      if (!res.success || !res.data) {
        throw new Error(
          res.message ||
            t('Failed to generate the Antigravity authorization link')
        )
      }
      setAuthUrl(res.data.auth_url)
      setSessionId(res.data.session_id)
      setState(res.data.state)
      resetCode()
      window.open(res.data.auth_url, '_blank', 'noopener,noreferrer')
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to generate the Antigravity authorization link')
      )
    } finally {
      setGenerating(false)
    }
  }, [proxy, resetCode, t])

  const handleExchange = useCallback(async () => {
    const trimmed = code.trim()
    if (!trimmed) return

    setExchanging(true)
    try {
      const res = await exchangeAntigravityCode({
        session_id: sessionId,
        state,
        code: trimmed,
        ...(proxy ? { proxy } : {}),
      })
      if (!res.success || !res.data) {
        throw new Error(
          res.message ||
            t('Failed to exchange the Antigravity authorization code')
        )
      }
      onCredential(JSON.stringify(res.data, null, 2))
      toast.success(t('Antigravity credentials created'))
      setAuthUrl('')
      setSessionId('')
      setState('')
      resetCode()
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to exchange the Antigravity authorization code')
      )
    } finally {
      setExchanging(false)
    }
  }, [code, onCredential, proxy, resetCode, sessionId, state, t])

  const handleRefresh = useCallback(async () => {
    if (!channelId && !refreshToken) return

    setRefreshing(true)
    try {
      if (channelId) {
        // The stored channel refreshes itself: the host keeps the account
        // fields and writes them back.
        requireServerSuccess(
          await refreshAntigravityChannelCredential(channelId)
        )
      } else {
        // Before the channel exists the token is renewed in place. Google does
        // not repeat project_id/email/plan_type, so merge them from the draft
        // or the saved key would lose the fields the host requires.
        const res = requireServerSuccess(
          await refreshAntigravityCredential({
            refresh_token: refreshToken,
            ...(proxy ? { proxy } : {}),
          })
        )
        const merged = res.data
          ? mergeAntigravityCredential(credential ?? '', res.data)
          : null
        if (!merged) {
          throw new Error(
            t('Failed to refresh credentials. Please try again later')
          )
        }
        onCredential(merged)
      }
      toast.success(t('Antigravity credentials refreshed'))
      onRefreshed?.()
    } catch (error) {
      handleServerError(
        error,
        t('Failed to refresh credentials. Please try again later')
      )
    } finally {
      setRefreshing(false)
    }
  }, [
    channelId,
    credential,
    onCredential,
    onRefreshed,
    proxy,
    refreshToken,
    t,
  ])

  const handleLoadQuota = useCallback(async () => {
    if (!channelId) return
    if (quota) {
      setQuota(null)
      return
    }

    setLoadingQuota(true)
    try {
      const res = await getAntigravityChannelQuota(channelId)
      if (!res.success || !res.data) {
        throw new Error(
          res.message ||
            t('Failed to query the Antigravity quota. Please try again later')
        )
      }
      setQuota(res.data.models ?? [])
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to query the Antigravity quota. Please try again later')
      )
    } finally {
      setLoadingQuota(false)
    }
  }, [channelId, quota, t])

  return (
    <OAuthPanelShell
      description={t(
        'Antigravity channels use an OAuth JSON credential as the key.'
      )}
      actions={
        <>
          <OAuthGenerateButton
            busy={generating}
            disabled={disabled}
            onClick={handleGenerate}
          />
          {channelId || refreshToken ? (
            <OAuthRefreshButton
              refreshing={refreshing}
              disabled={disabled}
              onClick={handleRefresh}
            />
          ) : null}
          {channelId ? (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={handleLoadQuota}
              disabled={loadingQuota}
            >
              {loadingQuota ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : null}
              {quota ? t('Hide quota') : t('View quota')}
            </Button>
          ) : null}
        </>
      }
    >
      {authUrl ? (
        <OAuthAuthorizationLink authUrl={authUrl}>
          <OAuthCodeExchange
            code={code}
            codeExtracted={codeExtracted}
            disabled={disabled}
            exchanging={exchanging}
            onCodeChange={handleCodeChange}
            onExchange={handleExchange}
          />
        </OAuthAuthorizationLink>
      ) : null}

      {quota ? (
        <div className='flex flex-col gap-2'>
          <Label className='text-xs'>{t('Remaining quota')}</Label>
          {quota.length === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('No quota information available')}
            </p>
          ) : (
            <div className='max-h-64 overflow-y-auto rounded-md border'>
              <table className='w-full text-xs'>
                <thead className='bg-muted/50 text-muted-foreground'>
                  <tr>
                    <th className='px-2 py-1 text-left font-medium'>
                      {t('Model')}
                    </th>
                    <th className='px-2 py-1 text-right font-medium'>
                      {t('Remaining')}
                    </th>
                    <th className='px-2 py-1 text-left font-medium'>
                      {t('Reset time')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {quota.map((row) => (
                    <tr key={row.model} className='border-t'>
                      <td className='px-2 py-1 font-mono'>{row.model}</td>
                      <td className='px-2 py-1 text-right'>
                        {Math.round(row.remaining_fraction * 100)}%
                      </td>
                      <td className='text-muted-foreground px-2 py-1'>
                        {row.reset_time || '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      ) : null}

      <OAuthCredentialDisclaimer
        text={t(
          "Disclaimer: Personal use only. Do not distribute or share any credentials. Use this channel only if you understand the flow and risks and comply with Google's terms and policies."
        )}
      />
    </OAuthPanelShell>
  )
}
