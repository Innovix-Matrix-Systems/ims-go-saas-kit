// Rewrites relative Markdown links (`../guides/tenancy.md#scope`) into site
// URLs (`/guides/tenancy/#scope`), so the same docs/ files link correctly on
// GitHub and on the site. Links that leave docs/ (../../AGENTS.md) point at
// the file on GitHub instead. Unknown targets are left as-is for
// scripts/check-links.mjs to report.
import { existsSync } from 'node:fs'
import { dirname, relative, resolve, sep } from 'node:path'

const visit = (node, fn) => {
  fn(node)
  for (const child of node.children ?? []) visit(child, fn)
}

/** @param {{ docsDir: string, repoUrl: string }} options */
export default function rehypeDocsLinks({ docsDir, repoUrl }) {
  const root = resolve(docsDir, '..')
  return (tree, file) => {
    const from = file.path ?? file.history?.[0]
    if (!from) return
    visit(tree, (node) => {
      if (node.type !== 'element' || node.tagName !== 'a') return
      const href = node.properties?.href
      if (typeof href !== 'string' || /^([a-z]+:|\/|#)/i.test(href)) return
      const [path, hash] = href.split('#')
      const target = resolve(dirname(from), decodeURI(path))
      const inDocs = relative(docsDir, target)
      const docsPath = inDocs.split(sep).join('/')
      // Files in docs/ that are not pages (the folder's README, old reviews,
      // proposals) are read on GitHub, like files outside docs/.
      const notAPage = docsPath === 'README.md' || /^(history|design)\//.test(docsPath)
      if (/\.mdx?$/.test(path) && !inDocs.startsWith('..') && !notAPage && existsSync(target)) {
        const slug = inDocs.split(sep).join('/').replace(/\.mdx?$/, '').replace(/(^|\/)index$/, '')
        node.properties.href = '/' + (slug ? slug + '/' : '') + (hash ? '#' + hash : '')
      } else if ((inDocs.startsWith('..') || notAPage) && existsSync(target)) {
        // Outside docs/ (AGENTS.md, .agents/skills, source files): link to GitHub.
        const repoPath = relative(root, target).split(sep).join('/')
        node.properties.href = `${repoUrl}/blob/main/${repoPath}` + (hash ? '#' + hash : '')
      }
    })
  }
}
