# MCP Automation Operations

The host loop invokes a subscription-backed Claude Code or Codex CLI every 15
minutes. It uses the configured local `investment-platform` MCP server only.
Paper and Binance Futures testnet are the only execution targets; both start
disabled.

## Setup

Apply the staged-run and automation migrations after the existing module
migrations. From the repository root:

```sh
docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < analysis/migrations/003_staged_runs.sql
docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < execution/migrations/003_automation_controls.sql
docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < execution/migrations/004_paper_equity_snapshots.sql
```

Build the MCP server and host loop:

```sh
mkdir -p ~/.local/bin
(cd mcp && go build -o ~/.local/bin/investment-mcp ./cmd/mcp-server)
(cd execution && go build -o agent-loop ./cmd/agent-loop)
```

Register the same stdio MCP server in **both** Claude Code and Codex, even if
only one is currently active. Each registration must run
`~/.local/bin/investment-mcp` with `DATABASE_URL` set to the local database
DSN. The host loop chooses a CLI with `active_agent`; it does not configure MCP
for that CLI. Confirm each client lists the `investment-platform` server before
enabling its gate.

Create the protected environment file from `ops/invest-agent-loop.env.example`:

```sh
install -d -m 700 ~/.config
install -m 600 ops/invest-agent-loop.env.example ~/.config/invest-agent-loop.env
```

Set `DATABASE_URL`, `AUTOMATION_ASSETS`, `CLAUDE_CODE_BIN`, and `CODEX_BIN`.
Set `BINANCE_API_KEY` and `BINANCE_API_SECRET` only when enabling testnet. The
default interval is `15m`; `AGENT_TIMEOUT` defaults to `10m`.

Install and start the user service:

```sh
mkdir -p ~/.config/systemd/user
cp ops/invest-agent-loop.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now invest-agent-loop.service
journalctl --user -u invest-agent-loop.service -f
```

The unit assumes this checkout is at `~/documents/farmdinheiro/invest`. Update
the installed unit's `WorkingDirectory` and `ExecStart` if it is elsewhere.

## Controls

Use the Ao Vivo dashboard or the MCP tools `get_automation_controls` and
`set_automation_controls`.

- `paper_enabled`: permits paper execution.
- `testnet_enabled`: permits Binance Futures testnet execution and requires
  Binance testnet credentials.
- `active_agent`: `claude_code` or `codex`; both CLI MCP registrations remain
  required.

With both gates off, each scheduled cycle exits without starting an agent.
With one or both gates on, the selected agent runs the staged workflow:
one `prepare_analysis` call, `get_analysis_context` for that pending run, ordered narrative submissions, committee submission,
`prepare_strategy`, then `apply_strategy_intents` for exactly the enabled
targets. It never enables a target itself.

## Manual Smoke Test

1. Verify both CLI MCP registrations by calling `get_automation_controls` from
   each CLI; both should reach the same database state.
2. Leave `testnet_enabled` false. Set `active_agent` to the client being
   tested and set `paper_enabled` true through the dashboard or
   `set_automation_controls`.
3. Restart `invest-agent-loop.service`, then watch
   `journalctl --user -u invest-agent-loop.service -f` for the immediate cycle.
   Confirm a completed staged analysis and paper decision/history in the
   dashboard.
4. Set `paper_enabled` false after the check. Enable `testnet_enabled` only
   after verifying testnet credentials and intentionally accepting a testnet
   order.

## Limits

- The executor is pinned to Binance Futures testnet; real-money execution is
  not implemented.
- Paper daily/weekly loss and drawdown use equity snapshots. Consecutive paper
  losses remain unavailable because fills have no cost basis, so realized P&L
  cannot be derived.
