import {type DragEvent, type ReactNode, useEffect, useRef, useState} from 'react'
import {CornerDownLeft, Paperclip, Square, SquareTerminal} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Textarea} from '@/components/ui/textarea'
import {FileChip, ImageChip} from '@/components/chat/Attachments'
import {MAX_ATTACHMENTS, MAX_TOTAL, type PendingAttachment, readAttachment} from '@/lib/attachments'
import {formatBytes} from '@/lib/format'
import {focusComposer, MOD} from '@/lib/keys'
import type {Attachment} from '@/lib/types'
import {cn} from '@/lib/utils'
import {ClipboardImage} from '../../../wailsjs/go/main/App'

// The clipboard image a paste event didn't carry (WebKitGTK on Wayland leaves screenshots out of clipboardData):
// the async Clipboard API first, then the backend's GTK read. Undefined when there is none.
async function clipboardImage(): Promise<File | undefined> {
  try {
    for (const item of await navigator.clipboard.read()) {
      const type = item.types.find((t) => t.startsWith('image/'))
      if (type) return new File([await item.getType(type)], `screenshot.${type.slice(6)}`, {type})
    }
  } catch { /* API missing or refused */ }
  const png = await ClipboardImage().catch(() => '')
  return png ? new File([Uint8Array.from(atob(png), (c) => c.charCodeAt(0))], 'screenshot.png', {type: 'image/png'}) : undefined
}

