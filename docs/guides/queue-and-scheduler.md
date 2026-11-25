---
title: Queue and scheduler
description: Background jobs on River with transactional enqueue and tenant-aware envelopes, and cron tasks in one list.
layers: [api, dashboard]
---

## Queue jobs

Jobs run on [River](https://riverqueue.com), a Postgres-backed queue — no Redis required. A job is
a **definition** registered in a `queue.Registry`:

```go title="internal/platform/mail/jobs.go"
func RegisterJob(registry *queue.Registry, driver Driver) error {
	return registry.Register(queue.Definition{
		Kind:        KindSend,                 // "mail.send"
		Description: "Delivers an email through the configured mail driver.",
		Queue:       queue.QueueDefault,       // critical | default | heavy
		Timeout:     time.Minute,
		SystemJob:   true,                     // no tenant needed
		Validate:    validateSendPayload,      // rejects bad payloads at enqueue time
		Handle: func(ctx context.Context, payload []byte) error {
			var input sendPayload
			if err := json.Unmarshal(payload, &input); err != nil {
				return err
			}
			return driver.Send(ctx, Message{To: input.To, Subject: input.Subject, Text: input.Text})
		},
	})
}
```

Register it from `internal/app/platform.go` (most modules expose `RegisterJobs(registry)`), so the
API, the worker and the CLI all know the job.

### Enqueueing

```go
err := engine.Enqueue(ctx, mail.KindSend, payload, queue.EnqueueOptions{})
```

Prefer **`EnqueueTx`** when the job belongs to a write: the job row commits in the same
transaction, so a rolled-back write never sends a mail, and a committed one always does.

```go
err := engine.EnqueueTx(ctx, tx, KindProcessImport, payload, queue.EnqueueOptions{})
```

### Tenancy travels with the job

River sees one job kind (`ims.job`) whose envelope is `{kind, payload, tenant_id, branch_id}`. The
dispatcher restores the tenant scope before calling your handler, so a tenant job sees exactly
the tenant that enqueued it. A tenant job enqueued **without** a tenant is refused.

### Queues and retries

| Queue | Default tries | Timeout | For |
| --- | --- | --- | --- |
| `critical` | 3 | 60 s | must-happen-now work |
| `default` | 5 | 120 s | mail, notifications |
| `heavy` | 1 | 30 min | imports, exports, backups |

Tune with `QUEUE_<NAME>_TRIES` / `_TIMEOUT` and `QUEUE_WORKER_CONCURRENCY`.

### Running workers

```bash
ims queue:work           # run workers until stopped (make queue)
ims queue:work --once    # drain what's queued, then exit
ims queue:stats          # depth per queue and state
```

`QUEUE_DRIVER=sync` runs jobs **inline** — handy for tests and one-off CLI use. Workers don't
hot-reload: restart `make queue` after changing a job. In the dashboard, **Queue** lists jobs
with their payload envelope and lets you retry, cancel or delete them.

## Scheduled tasks

Recurring work is **one list** in `internal/app/schedule.go` — Laravel's `Kernel::schedule`:

```go title="internal/app/schedule.go"
{
	ID:          "billing.expire-subscriptions",
	Spec:        "0 * * * *",            // cron, in SCHEDULE_TIMEZONE
	Description: "Marks overdue subscriptions as expired and starts scheduled ones.",
	Timeout:     10 * time.Minute,
	Run: func(ctx context.Context) error {
		expired, started, err := p.BillingSubs.ProcessPeriodEnds(ctx)
		if err != nil {
			return err
		}
		logger.Info("subscriptions expired", "count", expired, "scheduled_started", started)
		return nil
	},
},
```

- Overlapping runs are **skipped** by default; set `Overlap: scheduler.OverlapAllow` to allow them.
- While the app is in [maintenance mode](maintenance-mode.md) scheduled runs are **skipped**; set
  `EvenInMaintenance: true` on a task that must still run (the heartbeat and backups do).
- Every run is kept in the run history (below); set `NoHistory: true` on a task that runs too often
  for that to be useful (the every-minute heartbeat does).
- Tasks that depend on a feature are appended only when it is enabled (backups, the database
  cache, the dashboard).
- Only **one** scheduler runs per database (a Postgres advisory lock), so starting two by mistake
  is harmless.

```bash
ims schedule                 # run the scheduler (make schedule)
ims schedule:list            # every task with its spec
ims schedule:run <id>        # run one task now
```

Every run, scheduled or manual, is recorded in the `schedule_runs` table: when it started, how it
was started, whether it finished `ok` or `failed`, how long it took and its error. A run the
scheduler lost (the process stopped mid-run) is marked `interrupted` when the scheduler starts
again. `SCHEDULE_HISTORY_DAYS` (default `30`, `0` keeps everything) sets how long runs are kept;
`scheduler.prune-history` removes older ones every night.

The dashboard's **Scheduler** panel shows each task's schedule, next run and last ten runs, the
full history filtered by task and status, warns when no `ims schedule` process is checking in,
and has **Run now** for the same work as `ims schedule:run`. Run now is refused while the task
still has a run open, here or in the `ims schedule` process; a run open past its task's timeout
was lost with its process and no longer counts. A task that panics fails its run (the panic is
its error) instead of taking the process down.

### The built-in tasks

| Task | When | Does |
| --- | --- | --- |
| `scheduler.heartbeat` | every minute | records liveness for `/api/health` |
| `scheduler.prune-history` | 04:05 | applies `SCHEDULE_HISTORY_DAYS` to the run history |
| `dataprocessing.cleanup-completed` | 02:00 | deletes old completed import/export jobs |
| `notifications.purge-expired` | 03:30 | deletes notifications expired over 7 days ago |
| `oauth.prune` | hourly :15 | deletes expired MCP authorization codes and spent refresh tokens |
| `billing.expire-subscriptions` | every 6 h | expires overdue subscriptions |
| `billing.send-renewal-reminders` | 08:00 | reminders at 7, 3 and 1 days before expiry |
| `auth.prune-tokens` | 03:40 | deletes expired access tokens (and idle sessions) |
| `activity.prune` | 03:45 | applies `ACTIVITY_RETENTION_DAYS` |
| `delivery.prune` | 03:50 | applies `DELIVERY_RETENTION_DAYS` |
| `webhook.prune` | 03:55 | applies `WEBHOOK_RETENTION_DAYS` |
| `cache.purge-expired` | hourly :15 | database cache driver only |
| `backup.run`, `backup.clean` | `BACKUP_RUN_CRON`, `BACKUP_CLEAN_CRON` | when backups are enabled |
| `devdashboard.prune` | 03:00 | telemetry and dashboard audit retention |

:::note
The heartbeat lives in the cache. With `CACHE_DRIVER=memory` it is only visible inside the
scheduler process, so the API's health check can't see it — use `database` or `redis` anywhere
the API and scheduler run as separate processes.
:::
