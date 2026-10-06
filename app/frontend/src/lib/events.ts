import {EventsOff, EventsOn} from '../../wailsjs/runtime/runtime'
import {useAppStore} from '@/store/app'

// Go -> Wails events -> Zustand store -> components. Components never poll.
// Event names and payloads live here; keep in sync with the emitters in app/*.go.
export type EventMap = {
  'app:tick': TickEvent
}

export type TickEvent = {count: number; at: string}

function on<K extends keyof EventMap>(name: K, handler: (payload: EventMap[K]) => void) {
  EventsOn(name, handler)
  return () => EventsOff(name)
}

// Call once at startup. Add one `on(...)` per event, each writing into a store.
export function subscribeEvents() {
  const offs = [on('app:tick', (t) => useAppStore.getState().setTick(t))]
  return () => offs.forEach((off) => off())
}
