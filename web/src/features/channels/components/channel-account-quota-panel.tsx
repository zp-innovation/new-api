/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getChannelAccountQuotas } from '../api'

type Props = { channelType: number }

/**
 * Account-level quota for channels whose upstream reports one pool per account
 * rather than per-model limits.
 */
export function ChannelAccountQuotaPanel({ channelType }: Props) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['channel-account-quotas', channelType],
    queryFn: async () => requireServerSuccess(await getChannelAccountQuotas(channelType)),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })
  const rows = query.data?.data?.accounts ?? []

  if (query.isLoading) {
    return <Skeleton className='h-40 w-full' />
  }
  if (rows.length === 0) {
    return (
      <Empty className='min-h-40 border'>
        <EmptyHeader>
          <EmptyTitle>{t('No data')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center'>
        <Badge variant='secondary'>{rows.length}</Badge>
        <Button
          variant='outline'
          size='sm'
          className='ms-auto'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={query.isFetching ? 'animate-spin' : ''} data-icon='inline-start' />
          {t('Refresh')}
        </Button>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Channel')}</TableHead>
            <TableHead className='w-64'>{t('Remaining')}</TableHead>
            <TableHead className='w-28'>{t('Status')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const percent = row.total > 0 ? Math.max(0, Math.min(100, (row.remain / row.total) * 100)) : 0
            return (
              <TableRow key={row.channel_id}>
                <TableCell className='max-w-72 truncate'>
                  {row.channel_name || `#${row.channel_id}`}
                </TableCell>
                <TableCell>
                  <div className='flex items-center gap-2'>
                    <Progress value={percent} className='h-2 w-32' />
                    <span className='tabular-nums text-xs text-muted-foreground'>
                      {row.total > 0 ? `${percent.toFixed(1)}%` : '-'}
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge variant={row.status === 'ok' ? 'secondary' : 'destructive'}>
                    {row.status === 'ok' ? t('Available') : 'error'}
                  </Badge>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
