// Example: integrating go-backup with gorilla/mux.
package main

import (
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"os"

	backup "github.com/anchoo2kewl/go-backup"
	"github.com/anchoo2kewl/go-backup/gdrive"
	"github.com/anchoo2kewl/go-backup/pgstore"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func main() {
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	gdriveAuth := gdrive.NewAuth(
		os.Getenv("BACKUP_GOOGLE_CLIENT_ID"),
		os.Getenv("BACKUP_GOOGLE_CLIENT_SECRET"),
		"https://myapp.example.com/api/admin/backup/oauth/callback",
	)
	provider := gdrive.NewProvider(gdriveAuth)

	dumper, err := backup.NewPostgresDumper(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("dumper: %v", err)
	}

	encKey, err := hex.DecodeString(os.Getenv("BACKUP_ENCRYPTION_KEY"))
	if err != nil || len(encKey) != 32 {
		log.Fatalf("BACKUP_ENCRYPTION_KEY must be 64 hex chars (32 bytes). Generate: openssl rand -hex 32")
	}

	manager, err := backup.New(
		backup.WithStore(pgstore.New(db)),
		backup.WithDumper(dumper),
		backup.WithProvider(provider),
		backup.WithBasePath("/api/admin/backup"),
		backup.WithOAuthSuccessRedirect("https://myapp.example.com/admin?tab=backups"),
		backup.WithEncryptionKey(encKey),
	)
	if err != nil {
		log.Fatalf("backup manager: %v", err)
	}
	if err := manager.Start(); err != nil {
		log.Fatalf("backup start: %v", err)
	}
	defer manager.Stop()

	r := mux.NewRouter()
	// Mount backup handler under /api/admin/backup/
	// The trailing slash wildcard ensures all sub-paths are routed.
	r.PathPrefix("/api/admin/backup/").Handler(
		http.StripPrefix("/api/admin/backup", manager.Handler()),
	)
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from my app"))
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
