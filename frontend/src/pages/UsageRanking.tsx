import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import PageHeader from '../components/PageHeader'
import StateShell from '../components/StateShell'
import { useDataLoader } from '../hooks/useDataLoader'
import type { UsageAPIKeyRankingItem } from '../types'
import { formatBeijingTime } from '../utils/time'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { AlertTriangle, CalendarDays, CircleDollarSign, Hash, KeyRound, Search, Trophy, Zap } from 'lucide-react'

type RankingPeriod = 'day' | 'week' | 'month'

const PERIODS: RankingPeriod[] = ['day', 'week', 'month']
const RANK_LIMIT = 100

function safeNumber(value?: number | null): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function formatTokens(value?: number | null): string {
  return safeNumber(value).toLocaleString()
}

function formatCost(value?: number | null): string {
  const amount = safeNumber(value)
  if (amount >= 100) return `$${amount.toLocaleString(undefined, { maximumFractionDigits: 2 })}`
  if (amount >= 1) return `$${amount.toFixed(2)}`
  if (amount >= 0.01) return `$${amount.toFixed(4)}`
  return `$${amount.toFixed(6)}`
}

function formatShare(value: number, total: number): string {
  if (total <= 0) return '0.0%'
  return `${((value / total) * 100).toFixed(1)}%`
}

