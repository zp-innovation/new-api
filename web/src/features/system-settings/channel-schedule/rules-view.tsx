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
import { zodResolver } from '@hookform/resolvers/zod'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { SettingsCard } from '../components/settings-card'
import { SettingsSwitchField } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { RelatedPolicyLink } from '../request-policies/related-policy-link'
import { safeNumberFieldProps } from '../utils/numeric-field'
import type { ScheduleChannelOption } from './api'
import {
  formatWeekdays,
  parseScheduleRules,
  serializeScheduleRules,
} from './lib/schedule-rules'
import { RuleEditorDialog } from './rule-editor-dialog'
import {
  CHANNEL_SCHEDULE_CHECK_MINUTES_KEY,
  CHANNEL_SCHEDULE_DEFAULT_CHECK_MINUTES,
  CHANNEL_SCHEDULE_DEFAULT_TIMEZONE,
  CHANNEL_SCHEDULE_ENABLED_KEY,
  CHANNEL_SCHEDULE_MAX_CHECK_MINUTES,
  CHANNEL_SCHEDULE_RULES_KEY,
  CHANNEL_SCHEDULE_TIMEZONE_KEY,
  CHANNEL_SCHEDULE_TIMEZONES,
  type ChannelScheduleAction,
  type ChannelScheduleRule,
  type ChannelScheduleStatus,
} from './types'

type RulesViewProps = {
  options: Record<string, string>
  status: ChannelScheduleStatus
  channelOptions: ScheduleChannelOption[]
  retryTimes: number
  isSaving: boolean
  onSave: (options: Record<string, string>) => void
}

function createGlobalFormSchema(t: (key: string) => string) {
  return z.object({
    enabled: z.boolean(),
    timezone: z.string().min(1),
    checkMinutes: z
      .number()
      .int()
      .min(1, t('Enter a value between 1 and 1440'))
      .max(
        CHANNEL_SCHEDULE_MAX_CHECK_MINUTES,
        t('Enter a value between 1 and 1440')
      ),
  })
}

type GlobalFormValues = z.infer<ReturnType<typeof createGlobalFormSchema>>

