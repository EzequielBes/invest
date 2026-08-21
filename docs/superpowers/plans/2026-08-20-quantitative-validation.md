# Plano de Implementação — Validação Quantitativa e Auditoria

**Objetivo:** implementar o módulo `validation` como trilha de pesquisa e
auditoria observacional. Nenhuma task modifica o pipeline de decisão ou
execução.

## Restrições globais

- Go 1.22 e `github.com/jackc/pgx/v5@v5.6.0`, `github.com/google/uuid@v1.6.0`.
- `validation` não importa módulos irmãos; usa SQL puro para leituras de
  `simulation`, `execution` e `tracking`.
- Testes de banco usam `TEST_DATABASE_URL`, fixture IDs únicos e cleanup por
  ID; nunca limpeza por janela de tempo.
- Toda métrica derivada deve carregar unidade/segmento e toda ausência de dado
  necessário deve virar finding, não zero implícito.
- Sem rede externa em testes e sem chamada a Binance/LLM.

## Task 1 — Scaffold, migration e contrato de hipótese

Criar `validation/go.mod`, `docker-compose.yml`, `migrations/001_init.sql`,
`internal/storage/db.go`, `internal/storage/hypotheses.go` e testes.

- A migration cria `validation_hypotheses`, `validation_runs`,
  `validation_splits`, `validation_metrics`, `validation_findings` e
  `validation_attempts`; usar `TEXT` IDs e JSONB para config/evidência.
- `Hypothesis` exige descrição, universo, horizonte, política de custos,
  métricas primárias e regra de disponibilidade temporal.
- `ValidateHypothesis` rejeita contratos incompletos antes de persistir.
- `CreateRun` serializa config canonicamente, calcula SHA-256 e grava status
  inicial `running`.
- Testar campos obrigatórios, hash determinístico e round-trip de persistência.

## Task 2 — Splits temporais e findings de integridade

Criar `internal/validation/splits.go`, `availability.go` e testes puros.

- Tipos `Split{Kind, Start, End, EmbargoMinutes}` e `Finding`.
- Exigir um único holdout final e validar ordem não sobreposta:
  `train < validation < holdout`.
- `ValidateAvailability` recebe timestamp de decisão e timestamps de inputs;
  qualquer input futuro retorna finding `future_data` de severidade `invalid`.
- Persistir splits e findings no storage sem tocar tabelas alheias.

## Task 3 — Métricas de equity e execução

Criar `internal/metrics/equity.go`, `execution.go` e testes unitários.

- `EquityMetrics(points)` calcula drawdown/recovery/time-under-water para
  séries ordenadas, incluindo episódio aberto.
- `SlippageBps(side, requestedPrice, filledPrice)` normaliza buy/sell e
  recusa preço não positivo/lado desconhecido.
- `TurnoverPct(trades, averageEquity)` recusa equity não positivo.
- Casos de série plana, pico-recuperação, drawdown aberto, buy/sell e dados
  inválidos precisam de resultados verificáveis à mão.

## Task 4 — Auditoria de backtest existente

Criar leituras SQL em `internal/storage/backtests.go` e `executions.go`, e
`internal/audit/backtest.go`.

- Ler run, trades e equity curve de `simulation` diretamente.
- Validar que run está completed, tem curva e que custo (`fee_pct`) foi
  declarado; salvar métricas e findings na run de validação.
- Métricas: retorno, drawdown/recovery, turnover e contagem de trades. Não
  recalcular nem alterar `backtest_results`.
- Um backtest inexistente ou incompleto vira run `failed`/`inconclusive` com
  finding persistido.
- Criar integração contra fixtures isoladas no banco compartilhado.

## Task 5 — Auditoria de execução e CLI

Criar `internal/audit/execution.go` e `cmd/validate/main.go`.

- Ler fills reais de `executions` e associar apenas pelo `client_order_id`
  documentado; não assumir P&L, fechamento de posição ou funding.
- Persistir slippage apenas para fills válidos e finding para parcial,
  cancelado ou preço ausente.
- CLI: `-hypothesis-id`, `-backtest-run-id`, `-commit`, `-config-json`; abre
  conexão, valida contrato, executa auditoria e imprime status/findings.
- Falha de banco é fatal; finding de dados é resultado auditável e não derruba
  o processo.

## Task 6 — Web API e dashboard de relatórios

Somente após Task 5, acrescentar leitura ao `web-api` e uma tela de relatório
ao frontend, mantendo ambos read-only.

- `GET /api/validation-runs` e `GET /api/validation-runs/{id}`;
  limites/404 seguem o padrão existente.
- A UI mostra status, hash, métricas e findings; não mostra botões de aprovar,
  executar ou alterar limites.
- Handlers com `httptest`/fake; frontend compila sem dependência nova.

## Task 7 — Verificação final

- Aplicar migration e verificar tabelas.
- `go test ./... -v -count=1`, `go vet ./...`, `go build ./...` em
  `validation`.
- Rodar testes de `web-api` e build do frontend se Task 6 estiver concluída.
- Revisar manualmente que não houve import de `internal/` irmão, endpoint de
  escrita, chamada externa automática ou modificação de schema de outro
  módulo.
