import {create} from 'zustand'
import type {TickEvent} from '@/lib/events'

// Zustand store fed by Wails events (see lib/events.ts); components only read it.
type AppState = {
  tick: TickEvent | null
  setTick: (t: TickEvent) => void
}

export const useAppStore = create<AppState>((set) => ({
  tick: null,
  setTick: (tick) => set({tick}),
}))
