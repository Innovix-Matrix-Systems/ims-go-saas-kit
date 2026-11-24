// Applies the saved theme, accent and density before the dashboard loads, so
// a reload never flashes the wrong theme. A file (not inline) so a Content
// Security Policy can stay at script-src 'self'. Keep the keys and defaults
// in step with src/components/appearance.ts.
;(function () {
  try {
    var store = localStorage
    var root = document.documentElement
    var theme = store.getItem('ims.dev.theme')
    if (theme !== 'light' && theme !== 'dark') {
      theme = matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    }
    root.dataset.theme = theme
    root.dataset.accent = store.getItem('ims.dev.accent') || 'indigo'
    root.dataset.density = store.getItem('ims.dev.density') || 'comfortable'
  } catch (e) {
    /* storage disabled: defaults apply */
  }
})()
