# Documentation

These Markdown files are the project's documentation, read here on GitHub. The backend is the
[IMS Go API Starter](https://github.com/Innovix-Matrix-Systems/ims-go-api-starter)'s, whose site at
**[ims-go-api-starter.innovixmatrixsystem.com](https://ims-go-api-starter.innovixmatrixsystem.com)** publishes the same guides. Pages ending in
`.mdx` use the site's components and read best there.

| Section | Pages |
| --- | --- |
| **Start here** | [Introduction](getting-started/introduction.md) · [Quick start](getting-started/quick-start.mdx) · [How it works](getting-started/how-it-works.mdx) · [Coming from Laravel](getting-started/coming-from-laravel.md) · [Project tour](getting-started/project-tour.mdx) |
| **Tutorial** | [Build your first module](tutorial/first-module.mdx) · [Connect an AI agent](tutorial/connect-an-ai-agent.mdx) · [Sign in with GitHub locally](tutorial/sign-in-with-github.mdx) |
| **Building the API** | [Modules](guides/modules.md) · [Handlers & routes](guides/handlers-and-routes.md) · [Validation & errors](guides/validation-and-errors.md) · [Database](guides/database.md) · [Tenancy](guides/tenancy.md) · [Authentication](guides/authentication.md) · [OAuth in this starter](guides/oauth.md) · [Permissions](guides/permissions.md) · [Testing](guides/testing.md) · [CLI commands](guides/cli-commands.md) |
| **Features** | [Billing](guides/billing.md) · [Settings](guides/settings.md) · [Feature flags](guides/feature-flags.md) · [Queue & scheduler](guides/queue-and-scheduler.md) · [Files & storage](guides/files-and-storage.md) · [Activity log](guides/activity-log.md) · [Social login](guides/social-login.md) · [Notifications](guides/notifications.md) · [Webhooks](guides/webhooks.md) · [Import & export](guides/import-export.md) · [Account deletion & export](guides/privacy.md) · [Delivery](guides/delivery.md) · [Mail & i18n](guides/mail-and-i18n.md) · [Health & observability](guides/health-and-observability.md) · [Maintenance mode](guides/maintenance-mode.md) · [Live updates (SSE)](guides/realtime.md) |
| **Developer dashboard** | [Tour](dashboard/overview.md) · [Extending](dashboard/extending.md) |
| **Reference** | [HTTP API](reference/http-api.md) · [CLI](reference/cli.md) · [Configuration](reference/configuration.md) · [Permissions](reference/permissions.md) · [Make targets](reference/make.md) |
| **Operations** | [Local development](operations/local-development.md) · [Deploy](operations/deploy.md) · [Docker](operations/docker.md) · [Migrations](operations/migrations.md) · [Backups](operations/backups.md) · [Rollback](operations/rollback.md) · [Releases](operations/releases.md) |
| **Architecture** | [Overview](architecture/overview.md) · [Design decisions](architecture/decisions.md) |
| **Project** | [Contributing](project/contributing.md) · [Working with AI agents](project/ai-agents.md) · [Changelog](../CHANGELOG.md) |

The reference pages for the CLI, configuration, permissions and Make targets are **generated**
from the code (`make docs-gen`); don't edit them by hand. `history/` keeps past reviews for
context — it is not current documentation. `design/` holds design records
(currently [account deletion and data export](design/account-deletion-and-export.md)); like
`history/`, it is not published on the site.
