import { defineCollection } from 'astro:content'
import { glob } from 'astro/loaders'
import { docsSchema } from '@astrojs/starlight/schema'

// Pages come from the repository's docs/ folder. README.md is the GitHub
// index of that folder, history/ keeps old reviews and design/ holds proposals
// for unbuilt work; none of them is a page.
export const collections = {
  docs: defineCollection({
    loader: glob({
      base: '../docs',
      pattern: ['**/*.{md,mdx}', '!README.md', '!history/**', '!design/**'],
    }),
    schema: docsSchema(),
  }),
}
