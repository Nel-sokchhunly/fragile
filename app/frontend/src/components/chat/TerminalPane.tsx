import {type PointerEvent, useEffect, useRef, useState} from 'react'
import {attachTerminal} from '@/lib/terminal'

// Pane height as a share of the chat column; module-level so it holds across sessions (not persisted).
let savedPct = 40
const MIN_PCT = 15
const MAX_PCT = 80

// The session's shell, between the transcript and the composer. The xterm instance outlives this component
// (lib/terminal.ts); mounting shows it and focuses it. data-terminal tells the chat's key handlers to keep out.
export function TerminalPane({sessionId}: {sessionId: number}) {
  const box = useRef<HTMLDivElement>(null)
  const [pct, setPct] = useState(savedPct)
  useEffect(() => attachTerminal(sessionId, box.current!), [sessionId])

  // Drag the top edge to resize, relative to the chat column (the pane's parent).
  const drag = (e: PointerEvent<HTMLDivElement>) => {
    const col = e.currentTarget.parentElement?.parentElement
    if (!col) return
    e.preventDefault()
    const handle = e.currentTarget
    handle.setPointerCapture(e.pointerId)
    const move = (ev: globalThis.PointerEvent) => {
      const r = col.getBoundingClientRect()
      savedPct = Math.min(MAX_PCT, Math.max(MIN_PCT, ((r.bottom - ev.clientY) / r.height) * 100))
      setPct(savedPct)
    }
    const up = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', up)
      handle.removeEventListener('pointercancel', up)
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', up)
    handle.addEventListener('pointercancel', up)
  }

  return (
    <div className="relative flex min-h-0 shrink-0 flex-col border-t bg-background" style={{flexBasis: `${pct}%`}}>
      <div
        role="separator" aria-orientation="horizontal" aria-label="Resize terminal" onPointerDown={drag}
        className="absolute inset-x-0 -top-1 z-10 h-2 cursor-row-resize"
      />
      <div ref={box} data-terminal className="min-h-0 flex-1 overflow-hidden py-1.5 pl-3"/>
    </div>
  )
}
