package dev

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

type FileFilter struct {
	Search      string
	Type        string
	Disk        string
	Attached    *bool
	OwnerType   string
	OwnerID     int64
	CreatedBy   int64
	TenantID    int64
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	OrderBy     string
	OrderDir    string
	Limit       int32
	Offset      int32
}

type FileRepository interface {
	List(ctx context.Context, filter FileFilter) ([]FileItem, error)
	Count(ctx context.Context, filter FileFilter) (int64, error)
	Stats(ctx context.Context, tenantID int64) (FileStats, error)
}

type fileRepository struct {
	queries *dbgen.Queries
}

func NewFileRepository(queries *dbgen.Queries) FileRepository {
	return &fileRepository{queries: queries}
}

func (r *fileRepository) List(ctx context.Context, filter FileFilter) ([]FileItem, error) {
	rows, err := r.queries.ListDevFiles(ctx, dbgen.ListDevFilesParams{
		Search:      filter.Search,
		Type:        optionalText(filter.Type),
		Disk:        optionalText(filter.Disk),
		Attached:    filter.Attached,
		OwnerType:   optionalText(filter.OwnerType),
		OwnerID:     optionalID(filter.OwnerID),
		CreatedBy:   optionalID(filter.CreatedBy),
		TenantID:    optionalID(filter.TenantID),
		CreatedFrom: timestampArg(filter.CreatedFrom),
		CreatedTo:   timestampArg(filter.CreatedTo),
		OrderBy:     filter.OrderBy,
		OrderDir:    filter.OrderDir,
		PageOffset:  filter.Offset,
		PageLimit:   filter.Limit,
	})
	if err != nil {
		return nil, database.MapError("dev.list_files", err)
	}
	items := make([]FileItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapFileItem(row))
	}
	return items, nil
}

func (r *fileRepository) Count(ctx context.Context, filter FileFilter) (int64, error) {
	total, err := r.queries.CountDevFiles(ctx, dbgen.CountDevFilesParams{
		Search:      filter.Search,
		Type:        optionalText(filter.Type),
		Disk:        optionalText(filter.Disk),
		Attached:    filter.Attached,
		OwnerType:   optionalText(filter.OwnerType),
		OwnerID:     optionalID(filter.OwnerID),
		CreatedBy:   optionalID(filter.CreatedBy),
		TenantID:    optionalID(filter.TenantID),
		CreatedFrom: timestampArg(filter.CreatedFrom),
		CreatedTo:   timestampArg(filter.CreatedTo),
	})
	if err != nil {
		return 0, database.MapError("dev.count_files", err)
	}
	return total, nil
}

func (r *fileRepository) Stats(ctx context.Context, tenantID int64) (FileStats, error) {
	rows, err := r.queries.DevFileStats(ctx, optionalID(tenantID))
	if err != nil {
		return FileStats{}, database.MapError("dev.file_stats", err)
	}
	stats := FileStats{
		ByType: make([]FileTypeStat, 0, 4),
		ByDisk: make([]FileDiskStat, 0, 3),
	}
	byType := make(map[string]*FileTypeStat)
	byDisk := make(map[string]*FileDiskStat)
	for _, row := range rows {
		stats.TotalFiles += row.Files
		stats.TotalSize += row.TotalSize
		if row.Attached {
			stats.AttachedFiles += row.Files
		} else {
			stats.UnattachedFiles += row.Files
		}
		typeStat, ok := byType[row.Type]
		if !ok {
			typeStat = &FileTypeStat{Type: row.Type}
			byType[row.Type] = typeStat
		}
		typeStat.Files += row.Files
		typeStat.Size += row.TotalSize
		diskStat, ok := byDisk[row.Disk]
		if !ok {
			diskStat = &FileDiskStat{Disk: row.Disk}
			byDisk[row.Disk] = diskStat
		}
		diskStat.Files += row.Files
		diskStat.Size += row.TotalSize
	}
	for _, item := range byType {
		stats.ByType = append(stats.ByType, *item)
	}
	for _, item := range byDisk {
		stats.ByDisk = append(stats.ByDisk, *item)
	}
	sort.Slice(stats.ByType, func(i, j int) bool { return stats.ByType[i].Type < stats.ByType[j].Type })
	sort.Slice(stats.ByDisk, func(i, j int) bool { return stats.ByDisk[i].Disk < stats.ByDisk[j].Disk })
	return stats, nil
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalID(value int64) *int64 {
	if value < 1 {
		return nil
	}
	return &value
}

func timestampArg(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}
