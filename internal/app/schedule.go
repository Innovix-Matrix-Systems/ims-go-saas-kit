package app

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// schedule lists the recurring tasks run by `ims schedule` (Laravel's
// Kernel::schedule). Overlapping runs are skipped by default; tasks that
// depend on a feature are only listed when it is enabled.
func (p *Platform) schedule() []scheduler.Task {
	cfg, logger, queries := p.Config, p.Logger, p.Tx.Queries()

	tasks := []scheduler.Task{
		{
			ID:          "scheduler.heartbeat",
			Spec:        "* * * * *",
			Description: "Records scheduler liveness for health checks.",
			// Health stays green while the app is down for maintenance.
			EvenInMaintenance: true,
			// A beat a minute would bury the real tasks in the run history; the
			// heartbeat time itself is on the Scheduler panel.
			NoHistory: true,
			Run: func(ctx context.Context) error {
				p.ScheduleRunner.Heartbeat(ctx)
				return nil
			},
		},
		{
			ID:          "dataprocessing.cleanup-completed",
			Spec:        "0 2 * * *",
			Description: "Deletes completed processing jobs older than the retention window.",
			Timeout:     30 * time.Minute,
			Run:         dataprocessing.CleanupTask(p.Processing, p.Settings, logger),
		},
		{
			ID:          "notifications.purge-expired",
			Spec:        "30 3 * * *",
			Description: "Deletes notifications that expired more than 7 days ago.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				purged, err := queries.PurgeExpiredNotifications(ctx)
				if err != nil {
					return err
				}
				logger.Info("expired notifications purged", "count", purged)
				return nil
			},
		},
		{
			ID:          "oauth.prune",
			Spec:        "15 * * * *",
			Description: "Deletes expired MCP authorization codes and spent refresh tokens.",
			Timeout:     5 * time.Minute,
			Run: func(ctx context.Context) error {
				codes, err := queries.DeleteExpiredOAuthCodes(ctx)
				if err != nil {
					return err
				}
				refresh, err := queries.DeleteExpiredOAuthRefreshTokens(ctx)
				if err != nil {
					return err
				}
				logger.Info("oauth codes and refresh tokens pruned", "codes", codes, "refresh_tokens", refresh)
				return nil
			},
		},
		{
			ID:          "billing.expire-subscriptions",
			Spec:        "0 */6 * * *",
			Description: "Marks overdue subscriptions as expired (and suspends lapsed tenants with BILLING_SUSPEND_ON_EXPIRY).",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				expired, err := p.BillingSubs.ExpireOverdue(ctx)
				if err != nil {
					return err
				}
				logger.Info("subscriptions expired", "count", expired)
				if !cfg.Billing.SuspendOnExpiry {
					return nil
				}
				lapsed, err := p.BillingSubs.SuspendLapsedTenants(ctx, cfg.Billing.SuspendGrace, cfg.Tenancy.DefaultTenantID)
				if err != nil {
					return err
				}
				for _, tenant := range lapsed {
					tenancy.InvalidateTenantCode(ctx, p.Cache, tenant.Code)
				}
				logger.Info("lapsed tenants suspended", "count", len(lapsed))
				return nil
			},
		},
		{
			ID:          "announcements.deliver",
			Spec:        "* * * * *",
			Description: "Sends started announcements through their channels (in-app, mail, sms, push), once each.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				sent, err := p.Announcements.DeliverDue(ctx)
				if err != nil {
					return err
				}
				if sent > 0 {
					logger.Info("announcements delivered", "count", sent)
				}
				return nil
			},
		},
		{
			ID:          "billing.send-renewal-reminders",
			Spec:        "0 8 * * *",
			Description: "Notifies organizations whose subscription expires in 7, 3 or 1 days.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				sent, err := p.BillingSubs.SendRenewalReminders(ctx, logger, []int{7, 3, 1})
				if err != nil {
					return err
				}
				logger.Info("renewal reminders sent", "count", sent)
				return nil
			},
		},
		// Account erasure and export (docs/design/account-deletion-and-export.md).
		{
			ID:          "privacy.erase-due",
			Spec:        "*/15 * * * *",
			Description: "Erases accounts whose deletion grace period (privacy.deletion_grace_days) is over.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				erased, err := p.Privacy.EraseDue(ctx)
				if err != nil {
					return err
				}
				logger.Info("scheduled erasures run", "erased", erased)
				return nil
			},
		},
		{
			ID:          "privacy.erase-soft-deleted",
			Spec:        "45 2 * * *",
			Description: "Erases users deleted longer than privacy.erase_soft_deleted_after_days ago (off by default).",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				erased, err := p.Privacy.EraseSoftDeleted(ctx)
				if err != nil {
					return err
				}
				logger.Info("retention erasures run", "erased", erased)
				return nil
			},
		},
		{
			ID:          "privacy.expire-exports",
			Spec:        "20 * * * *",
			Description: "Deletes personal-data exports older than privacy.export_ttl_hours.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := p.Privacy.ExpireExports(ctx)
				if err != nil {
					return err
				}
				logger.Info("expired privacy exports deleted", "count", deleted)
				return nil
			},
		},
	}

	tasks = append(tasks, scheduler.Task{
		ID:          "auth.prune-tokens",
		Spec:        "40 3 * * *",
		Description: "Deletes expired access tokens (and sessions idle for AUTH_SESSION_IDLE_DAYS).",
		Timeout:     10 * time.Minute,
		Run: func(ctx context.Context) error {
			deleted, err := p.Sessions.PruneTokens(ctx, cfg.Auth.SessionIdleDays)
			if err != nil {
				return err
			}
			logger.Info("access tokens pruned", "deleted", deleted, "idle_days", cfg.Auth.SessionIdleDays)
			return nil
		},
	})

	if cfg.Webhooks.RetentionDays > 0 {
		tasks = append(tasks, scheduler.Task{
			ID:          "webhook.prune",
			Spec:        "55 3 * * *",
			Description: "Deletes webhook delivery logs older than WEBHOOK_RETENTION_DAYS.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := p.Webhooks.Prune(ctx)
				if err != nil {
					return err
				}
				logger.Info("webhook deliveries pruned", "deleted", deleted, "retention_days", cfg.Webhooks.RetentionDays)
				return nil
			},
		})
	}

	if days := cfg.Schedule.HistoryDays; days > 0 {
		tasks = append(tasks, scheduler.Task{
			ID:          "scheduler.prune-history",
			Spec:        "5 4 * * *",
			Description: "Deletes scheduler run history older than SCHEDULE_HISTORY_DAYS.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := p.ScheduleRunner.PruneHistory(ctx, days)
				if err != nil {
					return err
				}
				logger.Info("scheduler run history pruned", "deleted", deleted, "retention_days", days)
				return nil
			},
		})
	}

	if days := cfg.Delivery.RetentionDays; days > 0 {
		tasks = append(tasks, scheduler.Task{
			ID:          "delivery.prune",
			Spec:        "50 3 * * *",
			Description: "Deletes delivery-log rows older than DELIVERY_RETENTION_DAYS.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := p.Delivery.Prune(ctx, days)
				if err != nil {
					return err
				}
				logger.Info("delivery log pruned", "deleted", deleted, "retention_days", days)
				return nil
			},
		})
	}

	if days := cfg.Activity.RetentionDays; days > 0 {
		feed := activity.NewService(activity.NewRepository(queries))
		tasks = append(tasks, scheduler.Task{
			ID:          "activity.prune",
			Spec:        "45 3 * * *",
			Description: "Deletes activity-log entries older than ACTIVITY_RETENTION_DAYS.",
			Timeout:     30 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := feed.Purge(ctx, days)
				if err != nil {
					return err
				}
				logger.Info("activity log pruned", "deleted", deleted, "retention_days", days)
				return nil
			},
		})
	}

	if cfg.Cache.Driver == "database" {
		tasks = append(tasks, scheduler.Task{
			ID:          "cache.purge-expired",
			Spec:        "15 * * * *",
			Description: "Deletes expired rows from the database cache table.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				purged, err := queries.CacheDeleteExpired(ctx)
				if err != nil {
					return err
				}
				logger.Info("expired cache rows purged", "count", purged)
				return nil
			},
		})
	}

	if cfg.Backup.Enabled {
		tasks = append(tasks,
			scheduler.Task{
				ID:          "backup.run",
				Spec:        cfg.Backup.RunCron,
				Description: "Creates a database backup archive and copies it to the configured destinations.",
				Timeout:     cfg.Backup.Timeout,
				// A backup before a risky change still runs while down.
				EvenInMaintenance: true,
				Run: func(ctx context.Context) error {
					run, err := p.Backup.Enqueue(ctx, backup.TriggerSchedule, 0)
					if err != nil {
						return err
					}
					logger.Info("backup queued", "run_id", run.ID, "filename", run.Filename)
					return nil
				},
			},
			scheduler.Task{
				ID:                "backup.clean",
				Spec:              cfg.Backup.CleanCron,
				Description:       "Applies the backup retention policy and deletes expired archives.",
				Timeout:           30 * time.Minute,
				EvenInMaintenance: true,
				Run: func(ctx context.Context) error {
					result, err := p.Backup.Clean(ctx, false)
					if err != nil {
						return err
					}
					logger.Info("backup cleanup finished", "deleted", result.Deleted, "candidates", result.Candidates, "failed", result.Failed)
					return nil
				},
			},
		)
	}

	if dash := cfg.DevDashboard; dash.Enabled {
		telemetry := dev.NewTelemetryRepository(queries)
		audit := dev.NewActionRepository(queries)
		tasks = append(tasks, scheduler.Task{
			ID:          "devdashboard.prune",
			Spec:        "0 3 * * *",
			Description: "Deletes dashboard telemetry and audit rows older than their retention windows.",
			Timeout:     10 * time.Minute,
			Run: func(ctx context.Context) error {
				deleted, err := telemetry.Prune(ctx, dash.RetentionDays)
				if err != nil {
					return err
				}
				logger.Info("dev dashboard telemetry pruned", "deleted", deleted, "retention_days", dash.RetentionDays)
				pruned, err := audit.Prune(ctx, dash.AuditRetentionDays)
				if err != nil {
					return err
				}
				logger.Info("dev dashboard audit log pruned", "deleted", pruned, "retention_days", dash.AuditRetentionDays)
				return nil
			},
		})
	}

	return tasks
}
