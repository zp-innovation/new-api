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
import { Loader2, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import {
  addGeminiWebAccount,
  deleteGeminiWebAccount,
  listGeminiWebAccounts,
  setGeminiWebAccountStatus,
  type GeminiWebAccount,
} from '../api'

function geminiWebAccountStatusLabel(
  t: (key: string) => string,
  status: string
): string {
  if (status === 'enabled') return t('Enabled')
  if (status === 'disabled') return t('Disabled')
  return status
}

function geminiWebAccountErrorLabel(
  t: (key: string) => string,
  error: string
): string {
  const message = error.toLowerCase()
  if (
    message.includes('context deadline exceeded') ||
    message.includes('timeout')
  ) {
    return t('Gemini Web request timed out while refreshing the cookie')
  }
  if (
    message.includes('snlm0e') ||
    message.includes('cookie expired') ||
    message.includes('not signed in')
  ) {
    return t('Gemini Web cookie is no longer signed in; import a new cookie')
  }
  return error
}

type GeminiWebAccountsPanelProps = {
  channelId?: number
  disabled?: boolean
}

export function GeminiWebAccountsPanel(props: GeminiWebAccountsPanelProps) {
  const { t } = useTranslation()
  const [accounts, setAccounts] = useState<GeminiWebAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [label, setLabel] = useState('')
  const [cookie, setCookie] = useState('')
  const [updatingId, setUpdatingId] = useState<number | null>(null)

  const reload = useCallback(async () => {
    if (!props.channelId) {
      setAccounts([])
      return
    }
    setLoading(true)
    try {
      const res = await listGeminiWebAccounts(props.channelId)
      setAccounts(res.data ?? [])
    } catch (error) {
      handleServerError(error, t('Failed to load the Gemini Web account pool'))
    } finally {
      setLoading(false)
    }
  }, [props.channelId, t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reload()
  }, [reload])

  const handleAdd = useCallback(async () => {
    if (!props.channelId) return
    setSaving(true)
    try {
      let credential: { proxy?: unknown } = {}
      try {
        const parsed: unknown = JSON.parse(cookie.trim())
        if (parsed && typeof parsed === 'object') {
          credential = parsed as { proxy?: unknown }
        }
      } catch {
        // The server also accepts a raw Cookie header.
      }
      await addGeminiWebAccount(props.channelId, {
        label: label.trim(),
        cookie: cookie.trim(),
        proxy:
          typeof credential.proxy === 'string' ? credential.proxy : undefined,
      })
      setCookie('')
      setLabel('')
      toast.success(t('Gemini Web account imported'))
      await reload()
    } catch (error) {
      handleServerError(error, t('Failed to import the Gemini Web account'))
    } finally {
      setSaving(false)
    }
  }, [cookie, label, props.channelId, reload, t])

  // Disabling keeps the cookie for later, so an account that is temporarily
  // rate-limited or signed out can be parked without deleting it.
  const handleToggleStatus = useCallback(
    async (account: GeminiWebAccount) => {
      if (!props.channelId) return
      const nextStatus = account.status === 'enabled' ? 'disabled' : 'enabled'
      setUpdatingId(account.id)
      try {
        await setGeminiWebAccountStatus(props.channelId, account.id, nextStatus)
        toast.success(
          t(nextStatus === 'enabled' ? 'Account enabled' : 'Account disabled')
        )
        await reload()
      } catch (error) {
        handleServerError(error, t('Failed to update the account status'))
      } finally {
        setUpdatingId(null)
      }
    },
    [props.channelId, reload, t]
  )

  const handleDelete = useCallback(
    async (accountId: number) => {
      if (!props.channelId) return
      try {
        await deleteGeminiWebAccount(props.channelId, accountId)
        toast.success(t('Gemini Web account removed'))
        await reload()
      } catch (error) {
        handleServerError(error, t('Failed to remove the Gemini Web account'))
      }
    },
    [props.channelId, reload, t]
  )

  if (!props.channelId) {
    return (
      <div className='border-border/60 flex flex-col gap-2 border-y py-4'>
        <div className='text-sm font-medium'>{t('Gemini Web accounts')}</div>
        <p className='text-muted-foreground text-xs'>
          {t('Save this channel first, then add the Google accounts here.')}
        </p>
      </div>
    )
  }

  return (
    <div className='border-border/60 flex min-w-0 flex-col gap-3 overflow-hidden border-y py-4'>
      <div className='flex items-center justify-between'>
        <div className='text-sm font-medium'>{t('Gemini Web accounts')}</div>
        {loading ? (
          <Loader2 className='text-muted-foreground h-4 w-4 animate-spin' />
        ) : null}
      </div>

      <p className='text-muted-foreground text-xs'>
        {t(
          'Paste the full cookie string captured from a signed-in gemini.google.com session. It must contain __Secure-1PSID, __Secure-1PSIDTS and SAPISID.'
        )}
      </p>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Chrome device-bound cookies may reject HTTP cookie rotation. If the account expires repeatedly, sign in with Firefox and export a new cookie. Use a Google session on only one Gemini Web account and one server, and close the browser session after exporting.'
        )}
      </p>

      <div className='flex min-w-0 flex-col gap-2'>
        <Input
          value={label}
          onChange={(event) => setLabel(event.target.value)}
          placeholder={t('Account label (optional)')}
          aria-label={t('Account label')}
          className='w-full sm:max-w-xs'
          disabled={props.disabled}
        />
        <Textarea
          value={cookie}
          onChange={(event) => setCookie(event.target.value)}
          placeholder={t('Paste cookie JSON or a raw Cookie header')}
          aria-label={t('Gemini Web cookie')}
          className='max-h-40 min-h-24 w-full max-w-full resize-none overflow-auto font-mono text-xs break-all'
          disabled={props.disabled}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Cookie JSON must contain a cookie field with __Secure-1PSID, __Secure-1PSIDTS, and SAPISID.'
          )}
        </p>
      </div>

      <div className='flex justify-end'>
        <Button
          type='button'
          size='sm'
          onClick={() => void handleAdd()}
          disabled={props.disabled || saving || !cookie.trim()}
        >
          {saving ? <Loader2 className='mr-2 h-4 w-4 animate-spin' /> : null}
          {t('Add account')}
        </Button>
      </div>

      {accounts.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'No account yet: anonymous requests only reach the free Flash models.'
          )}
        </p>
      ) : (
        <ul className='flex flex-col gap-2'>
          {accounts.map((account) => {
            const displayLabel =
              account.label === 'Gemini Web account'
                ? t('Gemini Web account')
                : account.label

            return (
              <li
                key={account.id}
                className='border-border/60 flex min-w-0 items-center justify-between gap-2 rounded-md border px-3 py-2'
              >
                <div className='flex min-w-0 flex-col'>
                  <span className='truncate text-sm'>{displayLabel}</span>
                  <span className='text-muted-foreground truncate text-xs'>
                    {geminiWebAccountStatusLabel(t, account.status)} ·{' '}
                    {t('{{bytes}} bytes', { bytes: account.cookie_size })}
                    {account.last_error
                      ? ` · ${geminiWebAccountErrorLabel(t, account.last_error)}`
                      : ''}
                  </span>
                </div>
                <div className='flex shrink-0 items-center gap-2'>
                  <Switch
                    size='sm'
                    checked={account.status === 'enabled'}
                    disabled={props.disabled || updatingId === account.id}
                    onCheckedChange={() => void handleToggleStatus(account)}
                    aria-label={
                      account.status === 'enabled'
                        ? t('Disable account')
                        : t('Enable account')
                    }
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => void handleDelete(account.id)}
                    disabled={props.disabled}
                    aria-label={t('Remove account')}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
