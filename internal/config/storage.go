package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Storage disks work like Laravel's config/filesystems.php: every disk has a
// name, a driver (local or s3) and a visibility. Application code only names
// a disk (FILES_DISK=r2), so moving files from the server's disk to S3,
// Cloudflare R2, MinIO, DigitalOcean Spaces or Backblaze B2 is a .env change.
//
// Built-in disks:
//
//	private  local, APP_STORAGE_DIR/private, files are served by the app
//	public   local, APP_STORAGE_DIR/public, served at {APP_URL}/storage
//	s3       S3-compatible, from S3_* (only when S3_BUCKET etc. are set)
//
// More disks come from STORAGE_DISKS=media,archive, each configured with
// STORAGE_<NAME>_* variables (see .env.example).

// Storage disk drivers and visibilities.
const (
	DriverLocal       = "local"
	DriverS3          = "s3"
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// Storage holds every configured disk.
type Storage struct {
	PublicDir  string
	PrivateDir string
	S3         S3     // the built-in "s3" disk (S3_* settings)
	Disks      []Disk // all disks, built-ins first
	s3Public   bool
}

// AllDisks returns the configured disks. A Storage built by hand (tests) with
// only the directories set gets the built-in disks.
func (s Storage) AllDisks(appURL string) []Disk {
	if len(s.Disks) > 0 {
		return s.Disks
	}
	return s.builtins(appURL)
}

func (s Storage) builtins(appURL string) []Disk {
	disks := []Disk{
		{Name: "private", Driver: DriverLocal, Root: s.PrivateDir},
		{Name: "public", Driver: DriverLocal, Root: s.PublicDir, Public: true,
			URL: strings.TrimRight(appURL, "/") + "/storage"},
	}
	if s.S3.Configured() {
		disks = append(disks, Disk{Name: "s3", Driver: DriverS3, Root: s.S3.Root, Public: s.s3Public, S3: s.S3})
	}
	return disks
}

// Disk is one named storage disk.
type Disk struct {
	Name   string
	Driver string // DriverLocal | DriverS3
	// Root is the directory of a local disk, or a key prefix inside the
	// bucket of an s3 disk ("" = bucket root).
	Root string
	// URL is the public base URL of a local disk ("" = no public URL).
	URL string
	// Public disks hand out direct URLs; private disks are only read through
	// the application (authorized download route).
	Public bool
	S3     S3
}

// S3 holds the settings of one S3-compatible disk (aws-sdk-go-v2). Set
// Endpoint for anything that is not AWS:
//
//	Cloudflare R2  https://<account>.r2.cloudflarestorage.com, region "auto"
//	MinIO          http://minio:9000, PathStyle true
//	DO Spaces      https://<region>.digitaloceanspaces.com
type S3 struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
	// PublicURL is the public base URL of the bucket (CDN, R2 custom
	// domain). Empty means URLs are short-lived presigned links.
	PublicURL string
	Root      string // key prefix inside the bucket
}

// Files holds the disks used by the file resource.
type Files struct {
	Disk string // library files and attachments
	// PublicDisk holds profile photos when Disk is private: avatars are shown
	// in <img> tags, which cannot send a bearer token.
	PublicDisk string
}

// Configured reports whether the S3 disk has the settings it needs.
func (s S3) Configured() bool {
	return s.Region != "" && s.Bucket != "" && s.AccessKey != "" && s.SecretKey != ""
}

// Disk returns a configured disk by name.
func (s Storage) Disk(name string) (Disk, bool) {
	for _, disk := range s.Disks {
		if disk.Name == name {
			return disk, true
		}
	}
	return Disk{}, false
}

// DiskNames lists the configured disk names.
func (s Storage) DiskNames() []string {
	names := make([]string, 0, len(s.Disks))
	for _, disk := range s.Disks {
		names = append(names, disk.Name)
	}
	return names
}

var diskNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (c *Config) loadStorage() error {
	publicDir := getString("STORAGE_PUBLIC_DIR", "")
	if publicDir == "" {
		publicDir = filepath.Join(c.App.StorageDir, "public")
	}
	privateDir := getString("STORAGE_PRIVATE_DIR", "")
	if privateDir == "" {
		privateDir = filepath.Join(c.App.StorageDir, "private")
	}
	s3, s3Public, err := loadS3("S3_")
	if err != nil {
		return err
	}
	storage := Storage{PublicDir: publicDir, PrivateDir: privateDir, S3: s3, s3Public: s3Public}
	storage.Disks = storage.builtins(c.App.URL)

	for _, name := range splitCSV(getString("STORAGE_DISKS", "")) {
		name = strings.ToLower(name)
		if !diskNamePattern.MatchString(name) {
			return fmt.Errorf("STORAGE_DISKS: %q is not a valid disk name (lowercase letters, digits, _)", name)
		}
		if _, exists := storage.Disk(name); exists || name == "s3" {
			return fmt.Errorf("STORAGE_DISKS: disk %q is defined twice (private, public and s3 are built in)", name)
		}
		disk, err := c.loadExtraDisk(name)
		if err != nil {
			return err
		}
		storage.Disks = append(storage.Disks, disk)
	}
	c.Storage = storage
	return nil
}

