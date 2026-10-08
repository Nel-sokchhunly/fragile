import type {HighlighterCore} from 'shiki/core'
import {bundledLanguages} from 'shiki/langs'

// Lazy and light: shiki core + JS regex engine + two themes. Every bundled grammar is available (keys
// include aliases like ts/sh), but each is its own chunk fetched on first use; nothing loads until a code
// block renders.

let hl: Promise<HighlighterCore> | undefined
function highlighter() {
  hl ??= (async () => {
    const [{createHighlighterCore}, {createJavaScriptRegexEngine}, light, dark] = await Promise.all([
      import('shiki/core'),
      import('shiki/engine/javascript'),
      import('shiki/themes/github-light.mjs'),
      import('shiki/themes/github-dark.mjs'),
    ])
    return createHighlighterCore({themes: [light.default, dark.default], langs: [], engine: createJavaScriptRegexEngine()})
  })()
  return hl
}

// ponytail: unbounded cache keyed by lang+code; add an LRU if memory shows up in long sessions.
const cache = new Map<string, string>()

// Resolves to highlighted HTML, or null when the language is unsupported (caller shows plain text).
export async function highlight(code: string, lang: string): Promise<string | null> {
  const load = bundledLanguages[lang.toLowerCase() as keyof typeof bundledLanguages]
  if (!load) return null
  const key = `${lang}\0${code}`
  const hit = cache.get(key)
  if (hit) return hit
  const h = await highlighter()
  await h.loadLanguage(load())
  // defaultColor:false emits --shiki-light/--shiki-dark vars; index.css picks one per theme.
  const html = h.codeToHtml(code, {
    lang: lang.toLowerCase(),
    themes: {light: 'github-light', dark: 'github-dark'},
    defaultColor: false,
  })
  cache.set(key, html)
  return html
}
