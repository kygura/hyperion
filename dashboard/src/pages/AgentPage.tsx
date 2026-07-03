import { useCoreHealth } from '../hooks/useCore'
import { StatusStrip } from '../components/agent/StatusStrip'
import { RegimeBoard } from '../components/agent/RegimeBoard'
import { ThesesPanel } from '../components/agent/ThesesPanel'
import { ProposalsPanel } from '../components/agent/ProposalsPanel'
import { DecisionLog } from '../components/agent/DecisionLog'

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

      <div className="panel flex-1 min-h-[220px]">
        <div className="panel-header">
          <span className="panel-title">Agent Theses</span>
        </div>
        <div className="panel-body">
          <ThesesPanel online={online} />
        </div>
      </div>

      <div className="panel flex-1 min-h-[160px]">
        <div className="panel-header">
          <span className="panel-title">Pending Proposals</span>
        </div>
        <div className="panel-body">
          <ProposalsPanel online={online} />
        </div>
      </div>

      <div className="panel flex-1 min-h-[260px]">
        <div className="panel-header">
          <span className="panel-title">Decision Log</span>
        </div>
        <div className="panel-body">
          <DecisionLog online={online} />
        </div>
      </div>
    </div>
  )
}
