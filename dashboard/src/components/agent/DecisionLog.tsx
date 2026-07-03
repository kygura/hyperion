import { useEffect, useState } from 'react'
import { fetchJournal, type CoreJournalEntry, type CoreJournalEvent } from '../../lib/core-client'
import { useCoreStream } from '../../hooks/useCoreStream'

interface Props {
  online: boolean
}

function todayUTC(): string {
  return new Date().toISOString().slice(0, 10)
}

function shiftDate(date: string, delta: number): string {
  const d = new Date(`${date}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + delta)
  return d.toISOString().slice(0, 10)
}

function fmtTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '--:--:--'
  return d.toLocaleTimeString(undefined, { hour12: false })
}

function kindClass(kind: string): string {
  switch (kind) {
    case 'fill':
    case 'open':
      return 'bg-green/15 text-green'
    case 'close':
    case 'alert':
      return 'bg-amber/15 text-amber'
    case 'error':
      return 'bg-red/15 text-red'
    default:
      return 'bg-elevated text-text-secondary'
  }
}

function SkeletonRow({ i }: { i: number }) {
  return (
    <tr className="border-b border-border-subtle">
      <td className="px-2 py-2" colSpan={4}>
        <div className="h-3 bg-elevated animate-pulse" style={{ width: `${50 - i * 6}%` }} />
      </td>
    </tr>
  )
}

// DecisionLog is today's (or a picked day's) journal, newest first. Seeded
// from GET /api/journal?date= (whose entries come back oldest-first, per
// journal.ReadDay's append-order file read — reversed here for display), and
// live-prepended via the WS `journal` topic, but only while viewing today:
// the live frame carries no date of its own, so prepending on a historical
// day would misattribute it.
export function DecisionLog({ online }: Props) {
  const [date, setDate] = useState<string>(() => todayUTC())
  const [entries, setEntries] = useState<CoreJournalEntry[] | null>(null)
  const isToday = date === todayUTC()

  useEffect(() => {
    if (!online) return
    fetchJournal(date).then((list) => {
      if (!list) return
      setEntries([...list].reverse())
    })
  }, [date, online])

  useCoreStream('journal', (data) => {
    if (!isToday) return
    const e = data as CoreJournalEvent
    if (!e || !e.Kind) return
    const entry: CoreJournalEntry = {
      time: new Date().toISOString(),
      coin: e.Coin,
      kind: e.Kind,
      summary: e.Summary,
      verdict: e.Verdict,
    }
    setEntries((prev) => [entry, ...(prev ?? [])])
  })

  const list = entries ?? []

  return (
    <div>
      <div className="flex items-center justify-between px-2 py-1.5 border-b border-border-subtle">
        <div className="flex items-center gap-2">
          <button
            onClick={() => setDate((d) => shiftDate(d, -1))}
            className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary hover:text-text-primary"
          >
            ‹ Prev
          </button>
          <span className="font-mono text-[11px] text-text-primary tabular-nums">{date}</span>
          <button
            onClick={() => setDate((d) => shiftDate(d, 1))}
            disabled={isToday}
            className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
          >
            Next ›
          </button>
        </div>
        {!online && (
          <span className="text-[10px] uppercase tracking-wider text-amber">
            daemon offline — showing last known entries
          </span>
        )}
      </div>
      <table className="w-full text-[11px] font-mono tabular-nums">
        <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
          <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
            <th className="px-2 py-1.5 text-left font-medium">Time</th>
            <th className="px-2 py-1.5 text-left font-medium">Kind</th>
            <th className="px-2 py-1.5 text-left font-medium">Coin</th>
            <th className="px-2 py-1.5 text-left font-medium">Summary</th>
          </tr>
        </thead>
        <tbody>
          {entries === null && [0, 1, 2, 3].map((i) => <SkeletonRow key={i} i={i} />)}
          {entries !== null && list.length === 0 && (
            <tr>
              <td
                className="px-2 py-4 text-center text-text-secondary text-[11px] uppercase tracking-wider"
                colSpan={4}
              >
                no journal entries for {date}
              </td>
            </tr>
          )}
          {list.map((e, i) => (
            <tr key={`${e.time}-${i}`} className="border-b border-border-subtle hover:bg-hover">
              <td className="px-2 py-1.5 text-left text-text-secondary">{fmtTime(e.time)}</td>
              <td className="px-2 py-1.5 text-left">
                <span
                  className={`inline-block px-1.5 py-0.5 text-[9px] uppercase tracking-wider ${kindClass(e.kind)}`}
                >
                  {e.kind}
                </span>
              </td>
              <td className="px-2 py-1.5 text-left font-semibold text-text-primary">{e.coin}</td>
              <td className="px-2 py-1.5 text-left whitespace-normal text-text-primary">
                {e.summary}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
