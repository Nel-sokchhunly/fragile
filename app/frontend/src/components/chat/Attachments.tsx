import {useEffect, useState} from 'react'
import {FileText, X} from 'lucide-react'
import {Dialog, DialogContent, DialogTitle} from '@/components/ui/dialog'
import {api} from '@/lib/api'
import {formatBytes} from '@/lib/format'
import type {AttachmentInfo} from '@/lib/types'

type Source = string | (() => Promise<string>) // a data URL, or a lazy fetch of one

const isText = (type: string) => type.startsWith('text/') || type.includes('json')

// Full-size view of an image, or the content of a text file, in a dialog.
function PreviewDialog({name, image, source, onClose}: {name: string; image: boolean; source: Source; onClose: () => void}) {
  const [content, setContent] = useState<string>()
  const [error, setError] = useState(false)
  useEffect(() => {
    let live = true
    Promise.resolve(typeof source === 'string' ? source : source()).then((url) => {
      if (!live) return
      if (image) return setContent(url)
      const bin = atob(url.slice(url.indexOf(',') + 1))
      setContent(new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0))))
    }).catch(() => live && setError(true))
    return () => { live = false }
  }, [source, image])
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent aria-describedby={undefined} className="w-auto max-w-[90vw] sm:max-w-[90vw]">
        <DialogTitle className="truncate pr-8">{name}</DialogTitle>
        {error ? <p className="text-muted-foreground">Could not load the file.</p>
          : content === undefined ? <p className="text-muted-foreground">Loading…</p>
          : image ? <img src={content} alt={name} className="mx-auto max-h-[85vh] max-w-[90vw] object-contain"/>
          : <pre className="max-h-[75vh] overflow-auto rounded-md bg-surface-sunken p-3 font-mono text-xs whitespace-pre-wrap">{content}</pre>}
      </DialogContent>
    </Dialog>
  )
}

// Name + size of a non-image attachment; onRemove adds an x (composer only). Text files (with a source) open a preview.
export function FileChip({name, size, mediaType = '', source, onRemove}: {name: string; size: number; mediaType?: string; source?: Source; onRemove?: () => void}) {
  const [open, setOpen] = useState(false)
  const body = (
    <>
      <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden/>
      <span className="min-w-0 truncate">{name}</span>
      <span className="shrink-0 font-mono text-muted-foreground">{formatBytes(size)}</span>
    </>
  )
  return (
    <div className="flex h-7 max-w-56 items-center gap-1.5 rounded-md border border-border-default px-2 text-xs text-text-secondary" title={name}>
      {source && isText(mediaType)
        ? <button type="button" onClick={() => setOpen(true)} aria-label={`Preview ${name}`} className="flex min-w-0 flex-1 items-center gap-1.5 self-stretch">{body}</button>
        : <div className="flex min-w-0 flex-1 items-center gap-1.5">{body}</div>}
      {onRemove && (
        <button type="button" onClick={(e) => { e.stopPropagation(); onRemove() }} aria-label={`Remove ${name}`} className="-mr-1 shrink-0 rounded-sm p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground">
          <X className="size-3"/>
        </button>
      )}
      {open && source && <PreviewDialog name={name} image={false} source={source} onClose={() => setOpen(false)}/>}
    </div>
  )
}

// Thumbnail of an image attachment; onRemove adds an x (composer only). Click opens it full size.
export function ImageChip({src, name, onRemove}: {src?: string; name: string; onRemove?: () => void}) {
  const [open, setOpen] = useState(false)
  return (
    <div className="relative size-14 shrink-0 overflow-hidden rounded-md border border-border-default bg-surface-sunken" title={name}>
      {src && (
        <button type="button" onClick={() => setOpen(true)} aria-label={`Preview ${name}`} className="size-full">
          <img src={src} alt={name} className="size-full object-cover"/>
        </button>
      )}
      {onRemove && (
        <button type="button" onClick={(e) => { e.stopPropagation(); onRemove() }} aria-label={`Remove ${name}`} className="absolute top-0.5 right-0.5 rounded-sm bg-background/80 p-0.5 text-muted-foreground hover:text-foreground">
          <X className="size-3"/>
        </button>
      )}
      {open && src && <PreviewDialog name={name} image source={src} onClose={() => setOpen(false)}/>}
    </div>
  )
}

// Data URLs already fetched, so rows Virtuoso remounts on scroll don't refetch. Oldest dropped past the cap.
const cache = new Map<string, string>()
const CACHE_MAX = 40

// An image of a sent message, fetched once it is first rendered (Virtuoso only renders visible rows).
function SentImage({sessionId, itemId, index, a}: {sessionId: number; itemId: number; index: number; a: AttachmentInfo}) {
  const k = `${sessionId}:${itemId}:${index}`
  const [src, setSrc] = useState(() => cache.get(k))
  const [failed, setFailed] = useState(false)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (src) return
    let live = true
    api.getAttachment(sessionId, itemId, index).then((url) => {
      cache.set(k, url)
      if (cache.size > CACHE_MAX) cache.delete(cache.keys().next().value!)
      if (live) setSrc(url)
    }, () => live && setFailed(true))
    return () => { live = false }
  }, [k, src, sessionId, itemId, index])
  if (failed) return <FileChip name={a.name} size={a.size}/>
  return (
    <div className="h-24 overflow-hidden rounded-md border border-border-default bg-surface-sunken" title={a.name}>
      {src
        ? <button type="button" onClick={() => setOpen(true)} aria-label={`Preview ${a.name}`} className="h-full"><img src={src} alt={a.name} className="h-full max-w-60 object-contain"/></button>
        : <div className="h-full w-24"/>}
      {open && src && <PreviewDialog name={a.name} image source={src} onClose={() => setOpen(false)}/>}
    </div>
  )
}

// The attachments of a user chat message, right-aligned above its text.
export function SentAttachments({sessionId, itemId, attachments}: {sessionId: number; itemId: number; attachments: AttachmentInfo[]}) {
  return (
    <div className="flex flex-wrap justify-end gap-1.5">
      {attachments.map((a, i) => a.media_type.startsWith('image/')
        ? <SentImage key={i} sessionId={sessionId} itemId={itemId} index={i} a={a}/>
        : <FileChip key={i} name={a.name} size={a.size} mediaType={a.media_type} source={() => api.getAttachment(sessionId, itemId, i)}/>)}
    </div>
  )
}
