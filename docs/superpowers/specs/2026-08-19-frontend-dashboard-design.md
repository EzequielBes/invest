# Frontend (Dashboard de Acompanhamento)

Status: Aprovado para planejamento de implementação
Data: 2026-08-19
Sub-projeto 9 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o nono sub-projeto da plataforma de investimentos autônomos. Hoje
não existe nenhuma forma visual de acompanhar o que o sistema está
fazendo — só CLIs por módulo, consultas SQL manuais, e a camada MCP
(protocolo pensado para um cliente LLM, não para um navegador). Este
sub-projeto adiciona um dashboard web read-only: decisões do
strategist e suas execuções reais (testnet), estado de risco atual,
runs de análise, e resultados de backtest.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. Agentes de análise *(concluído)*
5. Agente estrategista + motor de decisão *(concluído)*
6. Camada MCP *(concluído)*
7. Integração multi-LLM (Codex + Claude) *(concluído)*
8. Ambiente real (execução em exchanges/corretoras) *(concluído)*
9. **Frontend** ← este documento
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Read-only.** Nenhuma ação de escrita pela UI nesta fase (não dispara
  `run_analysis`/`run_strategist`/`run_backtest`, não altera
  `risk_state`) — só visualização do que já foi persistido pelos outros
  módulos.
- **Novo módulo Go `web-api`**, seguindo a convenção já estabelecida do
  repositório (cada capacidade é seu próprio módulo). Expõe uma API REST
  simples (`net/http` da stdlib, sem framework — não há necessidade para
  o volume de endpoints desta fase) que lê diretamente das tabelas já
  existentes de `analysis`, `strategist`, `risk-engine`, `execution` e
  `simulation`, via SQL puro contra o mesmo Postgres compartilhado — sem
  nenhum import Go entre módulos. Este é o mesmo padrão já usado por
  `strategist/internal/storage.LatestPrice` (lê a tabela `candles` do
  módulo `market-data` diretamente via SQL, sem depender do módulo Go
  `market-data`); aqui não há nem sequer uma necessidade de orquestração
  como o `RunWithDSN` do `mcp` — é puramente leitura de tabelas que já
  existem, então não há problema de visibilidade de `internal/` a
  contornar.
- **Frontend em React + Vite**, uma SPA simples consumindo a API do
  `web-api`. Quatro telas: decisões do strategist + execuções, estado de
  risco atual, runs de análise, resultados de backtest. Atualização por
  polling simples (intervalo fixo, sem websocket) — suficiente para um
  dashboard de acompanhamento pessoal, sem necessidade de tempo real
  nesta fase.
- **Sem autenticação** — uso pessoal local, mesma política de segurança
  (ausência dela) já implícita em todo o resto do repositório, que roda
  inteiramente local/self-hosted.
- **Endpoints da API** (todos `GET`, todos paginados por `?limit=N`,
  default a definir no plano):
  - `GET /api/decisions` — decisões recentes do `strategist_decisions`
    (side, confidence, sizing_pct, rationale, proposed_quantity/value,
    risk_allowed, risk_reasons, execution_status, execution_order_id,
    execution_filled_quantity, execution_filled_price, created_at).
  - `GET /api/risk-state` — a linha *live* de `risk_state` (`run_id IS
    NULL`) junto com `risk_limits` (para dar contexto: valor atual vs.
    limite configurado).
  - `GET /api/analysis-runs` — runs recentes de `analysis_runs`.
  - `GET /api/analysis-runs/{id}` — uma run específica com todos os
    `analysis_results` associados (indicadores + narrativa por agente/
    ativo).
  - `GET /api/backtests` — runs recentes de `backtest_runs` (com
    `backtest_results` quando `status = 'completed'`).
  - `GET /api/backtests/{id}` — uma run específica com `backtest_trades`
    e `backtest_equity_curve` completos.
- **Em desenvolvimento**: Vite faz proxy de `/api/*` para o `web-api`
  (evita CORS). **Em uso local "de produção"**: o `web-api` também serve
  os arquivos estáticos do build do frontend (`dist/`) na raiz, um único
  processo servindo tudo.

### Fora de escopo (explicitamente adiado)

- **Qualquer ação de escrita pela UI** (disparar runs, ajustar risk_state,
  aprovar/rejeitar decisões) — cogitado no brainstorm, descartado para
  esta fase: exigiria uma camada de API com endpoints de escrita e
  validação adicional, mais apropriado para uma fase futura se necessário.
- **Autenticação/autorização** — uso pessoal local, sem necessidade
  nesta fase.
