# Documentation

These Markdown files are the kit's documentation. They read here on GitHub, and `docsite/`
publishes them as a site at
**[ims-go-saas-kit.innovixmatrixsystem.com](https://ims-go-saas-kit.innovixmatrixsystem.com)**.
Pages ending in `.mdx` use the site's components and read best there. Preview the site locally
with `make docs`.

| Section | Pages |
| --- | --- |
| **Start here** | [Introduction](getting-started/introduction.md) · [Quick start](getting-started/quick-start.mdx) · [Project tour](getting-started/project-tour.mdx) · [How it works](getting-started/how-it-works.mdx) · [Coming from Laravel](getting-started/coming-from-laravel.md) · [Make it yours](getting-started/make-it-yours.md) |
| **Tutorial** | [Your first feature](tutorial/first-module.mdx) · [Connect an AI agent](tutorial/connect-an-ai-agent.mdx) · [Sign in with GitHub locally](tutorial/sign-in-with-github.mdx) |
| **The app** | [Overview](app/overview.md) · [Organization workspace](app/organization-workspace.md) · [Staff console](app/staff-console.md) · [Adding a screen](app/adding-a-screen.md) · [Translations and theming](app/translations-and-theming.md) |
| **The backend** | [Modules](guides/modules.md) · [Handlers & routes](guides/handlers-and-routes.md) · [Validation & errors](guides/validation-and-errors.md) · [Database](guides/database.md) · [Testing](guides/testing.md) · [CLI commands](guides/cli-commands.md) |
| **Features** | Accounts: [Authentication](guides/authentication.md) · [Sign-up](guides/sign-up.md) · [Social login](guides/social-login.md) · [OAuth in this kit](guides/oauth.md)<br>Organizations: [Tenancy](guides/tenancy.md) · [Permissions](guides/permissions.md) · [Billing](guides/billing.md)<br>Product: [Settings](guides/settings.md) · [Feature flags](guides/feature-flags.md) · [Notifications](guides/notifications.md) · [Languages](guides/languages.md) · [Money and currency](guides/currencies.md)<br>Data: [Files & storage](guides/files-and-storage.md) · [Import & export](guides/import-export.md) · [Activity log](guides/activity-log.md) · [Account deletion & export](guides/privacy.md)<br>Integrations: [Webhooks](guides/webhooks.md) · [Live updates](guides/realtime.md) · [MCP server](guides/mcp.md)<br>Runtime: [Queue & scheduler](guides/queue-and-scheduler.md) · [Maintenance mode](guides/maintenance-mode.md) · [Delivery](guides/delivery.md) · [Health & observability](guides/health-and-observability.md) |
| **Developer dashboard** | [Tour](dashboard/overview.md) · [Extending](dashboard/extending.md) |
| **Operations** | [Local development](operations/local-development.md) · [Docker](operations/docker.md) · [Deploy](operations/deploy.md) · [Migrations](operations/migrations.md) · [Releases](operations/releases.md) · [Backups](operations/backups.md) · [Rollback](operations/rollback.md) |
| **Reference** | [HTTP API](reference/http-api.md) · [CLI](reference/cli.md) · [Configuration](reference/configuration.md) · [Make targets](reference/make.md) · [Permissions](reference/permissions.md) |
| **Architecture & project** | [Overview](architecture/overview.md) · [Design decisions](architecture/decisions.md) · [Working with AI agents](project/ai-agents.md) · [Contributing](project/contributing.md) · [Changelog](../CHANGELOG.md) |

The reference pages for the CLI, configuration, permissions and Make targets are **generated**
from the code (`make docs-gen`); don't edit them by hand. A new page must also be added to the
site's sidebar in `docsite/astro.config.mjs`: the site build fails otherwise.

`history/` keeps past reviews for context; it is not current documentation. `design/` holds design
records (currently [account deletion and data export](design/account-deletion-and-export.md)).
Neither is published on the site.
