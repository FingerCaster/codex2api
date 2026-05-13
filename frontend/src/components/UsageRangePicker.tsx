import { CalendarDays, ChevronDown, ChevronUp } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

export type UsageRangePreset =
  | 'all'
  | 'today'
  | 'yesterday'
  | '1h'
  | '6h'
  | '24h'
  | '7d'
  | '14d'
  | '30d'
  | 'thisMonth'
  | 'lastMonth'
  | 'custom'

export interface UsageRangeValue {
  preset: UsageRangePreset
  startDate: string
  endDate: string
}

const PRESET_BUTTONS: Exclude<UsageRangePreset, 'custom' | 'all' | '1h' | '6h'>[] = ['today', 'yesterday', '24h', '7d', '14d', '30d', 'thisMonth', 'lastMonth']

export function createUsageRangeValue(preset: Exclude<UsageRangePreset, 'custom'>): UsageRangeValue {
  const now = new Date()

  switch (preset) {
    case 'all':
      return {
        preset,
        startDate: '2020-01-01',
        endDate: formatDateInput(now),
      }
    case 'today':
      return {
        preset,
        startDate: formatDateInput(now),
        endDate: formatDateInput(now),
      }
    case 'yesterday': {
      const yesterday = addDays(now, -1)
      return {
        preset,
        startDate: formatDateInput(yesterday),
        endDate: formatDateInput(yesterday),
      }
    }
    case '1h':
    case '6h':
      return {
        preset,
        startDate: formatDateInput(now),
        endDate: formatDateInput(now),
      }
    case '24h':
      return {
        preset,
        startDate: formatDateInput(addDays(now, -1)),
        endDate: formatDateInput(now),
      }
    case '7d':
      return {
        preset,
        startDate: formatDateInput(addDays(now, -6)),
        endDate: formatDateInput(now),
      }
    case '14d':
      return {
        preset,
        startDate: formatDateInput(addDays(now, -13)),
        endDate: formatDateInput(now),
      }
    case '30d':
      return {
        preset,
        startDate: formatDateInput(addDays(now, -29)),
        endDate: formatDateInput(now),
      }
    case 'thisMonth': {
      const start = new Date(now.getFullYear(), now.getMonth(), 1)
      return {
        preset,
        startDate: formatDateInput(start),
        endDate: formatDateInput(now),
      }
    }
    case 'lastMonth': {
      const start = new Date(now.getFullYear(), now.getMonth() - 1, 1)
      const end = new Date(now.getFullYear(), now.getMonth(), 0)
      return {
        preset,
        startDate: formatDateInput(start),
        endDate: formatDateInput(end),
      }
    }
  }
}

export function getUsageRangeRequestRange(value: UsageRangeValue): { start: string; end: string } {
  const now = new Date()

  switch (value.preset) {
    case 'all':
      return {
        start: toLocalRFC3339(startOfDay(parseDateInput(value.startDate))),
        end: toLocalRFC3339(now),
      }
    case 'today':
      return {
        start: toLocalRFC3339(startOfDay(now)),
        end: toLocalRFC3339(now),
      }
    case 'yesterday': {
      const yesterday = addDays(now, -1)
      return {
        start: toLocalRFC3339(startOfDay(yesterday)),
        end: toLocalRFC3339(endOfDay(yesterday)),
      }
    }
    case '1h':
      return {
        start: toLocalRFC3339(new Date(now.getTime() - 60 * 60 * 1000)),
        end: toLocalRFC3339(now),
      }
    case '6h':
      return {
        start: toLocalRFC3339(new Date(now.getTime() - 6 * 60 * 60 * 1000)),
        end: toLocalRFC3339(now),
      }
    case '24h':
      return {
        start: toLocalRFC3339(new Date(now.getTime() - 24 * 60 * 60 * 1000)),
        end: toLocalRFC3339(now),
      }
    case '7d':
      return {
        start: toLocalRFC3339(startOfDay(addDays(now, -6))),
        end: toLocalRFC3339(now),
      }
    case '14d':
      return {
        start: toLocalRFC3339(startOfDay(addDays(now, -13))),
        end: toLocalRFC3339(now),
      }
    case '30d':
      return {
        start: toLocalRFC3339(startOfDay(addDays(now, -29))),
        end: toLocalRFC3339(now),
      }
    case 'thisMonth':
      return {
        start: toLocalRFC3339(new Date(now.getFullYear(), now.getMonth(), 1, 0, 0, 0, 0)),
        end: toLocalRFC3339(now),
      }
    case 'lastMonth': {
      const start = new Date(now.getFullYear(), now.getMonth() - 1, 1, 0, 0, 0, 0)
      const end = new Date(now.getFullYear(), now.getMonth(), 0, 23, 59, 59, 0)
      return {
        start: toLocalRFC3339(start),
        end: toLocalRFC3339(end),
      }
    }
    default:
      return {
        start: toLocalRFC3339(startOfDay(parseDateInput(value.startDate))),
        end: toLocalRFC3339(endOfDay(parseDateInput(value.endDate))),
      }
  }
}

