import {memo, useEffect, useState} from 'react'
import ReactMarkdown, {type Components} from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {BrowserOpenURL} from '../../wailsjs/runtime/runtime'
import {highlight} from '@/lib/shiki'

function CodeBlock({code, lang}: {code: string; lang: string}) {
  const [html, setHtml] = useState<string | null>(null)
  useEffect(() => {
    let live = true
    highlight(code, lang).then((h) => live && setHtml(h), () => {})
    return () => {
      live = false
    }
  }, [code, lang])
  return html ? (
    <div className="code-block" dangerouslySetInnerHTML={{__html: html}}/>
  ) : (
    <pre className="code-block"><code>{code}</code></pre>
  )
}

const components: Components = {
  pre: ({children}) => <>{children}</>,
  code: ({className, children}) => {
    const lang = /language-(\w+)/.exec(className ?? '')?.[1]
    const text = String(children)
    return lang || text.includes('\n')
      ? <CodeBlock code={text.replace(/\n$/, '')} lang={lang ?? ''}/>
      : <code className="rounded-sm bg-surface-sunken px-1 py-px font-mono text-[0.85em]">{children}</code>
  },
  // Agent output is untrusted and target=_blank does nothing in the webview: only http(s) opens, in the system browser.
  a: ({children, href}) => /^https?:\/\//i.test(href ?? '')
    ? <a href={href} onClick={(e) => { e.preventDefault(); BrowserOpenURL(href!) }}>{children}</a>
    : <span>{children}</span>,
}

export const Markdown = memo(function Markdown({children}: {children: string}) {
  return <div className="md"><ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>{children}</ReactMarkdown></div>
})
