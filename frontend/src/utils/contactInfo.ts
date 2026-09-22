export interface ContactInfoEntry {
  label: string
  value: string
}

export function parseContactInfo(raw: string): ContactInfoEntry[] {
  return raw
    .split(/[\r\n;；|]+/)
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((entry) => {
      const match = entry.match(/^([^:：]{1,16})\s*[:：]\s*(.+)$/)
      return {
        label: match?.[1]?.trim() || '',
        value: (match?.[2] || entry).trim(),
      }
    })
}
