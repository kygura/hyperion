import { useEffect, useRef, useState } from 'react'
import { postChat, type ChatTurn } from '../../lib/core-client'
import { useCoreHealth } from '../../hooks/useCore'

const STORAGE_KEY = 'hypertrader-chat'

interface Props {
  open: boolean
  onClose: () => void
}

function loadHistory(): ChatTurn[] {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed as ChatTurn[]
  } catch {
    return []
  }
}

function saveHistory(turns: ChatTurn[]) {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(turns))
  } catch {
    // storage full/unavailable — chat still works for the session, just won't persist
  }
}

// ChatDrawer is the global right-side chat panel, mounted once in AppShell and
// toggled from the TopNav on every page. History lives in sessionStorage so
// it survives route changes (but not a fresh tab), and is sent as the
// `history` array on every POST /api/chat call.
export function ChatDrawer({ open, onClose }: Props) {
  const { online } = useCoreHealth()
  const [turns, setTurns] = useState<ChatTurn[]>(() => loadHistory())
  const [input, setInput] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lastMeta, setLastMeta] = useState<{ provider: string; model: string } | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [turns, pending, error])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  const send = () => {
    const text = input.trim()
    if (!text || pending || !online) return
    const history = turns
    const userTurn: ChatTurn = { role: 'user', text }
    const withUser = [...history, userTurn]
    setTurns(withUser)
    saveHistory(withUser)
    setInput('')
    setPending(true)
    setError(null)
    postChat(text, history).then((res) => {
      setPending(false)
      if ('error' in res) {
        setError(res.error)
        return
      }
      setLastMeta({ provider: res.provider, model: res.model })
      const withReply = [...withUser, { role: 'assistant' as const, text: res.reply }]
      setTurns(withReply)
      saveHistory(withReply)
    })
  }

  if (!open) return null

  return (
    <div className="fixed inset-y-0 right-0 w-[380px] max-w-full z-50 flex flex-col bg-panel border-l border-border shadow-2xl">
      <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-panel-alt flex-shrink-0">
        <div className="flex flex-col">
          <span className="text-[11px] uppercase tracking-wider font-semibold text-text-primary">
            Agent Chat
          </span>
          {lastMeta && (
            <span className="text-[10px] font-mono text-text-secondary">
              {lastMeta.provider} · {lastMeta.model}
            </span>
          )}
        </div>
        <button
          onClick={onClose}
          className="text-text-secondary hover:text-text-primary text-[16px] leading-none px-1"
          aria-label="Close chat"
        >
          ×
        </button>
      </div>

      <div
        ref={scrollRef}
        className="flex-1 min-h-0 overflow-y-auto px-3 py-2 flex flex-col gap-2"
      >
        {turns.length === 0 && (
          <div className="text-[11px] uppercase tracking-wider text-text-secondary text-center mt-4">
            ask the agent about markets, positions, or its reasoning
          </div>
        )}
        {turns.map((t, i) => (
          <div
            key={i}
            className={`max-w-[85%] px-2 py-1.5 text-[12px] whitespace-pre-wrap ${
              t.role === 'user'
                ? 'self-end bg-elevated text-text-primary'
                : 'self-start bg-panel-alt border border-border-subtle text-text-primary'
            }`}
          >
            {t.text}
          </div>
        ))}
        {pending && (
          <div className="self-start text-[11px] uppercase tracking-wider text-text-secondary">
            agent is thinking…
          </div>
        )}
        {error && (
          <div className="self-stretch px-2 py-1.5 text-[11px] text-red bg-red/5 border border-red/20">
            {error}
          </div>
        )}
      </div>

      <div className="border-t border-border p-2 flex-shrink-0">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              send()
            }
          }}
          disabled={pending || !online}
          placeholder={
            !online
              ? 'daemon offline — start hyperagent'
              : pending
                ? 'agent is thinking…'
                : 'message the agent…'
          }
          className="w-full disabled:opacity-50 disabled:cursor-not-allowed"
        />
      </div>
    </div>
  )
}
