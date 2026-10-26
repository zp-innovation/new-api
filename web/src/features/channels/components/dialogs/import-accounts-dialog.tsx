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
import { CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  createChannel,
  fetchUpstreamModels,
  searchChannels,
  updateChannel,
} from '../../api'
import { ERROR_MESSAGES } from '../../constants'
import {
  credentialHasBlockingIssue,
  parseCredentialImport,
  type CredentialIssueCode,
  type ImportedCredential,
} from '../../lib/channel-credential-import'
import type { Channel } from '../../types'

/** i18n key per parser issue code; the parser itself stays text-free. */
const ISSUE_MESSAGE_KEYS: Record<CredentialIssueCode, string> = {
  'missing-access-token': 'Missing access_token',
  'missing-project-id': 'Missing project_id',
  'missing-account-id': 'Missing account_id',
  'missing-uid': 'Missing uid',
  'missing-refresh-token': 'Missing refresh_token',
  'credential-type-mismatch':
    'Credential type does not match this channel type',
}

type ImportRowStatus = 'created' | 'failed'

type ImportRow = {
  index: number
  name: string
  label: string
  status: ImportRowStatus
  /** Failure detail, or the warning left by a failed model discovery. */
  message?: string
  /** Comma separated models written back after a successful discovery. */
  models?: string
  /** Created channel row, resolved so the result view can reopen it. */
  channel?: Channel
}

type ImportAccountsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /**
   * Channel type of every credential in the batch
   * (57 Codex, 64 Antigravity, 65 WorkBuddy).
   */
  channelType: number
  /** Localized channel type name, used as the default name prefix. */
  channelTypeName: string
  baseURL?: string
  group?: string
  proxy?: string
  /** Models copied onto every new channel when discovery stays disabled. */
  models?: string
  /** Called once with the number of channels that were created. */
  onImported?: (count: number) => void
  /**
   * Opens a channel created by this batch in the parent editor, so the
   * operator does not have to close and reopen the page to keep editing.
   */
  onEditChannel?: (channel: Channel) => void
}

function buildSettingJson(proxy?: string): string {
  return JSON.stringify({ proxy: proxy?.trim() ?? '' })
}

/**
 * Bulk import for the OAuth channel types: paste or select several credential
 * JSON objects and create one channel per account in a single pass. Every
 * credential is stored as compact single-line JSON, because the key column is
 * newline-delimited and a pretty-printed value would be split into fragments.
 */
