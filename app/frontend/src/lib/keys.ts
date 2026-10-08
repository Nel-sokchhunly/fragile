// Modifier label for shortcut hints in tooltips: the key handlers accept Cmd or Ctrl either way.
export const IS_MAC = /Mac|iPhone|iPad/.test(navigator.platform)
export const MOD = IS_MAC ? '\u2318' : 'Ctrl+'
// Switching sessions by number: Cmd+1..9 on macOS, Alt+1..9 elsewhere.
export const SESSION_MOD = IS_MAC ? '\u2318' : 'Alt+'

// Focus the chat composer ([data-composer]) unless a dialog is open; a no-op while it is disabled.
export function focusComposer() {
  if (!document.querySelector('[role=dialog]')) document.querySelector<HTMLElement>('[data-composer]')?.focus()
}
