import {EventsOff, EventsOn} from '../../wailsjs/runtime/runtime'

// Go -> Wails events -> Zustand store -> components. Components never poll.
// Event names and payloads live here; keep in sync with the emitters in app/*.go.
// No events yet: the Phase 1 UI runs on store/mock.ts until the Go bindings land (#15/#17).
// The Go `app:tick` demo is no longer consumed by the UI.
export type EventMap = {}

function on<K extends keyof EventMap>(name: K, handler: (payload: EventMap[K]) => void) {
  EventsOn(name, handler)
  return () => EventsOff(name)
}

// Call once at startup. Add one `on(...)` per event, each writing into a store.
export function subscribeEvents() {
  const offs: (() => void)[] = [] // e.g. on('session:status', (e) => useAppStore.getState()...)
  return () => offs.forEach((off) => off())
}
