import {formatBytes} from './format'
import type {Attachment} from './types'

// Reading chat attachments in the browser. Limits mirror the backend's (it validates again).
export const MAX_ATTACHMENTS = 10
export const MAX_TOTAL = 20 << 20
const MAX_IMAGE = 5 << 20
const MAX_PDF = 10 << 20
const MAX_TEXT = 256 << 10
const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
// Fallback when the browser gives no (or a generic) type, as drops from some file managers do.
const BY_EXT: Record<string, string> = {png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', pdf: 'application/pdf'}

// An attachment ready to send, plus what the composer shows for it. preview = data URL, images only.
export type PendingAttachment = Attachment & {key: number; size: number; preview?: string}

let nextKey = 0

const readDataURL = (f: Blob) => new Promise<string>((resolve, reject) => {
  const r = new FileReader()
  r.onload = () => resolve(r.result as string)
  r.onerror = () => reject(r.error)
  r.readAsDataURL(f)
})

// Images and PDFs keep their type; anything else must be UTF-8 text and goes as text/plain.
// Rejects with a message naming the file.
export async function readAttachment(f: File): Promise<PendingAttachment> {
  const ext = f.name.includes('.') ? f.name.split('.').pop()!.toLowerCase() : ''
  const type = IMAGE_TYPES.includes(f.type) || f.type === 'application/pdf' ? f.type : BY_EXT[ext] ?? ''
  const image = type.startsWith('image/')
  const limit = image ? MAX_IMAGE : type ? MAX_PDF : MAX_TEXT
  if (f.size > limit) throw `${f.name} is too large (${formatBytes(f.size)}; ${image ? 'images' : type ? 'PDFs' : 'text files'} can be at most ${formatBytes(limit)})`
  if (!type) {
    const text = new TextDecoder().decode(await f.arrayBuffer())
    if (text.includes('�') || text.includes('\0')) throw `unsupported file type: ${f.name}`
  }
  const url = await readDataURL(f)
  const data = url.slice(url.indexOf(',') + 1)
  return {key: ++nextKey, name: f.name, media_type: type || 'text/plain', data, size: f.size, preview: image ? `data:${type};base64,${data}` : undefined}
}
