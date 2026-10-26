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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SelectItem } from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  pollWorkBuddyLogin,
  refreshWorkBuddyCredential,
  startWorkBuddyLogin,
} from '../api'
import { readCredentialField } from '../lib/oauth-credential'
import {
  normalizeWorkBuddyProduct,
  normalizeWorkBuddyRealm,
  type WorkBuddyProduct,
  type WorkBuddyRealm,
  withWorkBuddyProduct,
  workBuddyChatBaseURL,
  workBuddyCredentialEndpoint,
} from '../lib/workbuddy-credential'
import {
  OAuthAuthorizationLink,
  OAuthCredentialDisclaimer,
  OAuthGenerateButton,
  OAuthPanelSelect,
  OAuthPanelShell,
  OAuthRefreshButton,
} from './oauth-panel-parts'

/** The upstream answers the poll with a business code until the browser flow
 * completes, so a fixed cadence is enough and keeps the console quiet. */
const POLL_INTERVAL_MS = 3000

type WorkBuddyLoginPhase = 'idle' | 'pending' | 'ready' | 'expired'

type WorkBuddyOAuthPanelProps = {
  /** Channel proxy, forwarded to the OAuth calls. */
  proxy?: string
  /** Disables every action, e.g. while the editor is read-only. */
  disabled?: boolean
  /** Existing channel key, used to restore the realm and product selectors. */
  credential?: string
  /** Whether the derived chat address should be pushed into the form. */
  autoFillBaseURL?: boolean
  /** Receives the chat address derived from the selected realm and product. */
  onBaseURLChange?: (baseURL: string) => void
  /** Receives the credential JSON to store as the channel key. */
  onCredential: (credential: string) => void
}

/**
 * Drives the WorkBuddy device-authorization flow from the channel editor.
 *
 * WorkBuddy does not redirect: it authorizes through a device code, so the
 * panel opens the authorization URL and then polls until the upstream issues a
 * token. The credential is written into the key field as the compact
 * single-line JSON the key column requires.
 */
