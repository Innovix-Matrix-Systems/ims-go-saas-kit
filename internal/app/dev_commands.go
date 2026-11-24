package app

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/health"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/version"
)

// devCommands is the allowlist the developer dashboard's Commands panel can
// run — the same work as the matching `ims` CLI commands, in-process. Add an
// entry here to expose another maintenance task; anything that changes data
// sets Confirm, anything slow sets Background.
func devCommands(p *Platform, permissions *access.PermissionService, healthRegistry *health.Registry) []dev.Command {
	cfg := p.Config
	return []dev.Command{
		{
			Name: "about", Label: "About", Group: "Info",
			Description: "Versions, drivers and storage of this instance.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				rows := [][2]string{
					{"Application", cfg.App.Name}, {"Version", version.Version}, {"Environment", cfg.App.Env},
					{"Debug", strconv.FormatBool(cfg.App.Debug)}, {"URL", cfg.App.URL}, {"Go", runtime.Version()},
					{"Timezone", cfg.App.Timezone}, {"Cache driver", cfg.Cache.Driver}, {"Queue driver", cfg.Queue.Driver},
					{"Mail driver", cfg.Mail.Driver}, {"Files disk", cfg.Files.Disk},
					{"Tracing", tracingSummary(cfg.Tracing)},
					{"Disks", fmt.Sprint(p.Storage.Names())},
					{"Maintenance", maintenanceSummary(p.Maintenance.Checker().Current(ctx))},
				}
				for _, row := range rows {
					fmt.Fprintf(w, "%s\t%s\n", row[0], row[1])
				}
				return w.Flush()
			},
		},
		{
			Name: "migrate:status", Label: "Migration status", Group: "Database",
			Description: "Every migration and whether it has run, plus the River queue schema.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				pool := p.DB.Primary().Pool
				statuses, err := database.MigrationStatuses(ctx, pool)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "STATUS\tVERSION\tNAME")
				pending := 0
				for _, status := range statuses {
					state := "applied"
					if !status.Applied {
						state, pending = "pending", pending+1
					}
					fmt.Fprintf(w, "%s\t%d\t%s\n", state, status.Version, status.Name)
				}
				if err := w.Flush(); err != nil {
					return err
				}
				fmt.Fprintf(out, "\n%d migration(s), %d pending.\n", len(statuses), pending)
				if riverVersion, exists, err := database.RiverSchemaVersion(ctx, pool); err == nil && exists {
					fmt.Fprintf(out, "River schema: v%d\n", riverVersion)
				}
				return nil
			},
		},
		{
			Name: "health:check", Label: "Run health checks", Group: "Info",
			Description: "Run every health check now (database, cache, queue, scheduler, backups…).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				results := healthRegistry.Run(ctx)
				for _, result := range results.CheckResults {
					fmt.Fprintf(out, "%-8s %s: %s\n", result.Status, result.Name, result.ShortSummary)
				}
				if results.HasFailures() {
					return fmt.Errorf("one or more health checks failed")
				}
				return nil
			},
		},
		{
			Name: "permission:sync", Label: "Sync permissions", Group: "Sync", Confirm: true,
			Description: "Write the permission catalog (internal/authz/catalog.go) to the database.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				synced, err := permissions.SyncCatalog(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Synced %d permission(s).\n", synced)
				return nil
			},
		},
		{
			Name: "settings:sync", Label: "Sync settings", Group: "Sync",
			Description: "Add settings groups and global settings missing from the database (existing values are kept).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				created, err := p.Settings.Sync(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Created %d setting(s).\n", created)
				return nil
			},
		},
		{
			Name: "feature-flags:sync", Label: "Sync feature flags", Group: "Sync",
			Description: "Insert missing global feature flags with their catalog defaults.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				created, err := p.FeatureFlags.Sync(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Created %d feature flag(s).\n", created)
				return nil
			},
		},
		{
			Name: "down", Label: "Maintenance mode on", Group: "Maintenance", Confirm: true,
			Description: "Take the API down for maintenance: clients get 503 MAINTENANCE_MODE; the dashboard, health checks and docs keep working.",
			Options: []dev.CommandOption{
				{Name: "message", Label: "Message", Type: "text", Default: maintenance.DefaultMessage},
				{Name: "retry", Label: "Retry after (seconds)", Type: "number", Default: strconv.Itoa(maintenance.DefaultRetryAfter)},
				{Name: "with_secret", Label: "Generate bypass secret", Type: "bool", Default: "false",
					Description: "Print a secret that lets a client through with the X-Maintenance-Bypass header."},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				retry, _ := strconv.Atoi(options["retry"])
				state, secret, err := p.Maintenance.Down(ctx, maintenance.DownInput{
					Message: options["message"], RetryAfter: retry,
					GenerateSecret: options["with_secret"] == "true", ByUserID: authn.UserIDFromContext(ctx),
				})
				if err != nil {
					return err
				}
				WriteMaintenanceDown(out, state, secret)
				return nil
			},
		},
		{
			Name: "up", Label: "Maintenance mode off", Group: "Maintenance", Confirm: true,
			Description: "Bring the API back from maintenance mode.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				changed, err := p.Maintenance.Up(ctx, authn.UserIDFromContext(ctx))
				if err != nil {
					return err
				}
				WriteMaintenanceUp(out, changed)
				return nil
			},
		},
		{
			Name: "maintenance:status", Label: "Maintenance status", Group: "Maintenance",
			Description: "Whether the API is down for maintenance, since when and with which message.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				state, err := p.Maintenance.Status(ctx)
				if err != nil {
					return err
				}
				WriteMaintenanceStatus(out, state)
				return nil
			},
		},
		{
			Name: "cache:clear", Label: "Clear cache", Group: "Maintenance", Confirm: true,
			Description: "Empty the application cache (permissions, settings, flags and tenancy lookups are rebuilt on demand).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				if err := p.Cache.Clear(ctx); err != nil {
					return err
				}
				fmt.Fprintf(out, "Cache store %q cleared.\n", cfg.Cache.Driver)
				return nil
			},
		},
		{
			Name: "schedule:list", Label: "Scheduled tasks", Group: "Info",
			Description: "Every task the scheduler runs, with its cron expression.",
			Run: func(_ context.Context, _ map[string]string, out io.Writer) error {
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "TASK\tSCHEDULE\tDESCRIPTION")
				for _, task := range p.ScheduleRegistry.Tasks() {
					fmt.Fprintf(w, "%s\t%s\t%s\n", task.ID, task.Spec, task.Description)
				}
				return w.Flush()
			},
		},
		{
			Name: "queue:stats", Label: "Queue stats", Group: "Info",
			Description: "Jobs per queue: total, active and failed.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				stats, err := queue.Stats(ctx, p.DB.Primary().Pool)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "QUEUE\tTOTAL\tACTIVE\tFAILED")
				if len(stats) == 0 {
					fmt.Fprintln(w, "(none)\t0\t0\t0")
				}
				for _, stat := range stats {
					fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", stat.Queue, stat.Total, stat.Active, stat.Failed)
				}
				return w.Flush()
			},
		},
		{
			Name: "billing:expire-subscriptions", Label: "Expire subscriptions", Group: "Billing", Confirm: true,
			Description: "Mark subscriptions past their end date as expired (the scheduler does this daily).",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				expired, err := p.BillingSubs.ExpireOverdue(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Expired %d subscription(s).\n", expired)
				return nil
			},
		},
		{
			Name: "backup:run", Label: "Back up the database", Group: "Backups", Confirm: true, Background: true,
			Description: "Create a database backup archive and copy it to the configured destinations.",
			Options: []dev.CommandOption{
				{Name: "with_files", Label: "Include files", Type: "bool", Default: "false",
					Description: "Also archive BACKUP_FILES_PATHS."},
			},
			Timeout: cfg.Backup.Timeout,
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				withFiles := options["with_files"] == "true"
				run, err := p.Backup.RunNow(ctx, backup.TriggerDashboard, 0, backup.RunOptions{IncludeFiles: &withFiles})
				if run != nil {
					fmt.Fprintf(out, "Backup #%d %s\nFile: %s\nSize: %d bytes\n", run.ID, run.Status, run.Filename, run.SizeBytes)
					for _, destination := range run.Destinations {
						fmt.Fprintf(out, "  %s: %s\n", destination.Destination, destination.Status)
					}
				}
				return err
			},
		},
		{
			Name: "backup:clean", Label: "Clean old backups", Group: "Backups", Confirm: true,
			Description: "Apply the backup retention policy and delete expired archives.",
			Options: []dev.CommandOption{
				{Name: "dry_run", Label: "Dry run", Type: "bool", Default: "true", Description: "Only list what would be deleted."},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				result, err := p.Backup.Clean(ctx, options["dry_run"] == "true")
				if err != nil {
					return err
				}
				if result.DryRun {
					fmt.Fprintf(out, "Dry run: %d archive(s) would be deleted %v. Nothing was removed.\n", result.Candidates, result.IDs)
					return nil
				}
				fmt.Fprintf(out, "Deleted %d of %d archive(s); %d failed.\n", result.Deleted, result.Candidates, result.Failed)
				return nil
			},
		},
		{
			Name: "privacy:coverage", Label: "Privacy coverage", Group: "Info",
			Description: "Which module erases each column that may hold a user id; fails when one is not covered.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				coverage, err := p.Privacy.Coverage(ctx)
				if err != nil {
					return err
				}
				missing, err := privacy.WriteCoverage(out, coverage)
				if err != nil {
					return err
				}
				if missing > 0 {
					return privacy.CoverageError(missing)
				}
				return nil
			},
		},
		{
			Name: "activity:prune", Label: "Prune activity log", Group: "Maintenance", Confirm: true,
			Description: "Delete activity-log entries older than the given number of days.",
			Options: []dev.CommandOption{
				{Name: "days", Label: "Older than (days)", Type: "number", Default: strconv.Itoa(max(cfg.Activity.RetentionDays, 30))},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				days, _ := strconv.Atoi(options["days"])
				if days < 1 {
					return fmt.Errorf("days must be at least 1")
				}
				deleted, err := activity.NewService(p.Tx.Queries()).Purge(ctx, days)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Deleted %d activity entr(ies) older than %d day(s).\n", deleted, days)
				return nil
			},
		},
		{
			Name: "delivery:test", Label: "Send a test message", Group: "Delivery", Confirm: true,
			Description: "Send one message now through the configured mail, SMS or push driver.",
			Options: []dev.CommandOption{
				{Name: "to", Label: "To (e-mail, phone or user id)", Type: "text"},
				{Name: "channel", Label: "Channel", Type: "select", Default: string(delivery.Mail),
					Choices: []string{string(delivery.Mail), string(delivery.SMS), string(delivery.Push)}},
				{Name: "template", Label: "Template (empty: a plain test message)", Type: "text"},
				{Name: "locale", Label: "Locale", Type: "text"},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				to, channel := strings.TrimSpace(options["to"]), delivery.Channel(options["channel"])
				if to == "" {
					return fmt.Errorf("to is required")
				}
				message, err := DeliveryTestMessage(p.Delivery, channel, to, strings.TrimSpace(options["template"]), strings.TrimSpace(options["locale"]))
				if err != nil {
					return err
				}
				if err := p.Delivery.SendNow(ctx, message); err != nil {
					return err
				}
				fmt.Fprintf(out, "Sent a test %s to %s through the %s driver.\n", channel, to, p.Delivery.Driver(channel))
				return nil
			},
		},
		{
			Name: "delivery:prune", Label: "Prune delivery log", Group: "Delivery", Confirm: true,
			Description: "Delete delivery-log rows older than the given number of days.",
			Options: []dev.CommandOption{
				{Name: "days", Label: "Older than (days)", Type: "number", Default: strconv.Itoa(max(cfg.Delivery.RetentionDays, 1))},
			},
			Run: func(ctx context.Context, options map[string]string, out io.Writer) error {
				days, _ := strconv.Atoi(options["days"])
				if days < 1 {
					return fmt.Errorf("days must be at least 1")
				}
				deleted, err := p.Delivery.Prune(ctx, days)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Deleted %d delivery row(s) older than %d day(s).\n", deleted, days)
				return nil
			},
		},
		{
			Name: "notifications:purge-expired", Label: "Purge expired notifications", Group: "Maintenance", Confirm: true,
			Description: "Delete notifications that expired more than 7 days ago.",
			Run: func(ctx context.Context, _ map[string]string, out io.Writer) error {
				purged, err := p.Tx.Queries().PurgeExpiredNotifications(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Purged %d notification(s).\n", purged)
				return nil
			},
		},
	}
}

// tracingSummary is the About line for OpenTelemetry tracing.
func tracingSummary(tracing config.Tracing) string {
	switch {
	case !tracing.Enabled:
		return "off"
	case tracing.Endpoint == "":
		return "on (OTLP default endpoint)"
	default:
		return "on (" + tracing.Endpoint + ")"
	}
}
