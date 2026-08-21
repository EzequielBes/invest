module strategist

go 1.22

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	risk-engine v0.0.0-00010101000000-000000000000
)

require (
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/time v0.10.0 // indirect
)

require (
	execution v0.0.0-00010101000000-000000000000
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/crypto v0.32.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

replace risk-engine => ../risk-engine

replace execution => ../execution
