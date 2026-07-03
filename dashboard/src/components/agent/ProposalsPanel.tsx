import { Fragment, useCallback, useEffect, useState } from 'react'
import {
  approveProposal,
  fetchProposals,
  rejectProposal,
  type CoreProposal,
} from '../../lib/core-client'
import { fmtUsd } from '../../lib/metric-fmt'
import { ActionChip } from './ThesesPanel'

interface Props {
  online: boolean
}

const POLL_MS = 15_000
const COLS = 6

function fmtCountdown(expires: string, now: number): string {
  const ms = new Date(expires).getTime() - now
  if (!isFinite(ms)) return '--'
  if (ms <= 0) return 'expired'
  const totalSec = Math.floor(ms / 1000)
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

function SkeletonRow({ i }: { i: number }) {
  return (
    <tr className="border-b border-border-subtle">
      <td className="px-2 py-2" colSpan={COLS}>
        <div className="h-3 bg-elevated animate-pulse" style={{ width: `${50 - i * 8}%` }} />
      </td>
    </tr>
  )
}

// ProposalsPanel is propose-mode's confirmation queue: pending candidates
// awaiting a human Approve/Reject. Polled every 15s (plus refetch after any
// action) since proposals expire on a TTL the client doesn't otherwise track
// between actions. Approve/Reject error bodies (404 expired, 422 gate
// rejection) render verbatim under the row — the gate name is the product
// story and must never be swallowed.
export function ProposalsPanel({ online }: Props) {
  const [proposals, setProposals] = useState<CoreProposal[] | null>(null)
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set())
  const [rowErrors, setRowErrors] = useState<Map<string, string>>(new Map())
  const [now, setNow] = useState(() => Date.now())

  const refetch = useCallback(() => {
    fetchProposals().then((list) => setProposals(list))
  }, [])

  useEffect(() => {
    if (!online) return
    refetch()
    const id = setInterval(refetch, POLL_MS)
    return () => clearInterval(id)
  }, [online, refetch])

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  const act = useCallback(
    (id: string, kind: 'approve' | 'reject') => {
      setBusyIds((prev) => new Set(prev).add(id))
      setRowErrors((prev) => {
        if (!prev.has(id)) return prev
        const next = new Map(prev)
        next.delete(id)
        return next
      })
      const call = kind === 'approve' ? approveProposal : rejectProposal
      call(id).then((err) => {
        setBusyIds((prev) => {
          const next = new Set(prev)
          next.delete(id)
          return next
        })
        if (err) {
          setRowErrors((prev) => new Map(prev).set(id, err))
        }
        refetch()
      })
    },
    [refetch],
  )

  const list = proposals ?? []

  return (
    <div>
      {!online && (
        <div className="px-3 py-1.5 text-[10px] uppercase tracking-wider text-amber bg-amber/10 border-b border-border-subtle">
          daemon offline — showing last known proposals
        </div>
      )}
      <table className="w-full text-[11px] font-mono tabular-nums">
        <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
          <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
            <th className="px-2 py-1.5 text-left font-medium">Action</th>
            <th className="px-2 py-1.5 text-left font-medium">Asset</th>
            <th className="px-2 py-1.5 text-right font-medium">Size</th>
            <th className="px-2 py-1.5 text-right font-medium">Conf</th>
            <th className="px-2 py-1.5 text-right font-medium">Expires</th>
            <th className="px-2 py-1.5 text-right font-medium">Action</th>
          </tr>
        </thead>
        <tbody>
          {proposals === null && [0, 1].map((i) => <SkeletonRow key={i} i={i} />)}
          {proposals !== null && list.length === 0 && (
            <tr>
              <td
                className="px-2 py-4 text-center text-text-secondary text-[11px] uppercase tracking-wider"
                colSpan={COLS}
              >
                no pending proposals
              </td>
            </tr>
          )}
          {list.map((p) => {
            const busy = busyIds.has(p.id)
            const err = rowErrors.get(p.id)
            return (
              <Fragment key={p.id}>
                <tr className="border-b border-border-subtle hover:bg-hover">
                  <td className="px-2 py-1.5">
                    <ActionChip action={p.verdict.action} />
                  </td>
                  <td className="px-2 py-1.5 text-left font-semibold text-text-primary">
                    {p.verdict.asset}
                  </td>
                  <td className="px-2 py-1.5 text-right">{fmtUsd(p.verdict.size_usd)}</td>
                  <td className="px-2 py-1.5 text-right">
                    {(p.verdict.confidence * 100).toFixed(0)}%
                  </td>
                  <td className="px-2 py-1.5 text-right">{fmtCountdown(p.expires, now)}</td>
                  <td className="px-2 py-1.5 text-right whitespace-nowrap">
                    <button
                      onClick={() => act(p.id, 'approve')}
                      disabled={busy}
                      className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary hover:text-green hover:border-green disabled:opacity-40 disabled:cursor-not-allowed"
                    >
                      Approve
                    </button>
                    <button
                      onClick={() => act(p.id, 'reject')}
                      disabled={busy}
                      className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 ml-1 border border-border text-text-secondary hover:text-red hover:border-red disabled:opacity-40 disabled:cursor-not-allowed"
                    >
                      Reject
                    </button>
                  </td>
                </tr>
                {p.verdict.thesis && (
                  <tr className="border-b border-border-subtle">
                    <td
                      className="px-2 pb-1.5 text-left whitespace-normal text-text-secondary text-[10px]"
                      colSpan={COLS}
                    >
                      {p.verdict.thesis}
                    </td>
                  </tr>
                )}
                {err && (
                  <tr className="border-b border-border-subtle">
                    <td className="px-2 py-1 text-[10px] text-red bg-red/5" colSpan={COLS}>
                      {err}
                    </td>
                  </tr>
                )}
              </Fragment>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
