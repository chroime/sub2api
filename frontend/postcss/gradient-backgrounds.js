import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const { parseColor } = require('tailwindcss/lib/util/color.js')
const defaultPositions = {
  '--tw-gradient-from-position': '0%',
  '--tw-gradient-via-position': '50%',
  '--tw-gradient-to-position': '100%',
}

export default function gradientBackgrounds() {
  return {
    postcssPlugin: 'sub2api-gradient-backgrounds',
    OnceExit(root) {
      root.walkDecls(declaration => {
        if (defaultPositions[declaration.prop] && !declaration.value.trim()) {
          declaration.value = defaultPositions[declaration.prop]
        }
        const stop = declaration.prop.match(/^--tw-gradient-(from|to)$/)?.[1]
        if (!stop) return
        const value = declaration.value.replace(/\s+var\(--tw-gradient-(?:from|to)-position\)$/, '')
        const color = parseColor(value)
        // Opaque stops supply a solid fallback. A translucent decoration must
        // not gain a second tinted layer underneath its existing gradient.
        const opaque = color && (color.alpha === undefined || Number.parseFloat(color.alpha) === 1)
        declaration.cloneBefore({ prop: `--app-gradient-${stop}-color`, value: opaque ? value : 'initial' })
      })
    },
  }
}