export function ChannelScheduleRulesView(props: RulesViewProps) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<ChannelScheduleRule[]>(() =>
    parseScheduleRules(props.options[CHANNEL_SCHEDULE_RULES_KEY])
  )
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<ChannelScheduleRule | null>(
    null
  )
  const [editorKey, setEditorKey] = useState(0)
  // Minted when the editor opens for a new rule so rendering stays pure.
  const [draftRuleId, setDraftRuleId] = useState('')
  const [pendingDelete, setPendingDelete] =
    useState<ChannelScheduleRule | null>(null)

  const schema = useMemo(() => createGlobalFormSchema((key) => t(key)), [t])
  const form = useForm<GlobalFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      enabled: props.options[CHANNEL_SCHEDULE_ENABLED_KEY] === 'true',
      timezone:
        props.options[CHANNEL_SCHEDULE_TIMEZONE_KEY] ??
        CHANNEL_SCHEDULE_DEFAULT_TIMEZONE,
      checkMinutes:
        Number(props.options[CHANNEL_SCHEDULE_CHECK_MINUTES_KEY]) ||
        CHANNEL_SCHEDULE_DEFAULT_CHECK_MINUTES,
    },
  })

  const openCreate = () => {
    setEditingRule(null)
    setDraftRuleId(`rule-${Date.now()}`)
    setEditorKey((value) => value + 1)
    setEditorOpen(true)
  }

  const openEdit = (rule: ChannelScheduleRule) => {
    setEditingRule(rule)
    setEditorKey((value) => value + 1)
    setEditorOpen(true)
  }

  const upsertRule = (rule: ChannelScheduleRule) => {
    setRules((current) => {
      const index = current.findIndex((item) => item.id === rule.id)
      if (index === -1) return [...current, rule]
      const next = [...current]
      next[index] = rule
      return next
    })
  }

  const confirmDelete = () => {
    if (!pendingDelete) return
    setRules((current) =>
      current.filter((item) => item.id !== pendingDelete.id)
    )
    setPendingDelete(null)
  }

  const save = async () => {
    const valid = await form.trigger()
    if (!valid) return
    const values = form.getValues()
    const next: Record<string, string> = {
      [CHANNEL_SCHEDULE_ENABLED_KEY]: values.enabled ? 'true' : 'false',
      [CHANNEL_SCHEDULE_TIMEZONE_KEY]: values.timezone,
      [CHANNEL_SCHEDULE_CHECK_MINUTES_KEY]: String(values.checkMinutes),
      [CHANNEL_SCHEDULE_RULES_KEY]: serializeScheduleRules(rules),
    }
    const delta = Object.fromEntries(
      Object.entries(next).filter(
        ([key, value]) => props.options[key] !== value
      )
    )
    if (Object.keys(delta).length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    props.onSave(delta)
  }

  return (
    <Form {...form}>
      <div className='flex min-w-0 flex-col gap-4'>
        <SettingsPageFormActions
          onSave={() => void save()}
          isSaving={props.isSaving}
        />

        {props.retryTimes <= 0 ? (
          <Alert data-testid='channel-schedule-retry-warning'>
            <AlertTitle>
              {t(
                'Priority is layered: traffic only drops to the next layer on a retry.'
              )}
            </AlertTitle>
            <AlertDescription>
              {t(
                'The maximum retry count is 0, so this schedule cannot hand traffic to the lower layer. Raise it in Sessions and retries.'
              )}
              <RelatedPolicyLink section='routing' />
            </AlertDescription>
          </Alert>
        ) : null}

        <SettingsCard
          title={t('Channel time schedule')}
          description={t(
            'Switch channels between priority, weight and enabled state by local time.'
          )}
        >
          <div className='flex flex-col gap-4'>
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <SettingsSwitchField
                  controlId='channel_schedule_setting.enabled'
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  label={t('Enable channel time schedule')}
                  description={t(
                    'When off, no rule runs and values already applied stay unchanged'
                  )}
                />
              )}
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='timezone'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Timezone')}</FormLabel>
                    <FormControl>
                      <NativeSelect
                        className='w-full'
                        aria-label={t('Timezone')}
                        value={field.value}
                        onChange={(event) => field.onChange(event.target.value)}
                      >
                        {CHANNEL_SCHEDULE_TIMEZONES.map((zone) => (
                          <NativeSelectOption key={zone} value={zone}>
                            {zone}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='checkMinutes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Check interval')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        max={CHANNEL_SCHEDULE_MAX_CHECK_MINUTES}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>
        </SettingsCard>

        <SettingsCard
          title={t('Rules ({{count}})', { count: rules.length })}
          description={t(
            'The first matching rule wins; fields a rule does not set stay untouched.'
          )}
        >
          <div className='flex flex-col gap-3'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <Badge
                variant={
                  props.status.matched_rule_name ? 'default' : 'secondary'
                }
              >
                {props.status.matched_rule_name
                  ? t('Currently matched: {{name}}', {
                      name: props.status.matched_rule_name,
                    })
                  : t('No rule is active right now')}
              </Badge>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={openCreate}
              >
                <Plus data-icon='inline-start' />
                {t('Add rule')}
              </Button>
            </div>

            {rules.length === 0 ? (
              <p className='text-muted-foreground text-sm'>
                {t('No rules yet. Add one to switch channels by time.')}
              </p>
            ) : (
              rules.map((rule) => (
                <ScheduleRuleCard
                  key={rule.id}
                  rule={rule}
                  scheduleEnabled={props.status.enabled}
                  isMatched={props.status.matched_rule_id === rule.id}
                  onEdit={() => openEdit(rule)}
                  onDelete={() => setPendingDelete(rule)}
                  onToggle={(enabled) => upsertRule({ ...rule, enabled })}
                />
              ))
            )}
          </div>
        </SettingsCard>

        <RuleEditorDialog
          key={editorKey}
          open={editorOpen}
          onOpenChange={setEditorOpen}
          rule={editingRule}
          ruleId={editingRule?.id ?? draftRuleId}
          channelOptions={props.channelOptions}
          onSubmit={(rule) => upsertRule(rule)}
        />

        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(open) => {
            if (!open) setPendingDelete(null)
          }}
          title={t('Delete rule')}
          desc={t(
            'The rule is removed from the list; channels it already changed keep their current values.'
          )}
          confirmText={t('Delete')}
          destructive
          handleConfirm={confirmDelete}
        />
      </div>
    </Form>
  )
}

