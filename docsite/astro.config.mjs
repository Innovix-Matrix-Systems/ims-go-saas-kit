// @ts-check
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'astro/config'
import starlight from '@astrojs/starlight'
import rehypeDocsLinks from './src/plugins/rehype-docs-links.mjs'
import { unified } from '@astrojs/markdown-remark'

// The content lives in ../docs as plain Markdown, so it reads well on GitHub
// too; this folder is only the site shell (config, theme, components).
const version = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim()
const repoUrl = 'https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit'
const docsDir = fileURLToPath(new URL('../docs', import.meta.url))
const siteUrl = process.env.DOCS_SITE_URL || 'https://ims-go-saas-kit.innovixmatrixsystem.com'

const minutes = (text) => ({ text, variant: /** @type {const} */ ('tip') })

const sidebar = [
  {
    label: 'Start here',
    items: [
      'getting-started/introduction',
      'getting-started/quick-start',
      'getting-started/project-tour',
      'getting-started/how-it-works',
      'getting-started/coming-from-laravel',
      'getting-started/make-it-yours',
    ],
  },
  {
    label: 'Tutorial',
    items: [
      { slug: 'tutorial/first-module', badge: minutes('45 min') },
      { slug: 'tutorial/connect-an-ai-agent', badge: minutes('15 min') },
      { slug: 'tutorial/sign-in-with-github', badge: minutes('15 min') },
    ],
  },
  {
    label: 'The app',
    items: [
      'app/overview',
      'app/organization-workspace',
      'app/staff-console',
      'app/adding-a-screen',
      'app/translations-and-theming',
    ],
  },
  {
    label: 'The backend',
    items: [
      'guides/modules',
      'guides/handlers-and-routes',
      'guides/validation-and-errors',
      'guides/database',
      'guides/testing',
      'guides/cli-commands',
    ],
  },
  {
    label: 'Features',
    items: [
      {
        label: 'Accounts',
        items: ['guides/authentication', 'guides/sign-up', 'guides/social-login', 'guides/oauth'],
      },
      { label: 'Organizations', items: ['guides/tenancy', 'guides/permissions', 'guides/billing'] },
      {
        label: 'Product',
        items: ['guides/settings', 'guides/feature-flags', 'guides/notifications', 'guides/mail-and-i18n'],
      },
      {
        label: 'Data',
        items: ['guides/files-and-storage', 'guides/import-export', 'guides/activity-log', 'guides/privacy'],
      },
      { label: 'Integrations', items: ['guides/webhooks', 'guides/realtime', 'guides/mcp'] },
      {
        label: 'Runtime',
        items: [
          'guides/queue-and-scheduler',
          'guides/maintenance-mode',
          'guides/delivery',
          'guides/health-and-observability',
        ],
      },
    ],
  },
  { label: 'Developer dashboard', items: ['dashboard/overview', 'dashboard/extending'] },
  {
    label: 'Operations',
    items: [
      'operations/local-development',
      'operations/docker',
      'operations/deploy',
      'operations/migrations',
      'operations/releases',
      'operations/backups',
      'operations/rollback',
    ],
  },
  {
    label: 'Reference',
    items: ['reference/http-api', 'reference/cli', 'reference/configuration', 'reference/make', 'reference/permissions'],
  },
  {
    label: 'Architecture & project',
    items: [
      'architecture/overview',
      'architecture/decisions',
      'project/ai-agents',
      'project/contributing',
      'project/changelog',
    ],
  },
]

// Every page in docs/ must be reachable from the sidebar: a page that builds
// but is in no group is invisible to readers. Fails the build with the list.
const pagesInDocs = () => {
  const slugs = []
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name)
      const rel = relative(docsDir, path).split(sep).join('/')
      if (statSync(path).isDirectory()) {
        if (!['history', 'design', 'images'].includes(rel)) walk(path)
      } else if (/\.mdx?$/.test(name) && rel !== 'README.md' && rel !== 'index.mdx') {
        slugs.push(rel.replace(/\.mdx?$/, ''))
      }
    }
  }
  walk(docsDir)
  return slugs
}
const sidebarSlugs = (items) =>
  items.flatMap((item) =>
    typeof item === 'string' ? [item] : item.items ? sidebarSlugs(item.items) : item.slug ? [item.slug] : [],
  )
const listed = new Set(sidebarSlugs(sidebar))
const missing = pagesInDocs().filter((slug) => !listed.has(slug))
if (missing.length) {
  throw new Error(`docs pages missing from the sidebar (astro.config.mjs):\n  ${missing.join('\n  ')}`)
}

export default defineConfig({
  // The public address: canonical links, the sitemap and link previews use it.
  // A fork publishing elsewhere sets DOCS_SITE_URL instead.
  site: siteUrl,
  markdown: {
    // Relative links such as ../guides/tenancy.md work on GitHub and here.
    processor: unified({ rehypePlugins: [[rehypeDocsLinks, { docsDir, repoUrl }]] }),
  },
  vite: {
    resolve: {
      // Pages live outside this folder (../docs), where Node can't find
      // docsite/node_modules, so imports used in MDX are aliased here.
      alias: [
        { find: '@components', replacement: fileURLToPath(new URL('./src/components', import.meta.url)) },
        {
          find: /^@astrojs\/starlight\/components$/,
          replacement: fileURLToPath(new URL('./node_modules/@astrojs/starlight/dist/components.ts', import.meta.url)),
        },
      ],
    },
    server: { fs: { allow: ['..'] } },
  },
  integrations: [
    starlight({
      title: 'IMS Go SaaS Kit',
      description:
        'One Go binary, your whole SaaS: a multi-tenant kit with an organization app, a staff console, a developer dashboard and the API, queue and scheduler behind them.',
      logo: { src: './src/assets/logo.svg', alt: 'IMS' },
      favicon: '/favicon.svg',
      customCss: ['./src/styles/theme.css'],
      lastUpdated: true,
      pagination: true,
      credits: false,
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
      components: {
        SiteTitle: './src/components/SiteTitle.astro',
        Hero: './src/components/Hero.astro',
        Footer: './src/components/Footer.astro',
        Search: './src/components/Search.astro',
        PageTitle: './src/components/PageTitle.astro',
        ThemeSelect: './src/components/ThemeSelect.astro',
      },
      social: [{ icon: 'github', label: 'Source code on GitHub', href: repoUrl }],
      // Pages are ../docs/*, so the base points at docsite/ and URL resolution
      // turns docsite/../docs/x.md into docs/x.md. Generated pages opt out.
      editLink: { baseUrl: `${repoUrl}/edit/main/docsite/` },
      head: [
        { tag: 'meta', attrs: { name: 'docs-version', content: version } },
        { tag: 'meta', attrs: { name: 'author', content: 'Innovix Matrix System' } },
        { tag: 'link', attrs: { rel: 'preconnect', href: 'https://fonts.googleapis.com' } },
        { tag: 'link', attrs: { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' } },
        {
          tag: 'link',
          attrs: {
            rel: 'stylesheet',
            href: 'https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,600;12..96,700;12..96,800&family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap',
          },
        },
        // Link-preview card (Slack, LinkedIn, X…), built by `npm run brand`.
        { tag: 'meta', attrs: { property: 'og:image', content: `${siteUrl}/og.png` } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
        { tag: 'meta', attrs: { property: 'og:image:alt', content: 'IMS Go SaaS Kit, open source by Innovix Matrix System' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: `${siteUrl}/og.png` } },
      ],
      sidebar,
    }),
  ],
})