export function ImportAccountsDialog(props: ImportAccountsDialogProps) {
  const { t } = useTranslation()
  const [raw, setRaw] = useState('')
  const [namePrefix, setNamePrefix] = useState(props.channelTypeName)
  const [autoFetchModels, setAutoFetchModels] = useState(true)
  const [importing, setImporting] = useState(false)
  const [resolvingRow, setResolvingRow] = useState<number | null>(null)
  const [rows, setRows] = useState<ImportRow[]>([])

  useEffect(() => {
    if (!props.open) return
    setNamePrefix(props.channelTypeName)
  }, [props.open, props.channelTypeName])

  const parsed = useMemo(
    () => parseCredentialImport(raw, props.channelType, namePrefix),
    [raw, props.channelType, namePrefix]
  )

  const importable = useMemo(
    () =>
      parsed.credentials.filter((entry) => !credentialHasBlockingIssue(entry)),
    [parsed.credentials]
  )

  const handleOpenChange = useCallback(
    (next: boolean) => {
      // Keep the dialog open while channels are still being created.
      if (importing && !next) return
      props.onOpenChange(next)
      if (next) setRows([])
    },
    [importing, props]
  )

  const handleFiles = useCallback(
    async (fileList: FileList | null) => {
      const files = fileList ? [...fileList] : []
      if (files.length === 0) return

      const texts: string[] = []
      for (const file of files) {
        try {
          texts.push((await file.text()).trim())
        } catch {
          toast.error(
            t('Failed to parse JSON file: {{name}}', { name: file.name })
          )
          return
        }
      }

      setRaw((current) => [current.trim(), ...texts].filter(Boolean).join('\n'))
    },
    [t]
  )

  const resolveChannel = useCallback(async (name: string) => {
    const response = await searchChannels({ keyword: name, page_size: 20 })
    const items = response.data?.items ?? []
    // Channel names are not unique: an earlier import may share one. The
    // highest id is the row this batch just created.
    let match: Channel | null = null
    for (const item of items) {
      if (item.name !== name) continue
      if (!match || item.id > match.id) {
        match = item
      }
    }
    return match
  }, [])

  // The created row id is only resolved when it is needed: model discovery
  // needs it up front, while the result view's Edit action resolves it lazily
  // so a batch that is never reopened costs no extra requests.
  const handleEditChannel = useCallback(
    async (row: ImportRow) => {
      const editChannel = props.onEditChannel
      if (!editChannel) return
      if (row.channel) {
        editChannel(row.channel)
        return
      }
      setResolvingRow(row.index)
      try {
        const channel = await resolveChannel(row.name)
        if (!channel) {
          throw new Error(t('Failed to open the created channel'))
        }
        setRows((current) =>
          current.map((entry) =>
            entry.index === row.index ? { ...entry, channel } : entry
          )
        )
        editChannel(channel)
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : t('Failed to open the created channel')
        )
      } finally {
        setResolvingRow(null)
      }
    },
    [props, resolveChannel, t]
  )

  const handleImport = useCallback(async () => {
    if (importing || importable.length === 0) return

    setImporting(true)
    const nextRows: ImportRow[] = importable.map((entry) => ({
      index: entry.index,
      name: entry.name,
      label: entry.label,
      status: 'failed' as ImportRowStatus,
    }))
    setRows([...nextRows])

    let createdCount = 0
    for (let position = 0; position < importable.length; position += 1) {
      const credential: ImportedCredential = importable[position]
      const row: ImportRow = { ...nextRows[position] }
      try {
        const response = await createChannel({
          mode: 'single',
          channel: {
            name: credential.name,
            type: props.channelType,
            key: credential.key,
            base_url: props.baseURL?.trim() ?? '',
            models: props.models ?? '',
            group: props.group?.trim() || 'default',
            priority: 0,
            weight: 0,
            status: 1,
            auto_ban: 1,
            setting: buildSettingJson(props.proxy),
            settings: '{}',
            other: '',
          },
        })
        if (!response.success) {
          throw new Error(response.message || t(ERROR_MESSAGES.CREATE_FAILED))
        }

        row.status = 'created'
        createdCount += 1

        // Model discovery needs the new row id; the Edit action resolves it
        // on demand instead.
        if (autoFetchModels) {
          try {
            row.channel = (await resolveChannel(credential.name)) ?? undefined
          } catch {
            row.channel = undefined
          }
          try {
            if (!row.channel) {
              throw new Error(t(ERROR_MESSAGES.FETCH_MODELS_FAILED))
            }
            const fetched = await fetchUpstreamModels(row.channel.id)
            const list = Array.isArray(fetched.data) ? fetched.data : []
            if (!fetched.success || list.length === 0) {
              throw new Error(
                fetched.message || t(ERROR_MESSAGES.FETCH_MODELS_FAILED)
              )
            }
            const updated = await updateChannel(row.channel.id, {
              models: list.join(','),
            })
            if (!updated.success) {
              throw new Error(
                updated.message || t(ERROR_MESSAGES.UPDATE_FAILED)
              )
            }
            row.models = list.join(',')
          } catch (error) {
            row.message =
              error instanceof Error
                ? error.message
                : t(ERROR_MESSAGES.FETCH_MODELS_FAILED)
          }
        }
      } catch (error) {
        row.status = 'failed'
        row.message =
          error instanceof Error
            ? error.message
            : t(ERROR_MESSAGES.CREATE_FAILED)
      }

      nextRows[position] = row
      setRows([...nextRows])
    }

    setImporting(false)
    if (createdCount > 0) {
      toast.success(t('Imported {{count}} account(s)', { count: createdCount }))
      props.onImported?.(createdCount)
    }
  }, [autoFetchModels, importable, importing, props, resolveChannel, t])

  const skippedIssueCount = parsed.credentials.length - importable.length

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Import Accounts')}
      description={t(
        'Paste one or more OAuth credential JSON objects; each credential becomes its own channel.'
      )}
      contentClassName='sm:max-w-3xl'
      footer={
        !importing && rows.length > 0 ? (
          // The import already ran; a single finish action replaces the pair
          // so the dialog has a clear way out of the result view.
          <Button type='button' onClick={() => handleOpenChange(false)}>
            {t('Done')}
          </Button>
        ) : (
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={importing}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              onClick={() => void handleImport()}
              disabled={importing || importable.length === 0}
            >
              {importing ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : null}
              {importing
                ? t('Importing…')
                : t('Import {{count}} account(s)', {
                    count: importable.length,
                  })}
            </Button>
          </>
        )
      }
    >
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label htmlFor='import-accounts-raw'>
            {t('Paste credential JSON')}
          </Label>
          <Textarea
            id='import-accounts-raw'
            value={raw}
            onChange={(event) => setRaw(event.target.value)}
            placeholder={t('Paste credential JSON')}
            className='min-h-32 font-mono text-xs'
            disabled={importing}
          />
        </div>

        <div className='grid gap-4 sm:grid-cols-2'>
          <div className='space-y-2'>
            <Label htmlFor='import-accounts-prefix'>{t('Name prefix')}</Label>
            <Input
              id='import-accounts-prefix'
              value={namePrefix}
              onChange={(event) => setNamePrefix(event.target.value)}
              disabled={importing}
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='import-accounts-files'>
              {t('Credential JSON files')}
            </Label>
            <Input
              id='import-accounts-files'
              type='file'
              accept='.json,application/json'
              multiple
              disabled={importing}
              onChange={(event) => {
                const fileList = event.target.files
                // Allow selecting the same files again after a mistake.
                event.target.value = ''
                void handleFiles(fileList)
              }}
            />
          </div>
        </div>

        <div className='flex items-center gap-2'>
          <Checkbox
            id='import-accounts-models'
            checked={autoFetchModels}
            onCheckedChange={(value) => setAutoFetchModels(value === true)}
            disabled={importing}
          />
          <Label htmlFor='import-accounts-models' className='font-normal'>
            {t('Fetch models from upstream after import')}
          </Label>
        </div>

        {parsed.invalidCount > 0 ? (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Skipped {{count}} unparsable JSON block(s)', {
                count: parsed.invalidCount,
              })}
            </AlertDescription>
          </Alert>
        ) : null}

        {skippedIssueCount > 0 ? (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('{{count}} credential(s) cannot be imported', {
                count: skippedIssueCount,
              })}
            </AlertDescription>
          </Alert>
        ) : null}

        {rows.length === 0 && parsed.credentials.length > 0 ? (
          <ScrollArea className='max-h-64 rounded-md border'>
            <ul className='divide-y'>
              {parsed.credentials.map((credential) => {
                // Blocked entries stay visible so the reason is on screen.
                const blocked = credentialHasBlockingIssue(credential)
                return (
                  <li
                    key={credential.index}
                    className={cn(
                      'flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm',
                      blocked && 'opacity-60'
                    )}
                  >
                    <div className='min-w-0'>
                      <p className='truncate font-medium'>{credential.name}</p>
                      <p className='text-muted-foreground truncate font-mono text-xs'>
                        {credential.label}
                      </p>
                    </div>
                    <div className='flex flex-wrap items-center gap-1'>
                      {credential.issues.map((issue) => (
                        <Badge
                          key={issue.code}
                          variant={
                            issue.severity === 'error'
                              ? 'destructive'
                              : 'warning'
                          }
                        >
                          {t(ISSUE_MESSAGE_KEYS[issue.code])}
                        </Badge>
                      ))}
                    </div>
                  </li>
                )
              })}
            </ul>
          </ScrollArea>
        ) : null}

        {importable.length === 0 && parsed.invalidCount === 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Paste one or more OAuth credential JSON objects; each credential becomes its own channel.'
            )}
          </p>
        ) : null}

        {rows.length > 0 ? (
          <div className='space-y-2'>
            <p className='text-sm font-medium'>{t('Import result')}</p>
            <ScrollArea className='max-h-64 rounded-md border'>
              <ul className='divide-y'>
                {rows.map((row) => {
                  return (
                    <li
                      key={row.index}
                      className='flex items-start justify-between gap-2 px-3 py-2 text-sm'
                    >
                      <div className='min-w-0'>
                        <p className='truncate font-medium'>{row.name}</p>
                        <p className='text-muted-foreground truncate font-mono text-xs'>
                          {row.label}
                        </p>
                        {row.models ? (
                          <p className='text-muted-foreground text-xs'>
                            {t('Fetched {{count}} models', {
                              count: row.models.split(',').length,
                            })}
                          </p>
                        ) : null}
                        {row.message ? (
                          <p className='text-destructive text-xs'>
                            {row.message}
                          </p>
                        ) : null}
                      </div>
                      <div className='flex shrink-0 items-center gap-1'>
                        {row.status === 'created' && props.onEditChannel ? (
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            className='h-7'
                            disabled={resolvingRow === row.index}
                            onClick={() => void handleEditChannel(row)}
                          >
                            {resolvingRow === row.index ? (
                              <Loader2 className='h-4 w-4 animate-spin' />
                            ) : (
                              t('Edit')
                            )}
                          </Button>
                        ) : null}
                        {row.status === 'created' ? (
                          <CheckCircle2 className='text-success mt-0.5 h-4 w-4 shrink-0' />
                        ) : (
                          <XCircle className='text-destructive mt-0.5 h-4 w-4 shrink-0' />
                        )}
                      </div>
                    </li>
                  )
                })}
              </ul>
            </ScrollArea>
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}
