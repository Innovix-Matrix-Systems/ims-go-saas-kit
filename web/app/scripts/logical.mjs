// Fails when the app's stylesheets use physical left/right properties: layout must use
// logical ones (margin-inline-start, padding-inline-end, inset-inline-start, border-inline-end,
// text-align: start/end) so dir="rtl" mirrors it. A rule that must stay physical says why on the
// same line: /* physical: <reason> */.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, dirname, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const physical = [
  [/(margin|padding|border)-(left|right)\b/, 'use *-inline-start / *-inline-end'],
  [/(?<![-\w])(left|right)\s*:/, 'use inset-inline-start / inset-inline-end'],
  [/text-align\s*:\s*(left|right)/, 'use text-align: start / end'],
  [/float\s*:\s*(left|right)/, 'use float: inline-start / inline-end'],
  [/border-(top|bottom)-(left|right)-radius/, 'use border-start-start-radius and friends'],
]

const files = []
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path)
    else if (name.endsWith('.css')) files.push(path)
  }
}
walk(join(root, 'src'))

const problems = []
for (const file of files) {
  readFileSync(file, 'utf8')
    .split('\n')
    .forEach((line, index) => {
      if (line.includes('physical:')) return
      const code = line.replace(/\/\*.*?\*\//g, '')
      for (const [pattern, hint] of physical) {
        if (pattern.test(code)) problems.push(`${relative(root, file)}:${index + 1}: ${line.trim()}  (${hint})`)
      }
    })
}
if (problems.length) {
  console.error(problems.join('\n'))
  console.error(`\n${problems.length} physical left/right rule(s); right-to-left languages would not mirror them.`)
  process.exit(1)
}
console.log(`Logical properties check passed: ${files.length} stylesheets.`)
