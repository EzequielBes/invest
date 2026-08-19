module mcp

go 1.23.0

toolchain go1.23.12

require (
	analysis v0.0.0-00010101000000-000000000000
	github.com/jackc/pgx/v5 v5.6.0
	github.com/modelcontextprotocol/go-sdk v1.0.0
	risk-engine v0.0.0
	simulation v0.0.0-00010101000000-000000000000
	strategist v0.0.0-00010101000000-000000000000
)

require (
	execution v0.0.0-00010101000000-000000000000 // indirect
	github.com/anthropics/anthropic-sdk-go v1.9.0 // indirect
	github.com/google/jsonschema-go v0.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/crypto v0.32.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	golang.org/x/time v0.10.0 // indirect
)

replace risk-engine => ../risk-engine

replace analysis => ../analysis

replace strategist => ../strategist

replace simulation => ../simulation

replace execution => ../execution
