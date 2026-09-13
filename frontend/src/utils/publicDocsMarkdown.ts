import { marked } from 'marked'
import DOMPurify from 'dompurify'

export interface PublicDocsChapter {
  id: string
  text: string
  level: number
}

export function renderPublicDocsMarkdown(content: string, copyLabel?: string): { html: string; toc: PublicDocsChapter[] } {
  if (!content.trim()) return { html: '', toc: [] }
  const html = marked.parse(content, { async: false, gfm: true, breaks: true })
  const root = document.createElement('div')
  root.innerHTML = DOMPurify.sanitize(html, {
    ALLOWED_TAGS: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'br', 'hr', 'a', 'img', 'pre', 'code', 'blockquote', 'ul', 'ol', 'li', 'strong', 'em', 'del', 's', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'sup', 'sub'],
    ALLOWED_ATTR: ['href', 'src', 'alt', 'title', 'start', 'colspan', 'rowspan'],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
  })
  const toc: PublicDocsChapter[] = []
  const ids = new Set<string>()
  const anchors = new Map<string, string>()
  root.querySelectorAll('h1, h2, h3, h4, h5, h6').forEach(heading => {
    const text = heading.textContent?.trim() || ''
    const slug = text.toLowerCase().replace(/[^\p{L}\p{N}\s-]/gu, '').trim().replace(/\s+/g, '-') || 'section'
    let id = `docs-${slug}`
    let suffix = 2
    while (ids.has(id)) id = `docs-${slug}-${suffix++}`
    ids.add(id)
    if (!anchors.has(slug)) anchors.set(slug, id)
    heading.id = id
    toc.push({ id, text, level: Number(heading.tagName.slice(1)) })
  })
  root.querySelectorAll('a[href]').forEach(link => {
    const href = link.getAttribute('href') || ''
    if (href.startsWith('#')) {
      try {
        const id = anchors.get(decodeURIComponent(href.slice(1)))
        if (id) link.setAttribute('href', `#${id}`)
      } catch { /* Keep malformed fragments inert without failing the document. */ }
    } else if (/^(https?:)?\/\//i.test(href)) {
      link.setAttribute('target', '_blank')
      link.setAttribute('rel', 'noopener noreferrer')
    }
  })
  if (copyLabel) {
    root.querySelectorAll('pre').forEach(pre => {
      if (!pre.querySelector('code')) return
      const button = document.createElement('button')
      button.type = 'button'
      button.dataset.docsCopy = ''
      button.textContent = copyLabel
      button.title = copyLabel
      pre.append(button)
    })
  }
  return { html: root.innerHTML, toc }
}
