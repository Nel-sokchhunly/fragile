import {useState} from 'react'
import {SendHorizontal} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Textarea} from '@/components/ui/textarea'

// Enter sends, Shift+Enter newline (and Enter during IME composition is left alone).
export function Composer({onSend, placeholder, label}: {onSend: (text: string) => void; placeholder: string; label: string}) {
  const [text, setText] = useState('')
  const send = () => {
    if (!text.trim()) return
    onSend(text.trim())
    setText('')
  }
  return (
    <div className="flex items-end gap-2 border-t p-3">
      <Textarea
        value={text} onChange={(e) => setText(e.target.value)} rows={1} aria-label={label} placeholder={placeholder}
        className="max-h-40 min-h-9 min-w-0 flex-1 resize-none"
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); send() }
        }}
      />
      <Button size="icon" onClick={send} disabled={!text.trim()} aria-label="Send message"><SendHorizontal/></Button>
    </div>
  )
}