type ScheduleRuleCardProps = {
  rule: ChannelScheduleRule
  scheduleEnabled: boolean
  isMatched: boolean
  onEdit: () => void
  onDelete: () => void
  onToggle: (enabled: boolean) => void
}

function ScheduleRuleCard(props: ScheduleRuleCardProps) {
  const { t } = useTranslation()
  const rule = props.rule
  const stateLabel = () => {
    if (!rule.enabled || !props.scheduleEnabled) return t('Disabled')
    if (props.isMatched) return t('Active')
    return t('Not matched')
  }
  const stateVariant = () => {
    if (!rule.enabled || !props.scheduleEnabled) return 'outline' as const
    if (props.isMatched) return 'default' as const
    return 'secondary' as const
  }
  const describeEnabled = (action: ChannelScheduleAction): string => {
    if (action.enabled === undefined) return t('Not modified')
    return action.enabled ? t('Enabled') : t('Disabled')
  }
  // Actions carry no id, so rows are keyed by their target plus an occurrence
  // counter; that stays stable and unique without depending on the index.
  const seenTargets = new Map<string, number>()
  const rows = rule.actions.map((action) => {
    const target = `${action.mode}:${action.tag ?? (action.channel_ids ?? []).join(',')}`
    const occurrence = seenTargets.get(target) ?? 0
    seenTargets.set(target, occurrence + 1)
    return { key: `${rule.id}-${target}#${occurrence}`, action }
  })

  return (
    <div className='rounded-xl border'>
      <div className='bg-muted/20 flex flex-wrap items-center gap-3 border-b px-3 py-2.5'>
        <Switch
          checked={rule.enabled}
          onCheckedChange={props.onToggle}
          aria-label={`${t('Enabled')} ${rule.name}`}
        />
        <span className='text-sm font-medium'>{rule.name}</span>
        <Badge variant='outline'>{`${rule.start} – ${rule.end}`}</Badge>
        <span className='text-muted-foreground text-xs'>
          {formatWeekdays(rule, t)}
        </span>
        <Badge variant={stateVariant()}>{stateLabel()}</Badge>
        <span className='flex-1' />
        <Button type='button' size='sm' variant='ghost' onClick={props.onEdit}>
          <Pencil data-icon='inline-start' />
          {t('Edit rule')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          onClick={props.onDelete}
        >
          <Trash2 data-icon='inline-start' />
          {t('Delete rule')}
        </Button>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className='w-[32%]'>{t('Scope')}</TableHead>
            <TableHead>{t('Priority')}</TableHead>
            <TableHead>{t('Weight')}</TableHead>
            <TableHead>{t('Enabled state')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.key}>
              <TableCell>
                {row.action.mode === 'tag'
                  ? `tag: ${row.action.tag ?? ''}`
                  : (row.action.channel_ids ?? [])
                      .map((id) => `#${id}`)
                      .join(', ')}
              </TableCell>
              <TableCell>{row.action.priority ?? t('Not modified')}</TableCell>
              <TableCell>
                <span className='flex flex-wrap items-center gap-2'>
                  {row.action.weight ?? t('Not modified')}
                  {row.action.weight_mode === 'tag' ? (
                    <Badge variant='outline'>{t('Per tag')}</Badge>
                  ) : null}
                </span>
              </TableCell>
              <TableCell>{describeEnabled(row.action)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
