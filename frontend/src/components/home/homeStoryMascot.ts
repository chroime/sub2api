import mascotSource from '/xeno-alien-spin.svg?raw'

let nextInstance = 0

export function createHomeStoryMascot(): string {
  const document = new DOMParser().parseFromString(mascotSource, 'image/svg+xml')
  const root = document.documentElement
  const prefix = `home-story-${++nextInstance}-`
  const ids = new Map<string, string>()
  for (const node of root.querySelectorAll('[id]')) {
    const id = node.getAttribute('id')!
    ids.set(id, `${prefix}${id}`)
    node.setAttribute('id', `${prefix}${id}`)
  }
  // Only bundled, trusted artwork enters v-html. Namespacing keeps its paint
  // servers independent from other SVGs rendered on the same document.
  for (const node of [root, ...root.querySelectorAll('*')]) {
    for (const attribute of Array.from(node.attributes)) {
      let value = attribute.value.replace(/url\(#([^)]+)\)/g, (reference, id: string) =>
        ids.has(id) ? `url(#${ids.get(id)})` : reference,
      )
      if (attribute.localName === 'href' && value.startsWith('#')) {
        value = `#${ids.get(value.slice(1)) || value.slice(1)}`
      }
      if (attribute.localName === 'aria-labelledby' || attribute.localName === 'aria-describedby') {
        value = value.split(/\s+/).map(id => ids.get(id) || id).join(' ')
      }
      if (value !== attribute.value) attribute.value = value
    }
  }
  root.setAttribute('id', 'mascot-svg')
  root.setAttribute('data-motion-managed', 'true')
  root.setAttribute('aria-hidden', 'true')
  return new XMLSerializer().serializeToString(root)
}
