package dev

import (
	"time"

	"github.com/google/uuid"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

type FileItem struct {
	ID            int64     `json:"id"`
	UUID          string    `json:"uuid"`
	Name          string    `json:"name"`
	FileName      string    `json:"file_name"`
	Disk          string    `json:"disk"`
	Path          string    `json:"path"`
	MimeType      string    `json:"mime_type"`
	Size          int64     `json:"size"`
	Type          string    `json:"type"`
	Attached      bool      `json:"attached"`
	OwnerType     *string   `json:"owner_type"`
	OwnerID       *int64    `json:"owner_id"`
	OwnerName     *string   `json:"owner_name"`
	OwnerEmail    *string   `json:"owner_email"`
	Collection    *string   `json:"collection"`
	UploaderID    *int64    `json:"uploader_id"`
	UploaderName  *string   `json:"uploader_name"`
	UploaderEmail *string   `json:"uploader_email"`
	TenantID      *int64    `json:"tenant_id"`
	TenantName    *string   `json:"tenant_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func mapFileItem(row dbgen.ListDevFilesRow) FileItem {
	item := FileItem{
		ID:            row.ID,
		Name:          row.Name,
		FileName:      row.FileName,
		Disk:          row.Disk,
		Path:          row.Path,
		MimeType:      row.MimeType,
		Size:          row.Size,
		Type:          row.Type,
		Attached:      row.OwnerID != nil,
		OwnerType:     row.OwnerType,
		OwnerID:       row.OwnerID,
		OwnerName:     row.OwnerName,
		OwnerEmail:    row.OwnerEmail,
		Collection:    row.Collection,
		UploaderID:    row.CreatedBy,
		UploaderName:  row.UploaderName,
		UploaderEmail: row.UploaderEmail,
		TenantID:      row.TenantID,
		TenantName:    row.TenantName,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
	if row.Uuid.Valid {
		item.UUID = uuid.UUID(row.Uuid.Bytes).String()
	}
	return item
}

type FileTypeStat struct {
	Type  string `json:"type"`
	Files int64  `json:"files"`
	Size  int64  `json:"size"`
}

type FileDiskStat struct {
	Disk  string `json:"disk"`
	Files int64  `json:"files"`
	Size  int64  `json:"size"`
}

type FileStats struct {
	TotalFiles      int64          `json:"total_files"`
	TotalSize       int64          `json:"total_size"`
	AttachedFiles   int64          `json:"attached_files"`
	UnattachedFiles int64          `json:"unattached_files"`
	ByType          []FileTypeStat `json:"by_type"`
	ByDisk          []FileDiskStat `json:"by_disk"`
}
