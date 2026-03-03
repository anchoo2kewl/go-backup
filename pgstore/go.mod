module github.com/anchoo2kewl/go-backup/pgstore

go 1.21

require (
	github.com/anchoo2kewl/go-backup v0.1.0
	github.com/lib/pq v1.10.9
)

require github.com/robfig/cron/v3 v3.0.1 // indirect

replace github.com/anchoo2kewl/go-backup => ../
