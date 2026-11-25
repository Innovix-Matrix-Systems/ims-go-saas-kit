---
title: Files and storage
description: Named storage disks (local, S3, Cloudflare R2, MinIO), private and public visibility, and file attachments.
layers: [api, app, dashboard]
---

## Disks

Storage works like Laravel's `config/filesystems.php`: every **disk** has a name, a driver and a
visibility.

| Disk | Driver | Visibility | Notes |
| --- | --- | --- | --- |
| `private` | local (`STORAGE_PRIVATE_DIR`) | private | never served directly |
| `public` | local (`STORAGE_PUBLIC_DIR`) | public | served at `/storage/*` |
| `s3` | S3 | from `S3_VISIBILITY` | when `S3_*` is set |
| anything in `STORAGE_DISKS` | S3-compatible | per disk | `STORAGE_<NAME>_*` |

Code depends on the `storage.Disk` interface — `Put`, `Get`, `Delete`, `Exists`, `Size`, `URL`,
`TemporaryURL`, `Public` — so the driver behind a disk is configuration, not code.

### Moving uploads to the cloud is a `.env` change

```bash title=".env — Cloudflare R2"
STORAGE_DISKS=r2
STORAGE_R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
STORAGE_R2_BUCKET=uploads
STORAGE_R2_ACCESS_KEY=…
STORAGE_R2_SECRET_KEY=…
STORAGE_R2_REGION=auto
FILES_DISK=r2
```

```bash title=".env — MinIO"
STORAGE_DISKS=minio
STORAGE_MINIO_ENDPOINT=http://127.0.0.1:9000
STORAGE_MINIO_BUCKET=uploads
STORAGE_MINIO_PATH_STYLE=true
FILES_DISK=minio
```

AWS S3, DigitalOcean Spaces and Backblaze B2 work the same way. Set `…_PUBLIC_URL` (a CDN or
custom domain) to make a bucket disk public; otherwise downloads use short-lived presigned URLs.
`…_ROOT` adds a key prefix inside the bucket.

| Setting | Chooses |
| --- | --- |
| `FILES_DISK` | where uploaded files go |
| `FILES_PUBLIC_DISK` | where avatars go when `FILES_DISK` is private |
| `DATA_PROCESSING_DISK` | where import/export files go |
| `BACKUP_DESTINATIONS` | where backups are copied |

### Private means private

Private disks never hand out URLs. Their files are read through the **authorized download route**
(`GET /api/v1/files/{id}/download`), which checks the caller first. Public files get a URL computed
from disk + path — no stored URL snapshot, so moving disks never leaves stale links.

## Files and attachments

One `files` table holds every upload, attached to an owner **polymorphically**
(`owner_type`, `owner_id`, `collection`) — like spatie/laravel-medialibrary without conversions.
A Go registry declares which owner/collection pairs exist and which keep a **single** file (a
profile photo is replaced atomically):

```go title="internal/modules/files/model.go"
var collections = map[string]map[string]bool{
	OwnerUser: {
		CollectionProfile: true, // single file
	},
}
```

There are two route families, like users and notifications:

| Endpoint | Does |
| --- | --- |
| `POST /api/v1/files` | upload into the effective tenant (`file.create`) — multipart |
| `GET /api/v1/files` | list the tenant's files (`file.view.all`) |
| `GET /api/v1/files/{id}` | metadata (owner with `file.view`, or `file.view.all`) |
| `GET /api/v1/files/{id}/download` | the bytes, authorized |
| `DELETE /api/v1/files/{id}` | delete (owner with `file.create`, or `file.delete`) |
| `POST /api/v1/internal/files` | upload a **global** platform file (`file.internal.create`) |
| `GET /api/v1/internal/files` | list every tenant's and global files (`file.internal.view.all`) |
| `GET /api/v1/internal/files/{id}` (+ `/download`) | any file (owner or `file.internal.view`) |
| `DELETE /api/v1/internal/files/{id}` | any file (owner or `file.internal.delete`) |

`/files` runs behind `tenant.resolve`: a tenant user's files belong to their tenant, and a
platform account uploads into the tenant it impersonates (`X-Acting-Tenant` +
`X-Acting-Branch`; without them the upload is refused). `/internal/files` is for platform
accounts only (`tenant.internal`) and never stamps a tenant. The `file.internal.*` permissions
are system permissions; tenant Admin roles get the `file.*` ones.

**Accepted types** are an allow-list; SVG is deliberately refused because it can carry script.
Uploads count against the plan's `storage.mb` when the plan defines it.

## In the dashboard

**Files** browses every file across disks with thumbnails, a preview drawer (images and PDFs),
filters by owner, tenant, type and date, and audited download and delete.

## In the app

Members browse, upload and preview files under **Files** (`/org/files`), in a grid or a list. The profile photo and the organization logo use the same upload endpoints.
