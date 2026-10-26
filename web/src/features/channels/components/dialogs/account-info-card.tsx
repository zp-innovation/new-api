import { RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

/**
 * The account summary card shared by the Antigravity / WorkBuddy / OpenCode /
 * Command Code usage dialogs. The badges and fields differ per channel, so they
 * are passed as children; the card shell, its title and the optional refresh
 * button stay in one place.
 */
export function AccountInfoCard(props: {
  children: ReactNode
  onRefresh?: () => void
  isRefreshing?: boolean
}) {
  const { t } = useTranslation()

  return (
    <Card size='sm' className='bg-muted/30 gap-0 py-0'>
      <CardHeader className='p-4 pb-2'>
        <CardTitle className='text-muted-foreground text-xs font-medium'>
          {t('Account Info')}
        </CardTitle>
        {props.onRefresh ? (
          <CardAction>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={props.onRefresh}
              disabled={Boolean(props.isRefreshing)}
            >
              <RefreshCw data-icon='inline-start' />
              {t('Refresh')}
            </Button>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className='p-4 pt-0'>{props.children}</CardContent>
    </Card>
  )
}

export function InfoField(props: { label: string; value?: string }) {
  const value = props.value?.trim()
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 truncate text-sm font-medium' title={value || '-'}>
        {value || '-'}
      </div>
    </div>
  )
}