- **Tempo real (websockets/SSE)** — polling simples é suficiente para um
  dashboard de acompanhamento pessoal.
- **Qualquer mudança em `mcp` ou nos módulos existentes** — `web-api` só
  lê tabelas já existentes; nenhuma migração nova, nenhum módulo
  existente precisa de alteração.
- **Deploy real / hospedagem externa** — escopo é rodar localmente via
  Docker Compose, como todo o resto do repositório.
- **Gráficos avançados** (candlestick, indicadores técnicos plotados) —
  a tela de backtest pode mostrar a curva de equity como um gráfico de
  linha simples, mas nada além disso nesta fase.

## Arquitetura

```text
web-api/                         (novo módulo Go)
├── internal/storage/
│   ├── db.go                    # pgxpool.New(dsn), mesmo padrão de todo módulo
│   ├── decisions.go             # RecentDecisions(ctx, limit) — lê strategist_decisions
│   ├── riskstate.go             # LiveRiskState(ctx) — lê risk_state (run_id IS NULL) + risk_limits
│   ├── analysis.go              # RecentAnalysisRuns(ctx, limit), AnalysisRunDetail(ctx, id)
│   └── backtests.go             # RecentBacktests(ctx, limit), BacktestDetail(ctx, id)
├── internal/api/
│   ├── server.go                # mux, middleware básico (log, JSON content-type)
│   ├── decisions.go             # handler GET /api/decisions
│   ├── riskstate.go             # handler GET /api/risk-state
│   ├── analysis.go              # handlers GET /api/analysis-runs[/{id}]
│   └── backtests.go             # handlers GET /api/backtests[/{id}]
├── cmd/web-api/main.go          # lê DATABASE_URL, monta o server, serve estáticos do frontend
└── docker-compose.yml

frontend/                        (React + Vite, SPA)
├── src/
│   ├── api/client.ts            # fetch wrapper pros 6 endpoints
│   ├── pages/
│   │   ├── DecisionsPage.tsx
│   │   ├── RiskStatePage.tsx
│   │   ├── AnalysisRunsPage.tsx
│   │   └── BacktestsPage.tsx
│   ├── App.tsx                  # roteamento simples entre as 4 telas
│   └── main.tsx
├── vite.config.ts               # proxy /api → http://localhost:8080 em dev
└── package.json
```

`web-api` não importa nenhum outro módulo Go do repositório — só
`github.com/jackc/pgx/v5` (mesma dependência já usada por todo módulo
que fala com Postgres). Isso evita qualquer risco de repetir o problema
já visto no sub-projeto 7 (mudança de dependência transitiva quebrando
`mcp`): `web-api` é uma folha na árvore de módulos, ninguém depende dele,
e ele não depende de código Go de ninguém — só do schema do banco, que
já é compartilhado e estável.

## Tratamento de erros

- Erro de conexão com o banco na inicialização do `web-api`: fatal,
  processo não sobe (mesmo padrão de todo `cmd/*` existente).
- Erro de query dentro de um handler: HTTP 500 com um corpo JSON
  `{"error": "..."}`, nunca vaza detalhes internos de SQL na mensagem
  (mensagem genérica, log do erro completo no servidor).
- ID inexistente em `/api/analysis-runs/{id}` ou `/api/backtests/{id}`:
  HTTP 404.
- Frontend: falha de rede/erro da API mostra uma mensagem de erro simples
  na tela correspondente, sem quebrar as outras telas.

## Testes

Mesma política de rigor reduzido dos sub-projetos 4-10: teste direto nas
queries SQL de `internal/storage` (contra `TEST_DATABASE_URL`, mesmo
padrão de todo módulo) e nos handlers HTTP de `internal/api` (via
`httptest`, com um storage fake/interface). Sem teste end-to-end do
frontend nesta fase — validação manual (abrir o dashboard, conferir que
os dados batem com o banco) é suficiente para um dashboard read-only
pessoal.

## Critério de conclusão desta fase

O frontend está pronto quando:

- `web-api` roda localmente, lê `DATABASE_URL` do ambiente, e expõe os 6
  endpoints listados acima, todos retornando dados reais do Postgres
  compartilhado.
- O frontend (build de produção, servido pelo próprio `web-api`) mostra
  as 4 telas com dados reais: decisões + execuções, estado de risco
  atual, runs de análise, resultados de backtest.
- Em modo de desenvolvimento (`npm run dev` no frontend + `web-api`
  rodando em paralelo), o proxy do Vite funciona sem erro de CORS.
- Um ID inexistente em `/api/analysis-runs/{id}` ou `/api/backtests/{id}`
  retorna 404, não 500 nem uma página em branco.
