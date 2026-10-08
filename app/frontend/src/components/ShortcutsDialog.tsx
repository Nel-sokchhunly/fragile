import type {ReactNode} from 'react'
import {Keyboard} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Tip} from '@/components/ui/tooltip'
import {MOD, SESSION_MOD} from '@/lib/keys'

// Every app shortcut, as handled in App.tsx, ChatView.tsx and the composer. Keep in sync with the README table.
const SHORTCUTS: [keys: string[], action: string][] = [
  [[`${MOD}N`], 'New session'],
  [[`${SESSION_MOD}1`, '–', `${SESSION_MOD}9`], 'Switch to session 1–9'],
  [[`${MOD}⌫`], 'Delete the current session'],
  [[`${MOD}B`], 'Collapse / expand the sidebar'],
  [[`${MOD}K`, 'or', '/'], 'Focus the message box'],
  [['Enter'], 'Send message'],
  [['Shift+Enter'], 'New line'],
  [['Esc'], 'Interrupt the running turn'],
  [['Esc'], 'Agent output view → back to chat'],
  [[`${MOD}\``], 'Toggle the terminal'],
  [['?'], 'Show keyboard shortcuts'],
]

const Key = ({children}: {children: ReactNode}) => (
  <kbd className="rounded border border-border-default bg-surface-sunken px-1.5 py-px font-mono text-xs text-foreground">{children}</kbd>
)

export function ShortcutsDialog({side = 'bottom'}: {side?: 'bottom' | 'right'}) {
  return (
    <Dialog>
      <Tip content="Keyboard shortcuts (?)" side={side}>
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label="Keyboard shortcuts" data-shortcuts><Keyboard/></Button>
        </DialogTrigger>
      </Tip>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>{SESSION_MOD === 'Alt+' ? 'On Linux and Windows.' : 'On macOS.'}</DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2 text-[13px]">
          {SHORTCUTS.map(([keys, action]) => (
            <div key={action} className="contents">
              <dt className="flex items-center gap-1 whitespace-nowrap">
                {keys.map((k, i) => (k === '–' || k === 'or' ? <span key={i} className="text-muted-foreground">{k}</span> : <Key key={i}>{k}</Key>))}
              </dt>
              <dd className="text-text-secondary">{action}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}
