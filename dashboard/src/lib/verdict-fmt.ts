// Formatting helpers for CoreVerdict/CoreAction, shared by ThesesPanel and
// ProposalsPanel (both render the same action taxonomy). Split out of
// ThesesPanel.tsx because a component file may only export components
// (react-refresh/only-export-components).
import type { CoreAction, CoreEntry } from './core-client'
import { fmtPrice } from './metric-fmt'

export function actionClass(action: CoreAction): string {
  switch (action) {
    case 'open_long':
      return 'bg-green/15 text-green'
    case 'open_short':
      return 'bg-red/15 text-red'
    case 'close':
    case 'scale':
      return 'bg-amber/15 text-amber'
    default:
      return 'bg-elevated text-text-secondary'
  }
}

export function actionLabel(action: CoreAction): string {
  return action.replace('_', ' ')
}

export function fmtEntry(e: CoreEntry): string {
  if (e.price != null && isFinite(e.price)) return `${e.type} @ ${fmtPrice(e.price)}`
  return e.type
}