export default function UsageRanking() {
  const { t } = useTranslation()
  const [period, setPeriod] = useState<RankingPeriod>('day')
  const [search, setSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')

  const loadRanking = useCallback(() => (
    api.getUsageRanking({
      period,
      q: appliedSearch.trim() || undefined,
      limit: RANK_LIMIT,
    })
  ), [appliedSearch, period])

  const { data: ranking, loading, error, reload } = useDataLoader({
    initialData: null,
    load: loadRanking,
  })

  const maxCost = useMemo(() => {
    return Math.max(...(ranking?.items ?? []).map((item) => safeNumber(item.user_billed)), 0)
  }, [ranking])

  const periodRange = ranking
    ? t('usageRanking.rangeValue', {
        start: formatBeijingTime(ranking.start),
        end: formatBeijingTime(ranking.end),
      })
    : ''

  const applySearch = () => {
    setAppliedSearch(search.trim())
  }

  const clearSearch = () => {
    setSearch('')
    setAppliedSearch('')
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={t('usageRanking.title')}
        description={t('usageRanking.description')}
        onRefresh={reload}
        actionMeta={ranking ? t('usageRanking.updatedAt', { time: formatBeijingTime(ranking.updated_at) }) : undefined}
      />

      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-3 shadow-sm">
        <div className="inline-flex rounded-lg border border-border bg-muted/40 p-1">
          {PERIODS.map((item) => (
            <Button
              key={item}
              type="button"
              variant={period === item ? 'default' : 'ghost'}
              size="sm"
              className="min-w-20"
              onClick={() => setPeriod(item)}
            >
              <CalendarDays className="size-3.5" />
              {t(`usageRanking.period.${item}`)}
            </Button>
          ))}
        </div>

        <form
          className="flex min-w-[280px] flex-1 items-center justify-end gap-2 max-sm:min-w-0 max-sm:flex-col max-sm:items-stretch"
          onSubmit={(event) => {
            event.preventDefault()
            applySearch()
          }}
        >
          <div className="relative w-full max-w-md">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('usageRanking.searchPlaceholder')}
              className="pl-9"
            />
          </div>
          <div className="flex items-center gap-2 max-sm:w-full">
            <Button type="submit" variant="outline" className="max-sm:flex-1">
              <Search className="size-3.5" />
              {t('usageRanking.search')}
            </Button>
            {appliedSearch ? (
              <Button type="button" variant="ghost" onClick={clearSearch} className="max-sm:flex-1">
                {t('usageRanking.clearSearch')}
              </Button>
            ) : null}
          </div>
        </form>
      </div>

      <StateShell
        loading={loading}
        error={error}
        onRetry={reload}
        loadingTitle={t('usageRanking.loadingTitle')}
        loadingDescription={t('usageRanking.loadingDesc')}
        errorTitle={t('usageRanking.errorTitle')}
      >
        {ranking ? (
          <div className="space-y-5">
            <div className="grid grid-cols-4 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
              <RankingSummaryCard icon={<CircleDollarSign className="size-5" />} label={t('usageRanking.totalCost')} value={formatCost(ranking.total_user_billed)} hint={t('usageRanking.accountCost', { value: formatCost(ranking.total_account_billed) })} />
              <RankingSummaryCard icon={<Zap className="size-5" />} label={t('usageRanking.totalTokens')} value={formatTokens(ranking.total_tokens)} hint={t('usageRanking.totalRequestsHint', { count: formatTokens(ranking.total_requests) })} />
              <RankingSummaryCard icon={<KeyRound className="size-5" />} label={t('usageRanking.keyCount')} value={formatTokens(ranking.items.length)} hint={appliedSearch ? t('usageRanking.filteredBy', { keyword: appliedSearch }) : t('usageRanking.topLimit', { count: RANK_LIMIT })} />
              <RankingSummaryCard icon={<CalendarDays className="size-5" />} label={t('usageRanking.periodRange')} value={t(`usageRanking.period.${ranking.period}`)} hint={periodRange} />
            </div>

            <StateShell
              isEmpty={ranking.items.length === 0}
              emptyTitle={t('usageRanking.emptyTitle')}
              emptyDescription={appliedSearch ? t('usageRanking.emptySearchDesc') : t('usageRanking.emptyDesc')}
            >
              <Card className="overflow-hidden rounded-lg border-border shadow-sm">
                <CardContent className="p-0">
                  <Table>
                    <TableHeader>
                      <TableRow className="bg-muted/35">
                        <TableHead className="w-[72px]">{t('usageRanking.tableRank')}</TableHead>
                        <TableHead>{t('usageRanking.tableApiKey')}</TableHead>
                        <TableHead className="text-right">{t('usageRanking.tableCost')}</TableHead>
                        <TableHead className="text-right">{t('usageRanking.tableTokens')}</TableHead>
                        <TableHead className="text-right">{t('usageRanking.tableRequests')}</TableHead>
                        <TableHead className="text-right">{t('usageRanking.tableErrors')}</TableHead>
                        <TableHead className="text-right">{t('usageRanking.tableShare')}</TableHead>
                        <TableHead className="text-right">{t('usageRanking.tableLastUsed')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {ranking.items.map((item) => (
                        <RankingRow key={`${item.api_key_id}-${item.label}`} item={item} maxCost={maxCost} totalCost={ranking.total_user_billed} />
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            </StateShell>
          </div>
        ) : null}
      </StateShell>
    </div>
  )
}

function RankingSummaryCard({ icon, label, value, hint }: { icon: ReactNode; label: string; value: string; hint: string }) {
  return (
    <Card className="rounded-lg border-border shadow-sm">
      <CardContent className="flex min-h-[116px] items-center gap-4 p-4">
        <div className="flex size-11 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          {icon}
        </div>
        <div className="min-w-0">
          <div className="text-xs font-semibold text-muted-foreground">{label}</div>
          <div className="mt-1 truncate text-2xl font-bold tabular-nums text-foreground">{value}</div>
          <div className="mt-1 truncate text-xs text-muted-foreground" title={hint}>{hint}</div>
        </div>
      </CardContent>
    </Card>
  )
}

function RankingRow({ item, maxCost, totalCost }: { item: UsageAPIKeyRankingItem; maxCost: number; totalCost: number }) {
  const { t } = useTranslation()
  const cost = safeNumber(item.user_billed)
  const width = maxCost > 0 ? Math.max(6, Math.round((cost / maxCost) * 100)) : 0
  const errorCount = safeNumber(item.error_count)

  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2 font-semibold tabular-nums">
          {item.rank <= 3 ? <Trophy className="size-4 text-amber-500" /> : <Hash className="size-4 text-muted-foreground" />}
          {item.rank}
        </div>
      </TableCell>
      <TableCell>
        <div className="min-w-[180px] max-w-[360px]">
          <div className="truncate font-semibold text-foreground" title={item.label || t('usageRanking.unknownApiKey')}>
            {item.label || t('usageRanking.unknownApiKey')}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">ID {item.api_key_id || '-'}</div>
        </div>
      </TableCell>
      <TableCell className="text-right">
        <div className="ml-auto flex w-[160px] flex-col items-end gap-1">
          <span className="font-geist-mono text-sm font-semibold text-emerald-600 dark:text-emerald-400">{formatCost(cost)}</span>
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-emerald-500" style={{ width: `${width}%` }} />
          </div>
        </div>
      </TableCell>
      <TableCell className="text-right font-geist-mono text-sm tabular-nums">
        <div>{formatTokens(item.tokens)}</div>
        <div className="text-[11px] text-muted-foreground">
          {t('usageRanking.tokenBreakdown', { input: formatTokens(item.input_tokens), output: formatTokens(item.output_tokens) })}
        </div>
      </TableCell>
      <TableCell className="text-right font-geist-mono text-sm tabular-nums">{formatTokens(item.requests)}</TableCell>
      <TableCell className="text-right">
        {errorCount > 0 ? (
          <Badge className="border-transparent bg-amber-500/14 text-amber-700 dark:bg-amber-500/20 dark:text-amber-300">
            <AlertTriangle className="size-3" />
            {formatTokens(errorCount)}
          </Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        )}
      </TableCell>
      <TableCell className="text-right font-geist-mono text-sm tabular-nums">{formatShare(cost, totalCost)}</TableCell>
      <TableCell className="text-right text-sm text-muted-foreground whitespace-nowrap">{formatBeijingTime(item.last_used_at)}</TableCell>
    </TableRow>
  )
}
