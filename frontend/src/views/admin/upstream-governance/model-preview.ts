// Only the disposable preview receives this policy. Downloaded artifacts retain
// their original bytes, and generated scripts never enter the administrator DOM.
export const modelPreviewCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; child-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
export function modelPreviewDocument(html: string): string {
  const preview = `<!doctype html><meta http-equiv="Content-Security-Policy" content="${modelPreviewCSP}"><meta name="referrer" content="no-referrer">${html}`
  // srcdoc inherits the page's nonce CSP; a child meta policy cannot relax it.
  // Parse and authorize inline scripts only inside the already isolated frame.
  // Parsing generated HTML in the parent could start resource requests there.
  // The original artifact and administrator page policy remain untouched.
  const nonce = document.querySelector<HTMLScriptElement>('script[nonce]')?.nonce
  if (!nonce) return preview
  const payload = JSON.stringify(preview).replace(/</g, '\\u003c').replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029')
  const nonceAttribute = nonce.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')
  return `<!doctype html><meta http-equiv="Content-Security-Policy" content="${modelPreviewCSP}"><meta name="referrer" content="no-referrer"><script nonce="${nonceAttribute}">(function(source) {
    const nonce = document.currentScript.nonce;
    const parsed = new DOMParser().parseFromString(source, 'text/html');
    parsed.head.prepend(
      document.querySelector('meta[http-equiv="Content-Security-Policy"]').cloneNode(true),
      document.querySelector('meta[name="referrer"]').cloneNode(true)
    );
    for (const script of parsed.querySelectorAll('script')) {
      script.removeAttribute('nonce');
      const external = ['src', 'href', 'xlink:href'].some(attribute => script.hasAttribute(attribute)) || script.hasAttributeNS('http://www.w3.org/1999/xlink', 'href');
      if (!external) script.setAttribute('nonce', nonce);
    }
    document.open();
    document.write('<!doctype html>' + parsed.documentElement.outerHTML);
    document.close();
  })(${payload})</script>`
}
export function modelHTMLArtifacts(response: string, savedHTML = ''): { source: string; html: string }[] {
  const artifacts: { source: string; html: string }[] = []
  if (/^\s*(?:<!doctype\s+html[^>]*>\s*)?<html[\s>]/i.test(response)) {
    artifacts.push({ source: 'original', html: response })
  }
  const blocks = response.matchAll(/```(?:html|htm)[ \t]*\r?\n([\s\S]*?)\r?\n```/gi)
  let index = 0
  for (const block of blocks) artifacts.push({ source: `block:${++index}`, html: block[1] || '' })
  if (savedHTML && !artifacts.some(artifact => artifact.html === savedHTML)) artifacts.push({ source: 'saved', html: savedHTML })
  return artifacts
}
export function downloadModelArtifact(contents: string, filename: string): void {
  const url = URL.createObjectURL(new Blob([contents], { type: 'application/octet-stream;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}
