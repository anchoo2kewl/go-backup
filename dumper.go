package backup

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// DatabaseDumper streams a database backup to an io.Writer.
type DatabaseDumper interface {
	Dump(ctx context.Context, w io.Writer) error
	DatabaseName() string
}

// PostgresDumper uses pg_dump to stream a compressed backup.
type PostgresDumper struct {
	host   string
	port   string
	user   string
	dbname string
	pass   string
}

// NewPostgresDumper parses a DATABASE_URL and returns a PostgresDumper.
// Supports both URL format (postgres://user:pass@host:port/dbname) and
// key=value format (host=x port=5432 user=x password=x dbname=x).
func NewPostgresDumper(databaseURL string) (*PostgresDumper, error) {
	// Detect key=value format (contains "dbname=" or "host=" without "://")
	if !strings.Contains(databaseURL, "://") && (strings.Contains(databaseURL, "dbname=") || strings.Contains(databaseURL, "host=")) {
		return parseKeyValueDSN(databaseURL)
	}

	u, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("backup: invalid DATABASE_URL: %w", err)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	dbname := strings.TrimPrefix(u.Path, "/")
	return &PostgresDumper{
		host:   host,
		port:   port,
		user:   user,
		dbname: dbname,
		pass:   pass,
	}, nil
}

// parseKeyValueDSN parses "host=x port=5432 user=x password=x dbname=x sslmode=disable"
func parseKeyValueDSN(dsn string) (*PostgresDumper, error) {
	d := &PostgresDumper{port: "5432"}
	for _, part := range strings.Fields(dsn) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "host":
			d.host = kv[1]
		case "port":
			d.port = kv[1]
		case "user":
			d.user = kv[1]
		case "password":
			d.pass = kv[1]
		case "dbname":
			d.dbname = kv[1]
		}
	}
	if d.dbname == "" {
		return nil, fmt.Errorf("backup: key=value DSN missing dbname")
	}
	return d, nil
}

func (d *PostgresDumper) DatabaseName() string { return d.dbname }

// Dump runs pg_dump --format=custom and gzips the output into w.
func (d *PostgresDumper) Dump(ctx context.Context, w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()

	args := []string{
		"--host=" + d.host,
		"--port=" + d.port,
		"--username=" + d.user,
		"--dbname=" + d.dbname,
		"--format=custom",
		"--no-password",
	}

	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	if d.pass != "" {
		cmd.Env = append(cmd.Env, "PGPASSWORD="+d.pass)
	}
	// Inherit current process env (needed for PATH etc.) then add PGPASSWORD
	cmd.Env = append(inheritEnv(), "PGPASSWORD="+d.pass)
	cmd.Stdout = gz
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w: %s", err, stderr.String())
	}
	return gz.Close()
}

// BackupFilename generates a timestamped filename for a backup.
func BackupFilename(dbname string, t time.Time) string {
	return fmt.Sprintf("%s_%s.dump.gz", dbname, t.UTC().Format("2006-01-02_15-04-05"))
}

func inheritEnv() []string {
	env := make([]string, 0, 10)
	for _, key := range []string{"PATH", "HOME", "USER", "TMPDIR"} {
		if v := envLookup(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

func envLookup(key string) string {
	// use os.Getenv inline to avoid import loop with testing
	return getEnvVar(key)
}
