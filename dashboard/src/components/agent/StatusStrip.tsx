import type { CoreHealth } from '../../lib/core-client'

interface Props {
  health: CoreHealth | null
  online: boolean
}

// StatusStrip is the agent console's masthead: landing-console typography
// ("AGENT · RUNNING/OFFLINE") plus the mode and provider info the daemon's
// /api/health reports. `health`/`online` come from the page's single shared
// useCoreHealth() poll — panels never poll health independently.
export function StatusStrip({ health, online }: Props) {
  return (
    <div className="flex items-center gap-5 flex-wrap px-3 py-2 border border-border bg-panel-alt">
      <span
        className={`text-[11px] uppercase tracking-wider font-semibold ${
          online ? 'text-green' : 'text-text-secondary/50'
        }`}
      >
        AGENT · {online ? 'RUNNING' : 'OFFLINE'}
      </span>

      <span className="text-[10px] uppercase tracking-wider text-text-secondary">
        MODE{' '}
        <span className="font-mono text-text-primary normal-case">
          {health?.mode || '--'}
        </span>
      </span>

      <span className="text-[10px] uppercase tracking-wider text-text-secondary">
        BATCH{' '}
        <span className="font-mono text-text-primary normal-case">
          {health?.providers.batch || '--'}
        </span>
      </span>

      <span className="text-[10px] uppercase tracking-wider text-text-secondary">
        CHAT{' '}
        <span className="font-mono text-text-primary normal-case">
          {health?.providers.chat || '--'}
        </span>
      </span>

      <span className="ml-auto text-[10px] font-mono tabular-nums text-text-secondary">
        {health?.version ? `v${health.version}` : online ? '' : 'daemon offline — start hyperagent'}
      </span>
    </div>
  )
}
