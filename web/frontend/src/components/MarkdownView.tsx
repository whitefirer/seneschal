import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

/**
 * MarkdownView renders markdown content (AI output, chat replies) using the
 * app's theme via Tailwind + CSS variables. Used by Execution.tsx for AI step
 * streaming output and ChatPanel for assistant replies.
 */
export function MarkdownView({ content }: { content: string }) {
  return (
    <div className="markdown-body text-sm leading-relaxed">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // Map HTML elements to Tailwind-styled components so the output
          // matches the app theme without importing a separate stylesheet.
          p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
          h1: ({ children }) => <h1 className="text-lg font-bold mb-2 mt-3">{children}</h1>,
          h2: ({ children }) => <h2 className="text-base font-bold mb-2 mt-3">{children}</h2>,
          h3: ({ children }) => <h3 className="text-sm font-bold mb-1 mt-2">{children}</h3>,
          ul: ({ children }) => <ul className="list-disc pl-5 mb-2 space-y-0.5">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal pl-5 mb-2 space-y-0.5">{children}</ol>,
          li: ({ children }) => <li className="text-foreground">{children}</li>,
          code: ({ inline, children }: any) =>
            inline ? (
              <code className="px-1 py-0.5 rounded bg-muted text-foreground text-xs font-mono">{children}</code>
            ) : (
              <pre className="mb-2 p-2 rounded bg-muted text-foreground text-xs font-mono overflow-x-auto">
                <code>{children}</code>
              </pre>
            ),
          pre: ({ children }) => <>{children}</>,
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noopener noreferrer" className="text-primary underline">{children}</a>
          ),
          strong: ({ children }) => <strong className="font-bold text-foreground">{children}</strong>,
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-muted pl-3 italic text-muted-foreground mb-2">{children}</blockquote>
          ),
          table: ({ children }) => (
            <table className="mb-2 border-collapse text-xs">{children}</table>
          ),
          th: ({ children }) => <th className="border border-muted px-2 py-1 text-left font-bold">{children}</th>,
          td: ({ children }) => <td className="border border-muted px-2 py-1">{children}</td>,
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
