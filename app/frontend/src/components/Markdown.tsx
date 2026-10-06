import {memo, useEffect, useState} from 'react'
import ReactMarkdown, {type Components} from 'react-markdown'
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
      : <code className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]">{children}</code>
  },
  a: ({children, href}) => <a href={href} target="_blank" rel="noreferrer" className="underline underline-offset-2">{children}</a>,
}

export const Markdown = memo(function Markdown({children}: {children: string}) {
  return <div className="md"><ReactMarkdown components={components}>{children}</ReactMarkdown></div>
})
