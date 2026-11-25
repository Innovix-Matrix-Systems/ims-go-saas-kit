---
title: Make it yours
description: Turn the kit into your product — your repository, module path, binary and names in one command, then your brand in the app and your own docs site.
---

<!-- rename:keep -->
The kit is meant to become your product, not a dependency you track. Three steps make it yours:
your own repository, your names in the code, your brand in the app.

## 1. Your own repository

On GitHub, press **[Use this template](https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/generate)
→ Create a new repository**. You get a repository with a clean history that doesn't point back
here, unlike a fork. Clone it and continue with the [Quick start](./quick-start.mdx).

## 2. Your names: `make rename`

One command rewrites the kit's names in every tracked file:

```bash
make rename MODULE=github.com/acme/shop DRY=1                         # preview, changes nothing
make rename MODULE=github.com/acme/shop BINARY=shop TITLE="Acme Shop"
git add -A && git commit -m "chore: rename the kit to shop"
```

| Option | Default | Example |
| --- | --- | --- |
| `MODULE` (required) | — | `github.com/acme/shop` |
| `NAME`: project and deploy name | the last part of the module | `shop` |
| `BINARY`: binary, CLI and service user | the name | `shop` |
| `TITLE`: the display name | the name in title case | `Acme Shop` |

**What changes:**

- the Go module path in `go.mod` and every import;
- the `ims` binary and CLI (`ims migrate` becomes `shop migrate`), and the service user;
- the deploy names: systemd units (`ims-go-saas-kit.service` becomes `shop.service`), install
  paths (`/opt/ims/ims-go-saas-kit`), the Docker image and compose project;
- the display title "IMS Go SaaS Kit" in the API spec, the app, the dashboard, e-mails and these
  docs;
- the `IMS_` deploy variables (`IMS_BASE_DIR` becomes `SHOP_BASE_DIR`);
- the docs site's Worker name.

Then it runs `go mod tidy`, `make generate` and `make docs-gen`, because the generated OpenAPI
contract embeds the spec's title.

**What stays:** the seeded `@ims.com` accounts (change them in `.env`), credit links to Innovix
Matrix System, and any text between `rename:keep` and `rename:end` markers, like this page.

It refuses to run with uncommitted changes, so the rename is one commit you can review, and it
runs once: a project that has already been renamed is refused.

## 3. Your brand in the app

The app takes its look from design tokens. To make it yours:

1. Set the accent presets and default in `web/app/src/styles/tokens.css` and
   `web/app/src/components/appearance.ts`.
2. Replace the app icon, `web/app/public/favicon.svg`.
3. Set `'app.name'` in every language catalog (`web/app/src/i18n/`), if the title from
   `make rename` isn't the name you want people to see.
4. Run `npm --prefix web/app run check:style`: it fails if a colour you chose doesn't meet WCAG
   AA contrast in either theme.

The details are in [Translations and theming](../app/translations-and-theming.md).

## 4. Your docs

Keep `docs/`: it documents your codebase, and `make docs-gen` keeps its reference pages in step
with your code. To publish it as a site, set `DOCS_SITE_URL` and follow
[Releases](../operations/releases.md#publishing-the-docs-site). Not publishing one? Delete
`docsite/`, `.github/workflows/docs-deploy.yml` and the `docs` job in `.github/workflows/ci.yml`;
nothing else depends on them.
<!-- rename:end -->
