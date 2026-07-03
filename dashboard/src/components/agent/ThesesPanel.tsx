import { useEffect, useState } from 'react'
import { fetchVerdicts, type CoreAction, type CoreVerdict } from '../../lib/core-client'
import { useCoreStream } from '../../hooks/useCoreStream'
import { fmtPrice, fmtUsd } from '../../lib/metric-fmt'
import { actionClass, actionLabel, fmtEntry } from '../../lib/verdict-fmt'

interface Props {
  online: boolean
}

// ActionChip is shared with ProposalsPanel — both render the same CoreAction
// taxonomy on a pending or live verdict.
export function ActionChip({ action }: { action: CoreAction }) {
  return (
    <span
      className={`inline-block px-1.5 py-0.5 text-[9px] uppercase tracking-wider whitespace-nowrap ${actionClass(action)}`}
    >
      {actionLabel(action)}
    </span>
  )
}

function SkeletonRow({ i }: { i: number }) {
  return (
    <tr className="border-b border-border-subtle">
      <td className="px-2 py-2" colSpan={7}>
        <div className="h-3 bg-elevated animate-pulse" style={{ width: `${55 - i * 8}%` }} />
      </td>
    </tr>
  )
}

// ThesesPanel is the latest verdict per asset, ranked by confidence descending
// — the agent's written judgment, not just its action. Seeded from
// GET /api/verdicts, kept live via the WS `verdict` topic (each frame replaces
// the entry for its asset).
export function ThesesPanel({ online }: Props) {
  const [rows, setRows] = useState<Map<string, CoreVerdict> | null>(null)

  useEffect(() => {
    if (!online) return
    fetchVerdicts().then((list) => {
      if (!list) return
      setRows((prev) => {
        const next = new Map(prev ?? [])
        for (const v of list) next.set(v.asset, v)
        return next
      })
    })
  }, [online])

  useCoreStream('verdict', (data) => {
    const v = data as CoreVerdict
    if (!v || !v.asset) return
    setRows((prev) => {
      const next = new Map(prev ?? [])
      next.set(v.asset, v)
      return next
    })
  })

  const list = rows ? Array.from(rows.values()).sort((a, b) => b.confidence - a.confidence) : []

  return (
    <div>
      {!online && (
        <div className="px-3 py-1.5 text-[10px] uppercase tracking-wider text-amber bg-amber/10 border-b border-border-subtle">
          daemon offline — showing last known theses
        </div>
      )}
      <table className="w-full text-[11px] font-mono tabular-nums">
        <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
          <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
            <th className="px-2 py-1.5 text-left font-medium">Action</th>
            <th className="px-2 py-1.5 text-left font-medium">Asset</th>
            <th className="px-2 py-1.5 text-right font-medium">Conf</th>
            <th className="px-2 py-1.5 text-right font-medium">Size</th>
            <th className="px-2 py-1.5 text-right font-medium">Entry / Stop / TP</th>
            <th className="px-2 py-1.5 text-left font-medium">Thesis</th>
          </tr>
        </thead>
        <tbody>
          {rows === null && [0, 1, 2].map((i) => <SkeletonRow key={i} i={i} />)}
          {rows !== null && list.length === 0 && (
            <tr>
              <td
                className="px-2 py-4 text-center text-text-secondary text-[11px] uppercase tracking-wider"
                colSpan={6}
              >
                no theses yet — agent reasons on batch closes
              </td>
            </tr>
          )}
          {list.map((v) => (
            <tr key={v.asset} className="border-b border-border-subtle hover:bg-hover align-top">
              <td className="px-2 py-1.5">
                <ActionChip action={v.action} />
              </td>
              <td className="px-2 py-1.5 text-left font-semibold text-text-primary">{v.asset}</td>
              <td className="px-2 py-1.5 text-right">{(v.confidence * 100).toFixed(0)}%</td>
              <td className="px-2 py-1.5 text-right">{fmtUsd(v.size_usd)}</td>
              <td className="px-2 py-1.5 text-right text-text-secondary whitespace-nowrap">
                {fmtEntry(v.entry)} / {fmtPrice(v.stop)} / {fmtPrice(v.take_profit)}
              </td>
              <td className="px-2 py-1.5 text-left whitespace-normal text-text-primary">
                {v.thesis}
                {v.reading && (
                  <div className="mt-0.5 text-[10px] text-text-secondary">{v.reading}</div>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
