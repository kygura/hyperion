import { useCallback, useEffect, useRef, useState } from 'react'
import {
  fetchCoreBars,
  fetchCoreMarkets,
  type CoreAssetCtx,
  type CoreBar,
} from '../../lib/core-client'
import { useCoreStream } from '../../hooks/useCoreStream'
import { fmtPrice } from '../../lib/metric-fmt'

const POLL_MS = 30_000
const SPARK_N = 32

interface Row {
  coin: string
  mid: number
  bar: CoreBar
  assetCtx: CoreAssetCtx
  history: CoreBar[] // oldest-first, capped at SPARK_N — funding/OI-delta spark source
}

interface Props {
  online: boolean
}

function pushBar(history: CoreBar[], bar: CoreBar): CoreBar[] {
  const next = [...history, bar]
  return next.length > SPARK_N ? next.slice(next.length - SPARK_N) : next
}

function fmtFunding(n: number): string {
  if (!isFinite(n)) return '--'
  return `${(n * 100).toFixed(4)}%`
}

function fmtPct1(n: number): string {
  if (!isFinite(n)) return '--'
  const sign = n > 0 ? '+' : ''
  return `${sign}${(n * 100).toFixed(2)}%`
}

function pctClass(n: number): string {
  if (n > 0) return 'text-green'
  if (n < 0) return 'text-red'
  return 'text-text-secondary'
}

// Sparkline is a hand-rolled inline SVG polyline — no chart lib on this page.
function Sparkline({
  values,
  width = 64,
  height = 20,
  stroke = 'var(--text-secondary)',
}: {
  values: number[]
  width?: number
  height?: number
  stroke?: string
}) {
  if (values.length < 2) {
    return <svg width={width} height={height} aria-hidden />
  }
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const step = width / (values.length - 1)
  const points = values
    .map(
      (v, i) =>
        `${(i * step).toFixed(2)},${(height - ((v - min) / range) * height).toFixed(2)}`,
    )
    .join(' ')
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <polyline points={points} fill="none" stroke={stroke} strokeWidth="1.2" />
    </svg>
  )
}

