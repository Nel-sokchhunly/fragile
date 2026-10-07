import {useEffect, useState} from 'react'
import {FileText, X} from 'lucide-react'
import {api} from '@/lib/api'
import {formatBytes} from '@/lib/format'
import type {AttachmentInfo} from '@/lib/types'

// Name + size of a non-image attachment; onRemove adds an x (composer only).
export function FileChip({name, size, onRemove}: {name: string; size: number; onRemove?: () => void}) {
  return (
    <div className="flex h-7 max-w-56 items-center gap-1.5 rounded-md border border-border-default px-2 text-xs text-text-secondary" title={name}>
      <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden/>
      <span className="min-w-0 truncate">{name}</span>
      <span className="shrink-0 font-mono text-muted-foreground">{formatBytes(size)}</span>
      {onRemove && (
        <button type="button" onClick={onRemove} aria-label={`Remove ${name}`} className="-mr-1 shrink-0 rounded-sm p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground">
          <X className="size-3"/>
        </button>
      )}
    </div>
  )
}

// Thumbnail of an image attachment; onRemove adds an x (composer only).
export function ImageChip({src, name, onRemove}: {src?: string; name: string; onRemove?: () => void}) {
  return (
    <div className="relative size-14 shrink-0 overflow-hidden rounded-md border border-border-default bg-surface-sunken" title={name}>
      {src && <img src={src} alt={name} className="size-full object-cover"/>}
      {onRemove && (
        <button type="button" onClick={onRemove} aria-label={`Remove ${name}`} className="absolute top-0.5 right-0.5 rounded-sm bg-background/80 p-0.5 text-muted-foreground hover:text-foreground">
          <X className="size-3"/>
        </button>
      )}
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
      {src ? <img src={src} alt={a.name} className="h-full max-w-60 object-contain"/> : <div className="h-full w-24"/>}
    </div>
  )
}

// The attachments of a user chat message, right-aligned above its text.
export function SentAttachments({sessionId, itemId, attachments}: {sessionId: number; itemId: number; attachments: AttachmentInfo[]}) {
  return (
    <div className="flex flex-wrap justify-end gap-1.5">
      {attachments.map((a, i) => a.media_type.startsWith('image/')
        ? <SentImage key={i} sessionId={sessionId} itemId={itemId} index={i} a={a}/>
        : <FileChip key={i} name={a.name} size={a.size}/>)}
    </div>
  )
}
