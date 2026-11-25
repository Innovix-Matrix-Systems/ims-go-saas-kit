// Builds the site's company-brand images from the source logo, so they can be
// regenerated whenever the logo changes:
//
//   node scripts/brand.mjs        (from docsite/, or `npm run brand`)
//
// Source: brand/innovix-matrix-system-logo.svg, an SVG wrapping one large PNG
// of the wordmark on a white background. Outputs (committed):
//   src/assets/brand/innovix-logo.png        wordmark for light backgrounds
//   src/assets/brand/innovix-logo-light.png  wordmark for dark backgrounds
//   src/assets/brand/innovix-mark.png        the ring "o", for light backgrounds
//   src/assets/brand/innovix-mark-light.png  the ring "o", for dark backgrounds
//   public/og.png                            1200×630 link-preview card
import { mkdirSync, readFileSync } from 'node:fs'
import sharp from 'sharp'

const root = new URL('../', import.meta.url)
const path = (p) => new URL(p, root)
const SOURCE = 'brand/innovix-matrix-system-logo.svg'
const OUT = 'src/assets/brand/'

const TEAL_LIGHT = [232, 243, 239] // replaces the dark teal on dark backgrounds

// ── 1. The embedded PNG ─────────────────────────────────────────────────────
const svg = readFileSync(path(SOURCE), 'utf8')
const match = svg.match(/data:image\/png;base64,([A-Za-z0-9+/=\s]+)"/)
if (!match) throw new Error(`${SOURCE}: no embedded PNG found`)
const png = Buffer.from(match[1].replace(/\s/g, ''), 'base64')
const { data, info } = await sharp(png).ensureAlpha().raw().toBuffer({ resolveWithObject: true })
const { width, height } = info

// ── 2. White → transparent ("colour to alpha"), keeping anti-aliased edges ──
// A pixel is the logo colour blended over white; recover that colour and how
// opaque it was. Near-white (the background and the sticker halo) disappears.
const rgba = Buffer.alloc(width * height * 4)
for (let i = 0; i < width * height; i++) {
  const [r, g, b] = [data[i * 4], data[i * 4 + 1], data[i * 4 + 2]]
  let alpha = (255 - Math.min(r, g, b)) / 255
  if (alpha < 0.12) alpha = 0 // paper texture and halo noise
  const unblend = (c) => (alpha === 0 ? 0 : Math.round(Math.max(0, Math.min(255, (c - 255 * (1 - alpha)) / alpha))))
  rgba[i * 4] = unblend(r)
  rgba[i * 4 + 1] = unblend(g)
  rgba[i * 4 + 2] = unblend(b)
  rgba[i * 4 + 3] = Math.round(alpha * 255)
}

// ── 3. Bounding boxes: the whole wordmark, and the green ring ───────────────
const box = (test) => {
  let [x0, y0, x1, y1] = [width, height, -1, -1]
  for (let y = 0; y < height; y++)
    for (let x = 0; x < width; x++) {
      const i = (y * width + x) * 4
      if (test(rgba[i], rgba[i + 1], rgba[i + 2], rgba[i + 3], x, y)) {
        if (x < x0) x0 = x
        if (y < y0) y0 = y
        if (x > x1) x1 = x
        if (y > y1) y1 = y
      }
    }
  if (x1 < 0) throw new Error('empty bounding box')
  return { x0, y0, x1, y1 }
}
const pad = (b, p) => ({
  left: Math.max(0, b.x0 - p),
  top: Math.max(0, b.y0 - p),
  width: Math.min(width, b.x1 + p + 1) - Math.max(0, b.x0 - p),
  height: Math.min(height, b.y1 + p + 1) - Math.max(0, b.y0 - p),
})

const all = box((r, g, b, a) => a > 40)
// "MATRIX SYSTEM" (thin green capitals) sits in the lower part of the logo;
// the ring is the bright green blob above it.
const wordBottom = all.y0 + Math.round((all.y1 - all.y0) * 0.72)
const ring = box((r, g, b, a, x, y) => a > 200 && y < wordBottom && g > 150 && g > r + 80)

// Dark-teal pixels (the letters and the ring's outline) for the light variant.
const isTeal = (r, g, b) => r < 60 && g < 120 && b < 120 && g + b < 220

const variant = (light) => {
  if (!light) return rgba
  const out = Buffer.from(rgba)
  for (let i = 0; i < width * height; i++) {
    if (out[i * 4 + 3] && isTeal(out[i * 4], out[i * 4 + 1], out[i * 4 + 2])) {
      ;[out[i * 4], out[i * 4 + 1], out[i * 4 + 2]] = TEAL_LIGHT
    }
  }
  return out
}

const image = (buffer, region, targetWidth) =>
  sharp(buffer, { raw: { width, height, channels: 4 } })
    .extract(region)
    .resize({ width: targetWidth })
    .png({ compressionLevel: 9, palette: true, quality: 90 })

mkdirSync(path(OUT), { recursive: true })
const logoRegion = pad(all, 16)
const markRegion = pad(ring, 6)
for (const [light, suffix] of [[false, ''], [true, '-light']]) {
  const buffer = variant(light)
  await image(buffer, logoRegion, 720).toFile(path(`${OUT}innovix-logo${suffix}.png`).pathname.slice(1))
  await image(buffer, markRegion, 96).toFile(path(`${OUT}innovix-mark${suffix}.png`).pathname.slice(1))
}

// ── 4. Link-preview card (1200×630) ─────────────────────────────────────────
const productMark = readFileSync(path('src/assets/logo.svg'))
const companyLogo = await image(variant(true), logoRegion, 300).toBuffer()
const companyMeta = await sharp(companyLogo).metadata()

const card = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630">
  <defs>
    <radialGradient id="glow" cx="85%" cy="15%" r="70%">
      <stop offset="0" stop-color="#ff5c1a" stop-opacity="0.22"/>
      <stop offset="1" stop-color="#ff5c1a" stop-opacity="0"/>
    </radialGradient>
  </defs>
  <rect width="1200" height="630" fill="#0b0b0d"/>
  <rect width="1200" height="630" fill="url(#glow)"/>
  <rect x="0" y="0" width="1200" height="6" fill="#ff5c1a"/>
  <g font-family="Segoe UI, Inter, Helvetica, Arial, sans-serif">
    <text x="80" y="250" font-size="76" font-weight="700" fill="#f5f5f7">IMS Go SaaS Kit</text>
    <text x="80" y="320" font-size="34" fill="#c2c2c8">One Go binary. Your whole SaaS.</text>
    <text x="80" y="372" font-size="26" fill="#8c8c96">Organization app · staff console · multi-tenant API · developer dashboard</text>
    <text x="80" y="560" font-size="24" fill="#8c8c96">ims-go-saas-kit.innovixmatrixsystem.com</text>
    <text x="${1120 - 300}" y="${560 - companyMeta.height - 14}" font-size="20" fill="#8c8c96">Open source, by</text>
  </g>
</svg>`

await sharp(Buffer.from(card))
  .composite([
    { input: await sharp(productMark).resize(112, 112).png().toBuffer(), left: 80, top: 70 },
    { input: companyLogo, left: 1120 - 300, top: 560 - companyMeta.height + 8 },
  ])
  .png({ compressionLevel: 9 })
  .toFile(path('public/og.png').pathname.slice(1))

console.log('brand images written:', OUT, 'public/og.png')
