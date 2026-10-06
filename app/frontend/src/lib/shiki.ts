import type {HighlighterCore} from 'shiki/core'

// Lazy and light: shiki core + JS regex engine + two themes + only the grammars below, each
// fetched on first use as its own chunk. Nothing here loads until a code block renders.
const LANGS: Record<string, () => Promise<unknown>> = {
  ts: () => import('shiki/langs/typescript.mjs'),
  typescript: () => import('shiki/langs/typescript.mjs'),
  tsx: () => import('shiki/langs/tsx.mjs'),
  js: () => import('shiki/langs/javascript.mjs'),
  javascript: () => import('shiki/langs/javascript.mjs'),
  json: () => import('shiki/langs/json.mjs'),
  bash: () => import('shiki/langs/bash.mjs'),
  sh: () => import('shiki/langs/bash.mjs'),
  go: () => import('shiki/langs/go.mjs'),
  python: () => import('shiki/langs/python.mjs'),
  diff: () => import('shiki/langs/diff.mjs'),
  yaml: () => import('shiki/langs/yaml.mjs'),
  sql: () => import('shiki/langs/sql.mjs'),
}
const ALIAS: Record<string, string> = {ts: 'typescript', js: 'javascript', sh: 'bash'}

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
  const load = LANGS[lang]
  if (!load) return null
  const key = `${lang}\0${code}`
  const hit = cache.get(key)
  if (hit) return hit
  const h = await highlighter()
  await h.loadLanguage(load() as never)
  // defaultColor:false emits --shiki-light/--shiki-dark vars; index.css picks one per theme.
  const html = h.codeToHtml(code, {
    lang: ALIAS[lang] ?? lang,
    themes: {light: 'github-light', dark: 'github-dark'},
    defaultColor: false,
  })
  cache.set(key, html)
  return html
}
