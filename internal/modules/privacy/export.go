package privacy

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path"
	"strings"
)

// zipExport is the export archive. Each contributor writes into its own
// folder (identity/profile.json, files/12.png); the manifest sits at the root.
type zipExport struct {
	zip   *zip.Writer
	files []string
}

func newZipExport(out io.Writer) *zipExport {
	return &zipExport{zip: zip.NewWriter(out)}
}

func (z *zipExport) folder(name string) zipFolder { return zipFolder{archive: z, prefix: name} }

func (z *zipExport) root() zipFolder { return zipFolder{archive: z} }

func (z *zipExport) close() error { return z.zip.Close() }

func (z *zipExport) create(name string) (io.Writer, error) {
	z.files = append(z.files, name)
	return z.zip.Create(name)
}

// zipFolder implements privacy.Writer for one contributor.
type zipFolder struct {
	archive *zipExport
	prefix  string
}

func (f zipFolder) path(name string) string {
	// Names come from code, but a file name may echo an upload: keep it
	// inside the folder.
	name = strings.TrimLeft(path.Clean("/"+strings.ReplaceAll(name, "\\", "/")), "/")
	if f.prefix == "" {
		return name
	}
	return f.prefix + "/" + name
}

// JSON implements privacy.Writer.
func (f zipFolder) JSON(name string, v any) error {
	w, err := f.archive.create(f.path(name + ".json"))
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// File implements privacy.Writer.
func (f zipFolder) File(name string, r io.Reader) error {
	w, err := f.archive.create(f.path(name))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}
