// Checks the dashboard's design tokens and stylesheet rules:
//
//   - every text/surface pair, the accent, and every status colour on its
//     subtle background pass WCAG 2.2 AA (4.5:1) in both themes and with
//     every accent preset; the accent passes 3:1 as a focus indicator;
//   - every chart series colour stands out from the card (2:1; the figures
//     are always in text too, so colour never carries them alone);
//   - styles use the six type-scale tokens only (none below 11 px);
//   - uppercase only on table headers and sidebar section titles;
//   - no raw colour outside tokens.css (a declaration that must stay fixed,
//     such as the white behind an e-mail preview, ends in a "fixed:" comment).
//
// Run with `npm run check:contrast`; `make gen-check` runs it too.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const stylesDir = path.resolve(here, '../src/styles')
const tokensFile = path.join(stylesDir, 'tokens.css')
const failures = []

// ── Tokens ──────────────────────────────────────────────

const tokens = stripComments(readFileSync(tokensFile, 'utf8'))
const blocks = [...tokens.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({
  selector: m[1].trim(),
  vars: Object.fromEntries([...m[2].matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)].map((d) => [d[1], d[2].trim()])),
}))
const accents = [...new Set(blocks.flatMap((b) => [...b.selector.matchAll(/data-accent="(\w+)"/g)].map((m) => m[1])))]

function themeVars(theme, accent) {
  const vars = {}
  for (const block of blocks) {
    const applies = block.selector.split(',').some((part) => {
      const s = part.trim()
      return s === ':root' || s === `[data-accent="${accent}"]` || (theme === 'dark' && s === ':root[data-theme="dark"]')
    })
    if (applies) Object.assign(vars, block.vars)
  }
  return vars
}

function resolve(value, vars, depth = 0) {
  if (depth > 20) throw new Error(`token cycle at ${value}`)
  return value.replace(/var\((--[\w-]+)\)/g, (_, name) => {
    if (!(name in vars)) throw new Error(`unknown token ${name}`)
    return resolve(vars[name], vars, depth + 1)
  })
}

function color(value, vars) {
  const v = resolve(value, vars).trim()
  const mix = v.match(/^color-mix\(in srgb,\s*(.+?)\s+([\d.]+)%,\s*(.+)\)$/)
  if (mix) {
    const a = color(mix[1], vars)
    const b = color(mix[3], vars)
    const p = Number(mix[2]) / 100
    return a.map((c, i) => c * p + b[i] * (1 - p))
  }
  const hex = v.match(/^#([0-9a-f]{6})$/i)
  if (hex) return [0, 2, 4].map((i) => parseInt(hex[1].slice(i, i + 2), 16))
  throw new Error(`cannot read colour ${value} (${v})`)
}

function luminance(rgb) {
  const [r, g, b] = rgb.map((c) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function ratio(fg, bg) {
  const [a, b] = [luminance(fg), luminance(bg)].sort((x, y) => y - x)
  return (a + 0.05) / (b + 0.05)
}

const surfaces = ['--surface-0', '--surface-1', '--surface-2', '--surface-inset']
const pairs = [
  ...['--text-1', '--text-2', '--text-3'].flatMap((fg) => surfaces.map((bg) => [fg, bg, 4.5])),
  ['--accent', '--surface-0', 4.5],
  ['--accent', '--surface-1', 4.5],
  ['--accent', '--accent-subtle', 4.5],
  ['--accent-fg', '--accent', 4.5],
  ['--accent', '--surface-inset', 3],
  ...['ok', 'warn', 'danger', 'info', 'violet'].flatMap((tone) => [
    [`--${tone}-fg`, `--${tone}-subtle`, 4.5],
    [`--${tone}-fg`, '--surface-1', 4.5],
  ]),
  ...[1, 2, 3, 4, 5, 6].map((n) => [`--series-${n}`, '--surface-1', 2]),
]

let checked = 0
for (const theme of ['light', 'dark']) {
  for (const accent of accents) {
    const vars = themeVars(theme, accent)
    for (const [fg, bg, min] of pairs) {
      if (accent !== 'indigo' && !fg.startsWith('--accent') && !bg.startsWith('--accent')) continue
      const r = ratio(color(`var(${fg})`, vars), color(`var(${bg})`, vars))
      checked++
      if (r < min) failures.push(`${theme}/${accent}: ${fg} on ${bg} is ${r.toFixed(2)}:1, needs ${min}:1`)
    }
  }
}

// ── Stylesheet rules ────────────────────────────────────

const typeScale = new Set(Object.keys(blocks.find((b) => b.selector === ':root').vars).filter((n) => n.startsWith('--fs-') && n !== '--fs-body'))
for (const name of typeScale) {
  const px = Number(resolve(`var(${name})`, themeVars('light', 'indigo')).replace('px', ''))
  if (!(px >= 11)) failures.push(`tokens.css: ${name} is below 11px`)
}
if (typeScale.size > 6) failures.push(`tokens.css: ${typeScale.size} font sizes, at most 6`)

const uppercaseAllowed = /(^|[\s,>])th\b|\.nav-section|cmdk-group-heading/

for (const file of cssFiles(stylesDir)) {
  const name = path.relative(stylesDir, file)
  // Drop declarations marked "/* fixed: why */" before checking colours.
  const css = readFileSync(file, 'utf8').replace(/[^;{}]*;\s*\/\*\s*fixed:[^*]*\*\//g, ';')
  for (const rule of stripComments(css).matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const selector = rule[1].trim()
    const body = rule[2]
    for (const size of body.matchAll(/font-size\s*:\s*([^;]+)/g)) {
      const value = size[1].trim()
      const token = value.match(/^var\((--fs-[\w-]+)\)$/)
      if (value !== 'inherit' && !(token && (typeScale.has(token[1]) || token[1] === '--fs-body'))) {
        failures.push(`${name}: ${selector} uses font-size ${value}; use a --fs-* token`)
      }
    }
    if (/text-transform\s*:\s*uppercase/.test(body) && !uppercaseAllowed.test(selector)) {
      failures.push(`${name}: ${selector} is uppercase; only table headers and sidebar section titles are`)
    }
    if (file !== tokensFile && /#[0-9a-f]{3,8}\b|rgba?\(/i.test(body)) {
      failures.push(`${name}: ${selector} uses a raw colour; use a token`)
    }
  }
}

if (failures.length > 0) {
  console.error(`Dashboard style check failed (${failures.length}):`)
  for (const failure of failures) console.error(`  - ${failure}`)
  process.exit(1)
}
console.log(`Dashboard style check passed: ${checked} contrast pairs, ${accents.length} accents, both themes.`)

function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

function cssFiles(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry)
    if (statSync(full).isDirectory()) return cssFiles(full)
    return entry.endsWith('.css') ? [full] : []
  })
}
