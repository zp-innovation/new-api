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

import { Copy, ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

/**
 * Presentation shared by the provider OAuth channel panels.
 *
 * Each panel drives a different authorization flow, but the surrounding card
 * frame, the authorization-link row, the code exchange block and the amber
 * disclaimer are the same markup. Only presentation lives here: workflow
 * state and handlers stay in the panel, while the copy action uses the shared
 * clipboard hook so every flow reports the same success and failure messages.
 */
export function OAuthPanelShell({
  description,
  actions,
  children,
}: {
  description: string
  actions: ReactNode
  children: ReactNode
}) {
  return (
    <div className='border-border/60 flex flex-col gap-3 border-y py-4'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <div className='text-muted-foreground text-xs'>{description}</div>
        <div className='flex flex-wrap items-center gap-2'>{actions}</div>
      </div>
      {children}
    </div>
  )
}

/** The "Get authorization link" action, shared by the browser-login flows. */
export function OAuthGenerateButton({
  busy,
  disabled,
  extraDisabled,
  onClick,
}: {
  busy: boolean
  disabled?: boolean
  extraDisabled?: boolean
  onClick: () => void
}) {
  const { t } = useTranslation()

  return (
    <Button
      type='button'
      variant='outline'
      size='sm'
      onClick={onClick}
      disabled={disabled || busy || extraDisabled}
    >
      {busy ? (
        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
      ) : (
        <ExternalLink className='mr-2 h-4 w-4' />
      )}
      {busy ? t('Generating...') : t('Get authorization link')}
    </Button>
  )
}

/** The "Refresh credential" action shared by the panels that can renew one. */
export function OAuthRefreshButton({
  refreshing,
  disabled,
  onClick,
  showRefreshingLabel = true,
}: {
  refreshing: boolean
  disabled?: boolean
  onClick: () => void
  showRefreshingLabel?: boolean
}) {
  const { t } = useTranslation()
  const label =
    refreshing && showRefreshingLabel
      ? t('Refreshing...')
      : t('Refresh credential')

  return (
    <Button
      type='button'
      variant='outline'
      size='sm'
      onClick={onClick}
      disabled={disabled || refreshing}
    >
      {refreshing ? (
        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
      ) : (
        <RefreshCw className='mr-2 h-4 w-4' />
      )}
      {label}
    </Button>
  )
}

/** The generated authorization link with its copy action. */
export function OAuthAuthorizationLink({
  authUrl,
  children,
}: {
  authUrl: string
  children?: ReactNode
}) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard({
    successMessage: t('Copied'),
  })

  return (
    <div className='flex flex-col gap-2'>
      <Label className='text-xs'>{t('Authorization link')}</Label>
      <div className='flex items-center gap-2'>
        <Input readOnly value={authUrl} className='font-mono text-xs' />
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void copyToClipboard(authUrl)}
        >
          <Copy className='mr-2 h-4 w-4' />
          {t('Copy')}
        </Button>
      </div>
      {children}
    </div>
  )
}

/** The authorization-code paste block used by the redirect-based flows. */
export function OAuthCodeExchange({
  code,
  codeExtracted,
  exchanging,
  disabled,
  onCodeChange,
  onExchange,
}: {
  code: string
  codeExtracted: boolean
  exchanging: boolean
  disabled?: boolean
  onCodeChange: (value: string) => void
  onExchange: () => void
}) {
  const { t } = useTranslation()

  return (
    <>
      <p className='text-muted-foreground text-xs'>
        {t(
          'After signing in, the browser is redirected to a localhost callback that is not running. Copy the code parameter from the address bar and paste it below.'
        )}
      </p>
      <p className='text-muted-foreground text-xs'>
        {t(
          'You can also paste the whole callback URL; the authorization code is extracted automatically.'
        )}
      </p>
      <div className='flex items-center gap-2'>
        <Input
          value={code}
          onChange={(event) => onCodeChange(event.target.value)}
          placeholder={t('Paste the authorization code')}
          className='font-mono text-xs'
        />
        <Button
          type='button'
          size='sm'
          onClick={onExchange}
          disabled={disabled || exchanging || !code.trim()}
        >
          {exchanging ? (
            <Loader2 className='mr-2 h-4 w-4 animate-spin' />
          ) : null}
          {exchanging ? t('Exchanging...') : t('Create credential')}
        </Button>
      </div>
      {codeExtracted ? (
        <p className='text-muted-foreground text-xs'>
          {t('Authorization code extracted from the callback link')}
        </p>
      ) : null}
    </>
  )
}

/** The amber credentials disclaimer shared by the personal-use channels. */
export function OAuthCredentialDisclaimer({ text }: { text: string }) {
  return (
    <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
      <AlertDescription>{text}</AlertDescription>
    </Alert>
  )
}

/** A labelled select row; the option list is passed as SelectItem children. */
export function OAuthPanelSelect({
  label,
  value,
  items,
  disabled,
  onValueChange,
  children,
}: {
  label: string
  value: string
  items: { value: string; label: string }[]
  disabled?: boolean
  onValueChange: (value: string | null) => void
  children: ReactNode
}) {
  return (
    <div className='flex flex-col gap-2'>
      <Label className='text-xs'>{label}</Label>
      <Select
        disabled={disabled}
        items={items}
        onValueChange={onValueChange}
        value={value}
      >
        <SelectTrigger size='sm'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>{children}</SelectGroup>
        </SelectContent>
      </Select>
    </div>
  )
}
