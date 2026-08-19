module mcp

go 1.23.0

toolchain go1.23.12

require (
	github.com/jackc/pgx/v5 v5.6.0
	github.com/modelcontextprotocol/go-sdk v1.0.0
	risk-engine v0.0.0-00010101000000-000000000000
)

require (
	github.com/google/jsonschema-go v0.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace risk-engine => ../risk-engine

replace analysis => ../analysis

replace strategist => ../strategist

replace simulation => ../simulation
