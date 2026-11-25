---
title: Releases and versioning
description: The VERSION file, semantic versioning, the changelog, cutting a release and the reference docs.
---

## One version, one file

The `VERSION` file at the repository root holds the current release — `0.2.0` — as plain
[semver](https://semver.org) (no leading `v`). Everything reads it:

| Where | Shows |
| --- | --- |
| `make build` | stamps it into the binary: `ims version` → `ims 0.2.0` |
| `go run`, air, tests | the same number with a suffix: `ims 0.2.0-dev` — a local build is never mistaken for a release |
| `/api/v1/system/info`, `/api/health`, the dashboard | the running version |
| the README | the version badge |

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
   git commit -am "release: v0.3.0"
   ```

4. Tag and push:

   ```bash
   git tag -a v0.3.0 -m "v0.3.0"
   git push origin main --tags
   ```

5. On GitHub, **draft a release** for the tag with the changelog section as notes, and publish it.
   Publishing it also deploys the documentation site (below).
6. Deploy the binary with the [deploy runbook](./deploy.md) (`release.sh` builds the app and the
   dashboard, embeds them, stamps the tag's version and runs migrations as a gated step).

## Documentation

The pages live in `docs/` as Markdown, so they read on GitHub too. `docsite/` is the site that
publishes them (Astro Starlight): the theme, the landing page and the sidebar.

```bash
make docs-gen           # regenerate the reference pages (CLI, config, permissions, make)
make docs               # preview the site at http://localhost:4321 with hot reload
make docs-build         # build it and check every link and anchor
```

Run `make docs-gen` after a change to the CLI, configuration, permissions or Make targets; CI
fails when the reference pages are stale. The site build fails when a page in `docs/` is missing
from the sidebar (`docsite/astro.config.mjs`), or when a link or `#anchor` points nowhere. The
site needs Node 22.23 or newer.

### Publishing the docs site

`.github/workflows/docs-deploy.yml` builds the site and deploys it to Cloudflare Workers as static
files when a release is published, or by hand from the Actions tab (for a docs-only fix). One-time
setup:

1. In Cloudflare, create an API token from the **Edit Cloudflare Workers** template, and copy
   your account ID.
2. In the GitHub repository, **Settings → Secrets and variables → Actions**: add the secrets
   `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`, and the variable `DOCS_SITE_URL` (the
   public address).
3. **Settings → Environments**: create an environment named `docs`.
4. After the first deploy, attach your domain to the Worker in Cloudflare (**Worker → Settings →
   Domains & Routes → Custom domain**). The Worker's name is in `docsite/wrangler.jsonc`.
