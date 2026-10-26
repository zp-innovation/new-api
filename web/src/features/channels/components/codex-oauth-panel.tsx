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
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'

import {
  exchangeCodexCode,
  generateCodexAuthURL,
  refreshCodexCredential,
} from '../api'
import {
  OAuthAuthorizationLink,
  OAuthCodeExchange,
  OAuthCredentialDisclaimer,
  OAuthGenerateButton,
  OAuthPanelShell,
  OAuthRefreshButton,
} from './oauth-panel-parts'
import { useAuthorizationCodeInput } from './hooks/use-authorization-code-input'

type CodexOAuthPanelProps = {
  /** Existing channel id; enables the refresh action when present. */
  channelId?: number
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
 * Drives the Codex authorization-code flow from the channel editor: generate
 * the OpenAI consent URL, paste the returned code, then write the credential
 * into the channel key field.
 */
export function CodexOAuthPanel({
  channelId,
  proxy,
  disabled,
  onCredential,
  onRefreshed,
}: CodexOAuthPanelProps) {
  const { t } = useTranslation()
  const [authUrl, setAuthUrl] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [state, setState] = useState('')
  const { code, codeExtracted, handleCodeChange, resetCode } =
    useAuthorizationCodeInput()
  const [generating, setGenerating] = useState(false)
  const [exchanging, setExchanging] = useState(false)
  const [refreshing, setRefreshing] = useState(false)

  const handleGenerate = useCallback(async () => {
    setGenerating(true)
    try {
      const res = await generateCodexAuthURL(proxy ? { proxy } : {})
      if (!res.success || !res.data) {
        throw new Error(
          res.message || t('Failed to generate the Codex authorization link')
        )
      }
      setAuthUrl(res.data.auth_url)
      setSessionId(res.data.session_id)
      setState(res.data.state)
      resetCode()
      window.open(res.data.auth_url, '_blank', 'noopener,noreferrer')
    } catch (error) {
      handleServerError(
        error,
        t('Failed to generate the Codex authorization link')
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
      const res = await exchangeCodexCode({
        session_id: sessionId,
        state,
        code: trimmed,
        ...(proxy ? { proxy } : {}),
      })
      if (!res.success || !res.data) {
        throw new Error(
          res.message || t('Failed to exchange the Codex authorization code')
        )
      }
      onCredential(JSON.stringify(res.data, null, 2))
      toast.success(t('Codex credentials created'))
      setAuthUrl('')
      setSessionId('')
      setState('')
      resetCode()
    } catch (error) {
      handleServerError(
        error,
        t('Failed to exchange the Codex authorization code')
      )
    } finally {
      setExchanging(false)
    }
  }, [code, onCredential, proxy, resetCode, sessionId, state, t])

  const handleRefresh = useCallback(async () => {
    if (!channelId) return

    setRefreshing(true)
    try {
      const res = await refreshCodexCredential(channelId)
      if (!res.success) {
        throw new Error(res.message || t('Failed to refresh credential'))
      }
      toast.success(t('Credential refreshed'))
      onRefreshed?.()
    } catch (error) {
      handleServerError(
        error,
        t('Failed to refresh credentials. Please try again later')
      )
    } finally {
      setRefreshing(false)
    }
  }, [channelId, onRefreshed, t])

  return (
    <OAuthPanelShell
      description={t(
        'Codex channels use an OAuth JSON credential as the key.'
      )}
      actions={
        <>
          <OAuthGenerateButton
            busy={generating}
            disabled={disabled}
            onClick={handleGenerate}
          />
          {channelId ? (
            <OAuthRefreshButton
              refreshing={refreshing}
              disabled={disabled}
              onClick={handleRefresh}
            />
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

      <OAuthCredentialDisclaimer
        text={t(
          "Disclaimer: Personal use only. Do not distribute or share any credentials. This channel has prerequisites and requires prior setup; use it only if you understand the flow and risks, and comply with OpenAI's terms and policies. Credentials and configuration are for Codex CLI integration only, and are not intended for any other client, platform, or channel."
        )}
      />
    </OAuthPanelShell>
  )
}
