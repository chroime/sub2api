import tailwindcss from 'tailwindcss'
import autoprefixer from 'autoprefixer'
import gradientBackgrounds from './postcss/gradient-backgrounds.js'

export default {
  plugins: [tailwindcss(), gradientBackgrounds(), autoprefixer()]
}
