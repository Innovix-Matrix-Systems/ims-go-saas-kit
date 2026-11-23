// Applies the saved theme, accent and density before the app loads, so a
// reload never flashes the wrong theme. A file (not inline) so the Content
// Security Policy can stay at script-src 'self'. Keep the keys and defaults
// in step with src/components/appearance.ts.
;(function () {
  try {
    var store = localStorage
    var root = document.documentElement
    var theme = store.getItem('ims.app.theme')
    if (theme !== 'light' && theme !== 'dark') {
      theme = matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    }
    root.dataset.theme = theme
    root.dataset.accent = store.getItem('ims.app.accent') || 'indigo'
    root.dataset.density = store.getItem('ims.app.density') || 'comfortable'
    var locale = store.getItem('ims.app.locale')
    if (locale === 'en' || locale === 'bn') root.lang = locale
  } catch (e) {
    /* storage disabled: defaults apply */
  }
})()
