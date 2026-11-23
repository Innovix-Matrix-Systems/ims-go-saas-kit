---
title: Releases and versioning
description: The VERSION file, semantic versioning, the changelog, cutting a release and publishing these docs.
---

## One version, one file

The `VERSION` file at the repository root holds the current release — `0.2.1` — as plain
[semver](https://semver.org) (no leading `v`). Everything reads it:

| Where | Shows |
| --- | --- |
| `make build` | stamps it into the binary: `ims version` → `ims 0.2.1` |
| `go run`, air, tests | the same number with a suffix: `ims 0.2.1-dev` — a local build is never mistaken for a release |
| `/api/v1/system/info`, `/api/health`, the dashboard | the running version |
| this site | the badge next to the title |

`internal/version/version_test.go` fails if `VERSION` isn't valid semver.

## What the numbers mean

While the version starts with **0.**, the API is young: a **minor** bump (`0.1.0 → 0.2.0`) may
contain breaking changes, a **patch** bump (`0.1.0 → 0.1.1`) never does. From **1.0.0** on, the
usual rules apply:

| Bump | When |
| --- | --- |
| **major** | breaking changes to the HTTP API, config or CLI |
| **minor** | new features, new endpoints, new settings — backwards compatible |
| **patch** | fixes only |

## Cutting a release

1. Make sure `main` is green (CI runs format, lint, tests, generated-code and contract checks).
2. Update **`VERSION`** and move the "Unreleased" notes in **`CHANGELOG.md`** under the new number
   with today's date.
3. Regenerate anything version-dependent and commit:

   ```bash
   make docs-gen gen-check
   git commit -am "release: v0.2.0"
   ```

4. Tag and push:

   ```bash
   git tag -a v0.2.0 -m "v0.2.0"
   git push origin main --tags
   ```

5. On GitHub, **draft a release** for the tag with the changelog section as notes, and publish it.
6. Deploy the binary with the [deploy runbook](./deploy.md) (`release.sh` builds with the tag's
   version and runs migrations as a gated step).

## Publishing the documentation

The docs site is a fully static [Astro Starlight](https://starlight.astro.build) build in
`website/`; its pages are this repository's `docs/` folder. Cloudflare only serves the built files —
nothing of the Go application runs there.

**Automatically:** publishing a GitHub Release runs `.github/workflows/docs-deploy.yml`, which
builds the site and uploads it to Cloudflare Workers (static assets). You can also run the
workflow by hand from the **Actions** tab, e.g. after a docs-only fix.

**One-time setup:**

1. In Cloudflare: **My Profile → API Tokens → Create Token**, template **Edit Cloudflare
   Workers**.
2. In GitHub: **Settings → Secrets and variables → Actions**, add `CLOUDFLARE_API_TOKEN` and
   `CLOUDFLARE_ACCOUNT_ID` (shown on your Cloudflare Workers overview page).
3. **Workers & Pages → Your subdomain** must be registered once per Cloudflare account, or the
   first deploy fails with "You need to register a workers.dev subdomain".

The first deploy creates the `ims-go-admin-starter` Worker. The site is published at
**[ims-go-api-starter.innovixmatrixsystem.com](https://ims-go-api-starter.innovixmatrixsystem.com)**, a **Custom Domain** attached to the Worker in the Cloudflare
dashboard (**Worker → Settings → Domains & Routes → Add → Custom domain**). Cloudflare creates the DNS
record and certificate. The domain is not in `wrangler.jsonc`, so the deploy token needs no zone
permissions, and later deploys keep it.

The site's own address (canonical links, sitemap) defaults to that domain in
`website/astro.config.mjs`. If you fork the starter and publish somewhere else, set a repository
**variable** `DOCS_SITE_URL` to your address.

**By hand**, from your machine:

```bash
cd website
npm ci
npx wrangler login      # once
npm run deploy          # builds, checks every link, uploads dist/
```

### Writing docs locally

```bash
make docs               # live preview at http://localhost:4321 (Node 22.23+)
make docs-build         # production build + link check
make docs-gen           # regenerate the reference pages (CLI, config, permissions, make)
```
