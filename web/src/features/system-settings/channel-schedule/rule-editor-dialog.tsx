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
import { Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useFieldArray, useForm, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import type { ScheduleChannelOption } from './api'
import {
  createEmptyActionForm,
  createEmptyRuleForm,
  createScheduleRuleFormSchema,
  formValuesToRule,
  ruleToFormValues,
  WEEKDAY_VALUES,
  weekdayLabelKey,
  type ScheduleRuleFormValues,
} from './lib/schedule-rules'
import {
  CHANNEL_SCHEDULE_MAX_PRIORITY,
  CHANNEL_SCHEDULE_MAX_SHARE,
  CHANNEL_SCHEDULE_MAX_WEIGHT,
  type ChannelScheduleRule,
} from './types'

type RuleEditorDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** null creates a new rule. */
  rule: ChannelScheduleRule | null
  ruleId: string
  channelOptions: ScheduleChannelOption[]
  onSubmit: (rule: ChannelScheduleRule) => void
}

export function RuleEditorDialog(props: RuleEditorDialogProps) {
  const { t } = useTranslation()
  const schema = useMemo(() => createScheduleRuleFormSchema(t), [t])
  const initialValues = useMemo(
    () =>
      props.rule
        ? ruleToFormValues(props.rule)
        : createEmptyRuleForm(props.ruleId),
    [props.rule, props.ruleId]
  )
  const form = useForm<ScheduleRuleFormValues>({
    resolver: zodResolver(schema),
    defaultValues: initialValues,
  })

  const submit = form.handleSubmit((values) => {
    props.onSubmit(formValuesToRule(values))
    props.onOpenChange(false)
  })

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {props.rule ? t('Edit rule') : t('Add rule')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'The window is [start, end); an end earlier than the start wraps past midnight, and equal values cover the whole day.'
            )}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form
            id='channel-schedule-rule-form'
            onSubmit={submit}
            className='flex flex-col gap-5'
          >
            <div className='grid gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem className='sm:col-span-2'>
                    <FormLabel>{t('Rule name')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Rule name')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='start'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Start time')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder='07:55' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='end'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('End time')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder='22:00' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between gap-4'>
                  <FormLabel>{t('Enabled')}</FormLabel>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <WeekdayField form={form} />

            <ActionFields form={form} channelOptions={props.channelOptions} />
          </form>
        </Form>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form='channel-schedule-rule-form'>
            {t('Save rule')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Days are a multi-select. Leaving every day unchecked means the rule runs every
 * day, which the hint spells out because an empty selection is easy to misread.
 */
function WeekdayField(props: { form: UseFormReturn<ScheduleRuleFormValues> }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='weekdays'
      render={({ field }) => {
        const selected = field.value ?? []
        const toggle = (day: number, checked: boolean) => {
          const next = checked
            ? [...selected, day]
            : selected.filter((value) => value !== day)
          field.onChange(next)
        }
        return (
          <FormItem>
            <FormLabel>{t('Weekdays')}</FormLabel>
            <FormControl>
              <div className='flex flex-wrap gap-x-4 gap-y-2'>
                {WEEKDAY_VALUES.map((day) => (
                  <Label
                    key={day}
                    className='flex items-center gap-2 font-normal'
                  >
                    <Checkbox
                      checked={selected.includes(day)}
                      onCheckedChange={(checked) =>
                        toggle(day, checked === true)
                      }
                    />
                    {t(weekdayLabelKey(day))}
                  </Label>
                ))}
              </div>
            </FormControl>
            <p className='text-muted-foreground text-xs'>{t('Every day')}</p>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}

/**
 * Tags are free text, the same way the channel editor and the bulk tag dialog
 * treat them. Existing tags are only offered as suggestions: an administrator
 * has to be able to write a rule for a tag before any channel carries it, and a
 * list restricted to tags already in use would make the tag scope impossible to
 * configure on a fresh install.
 */
const TAG_SUGGESTIONS_ID = 'channel-schedule-tag-suggestions'

function ActionFields(props: {
  form: UseFormReturn<ScheduleRuleFormValues>
  channelOptions: ScheduleChannelOption[]
}) {
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({
    control: props.form.control,
    name: 'actions',
  })
  // The array is typed as the per-action list, but a rule-level issue (mixing
  // per-account and per-tag weights) is stored here as one error object.
  const actionsError = props.form.formState.errors.actions
  const ruleLevelError =
    actionsError && 'message' in actionsError && actionsError.message
      ? String(actionsError.message)
      : null
  const tagOptions = useMemo(() => {
    const tags = new Set<string>()
    for (const option of props.channelOptions) {
      if (option.tag) tags.add(option.tag)
    }
    return [...tags].sort((left, right) => left.localeCompare(right))
  }, [props.channelOptions])

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between gap-3'>
        <span className='text-sm font-medium'>
          {t('Actions ({{count}})', { count: fields.length })}
        </span>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => append(createEmptyActionForm())}
        >
          <Plus data-icon='inline-start' />
          {t('Add action')}
        </Button>
      </div>

      {tagOptions.length > 0 ? (
        <datalist id={TAG_SUGGESTIONS_ID}>
          {tagOptions.map((tag) => (
            <option key={tag} value={tag} />
          ))}
        </datalist>
      ) : null}

      {fields.map((field, index) => (
        <ActionEditor
          key={field.id}
          form={props.form}
          index={index}
          tagOptions={tagOptions}
          channelOptions={props.channelOptions}
          canRemove={fields.length > 1}
          onRemove={() => remove(index)}
        />
      ))}
      {ruleLevelError ? (
        <p role='alert' className='text-destructive text-sm'>
          {ruleLevelError}
        </p>
      ) : null}
    </div>
  )
}

function ActionEditor(props: {
  form: UseFormReturn<ScheduleRuleFormValues>
  index: number
  tagOptions: string[]
  channelOptions: ScheduleChannelOption[]
  canRemove: boolean
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const { form, index } = props
  const mode = form.watch(`actions.${index}.mode`)

  return (
    <div className='bg-muted/20 flex flex-col gap-4 rounded-xl border p-3'>
      <div className='flex items-center justify-between gap-3'>
        <Select
          value={mode}
          onValueChange={(value) =>
            form.setValue(`actions.${index}.mode`, value as 'tag' | 'channel', {
              shouldDirty: true,
            })
          }
        >
          <SelectTrigger
            className='w-full sm:w-[160px]'
            aria-label={t('Scope')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectItem value='tag'>{t('By tag')}</SelectItem>
            <SelectItem value='channel'>{t('By channel')}</SelectItem>
          </SelectContent>
        </Select>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          disabled={!props.canRemove}
          onClick={props.onRemove}
        >
          <Trash2 data-icon='inline-start' />
          {t('Remove action')}
        </Button>
      </div>

      {mode === 'tag' ? (
        <FormField
          control={form.control}
          name={`actions.${index}.tag`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Tag')}</FormLabel>
              <FormControl>
                <Input
                  list={
                    props.tagOptions.length > 0 ? TAG_SUGGESTIONS_ID : undefined
                  }
                  autoComplete='off'
                  placeholder={t('Tag Name')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      ) : (
        <FormField
          control={form.control}
          name={`actions.${index}.channelIds`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Channels')}</FormLabel>
              <FormControl>
                <ChannelTargetPicker
                  options={props.channelOptions}
                  selected={field.value ?? []}
                  onChange={field.onChange}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <div className='flex flex-col gap-2'>
        <ActionPriorityField form={form} index={index} />
        <ActionWeightField form={form} index={index} />
        <ActionEnabledField form={form} index={index} />
      </div>
    </div>
  )
}

function ActionPriorityField(props: {
  form: UseFormReturn<ScheduleRuleFormValues>
  index: number
}) {
  const { t } = useTranslation()
  const { form, index } = props
  const checked = form.watch(`actions.${index}.usePriority`)
  return (
    <div className='flex flex-wrap items-center gap-3'>
      <Label className='flex items-center gap-2 font-normal'>
        <Checkbox
          checked={checked}
          onCheckedChange={(next) =>
            form.setValue(`actions.${index}.usePriority`, next === true, {
              shouldDirty: true,
            })
          }
        />
        {t('Priority')}
      </Label>
      <Input
        type='number'
        min={0}
        max={CHANNEL_SCHEDULE_MAX_PRIORITY}
        disabled={!checked}
        aria-label={t('Priority')}
        className='w-32'
        value={form.watch(`actions.${index}.priority`)}
        onChange={(event) => {
          const next = event.target.valueAsNumber
          if (Number.isFinite(next)) {
            form.setValue(`actions.${index}.priority`, next, {
              shouldDirty: true,
            })
          }
        }}
      />
    </div>
  )
}

/**
 * Weight is either written to every account in scope or, in per-tag mode,
 * treated as the tag's share and split across the accounts that tag holds right
 * now. Only the split holds a ratio between two tags while accounts are added
 * and removed; a fixed per-account number drifts with the count.
 */
function ActionWeightField(props: {
  form: UseFormReturn<ScheduleRuleFormValues>
  index: number
}) {
  const { t } = useTranslation()
  const { form, index } = props
  const mode = form.watch(`actions.${index}.weightMode`)
  const isShare = mode === 'tag'
  return (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap items-start gap-3'>
        <FormField
          control={form.control}
          name={`actions.${index}.weightMode`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Weight mode')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger className='w-40'>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectItem value='none'>{t('Not modified')}</SelectItem>
                  <SelectItem value='channel'>{t('Per account')}</SelectItem>
                  <SelectItem value='tag'>{t('Per tag')}</SelectItem>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name={`actions.${index}.weight`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{isShare ? t('Share') : t('Weight')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={isShare ? 1 : 0}
                  max={
                    isShare
                      ? CHANNEL_SCHEDULE_MAX_SHARE
                      : CHANNEL_SCHEDULE_MAX_WEIGHT
                  }
                  disabled={mode === 'none'}
                  className='w-32'
                  value={field.value}
                  onChange={(event) => {
                    const next = event.target.valueAsNumber
                    if (Number.isFinite(next)) field.onChange(next)
                  }}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
      {isShare ? (
        <FormDescription>
          {t('Give each tag the same share to split traffic evenly.')}{' '}
          {t(
            "Split this tag's weight across its accounts so the ratio holds as accounts change."
          )}
        </FormDescription>
      ) : null}
    </div>
  )
}

function ActionEnabledField(props: {
  form: UseFormReturn<ScheduleRuleFormValues>
  index: number
}) {
  const { t } = useTranslation()
  const { form, index } = props
  const checked = form.watch(`actions.${index}.useEnabled`)
  const enabled = form.watch(`actions.${index}.enabled`)
  return (
    <div className='flex flex-wrap items-center gap-3'>
      <Label className='flex items-center gap-2 font-normal'>
        <Checkbox
          checked={checked}
          onCheckedChange={(next) =>
            form.setValue(`actions.${index}.useEnabled`, next === true, {
              shouldDirty: true,
            })
          }
        />
        {t('Enabled state')}
      </Label>
      <Select
        value={enabled ? 'enabled' : 'disabled'}
        onValueChange={(value) =>
          form.setValue(`actions.${index}.enabled`, value === 'enabled', {
            shouldDirty: true,
          })
        }
      >
        <SelectTrigger
          className='w-32'
          disabled={!checked}
          aria-label={t('Enabled state')}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value='enabled'>{t('Enabled')}</SelectItem>
          <SelectItem value='disabled'>{t('Disabled')}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}

/**
 * Channel targets are picked from a searchable list. A tag cannot be used here:
 * a channel belongs to exactly one tag, and the backend resolves by-channel
 * actions from explicit ids.
 */
function ChannelTargetPicker(props: {
  options: ScheduleChannelOption[]
  selected: number[]
  onChange: (next: number[]) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return props.options
    return props.options.filter(
      (option) =>
        option.name.toLowerCase().includes(needle) ||
        String(option.id) === needle ||
        option.tag.toLowerCase().includes(needle)
    )
  }, [props.options, search])

  const toggle = (id: number, checked: boolean) => {
    props.onChange(
      checked
        ? [...props.selected, id]
        : props.selected.filter((value) => value !== id)
    )
  }

  return (
    <div className='flex flex-col gap-2'>
      <Input
        value={search}
        onChange={(event) => setSearch(event.target.value)}
        placeholder={t('Search channels')}
        aria-label={t('Search channels')}
      />
      <div className='max-h-40 overflow-y-auto rounded-lg border'>
        {filtered.length === 0 ? (
          <p className='text-muted-foreground p-3 text-xs'>
            {t('No channels match the search')}
          </p>
        ) : (
          filtered.map((option) => (
            <Label
              key={option.id}
              className={cn(
                'flex items-center gap-2 border-b px-3 py-2 font-normal last:border-b-0'
              )}
            >
              <Checkbox
                checked={props.selected.includes(option.id)}
                onCheckedChange={(checked) =>
                  toggle(option.id, checked === true)
                }
              />
              <span className='text-muted-foreground w-10 text-xs'>
                #{option.id}
              </span>
              <span className='min-w-0 flex-1 truncate text-sm'>
                {option.name}
              </span>
              {option.tag ? (
                <span className='text-muted-foreground text-xs'>
                  {option.tag}
                </span>
              ) : null}
            </Label>
          ))
        )}
      </div>
    </div>
  )
}
