import {type ReactNode, useState} from 'react'
import {CornerDownLeft} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Textarea} from '@/components/ui/textarea'
import {cn} from '@/lib/utils'

// Enter sends, Shift+Enter newline (and Enter during IME composition is left alone).
// onSend rejects with the backend's message, shown under the box (the text is kept).
// `disabledReason` turns the box off and says why (may hold an action). data-composer lets the global shortcuts focus it.
export function Composer({onSend, placeholder, label, disabledReason}: {onSend: (text: string) => Promise<void>; placeholder: string; label: string; disabledReason?: ReactNode}) {
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const off = !!disabledReason
  const ready = !off && !!text.trim()
  const send = async () => {
    if (!ready) return
    setError('')
    try {
      await onSend(text.trim())
      setText('')
    } catch (e) {
      setError(String(e))
    }
  }
  return (
    <div className="px-6 pt-1 pb-3">
      <div className="mx-auto max-w-[680px]">
        <div className="relative">
          <Textarea
            data-composer value={text} onChange={(e) => setText(e.target.value)} rows={1} aria-label={label} placeholder={placeholder} disabled={off}
            className="max-h-40 min-h-10 resize-none rounded-[10px] py-[9px] pr-10 pl-3.5"
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send() }
            }}
          />
          <Button
            variant="ghost" size="icon-xs" onClick={send} disabled={!ready} aria-label="Send message" title="Send (Enter, Shift+Enter for newline)"
            className={cn('absolute right-2 bottom-2', ready && 'text-foreground')}
          >
            <CornerDownLeft/>
          </Button>
        </div>
        {off && <div className="mt-1 text-[13px] text-muted-foreground">{disabledReason}</div>}
        {error && <p role="alert" className="mt-1 text-[13px] text-destructive">{error}</p>}
      </div>
    </div>
  )
}