// Enter sends, Shift+Enter newline (and Enter during IME composition is left alone).
// onSend rejects with the backend's message, shown under the box (the text and attachments are kept).
// Files come from the paperclip, drag-and-drop onto the box, or paste; a message may be attachments only.
// `onInterrupt` (set while the orchestrator works) adds an Interrupt button; data-interrupt lets ChatView's Esc click it.
// `disabledReason` turns the box off and says why (may hold an action). data-composer lets the global shortcuts focus it.
// `terminal` adds the terminal pane toggle left of the box (it stays usable while the box is off).
export function Composer({onSend, onInterrupt, placeholder, label, disabledReason, terminal, allowPDF = true}: {
  onSend: (text: string, attachments: Attachment[]) => Promise<void>; onInterrupt?: () => Promise<void>
  placeholder: string; label: string; disabledReason?: ReactNode
  allowPDF?: boolean
  terminal?: {open: boolean; onToggle: () => void}
}) {
  const [text, setText] = useState('')
  const [files, setFiles] = useState<PendingAttachment[]>([])
  const filesRef = useRef(files) // current list for the async reads below
  const [reading, setReading] = useState(0)
  const [dragging, setDragging] = useState(false)
  const [interrupting, setInterrupting] = useState(false)
  const [error, setError] = useState('')
  const input = useRef<HTMLInputElement>(null)
  const off = !!disabledReason
  const ready = !off && !reading && (!!text.trim() || files.length > 0)
  const update = (next: PendingAttachment[]) => { filesRef.current = next; setFiles(next) }
  // Focus when shown (ChatView remounts per session) and when re-enabled (after Resume brings the orchestrator back),
  // but leave an open terminal (which focuses itself first) alone.
  useEffect(() => { if (!off && !document.activeElement?.closest('[data-terminal]')) focusComposer() }, [off])

  const add = async (list: FileList | File[] | null) => {
    const picked = [...(list ?? [])]
    if (off || !picked.length) return
    setError('')
    setReading((n) => n + 1)
    const errs: string[] = []
    const got: PendingAttachment[] = []
    for (const f of picked) {
      try { const attachment = await readAttachment(f); if (!allowPDF && attachment.media_type === "application/pdf") throw "Codex supports images and text, not PDFs; send extracted text instead"; got.push(attachment) } catch (e) { errs.push(String(e)) }
    }
    setReading((n) => n - 1)
    const next = [...filesRef.current]
    for (const a of got) {
      if (next.length >= MAX_ATTACHMENTS) { errs.push(`at most ${MAX_ATTACHMENTS} attachments per message`); break }
      if (next.reduce((s, x) => s + x.size, a.size) > MAX_TOTAL) { errs.push(`${a.name} not added: attachments can total at most ${formatBytes(MAX_TOTAL)}`); continue }
      next.push(a)
    }
    update(next)
    setError(errs.join('\n'))
  }
  const send = async () => {
    if (!ready) return
    setError('')
    try {
      await onSend(text.trim(), files.map(({name, media_type, data}) => ({name, media_type, data})))
      setText('')
      update([])
      focusComposer() // the Send button may have taken focus
    } catch (e) {
      setError(String(e))
    }
  }
  const interrupt = async () => {
    if (!onInterrupt || interrupting) return
    setError('')
    setInterrupting(true)
    try {
      await onInterrupt()
      focusComposer() // the button goes away once the turn ends
    } catch (e) {
      setError(String(e))
    } finally {
      setInterrupting(false)
    }
  }
  const hasFiles = (e: DragEvent) => !off && e.dataTransfer.types.includes('Files')

  return (
    <div className="px-6 pt-1 pb-3">
      <div
        className="mx-auto max-w-[680px]"
        onDragOver={(e) => { if (hasFiles(e)) { e.preventDefault(); setDragging(true) } }}
        onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false) }}
        onDrop={(e) => { if (hasFiles(e)) { e.preventDefault(); setDragging(false); void add(e.dataTransfer.files) } }}
      >
        {files.length > 0 && (
          <div className="mb-1.5 flex flex-wrap items-end gap-1.5">
            {files.map((f) => {
              const remove = () => update(filesRef.current.filter((x) => x.key !== f.key))
              return f.preview ? <ImageChip key={f.key} src={f.preview} name={f.name} onRemove={remove}/> : <FileChip key={f.key} name={f.name} size={f.size} mediaType={f.media_type} source={`data:${f.media_type};base64,${f.data}`} onRemove={remove}/>
            })}
          </div>
        )}
        <div className="flex items-end gap-1.5">
          {terminal && (
            <Button
              variant="ghost" size="icon-sm" onClick={terminal.onToggle} aria-pressed={terminal.open} aria-label="Terminal" title={`Terminal (${MOD}\`)`}
              className={cn('mb-1 shrink-0', terminal.open && 'bg-accent text-foreground')}
            >
              <SquareTerminal/>
            </Button>
          )}
          <div className="relative min-w-0 flex-1">
            <input ref={input} type="file" accept={allowPDF ? undefined : "image/png,image/jpeg,image/gif,image/webp,text/*,.txt,.md,.json,.csv,.log"} multiple hidden onChange={(e) => { void add(e.target.files); e.target.value = '' }}/>
            <Button
              variant="ghost" size="icon-xs" onClick={() => input.current?.click()} disabled={off} aria-label="Attach files" title="Attach files (or drop / paste them)"
              className="absolute bottom-2 left-2"
            >
              <Paperclip/>
            </Button>
            <Textarea
              data-composer value={text} onChange={(e) => setText(e.target.value)} rows={1} aria-label={label} placeholder={placeholder} disabled={off}
              className={cn('max-h-40 min-h-10 resize-none rounded-[10px] py-[9px] pl-9', onInterrupt ? 'pr-16' : 'pr-10', dragging && 'border-ring')}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send() }
              }}
              onPaste={(e) => {
                // Pasted files (e.g. screenshots) are attached. WebKitGTK puts them in items, often not in files, so items
                // come first. A clipboard that also has plain text still pastes the text; otherwise the default is stopped,
                // and a paste carrying nothing usable falls back to reading the clipboard image directly.
                const cd = e.clipboardData
                let pasted = Array.from(cd.items).filter((i) => i.kind === 'file' || i.type.startsWith('image/')).map((i) => i.getAsFile()).filter((f): f is File => !!f)
                if (!pasted.length) pasted = [...cd.files]
                if (cd.types.includes('text/plain')) { if (pasted.length) void add(pasted); return }
                e.preventDefault()
                void (pasted.length ? add(pasted) : clipboardImage().then((f) => add(f ? [f] : null)))
              }}
            />
            <div className="absolute right-2 bottom-2 flex gap-0.5">
              {onInterrupt && (
                <Button
                  data-interrupt variant="ghost" size="icon-xs" onClick={interrupt} disabled={interrupting} aria-label="Interrupt" title="Interrupt (Esc)"
                  className="text-foreground"
                >
                  <Square className="fill-current"/>
                </Button>
              )}
              <Button
                variant="ghost" size="icon-xs" onClick={send} disabled={!ready} aria-label="Send message" title="Send (Enter, Shift+Enter for newline)"
                className={cn(ready && 'text-foreground')}
              >
                <CornerDownLeft/>
              </Button>
            </div>
          </div>
        </div>
        {reading > 0 && <p className="mt-1 text-[13px] text-muted-foreground">Reading files...</p>}
        {off && <div className="mt-1 text-[13px] text-muted-foreground">{disabledReason}</div>}
        {error && <p role="alert" className="mt-1 text-[13px] whitespace-pre-line text-destructive">{error}</p>}
      </div>
    </div>
  )
}
