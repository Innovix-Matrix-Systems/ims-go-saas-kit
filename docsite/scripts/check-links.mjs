// Post-build link check over the generated HTML (dist/). Fails the build when
// an internal link points at a page or #anchor that does not exist, or when a
// Markdown link (./page.md) was not rewritten to a site URL — which means its
// target file is missing or misspelled in docs/.
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

const dist = new URL('../dist/', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1')
const files = []
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path)
    else if (name.endsWith('.html')) files.push(path)
  }
}
walk(dist)

const urlOf = (file) => '/' + relative(dist, file).split(sep).join('/').replace(/index\.html$/, '').replace(/\.html$/, '/')
const ids = new Map()
const idsOf = (file) => {
  if (!ids.has(file)) ids.set(file, new Set([...readFileSync(file, 'utf8').matchAll(/\sid="([^"]+)"/g)].map((m) => m[1])))
  return ids.get(file)
}
const pageFile = (url) => {
  const clean = decodeURI(url.split('#')[0].split('?')[0])
  for (const candidate of [join(dist, clean, 'index.html'), join(dist, clean)]) {
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate
  }
  return null
}

// Links into the running app (the API reference, the dashboard) are not pages.
const external = /^(https?:|mailto:|\/\/|\/docs\/?$|\/dev\/)/

const problems = []
for (const file of files) {
  const html = readFileSync(file, 'utf8')
  const from = urlOf(file)
  for (const [, href] of html.matchAll(/<a[^>]+href="([^"]+)"/g)) {
    if (external.test(href) || href.startsWith('#') && href.length === 1) continue
    if (/\.mdx?(#|$)/.test(href)) {
      problems.push(`${from}: unresolved Markdown link ${href} (missing or misspelled file?)`)
      continue
    }
    if (href.startsWith('#')) {
      if (!idsOf(file).has(decodeURIComponent(href.slice(1)))) problems.push(`${from}: missing anchor ${href}`)
      continue
    }
    if (!href.startsWith('/') || /\.(png|svg|jpe?g|webp|gif|css|js|xml|txt|ico)(\?|$)/.test(href)) continue
    const target = pageFile(href)
    if (!target) {
      problems.push(`${from}: broken link ${href}`)
      continue
    }
    const hash = href.split('#')[1]
    if (hash && !idsOf(target).has(decodeURIComponent(hash))) problems.push(`${from}: missing anchor ${href}`)
  }
}

if (problems.length) {
  console.error(`\n✗ ${problems.length} broken link(s):\n  ` + [...new Set(problems)].join('\n  ') + '\n')
  process.exit(1)
}
console.log(`✓ links: ${files.length} pages, every internal link and anchor resolves`)