// DivergingBar renders a signed value (e.g. CVD) as a bar growing left/right
// from a center zero line — green right (positive), red left (negative).
function DivergingBar({
  value,
  max,
  width = 64,
  height = 14,
}: {
  value: number
  max: number
  width?: number
  height?: number
}) {
  const half = width / 2
  const scale = max > 0 ? Math.min(Math.abs(value) / max, 1) : 0
  const barWidth = half * scale
  const positive = value >= 0
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`}>
      <line x1={half} y1={0} x2={half} y2={height} stroke="var(--border)" strokeWidth="1" />
      {barWidth > 0 && (
        <rect
          x={positive ? half : half - barWidth}
          y={2}
          width={barWidth}
          height={height - 4}
          fill={positive ? 'var(--green)' : 'var(--red)'}
        />
      )}
    </svg>
  )
}

function SkeletonRow({ i }: { i: number }) {
  return (
    <tr className="border-b border-border-subtle">
      <td className="px-2 py-2" colSpan={7}>
        <div
          className="h-3 bg-elevated animate-pulse"
          style={{ width: `${60 - i * 8}%` }}
        />
      </td>
    </tr>
  )
}

// RegimeBoard is the "liquidity drivers at a glance" panel: one row per
// tracked market from GET /api/markets, re-polled every 30s, rolled forward
// live via WS `bar` frames. Spark history is seeded per-coin from
// GET /api/bars on mount (parallel, tolerating individual failures).
export function RegimeBoard({ online }: Props) {
  const [rows, setRows] = useState<Map<string, Row> | null>(null)
  const seededRef = useRef<Set<string>>(new Set())

  // reseed is deliberately not an `async` function: setState calls live
  // inside `.then` callbacks (deferred to a later microtask), never in the
  // synchronous body of the effect that invokes it — same shape as the
  // existing useMeta.ts poll hook.
  const reseed = useCallback(() => {
    fetchCoreMarkets().then((markets) => {
      if (!markets) return

      setRows((prev) => {
        const next = new Map(prev ?? [])
        for (const m of markets) {
          const existing = next.get(m.coin)
          next.set(m.coin, {
            coin: m.coin,
            mid: m.mid,
            bar: m.bar,
            assetCtx: m.asset_ctx,
            history: existing?.history ?? [],
          })
        }
        return next
      })

      const toSeed = markets.filter((m) => !seededRef.current.has(m.coin))
      if (toSeed.length === 0) return
      Promise.allSettled(toSeed.map((m) => fetchCoreBars(m.coin, undefined, SPARK_N))).then(
        (results) => {
          setRows((prev) => {
            if (!prev) return prev
            const next = new Map(prev)
            toSeed.forEach((m, i) => {
              seededRef.current.add(m.coin)
              const res = results[i]
              if (res.status === 'fulfilled' && res.value && res.value.length > 0) {
                const row = next.get(m.coin)
                if (row) next.set(m.coin, { ...row, history: res.value })
              }
            })
            return next
          })
        },
      )
    })
  }, [])

  useEffect(() => {
    reseed()
    const id = setInterval(reseed, POLL_MS)
    return () => clearInterval(id)
  }, [reseed])

  useCoreStream('bar', (data) => {
    const bar = data as CoreBar
    if (!bar || !bar.Final) return
    setRows((prev) => {
      if (!prev) return prev
      const row = prev.get(bar.Coin)
      if (!row) return prev
      const next = new Map(prev)
      next.set(bar.Coin, { ...row, bar, history: pushBar(row.history, bar) })
      return next
    })
  })

  const list = rows ? Array.from(rows.values()) : []
  const maxAbsCVD = Math.max(1, ...list.map((r) => Math.abs(r.bar.CVD)))

  return (
    <div>
      {!online && (
        <div className="px-3 py-1.5 text-[10px] uppercase tracking-wider text-amber bg-amber/10 border-b border-border-subtle">
          daemon offline — showing last known data
        </div>
      )}
      <table className="w-full text-[11px] font-mono tabular-nums">
        <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
          <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
            <th className="px-2 py-1.5 text-left font-medium">Coin</th>
            <th className="px-2 py-1.5 text-right font-medium">Last / Δ</th>
            <th className="px-2 py-1.5 text-right font-medium">Funding</th>
            <th className="px-2 py-1.5 text-left font-medium">OI Δ</th>
            <th className="px-2 py-1.5 text-left font-medium">CVD</th>
            <th className="px-2 py-1.5 text-right font-medium">Basis</th>
            <th className="px-2 py-1.5 text-right font-medium">Liq Prox</th>
          </tr>
        </thead>
        <tbody>
          {rows === null &&
            [0, 1, 2, 3].map((i) => <SkeletonRow key={i} i={i} />)}
          {rows !== null && list.length === 0 && (
            <tr>
              <td
                className="px-2 py-4 text-center text-text-secondary text-[11px] uppercase tracking-wider"
                colSpan={7}
              >
                no markets warming up yet
              </td>
            </tr>
          )}
          {list.map((r) => {
            const oiSeries = r.history.map((b) => b.OIDelta)
            const fundingSeries = r.history.map((b) => b.Funding)
            return (
              <tr key={r.coin} className="border-b border-border-subtle hover:bg-hover">
                <td className="px-2 py-1.5 text-left font-semibold text-text-primary">
                  {r.coin}
                </td>
                <td className="px-2 py-1.5 text-right">
                  <div>{fmtPrice(r.bar.Close)}</div>
                  <div className={`text-[10px] ${pctClass(r.bar.Return)}`}>
                    {fmtPct1(r.bar.Return)}
                  </div>
                </td>
                <td className="px-2 py-1.5 text-right">
                  <div className={pctClass(r.bar.Funding)}>{fmtFunding(r.bar.Funding)}</div>
                  <div className="flex justify-end">
                    <Sparkline values={fundingSeries} />
                  </div>
                </td>
                <td className="px-2 py-1.5 text-left">
                  <Sparkline values={oiSeries} />
                </td>
                <td className="px-2 py-1.5 text-left">
                  <DivergingBar value={r.bar.CVD} max={maxAbsCVD} />
                </td>
                <td className="px-2 py-1.5 text-right">
                  <span className={pctClass(r.bar.Basis)}>{fmtPct1(r.bar.Basis)}</span>
                </td>
                <td className="px-2 py-1.5 text-right">
                  <span
                    className={
                      r.bar.LiqProx > 0.8
                        ? 'text-red'
                        : r.bar.LiqProx > 0.5
                          ? 'text-amber'
                          : 'text-text-secondary'
                    }
                  >
                    {(r.bar.LiqProx * 100).toFixed(0)}%
                  </span>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
