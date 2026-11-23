---
title: Import and export
description: Background spreadsheet imports and exports with real progress, per-row errors and safe downloads.
---

Large imports and exports run as **data-processing jobs**: the request returns at once with a job,
a queue worker does the work, and the client polls the job for progress and a download.

## Users: the built-in example

| Endpoint | Does |
| --- | --- |
| `POST /api/v1/user/import-template` | download a template spreadsheet (`user.import`) |
| `POST /api/v1/user/import-background` | upload `.xlsx` or `.csv`; starts a `user.import` job |
| `POST /api/v1/user/export-background` | starts a `user.export` job with the list filters |
| `GET /api/v1/data-processing/jobs/user` | the caller's jobs |
| `GET /api/v1/data-processing/job/{jobId}` | status, progress and per-row errors |
| `GET /api/v1/data-processing/job/{jobId}/download` | the export file — only when `COMPLETED` |
| `POST /api/v1/data-processing/job/{jobId}/retry` | run a `FAILED` job again with the same filters or file |
| `DELETE /api/v1/data-processing/job/{jobId}` | remove a finished job and its file |
| `POST /api/v1/internal/data-processing/job/{jobId}/retry`, `DELETE /api/v1/internal/data-processing/job/{jobId}` | the same for staff not acting in an organization (their writes on organization routes are refused) |

```json
{
  "type": "user.import",
  "status": "COMPLETED",
  "total_items": 250, "processed_items": 250, "success_count": 247, "error_count": 3,
  "progress_percentage": 100,
  "errors": [{ "row": 14, "errors": { "email": ["The email has already been taken."] } }]
}
```

- **Honest status**: progress is real, and a failure is a `FAILED` job with a message — never a
  silent success.
- **Formats**: `.xlsx` and `.csv`. Legacy `.xls` is rejected.
- **Safe cells**: exported values that start with `=`, `+`, `-` or `@` are neutralised, so a
  spreadsheet can't run a formula a user typed in.
- **Bounded**: imports stream rows instead of loading gigabytes into memory.
- **Files** live on `DATA_PROCESSING_DISK` (private by default).
- **Retention**: finished jobs are deleted after their organization's
  `data_processing_job.cleanup_days` setting (default 30; the organization's own value wins over
  the platform's) by the `dataprocessing.cleanup-completed` task. Every job reports `expires_at`.
- **Retry**: a module makes its failed jobs retryable with
  `dataprocessing.RegisterRerun(dataprocessing.RerunKey(type, entity), rerun)`; the rerun queues
  the same job row again (users and activity exports do).
- **Activity**: `POST /api/v1/activity/export` (an organization's log) and
  `POST /api/v1/internal/activity/export` (staff: every organization and the system log) export
  every entry matching the activity filters (`internal/modules/activity/export`).

## Building your own

`identity.ExchangeService` is the pattern to copy: register two queue jobs (`…import`,
`…export`), create a job row with `dataprocessing.Service`, enqueue with the tenant scope, then
report progress with `UpdateProgress` as rows are processed. The
`feature.data_processing` [flag](./feature-flags.md) gates the whole feature.
