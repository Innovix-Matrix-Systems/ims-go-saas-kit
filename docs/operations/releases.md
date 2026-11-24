---
title: Releases and versioning
description: The VERSION file, semantic versioning, the changelog, cutting a release and the reference docs.
---

## One version, one file

The `VERSION` file at the repository root holds the current release — `0.1.6` — as plain
[semver](https://semver.org) (no leading `v`). Everything reads it:

| Where | Shows |
| --- | --- |
| `make build` | stamps it into the binary: `ims version` → `ims 0.1.6` |
| `go run`, air, tests | the same number with a suffix: `ims 0.1.6-dev` — a local build is never mistaken for a release |
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

## Documentation

This repository publishes no documentation site: `docs/` reads on GitHub, and the backend's guides
are also on the API starter's site ([ims-go-api-starter.innovixmatrixsystem.com](https://ims-go-api-starter.innovixmatrixsystem.com)). After a change to the
CLI, configuration, permissions or Make targets, regenerate the reference pages:

```bash
make docs-gen           # regenerate the reference pages (CLI, config, permissions, make)
```
