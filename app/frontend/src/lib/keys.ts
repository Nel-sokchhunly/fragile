// Modifier label for shortcut hints in tooltips: the key handlers accept Cmd or Ctrl either way.
export const MOD = /Mac|iPhone|iPad/.test(navigator.platform) ? '\u2318' : 'Ctrl+'

// Focus the chat composer ([data-composer]) unless a dialog is open; a no-op while it is disabled.
export function focusComposer() {
  if (!document.querySelector('[role=dialog]')) document.querySelector<HTMLElement>('[data-composer]')?.focus()
}