export function WorkBuddyOAuthPanel({
  proxy,
  disabled,
  credential: storedCredential,
  autoFillBaseURL,
  onBaseURLChange,
  onCredential,
}: WorkBuddyOAuthPanelProps) {
  const { t } = useTranslation()
  const initialEndpoint = workBuddyCredentialEndpoint(storedCredential ?? '')
  const [realm, setRealm] = useState<WorkBuddyRealm>(
    initialEndpoint?.realm ?? 'intl'
  )
  const [product, setProduct] = useState<WorkBuddyProduct>(
    initialEndpoint?.product ?? 'cli'
  )
  const [authUrl, setAuthUrl] = useState('')
  const [phase, setPhase] = useState<WorkBuddyLoginPhase>('idle')
  const [credential, setCredential] = useState('')
  const [starting, setStarting] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  // A freshly polled credential shadows the stored key until the drawer closes.
  const activeCredential = credential || storedCredential || ''
  const refreshToken = readCredentialField(activeCredential, 'refresh_token')
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const onBaseURLChangeRef = useRef(onBaseURLChange)
  const endpointRef = useRef({ realm, product })

  useEffect(() => {
    onBaseURLChangeRef.current = onBaseURLChange
  }, [onBaseURLChange])

  useEffect(() => {
    endpointRef.current = { realm, product }
  }, [realm, product])

  useEffect(() => {
    const endpoint = workBuddyCredentialEndpoint(storedCredential ?? '')
    if (!endpoint) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setRealm(endpoint.realm)
    setProduct(endpoint.product)
  }, [storedCredential])

  useEffect(() => {
    if (autoFillBaseURL === false) return
    const current = endpointRef.current
    onBaseURLChangeRef.current?.(
      workBuddyChatBaseURL(current.realm, current.product)
    )
  }, [autoFillBaseURL])

  const stopPolling = useCallback(() => {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  // A poll in flight when the drawer closes would keep hitting the console
  // endpoints; the interval dies with the panel.
  useEffect(() => stopPolling, [stopPolling])

  const handleStart = useCallback(async () => {
    stopPolling()
    setStarting(true)
    setPhase('idle')
    setCredential('')
    try {
      const res = await startWorkBuddyLogin({
        realm,
        product,
        ...(proxy ? { proxy } : {}),
      })
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Failed to start the WorkBuddy login'))
      }
      const loginState = res.data.state
      setAuthUrl(res.data.auth_url)
      setPhase('pending')
      window.open(res.data.auth_url, '_blank', 'noopener,noreferrer')

      timerRef.current = setInterval(() => {
        void (async () => {
          try {
            const polled = await pollWorkBuddyLogin({
              state: loginState,
              ...(proxy ? { proxy } : {}),
            })
            const next = polled.data?.status
            if (next === 'ready' && polled.data?.credential) {
              stopPolling()
              setCredential(polled.data.credential)
              setPhase('ready')
              const endpoint = workBuddyCredentialEndpoint(
                polled.data.credential
              )
              if (endpoint) {
                setRealm(endpoint.realm)
                setProduct(endpoint.product)
                onBaseURLChangeRef.current?.(endpoint.baseURL)
              }
              onCredential(polled.data.credential)
              toast.success(t('WorkBuddy credentials created'))
              return
            }
            if (next === 'expired') {
              stopPolling()
              setPhase('expired')
              toast.error(
                t('The authorization request expired. Generate a new link.')
              )
            }
          } catch (error) {
            stopPolling()
            setPhase('expired')
            toast.error(
              error instanceof Error
                ? error.message
                : t('Failed to poll the WorkBuddy login')
            )
          }
        })()
      }, POLL_INTERVAL_MS)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to start the WorkBuddy login')
      )
    } finally {
      setStarting(false)
    }
  }, [onCredential, product, proxy, realm, stopPolling, t])

  // Manual renewal keeps a saved key usable after its access token expires;
  // the upstream rotates the refresh token, so the returned value replaces it.
  const handleRefresh = useCallback(async () => {
    if (!refreshToken) return

    setRefreshing(true)
    try {
      const res = requireServerSuccess(
        await refreshWorkBuddyCredential({
          credential: activeCredential,
          ...(proxy ? { proxy } : {}),
        })
      )
      const refreshed = res.data?.credential
      if (!refreshed) {
        throw new Error(t('Failed to refresh the WorkBuddy credential'))
      }
      setCredential(refreshed)
      const endpoint = workBuddyCredentialEndpoint(refreshed)
      if (endpoint) {
        setRealm(endpoint.realm)
        setProduct(endpoint.product)
        onBaseURLChangeRef.current?.(endpoint.baseURL)
      }
      onCredential(refreshed)
      toast.success(t('WorkBuddy credentials refreshed'))
    } catch (error) {
      handleServerError(
        error,
        t('Failed to refresh the WorkBuddy credential')
      )
    } finally {
      setRefreshing(false)
    }
  }, [activeCredential, onCredential, proxy, refreshToken, t])


  // The product only selects the outbound identity, so switching it rewrites
  // the stored credential instead of forcing another sign-in.
  const handleRealmChange = useCallback(
    (next: string) => {
      const nextRealm = normalizeWorkBuddyRealm(next)
      setRealm(nextRealm)
      onBaseURLChangeRef.current?.(workBuddyChatBaseURL(nextRealm, product))
    },
    [product]
  )

  const handleProductChange = useCallback(
    (next: string) => {
      const nextProduct = normalizeWorkBuddyProduct(next)
      setProduct(nextProduct)
      onBaseURLChangeRef.current?.(workBuddyChatBaseURL(realm, nextProduct))
      if (!credential) return
      const updated = withWorkBuddyProduct(credential, nextProduct)
      setCredential(updated)
      onCredential(updated)
    },
    [credential, onCredential, realm]
  )

  return (
    <OAuthPanelShell
      description={t(
        'WorkBuddy channels use an OAuth JSON credential as the key.'
      )}
      actions={
        <>
          <OAuthGenerateButton
            busy={starting}
            disabled={disabled}
            onClick={handleStart}
          />
          {refreshToken ? (
            <OAuthRefreshButton
              refreshing={refreshing}
              disabled={disabled}
              onClick={handleRefresh}
            />
          ) : null}
        </>
      }
    >
      <div className='grid gap-2 sm:grid-cols-2'>
        <OAuthPanelSelect
          label={t('Realm')}
          value={realm}
          items={[
            { value: 'intl', label: t('International') },
            { value: 'cn', label: t('China Mainland') },
          ]}
          disabled={disabled || phase === 'pending'}
          onValueChange={(value) => handleRealmChange(value ?? '')}
        >
          <SelectItem value='intl'>{t('International')}</SelectItem>
          <SelectItem value='cn'>{t('China Mainland')}</SelectItem>
        </OAuthPanelSelect>
        <OAuthPanelSelect
          label={t('Product')}
          value={product}
          items={[
            { value: 'cli', label: 'CLI' },
            { value: 'vscode', label: 'VSCode' },
            { value: 'workbuddy', label: 'Desktop' },
          ]}
          disabled={disabled || phase === 'pending'}
          onValueChange={(value) => {
            if (value) handleProductChange(value)
          }}
        >
          <SelectItem value='cli'>CLI</SelectItem>
          <SelectItem value='vscode'>VSCode</SelectItem>
          <SelectItem value='workbuddy'>{t('Desktop')}</SelectItem>
        </OAuthPanelSelect>
      </div>

      {authUrl ? (
        <OAuthAuthorizationLink authUrl={authUrl}>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Open the link, finish signing in, and the credential is written into the key field automatically.'
            )}
          </p>
          {phase === 'pending' ? (
            <p className='text-muted-foreground flex items-center gap-2 text-xs'>
              <Loader2 className='h-3 w-3 animate-spin' />
              {t('Waiting for authorization…')}
            </p>
          ) : null}
          {phase === 'ready' ? (
            <p className='text-muted-foreground text-xs'>{t('Authorized')}</p>
          ) : null}
          {phase === 'expired' ? (
            <p className='text-muted-foreground text-xs'>
              {t('The authorization request expired. Generate a new link.')}
            </p>
          ) : null}
        </OAuthAuthorizationLink>
      ) : null}

      <OAuthCredentialDisclaimer
        text={t(
          'Disclaimer: Personal use only. Do not distribute or share any credentials. Use this channel only if you understand the flow and risks and comply with the WorkBuddy terms and policies.'
        )}
      />
    </OAuthPanelShell>
  )
}
