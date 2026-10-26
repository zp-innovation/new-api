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
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import type { DeviceLoginPhase } from './hooks/use-device-login-polling'

type DeviceLoginAuthorizationProps = {
  authUrl: string
  userCode?: string
  /** Show the copy button when the link is something the user must paste. */
  copyable?: boolean
}

/** Authorization link shared by the device-login panels. */
export function DeviceLoginAuthorization({
  authUrl,
  userCode,
  copyable = false,
}: DeviceLoginAuthorizationProps) {
  const { t } = useTranslation()
  const link = <Input readOnly value={authUrl} className='font-mono text-xs' />
  return (
    <div className='flex flex-col gap-2'>
      <Label className='text-xs'>{t('Authorization link')}</Label>
      {copyable ? (
        <div className='flex items-center gap-2'>
          {link}
          <CopyButton
            value={authUrl}
            variant='outline'
            size='sm'
            tooltip={t('Copy')}
            successTooltip={t('Copied')}
          />
        </div>
      ) : (
        link
      )}
      {userCode ? (
        <div className='text-muted-foreground text-xs'>
          {t('User code')}:{' '}
          <span className='text-foreground font-mono'>{userCode}</span>
        </div>
      ) : null}
    </div>
  )
}

/** Terminal state of a device login, shown once the upstream refuses or expires it. */
export function DeviceLoginOutcome({ phase }: { phase: DeviceLoginPhase }) {
  const { t } = useTranslation()
  if (phase !== 'expired' && phase !== 'denied') return null
  return (
    <Alert variant={phase === 'denied' ? 'destructive' : 'default'}>
      <AlertDescription>
        {phase === 'denied'
          ? t('The authorization request was denied.')
          : t('The authorization request expired. Generate a new link.')}
      </AlertDescription>
    </Alert>
  )
}
