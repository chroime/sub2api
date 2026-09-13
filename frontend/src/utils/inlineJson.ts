export function serializeInlineJson(value: Record<string, unknown>): string {
  // JSON is embedded in a script element; Markdown may contain a closing script tag.
  return JSON.stringify(value).replace(/</g, '\\u003c')
}
