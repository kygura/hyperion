import { useCoreHealth } from '../hooks/useCore'
import { StatusStrip } from '../components/agent/StatusStrip'
import { RegimeBoard } from '../components/agent/RegimeBoard'

// AgentPage is the agent console (`/dashboard/agent`): the intelligence
// surface the daemon computes, laid out as panels stacked vertically. It
// owns the single useCoreHealth() poll and hands `health`/`online` down —
// panels never poll health independently.
export default function AgentPage() {
  const { health, online } = useCoreHealth()

  return (
    <div className="h-full w-full overflow-auto bg-body flex flex-col gap-3 p-3">
      <StatusStrip health={health} online={online} />

      <div className="panel flex-1 min-h-[280px]">
        <div className="panel-header">
          <span className="panel-title">Liquidity Regime</span>
        </div>
        <div className="panel-body">
          <RegimeBoard online={online} />
        </div>
      </div>
    </div>
  )
}
