import {useState} from 'react'
import {SendHorizontal} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Textarea} from '@/components/ui/textarea'

// Enter sends, Shift+Enter newline (and Enter during IME composition is left alone).
// onSend rejects with the backend's message, shown under the box (the text is kept).
// `disabledReason` turns the box off and says why.
export function Composer({onSend, placeholder, label, disabledReason}: {onSend: (text: string) => Promise<void>; placeholder: string; label: string; disabledReason?: string}) {
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const off = !!disabledReason
  const send = async () => {
    if (!text.trim() || off) return
    setError('')
    try {
      await onSend(text.trim())
      setText('')
    } catch (e) {
      setError(String(e))
    }
  }
  return (
    <div className="border-t p-3">
      <div className="flex items-end gap-2">
        <Textarea
          value={text} onChange={(e) => setText(e.target.value)} rows={1} aria-label={label} placeholder={placeholder} disabled={off}
          className="max-h-40 min-h-9 min-w-0 flex-1 resize-none"
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send() }
          }}
        />
        <Button size="icon" onClick={send} disabled={off || !text.trim()} aria-label="Send message"><SendHorizontal/></Button>
      </div>
      {off && <p className="mt-1.5 text-xs text-muted-foreground">{disabledReason}</p>}
      {error && <p role="alert" className="mt-1.5 text-xs text-destructive">{error}</p>}
    </div>
  )
}
