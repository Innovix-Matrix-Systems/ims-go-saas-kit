package files

import "time"

// Response is the public file representation (fields:
// url/name/file_name/mime_type/size/type/created_at; media_id is dropped with
// the merged table, uuid and attached are additive).
type Response struct {
	ID        int64  `json:"id"`
	UUID      string `json:"uuid"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Name      string `json:"name"`
	FileName  string `json:"file_name"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	Attached  bool   `json:"attached"`
	Generated bool   `json:"generated"`
	// Collection is what an attached file is for ("logo", "profile",
	// "output" for an export); nil for library files.
	Collection *string   `json:"collection"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewResponse maps a file to its public representation.
func NewResponse(file File, url string) Response {
	return Response{
		ID:         file.ID,
		UUID:       file.UUID.String(),
		Type:       file.Type,
		URL:        url,
		Name:       file.Name,
		FileName:   file.FileName,
		MimeType:   file.MimeType,
		Size:       file.Size,
		Attached:   file.Attached(),
		Generated:  file.Generated(),
		Collection: file.Collection,
		CreatedAt:  file.CreatedAt,
	}
}
