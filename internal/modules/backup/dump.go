package backup

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

type DumpInfo struct {
	Version  string
	Database string
}

type Dumper interface {
	Dump(ctx context.Context, dsn, output string) (DumpInfo, error)
	Version(ctx context.Context) (string, error)
}

type pgDumper struct {
	path string
}

func newPgDumper(path string) *pgDumper {
	if strings.TrimSpace(path) == "" {
		path = "pg_dump"
	}
	return &pgDumper{path: path}
}

var pgVersionPattern = regexp.MustCompile(`\(PostgreSQL\)\s+([0-9][0-9.]*)`)

func (d *pgDumper) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, d.path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pg_dump is not available at %q: %w (install the PostgreSQL client tools or set BACKUP_PG_DUMP_PATH)", d.path, err)
	}
	text := strings.TrimSpace(string(output))
	if match := pgVersionPattern.FindStringSubmatch(text); len(match) == 2 {
		return match[1], nil
	}
	if text == "" {
		return "", fmt.Errorf("pg_dump at %q returned no version output", d.path)
	}
	return text, nil
}

func (d *pgDumper) Dump(ctx context.Context, dsn, output string) (DumpInfo, error) {
	version, err := d.Version(ctx)
	if err != nil {
		return DumpInfo{}, err
	}
	env, database, err := pgEnvFromDSN(dsn)
	if err != nil {
		return DumpInfo{}, err
	}
	if database == "" {
		return DumpInfo{}, fmt.Errorf("backup: DATABASE_DSN does not name a database")
	}

	cmd := exec.CommandContext(ctx, d.path,
		"--format=custom",
		"--compress=9",
		"--no-owner",
		"--no-privileges",
		"--file="+output,
	)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return DumpInfo{}, fmt.Errorf("pg_dump failed: %s", detail)
	}
	return DumpInfo{Version: version, Database: database}, nil
}

func pgEnvFromDSN(dsn string) ([]string, string, error) {
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, "", fmt.Errorf("backup: parse DATABASE_DSN: %w", err)
	}
	sslmode := "prefer"
	if parsed.TLSConfig == nil {
		sslmode = "disable"
	}
	if u, err := url.Parse(dsn); err == nil {
		if value := u.Query().Get("sslmode"); value != "" {
			sslmode = value
		}
	}

	values := [][2]string{
		{"PGHOST", parsed.Host},
		{"PGPORT", fmt.Sprintf("%d", parsed.Port)},
		{"PGUSER", parsed.User},
		{"PGPASSWORD", parsed.Password},
		{"PGDATABASE", parsed.Database},
		{"PGSSLMODE", sslmode},
		{"PGCONNECT_TIMEOUT", "10"},
	}
	if searchPath := parsed.RuntimeParams["search_path"]; searchPath != "" {
		values = append(values, [2]string{"PGOPTIONS", "-c search_path=" + searchPath})
	}
	overrides := make(map[string]bool, len(values))
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, key := range values {
		overrides[key[0]] = true
	}
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && overrides[name] {
			continue
		}
		env = append(env, entry)
	}
	for _, pair := range values {
		env = append(env, pair[0]+"="+pair[1])
	}
	return env, parsed.Database, nil
}