func (c *Config) loadExtraDisk(name string) (Disk, error) {
	prefix := "STORAGE_" + strings.ToUpper(name) + "_"
	driver := strings.ToLower(getString(prefix+"DRIVER", DriverS3))
	switch driver {
	case DriverLocal:
		root := getString(prefix+"ROOT", "")
		if root == "" {
			return Disk{}, fmt.Errorf("%sROOT is required for a local disk", prefix)
		}
		if !filepath.IsAbs(root) {
			root = filepath.Join(c.App.StorageDir, root)
		}
		url := strings.TrimRight(getString(prefix+"URL", ""), "/")
		public, err := visibility(prefix, url != "")
		if err != nil {
			return Disk{}, err
		}
		if public && url == "" {
			return Disk{}, fmt.Errorf("%sURL is required for a public local disk", prefix)
		}
		return Disk{Name: name, Driver: DriverLocal, Root: root, URL: url, Public: public}, nil
	case DriverS3:
		s3, public, err := loadS3(prefix)
		if err != nil {
			return Disk{}, err
		}
		if s3.Region == "" && s3.Endpoint != "" {
			s3.Region = "auto" // R2 and most S3-compatible services ignore it
		}
		if !s3.Configured() {
			return Disk{}, fmt.Errorf("disk %q needs %sBUCKET, %sACCESS_KEY and %sSECRET_KEY (and %sREGION or %sENDPOINT)",
				name, prefix, prefix, prefix, prefix, prefix)
		}
		return Disk{Name: name, Driver: DriverS3, Root: s3.Root, Public: public, S3: s3}, nil
	default:
		return Disk{}, fmt.Errorf("%sDRIVER %q is not supported (local|s3)", prefix, driver)
	}
}

// loadS3 reads <prefix>ENDPOINT, REGION, BUCKET, ACCESS_KEY, SECRET_KEY,
// PATH_STYLE, PUBLIC_URL, ROOT and VISIBILITY. A disk with a PUBLIC_URL is
// public unless VISIBILITY says otherwise.
func loadS3(prefix string) (S3, bool, error) {
	pathStyle, err := getBool(prefix+"PATH_STYLE", false)
	if err != nil {
		return S3{}, false, err
	}
	s3 := S3{
		Endpoint:  getString(prefix+"ENDPOINT", ""),
		Region:    getString(prefix+"REGION", ""),
		Bucket:    getString(prefix+"BUCKET", ""),
		AccessKey: getString(prefix+"ACCESS_KEY", ""),
		SecretKey: getString(prefix+"SECRET_KEY", ""),
		PathStyle: pathStyle,
		PublicURL: strings.TrimRight(getString(prefix+"PUBLIC_URL", ""), "/"),
		Root:      strings.Trim(getString(prefix+"ROOT", ""), "/"),
	}
	public, err := visibility(prefix, s3.PublicURL != "")
	return s3, public, err
}

func visibility(prefix string, defaultPublic bool) (bool, error) {
	def := VisibilityPrivate
	if defaultPublic {
		def = VisibilityPublic
	}
	switch value := strings.ToLower(getString(prefix+"VISIBILITY", def)); value {
	case VisibilityPublic:
		return true, nil
	case VisibilityPrivate:
		return false, nil
	default:
		return false, fmt.Errorf("%sVISIBILITY %q is not supported (public|private)", prefix, value)
	}
}

func (c *Config) loadFiles() error {
	c.Files = Files{
		Disk:       getString("FILES_DISK", "public"),
		PublicDisk: getString("FILES_PUBLIC_DISK", "public"),
	}
	return nil
}

func (c *Config) validateStorage() error {
	names := strings.Join(c.Storage.DiskNames(), "|")
	if _, ok := c.Storage.Disk(c.Files.Disk); !ok {
		if c.Files.Disk == "s3" {
			return errors.New("FILES_DISK=s3 requires S3_REGION, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY")
		}
		return fmt.Errorf("FILES_DISK %q is not a configured disk (%s)", c.Files.Disk, names)
	}
	public, ok := c.Storage.Disk(c.Files.PublicDisk)
	if !ok {
		return fmt.Errorf("FILES_PUBLIC_DISK %q is not a configured disk (%s)", c.Files.PublicDisk, names)
	}
	if !public.Public {
		return fmt.Errorf("FILES_PUBLIC_DISK %q must be a public disk", c.Files.PublicDisk)
	}
	if _, ok := c.Storage.Disk(c.Processing.Disk); !ok {
		if c.Processing.Disk == "s3" {
			return errors.New("DATA_PROCESSING_DISK=s3 requires S3_REGION, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY")
		}
		return fmt.Errorf("DATA_PROCESSING_DISK %q is not a configured disk (%s)", c.Processing.Disk, names)
	}
	return nil
}