export function getUsageRangeLabel(value: UsageRangeValue, t: (key: string) => string): string {
  if (value.preset !== 'custom') {
    return t(`usage.rangePreset.${value.preset}`)
  }
  return `${value.startDate.replace(/-/g, '/')} - ${value.endDate.replace(/-/g, '/')}`
}

interface UsageRangePickerProps {
  value: UsageRangeValue
  onApply: (value: UsageRangeValue) => void
}

export default function UsageRangePicker({ value, onApply }: UsageRangePickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<UsageRangeValue>(value)
  const rootRef = useRef<HTMLDivElement>(null)

  const updateDraftDate = (key: 'startDate' | 'endDate', nextDate: string) => {
    setDraft((current) => ({
      ...current,
      preset: 'custom',
      [key]: nextDate,
    }))
  }

  useEffect(() => {
    if (!open) {
      setDraft(value)
    }
  }, [open, value])

  useEffect(() => {
    if (!open) return

    const handlePointerDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false)
      }
    }

    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleEscape)

    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [open])

  const canApply = useMemo(() => {
    if (!draft.startDate || !draft.endDate) return false
    return draft.startDate <= draft.endDate
  }, [draft.endDate, draft.startDate])

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        className={cn(
          'flex h-9 min-w-[104px] items-center justify-between gap-2 rounded-lg border border-primary/45 bg-background px-3 text-left text-sm shadow-sm transition-[border-color,box-shadow,background-color]',
          'hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/25',
          open && 'bg-primary/5 ring-2 ring-ring/20',
        )}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="inline-flex min-w-0 items-center gap-2">
          <CalendarDays className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate text-[14px] font-medium leading-none text-foreground">{getUsageRangeLabel(value, (key) => t(key))}</span>
        </span>
        {open ? <ChevronUp className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />}
      </button>

      {open ? (
        <div className="absolute left-0 top-[calc(100%+0.5rem)] z-50 w-[320px] max-w-[calc(100vw-1rem)] overflow-hidden rounded-lg border border-border bg-popover shadow-[0_14px_36px_hsl(220_35%_12%/0.16)]">
          <div className="grid grid-cols-2 gap-x-4 gap-y-1 p-2">
            {PRESET_BUTTONS.map((preset) => {
              const active = draft.preset === preset
              return (
                <button
                  key={preset}
                  type="button"
                  className={cn(
                    'h-7 rounded-md px-3 text-center text-sm font-medium transition-colors',
                    active
                      ? 'bg-primary/12 text-primary'
                      : 'text-muted-foreground hover:bg-muted/70 hover:text-foreground',
                  )}
                  onClick={() => setDraft(createUsageRangeValue(preset))}
                >
                  {t(`usage.rangePreset.${preset}`)}
                </button>
              )
            })}
          </div>

          <div className="border-t border-border bg-background/70 p-3">
            <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-end gap-2">
              <div className="min-w-0">
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">{t('usage.rangeStartDate')}</div>
                <Input
                  type="date"
                  value={draft.startDate}
                  className="h-9 min-w-0 rounded-md bg-background px-2.5 text-sm"
                  onInput={(event) => updateDraftDate('startDate', event.currentTarget.value)}
                  onChange={(event) => updateDraftDate('startDate', event.target.value)}
                />
              </div>
              <div className="pb-2 text-lg leading-none text-muted-foreground">→</div>
              <div className="min-w-0">
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">{t('usage.rangeEndDate')}</div>
                <Input
                  type="date"
                  value={draft.endDate}
                  className="h-9 min-w-0 rounded-md bg-background px-2.5 text-sm"
                  onInput={(event) => updateDraftDate('endDate', event.currentTarget.value)}
                  onChange={(event) => updateDraftDate('endDate', event.target.value)}
                />
              </div>
            </div>

            <div className="mt-3 flex justify-end">
              <Button
                size="sm"
                disabled={!canApply}
                className="h-8 rounded-lg px-4 text-sm font-semibold shadow-none"
                onClick={() => {
                  if (!canApply) return
                  onApply(draft)
                  setOpen(false)
                }}
              >
                {t('usage.rangeApply')}
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date)
  next.setDate(next.getDate() + days)
  return next
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate(), 0, 0, 0, 0)
}

function endOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate(), 23, 59, 59, 0)
}

function formatDateInput(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function parseDateInput(value: string): Date {
  const [year, month, day] = value.split('-').map((part) => Number(part))
  return new Date(year, (month || 1) - 1, day || 1, 0, 0, 0, 0)
}

function toLocalRFC3339(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  const offset = date.getTimezoneOffset()
  const sign = offset <= 0 ? '+' : '-'
  const absOffset = Math.abs(offset)
  const tzH = pad(Math.floor(absOffset / 60))
  const tzM = pad(absOffset % 60)
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}${sign}${tzH}:${tzM}`
}
