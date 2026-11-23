package config

import (
	"fmt"
	"strings"
	"time"
)

// Database connections (DATABASE_*, DB_<NAME>_*).

// Database holds the named connection configurations.
type Database struct {
	Primary     Connection
	Connections []Connection
}

// Connection describes one named database connection.
type Connection struct {
	Name        string
	Driver      string
	DSN         string
	Path        string
	MaxConns    int32
	MinConns    int32
	ConnTimeout time.Duration
	// StatementTimeout / IdleInTxTimeout are server-side backstops for code
	// paths without a context deadline (CLI, ad-hoc calls). 0 disables.
	StatementTimeout time.Duration
	IdleInTxTimeout  time.Duration
}

func validateConnection(conn Connection) error {
	switch conn.Driver {
	case "pgsql":
		if conn.DSN == "" {
			return fmt.Errorf("DATABASE_%s_DSN must not be empty for connection %q", envKey(conn.Name), conn.Name)
		}
	case "sqlite":
		return fmt.Errorf("database connection %q: sqlite is not supported (PostgreSQL only in v1; deferred to a future version)", conn.Name)
	default:
		return fmt.Errorf("database connection %q: unsupported driver %q", conn.Name, conn.Driver)
	}
	if conn.MaxConns < 1 {
		return fmt.Errorf("database connection %q: max connections must be at least 1", conn.Name)
	}
	if conn.MinConns < 0 || conn.MinConns > conn.MaxConns {
		return fmt.Errorf("database connection %q: min connections must be between 0 and max connections", conn.Name)
	}
	return nil
}

func (c *Config) loadDatabase() error {
	primary, err := loadConnection("primary", "DATABASE_")
	if err != nil {
		return err
	}
	connections := []Connection{primary}

	for _, name := range splitCSV(getString("DATABASE_CONNECTIONS", "")) {
		if strings.EqualFold(name, "primary") {
			continue
		}
		conn, err := loadConnection(name, "DATABASE_"+envKey(name)+"_")
		if err != nil {
			return err
		}
		connections = append(connections, conn)
	}

	c.Database = Database{Primary: primary, Connections: connections}
	return nil
}

func loadConnection(name, prefix string) (Connection, error) {
	conn := Connection{
		Name:   name,
		Driver: getString(prefix+"DRIVER", "pgsql"),
		DSN:    getString(prefix+"DSN", "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api?sslmode=disable"),
		Path:   getString(prefix+"PATH", ""),
	}

	maxConns, err := getInt(prefix+"MAX_CONNS", 20)
	if err != nil {
		return Connection{}, err
	}
	minConns, err := getInt(prefix+"MIN_CONNS", 2)
	if err != nil {
		return Connection{}, err
	}
	connTimeout, err := getDuration(prefix+"CONN_TIMEOUT", 5*time.Second)
	if err != nil {
		return Connection{}, err
	}
	conn.MaxConns = int32(maxConns)
	conn.MinConns = int32(minConns)
	conn.ConnTimeout = connTimeout
	if conn.StatementTimeout, err = getDuration(prefix+"STATEMENT_TIMEOUT", 5*time.Minute); err != nil {
		return Connection{}, err
	}
	if conn.IdleInTxTimeout, err = getDuration(prefix+"IDLE_IN_TX_TIMEOUT", time.Minute); err != nil {
		return Connection{}, err
	}
	return conn, nil
}

func envKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
