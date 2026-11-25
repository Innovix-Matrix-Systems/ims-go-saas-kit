package dev

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

func TestFileRepository(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	ctx := context.Background()
	dbtest.Clean(t, pool, "files", "users", "tenants")

	now := time.Now().UTC().Truncate(time.Second)
	var tenantA, tenantB, userA, userB, internal int64
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name, code) VALUES ('Acme', 'ACME') RETURNING id`).Scan(&tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name, code) VALUES ('Globex', 'GLBX') RETURNING id`).Scan(&tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (tenant_id, name, email, password) VALUES ($1, 'Alice', 'alice@example.test', 'x') RETURNING id`, tenantA).Scan(&userA); err != nil {
		t.Fatalf("insert user A: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (tenant_id, name, email, password) VALUES ($1, 'Bob', 'bob@example.test', 'x') RETURNING id`, tenantB).Scan(&userB); err != nil {
		t.Fatalf("insert user B: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (name, email, password) VALUES ('Internal', 'dev@example.test', 'x') RETURNING id`).Scan(&internal); err != nil {
		t.Fatalf("insert internal user: %v", err)
	}

	insertFile := func(name, fileName, disk, mimeType, fileType string, size int64, ownerID *int64, uploader int64, createdAt time.Time) int64 {
		t.Helper()
		var id int64
		var ownerType, collection *string
		if ownerID != nil {
			owner := "user"
			attachment := "profile"
			ownerType = &owner
			collection = &attachment
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO files
			 (uuid, name, file_name, disk, path, mime_type, size, type, owner_type, owner_id, collection, created_by, created_at, updated_at, tenant_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, (SELECT tenant_id FROM users WHERE id = $12)) RETURNING id`,
			uuid.New(), name, fileName, disk, "files/"+name, mimeType, size, fileType,
			ownerType, ownerID, collection, uploader, createdAt,
		).Scan(&id); err != nil {
			t.Fatalf("insert file %s: %v", name, err)
		}
		return id
	}

	fileA := insertFile("Alpha", "alpha.png", "public", "image/png", "image", 1000, nil, userA, now.Add(-72*time.Hour))
	fileB := insertFile("Report", "report.pdf", "private", "application/pdf", "document", 2000, &userB, userB, now.Add(-48*time.Hour))
	fileC := insertFile("Zulu", "zulu.jpg", "s3", "image/jpeg", "image", 3000, &userA, userA, now.Add(-24*time.Hour))
	fileD := insertFile("Binary", "archive.bin", "public", "application/octet-stream", "other", 500, nil, internal, now)

	repo := NewFileRepository(queries)
	all, err := repo.List(ctx, FileFilter{Limit: 10, OrderBy: "created_at", OrderDir: "desc"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 4 || all[0].ID != fileD || all[1].ID != fileC || all[2].ID != fileB || all[3].ID != fileA {
		t.Fatalf("List() order = %+v, want [D, C, B, A]", ids(all))
	}
	if all[2].OwnerName == nil || *all[2].OwnerName != "Bob" || all[2].TenantName == nil || *all[2].TenantName != "Globex" {
		t.Fatalf("List() enrichment = owner:%v tenant:%v, want Bob/Globex", all[2].OwnerName, all[2].TenantName)
	}
	if all[2].UploaderEmail == nil || *all[2].UploaderEmail != "bob@example.test" {
		t.Fatalf("List() uploader = %v, want bob@example.test", all[2].UploaderEmail)
	}

	tenantFiles, err := repo.List(ctx, FileFilter{TenantID: tenantA, Limit: 10})
	if err != nil {
		t.Fatalf("List(tenant) error = %v", err)
	}
	if len(tenantFiles) != 2 || tenantFiles[0].ID != fileC || tenantFiles[1].ID != fileA {
		t.Fatalf("List(tenant) = %v, want [C, A]", ids(tenantFiles))
	}

	images, err := repo.List(ctx, FileFilter{Type: "image", Limit: 10})
	if err != nil {
		t.Fatalf("List(type) error = %v", err)
	}
	if len(images) != 2 || images[0].ID != fileC || images[1].ID != fileA {
		t.Fatalf("List(type) = %v, want [C, A]", ids(images))
	}

	attached := true
	attachments, err := repo.List(ctx, FileFilter{Attached: &attached, Limit: 10})
	if err != nil {
		t.Fatalf("List(attached) error = %v", err)
	}
	if len(attachments) != 2 || attachments[0].ID != fileC || attachments[1].ID != fileB {
		t.Fatalf("List(attached) = %v, want [C, B]", ids(attachments))
	}

	s3, err := repo.List(ctx, FileFilter{Disk: "s3", Limit: 10})
	if err != nil || len(s3) != 1 || s3[0].ID != fileC {
		t.Fatalf("List(disk) = %v, %v, want [C]", ids(s3), err)
	}

	search, err := repo.List(ctx, FileFilter{Search: "report", Limit: 10})
	if err != nil || len(search) != 1 || search[0].ID != fileB {
		t.Fatalf("List(search) = %v, %v, want [B]", ids(search), err)
	}

	byName, err := repo.List(ctx, FileFilter{Limit: 10, OrderBy: "name", OrderDir: "asc"})
	if err != nil {
		t.Fatalf("List(name) error = %v", err)
	}
	if len(byName) != 4 || byName[0].ID != fileA || byName[1].ID != fileD || byName[2].ID != fileB || byName[3].ID != fileC {
		t.Fatalf("List(name) = %v, want [A, D, B, C]", ids(byName))
	}

	page, err := repo.List(ctx, FileFilter{Limit: 2, Offset: 2, OrderBy: "created_at", OrderDir: "desc"})
	if err != nil || len(page) != 2 || page[0].ID != fileB || page[1].ID != fileA {
		t.Fatalf("List(page) = %v, %v, want [B, A]", ids(page), err)
	}

	total, err := repo.Count(ctx, FileFilter{})
	if err != nil || total != 4 {
		t.Fatalf("Count() = %d, %v, want 4", total, err)
	}
	total, err = repo.Count(ctx, FileFilter{TenantID: tenantA})
	if err != nil || total != 2 {
		t.Fatalf("Count(tenant) = %d, %v, want 2", total, err)
	}

	stats, err := repo.Stats(ctx, 0)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.TotalFiles != 4 || stats.TotalSize != 6500 || stats.AttachedFiles != 2 || stats.UnattachedFiles != 2 {
		t.Fatalf("Stats() totals = %+v", stats)
	}
	if stat := findTypeStat(t, stats, "image"); stat.Files != 2 || stat.Size != 4000 {
		t.Fatalf("Stats() image = %+v, want 2/4000", stat)
	}
	if stat := findDiskStat(t, stats, "public"); stat.Files != 2 || stat.Size != 1500 {
		t.Fatalf("Stats() public = %+v, want 2/1500", stat)
	}

	tenantStats, err := repo.Stats(ctx, tenantA)
	if err != nil {
		t.Fatalf("Stats(tenant) error = %v", err)
	}
	if tenantStats.TotalFiles != 2 || tenantStats.TotalSize != 4000 || tenantStats.AttachedFiles != 1 || tenantStats.UnattachedFiles != 1 {
		t.Fatalf("Stats(tenant) = %+v, want 2 files/4000 bytes", tenantStats)
	}
}

func ids(items []FileItem) []int64 {
	values := make([]int64, 0, len(items))
	for _, item := range items {
		values = append(values, item.ID)
	}
	return values
}

func findTypeStat(t *testing.T, stats FileStats, fileType string) FileTypeStat {
	t.Helper()
	for _, item := range stats.ByType {
		if item.Type == fileType {
			return item
		}
	}
	t.Fatalf("type stat %q missing from %+v", fileType, stats.ByType)
	return FileTypeStat{}
}

func findDiskStat(t *testing.T, stats FileStats, disk string) FileDiskStat {
	t.Helper()
	for _, item := range stats.ByDisk {
		if item.Disk == disk {
			return item
		}
	}
	t.Fatalf("disk stat %q missing from %+v", disk, stats.ByDisk)
	return FileDiskStat{}
}
