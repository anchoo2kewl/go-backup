module github.com/anchoo2kewl/go-backup/gdrive

go 1.23.0

require (
	github.com/anchoo2kewl/go-backup v0.1.0
	golang.org/x/oauth2 v0.27.0
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
)

replace github.com/anchoo2kewl/go-backup => ../
