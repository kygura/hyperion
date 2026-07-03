import { useState } from 'react'
import { Outlet } from 'react-router-dom'
import { TopNav } from './TopNav'
import { BottomTicker } from './BottomTicker'
import { ChatDrawer } from '../agent/ChatDrawer'

export function AppShell() {
  const [chatOpen, setChatOpen] = useState(false)

  return (
    <div className="flex flex-col h-full w-full">
      <TopNav chatOpen={chatOpen} onToggleChat={() => setChatOpen((v) => !v)} />
      <main className="flex-1 min-h-0 min-w-0 overflow-hidden">
        <Outlet />
      </main>
      <BottomTicker />
      <ChatDrawer open={chatOpen} onClose={() => setChatOpen(false)} />
    </div>
  )
}
