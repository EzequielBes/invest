# Camada MCP

Status: Aprovado para planejamento de implementação
Data: 2026-08-18
Sub-projeto 6 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o sexto sub-projeto da plataforma de investimentos autônomos. Os
sub-projetos 1-5 (todos concluídos) construíram, nesta ordem: coleta de
dados de mercado (`market-data`), validação mecânica de risco
(`risk-engine`), backtest determinístico (`simulation`), agentes de
análise que geram indicadores + narrativa por domínio (`analysis`), e um
agente estrategista que decide compra/venda/manter e valida contra o
risk-engine (`strategist`).

Até agora, cada uma dessas capacidades só é acionável por um humano
rodando uma CLI (`cmd/market-data`, `cmd/backtest`, `cmd/analysis`,
`cmd/strategist`) ou por um teste de integração chamando a função Go
diretamente. Este sub-projeto expõe as capacidades sob-demanda (análise,
decisão, backtest) e o controle operacional do risk-engine como
**ferramentas MCP** (Model Context Protocol), para que um agente de LLM
(Claude Code, Claude Desktop, ou — a partir do sub-projeto 7 — Codex)
possa acioná-las numa conversa, sem o humano precisar digitar comandos de
CLI manualmente. Não muda a lógica de nenhum módulo existente — só adiciona
uma camada de acesso.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. Agentes de análise *(concluído)*
5. Agente estrategista + motor de decisão *(concluído)*
6. **Camada MCP** ← este documento
7. Integração multi-LLM (Codex + Claude)
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Módulo Go novo** (`mcp/`, `go.mod` próprio), mesmo padrão de estrutura
  dos módulos anteriores (`cmd/`, `internal/`), mas **sem migração** — este
  módulo não persiste nada próprio, é uma ponte fina entre o protocolo MCP
  e as bibliotecas dos módulos existentes.
- **Transporte stdio, uso local**: `cmd/mcp-server` sobe um servidor MCP
  via stdio, para ser configurado como MCP server no Claude Code, Claude
  Desktop, ou Codex CLI rodando na mesma máquina. Sem HTTP, sem
  autenticação, sem exposição de porta — acesso remoto (se algum dia fizer
  sentido) é decisão de um sub-projeto futuro (9, frontend), não deste.
- **SDK oficial do MCP para Go** (`github.com/modelcontextprotocol/go-sdk`,
  pacote `mcp`). A versão exata a pinar (e se exige um Go mais novo que
  1.22, como já aconteceu com o `anthropic-sdk-go` no sub-projeto 4) é
  verificada na hora de escrever/executar o plano — mesma convenção já
  estabelecida: nunca `go get` sem versão explícita depois de descobrir a
  restrição real.
- **Refactor mecânico em três módulos existentes**, pré-requisito para o
  módulo MCP poder chamar cada pipeline como biblioteca Go em vez de
  subprocesso:
  - `analysis`: mover a função `Run` já exportada (hoje em
    `cmd/analysis/main.go`, `package main` — não importável de outro
    módulo) para um pacote público novo `analysis/runner`. `cmd/analysis`
    passa a importar `analysis/runner` e fica só com parsing de flag e
    conexão de banco.
  - `strategist`: mesma operação — `Run` de `cmd/strategist/main.go` vai
    para `strategist/runner`.
  - `simulation`: não tem hoje nenhuma função exportada reaproveitável — a
    orquestração vive dentro do `run()` não-exportado de
    `cmd/backtest/main.go`, que chama `internal/engine.Run` (esse sim já
    existe, mas é `internal/`, não importável de fora do módulo
    `simulation`). Criar `simulation/runner`, pacote público novo,
    envolvendo a mesma validação de flags + construção da
    `MovingAverageCrossStrategy` + chamada a `internal/engine.Run` que já
    existe hoje em `cmd/backtest/main.go`.
  - Em nenhum dos três casos a lógica muda — é mover código de lugar,
    preservando comportamento e assinatura. `risk-engine` não precisa de
    refactor nenhum: `risk`/`storage` já são pacotes públicos, importáveis
    diretamente (é o padrão que este refactor está replicando nos outros
    três módulos).
- **Seis ferramentas MCP**, detalhadas na próxima seção.

### Fora de escopo (explicitamente adiado)

- **Resources e prompts MCP** — só *tools* nesta fase. O protocolo MCP
  também define "resources" (dados navegáveis, ex. expor
  `analysis_results`/`strategist_decisions` recentes como recursos
  consultáveis) e "prompts" (templates reutilizáveis) — nenhum dos dois
  está nesta fase; pode voltar depois se um caso de uso concreto pedir.
- **HTTP/SSE, autenticação, exposição remota** — só stdio local. Ver acima.
- **Agendamento automático de qualquer pipeline via MCP** — as tools rodam
  sob demanda, disparadas pela conversa; nenhum cron/scheduler embutido
  (mesma decisão já tomada em `analysis`/`strategist`/`simulation`).
- **Qualquer mudança de comportamento nos módulos refatorados** — o
  refactor é puramente mecânico (mover `Run`/criar wrapper), não uma
  reescrita. Se o plano de implementação identificar que a lógica movida
  precisa mudar para funcionar como biblioteca, isso é um problema a
  reportar, não a resolver silenciosamente.
- **Escrever em `market-data`** — a tool de preço é só leitura, mesmo
  padrão que `analysis`/`strategist`/`simulation` já usam (cada um com sua
  própria cópia fina de leitura contra `candles`, nunca importando
  `market-data/internal/storage`, que é `internal/`).

## Arquitetura

```text
mcp/
├── go.mod                    (module mcp; require risk-engine, analysis,
│                              strategist, simulation via replace locais)
├── docker-compose.yml        (golang:1.22, rede market-data_default)
├── cmd/
│   └── mcp-server/
│       └── main.go           (monta o servidor MCP via stdio, registra as 6 tools)
└── internal/
    ├── storage/
    │   └── marketdata.go     (leitura fina: candle mais recente — mesmo
    │                          padrão de analysis/strategist/simulation)
    └── tools/
        ├── analysis.go       (tool run_analysis -> analysis/runner.Run)
        ├── strategist.go     (tool run_strategist -> strategist/runner.Run)
        ├── backtest.go       (tool run_backtest -> simulation/runner)
        ├── risk.go           (tools get_risk_state, set_risk_state -> risk-engine/storage)
        └── price.go          (tool get_latest_price -> internal/storage/marketdata.go)
```

Refactors nos módulos existentes (fora da árvore de `mcp/`):

```text
analysis/runner/        (pacote público novo — Run movido de cmd/analysis/main.go)
strategist/runner/      (pacote público novo — Run movido de cmd/strategist/main.go)
simulation/runner/      (pacote público novo — wrapper novo em torno de internal/engine.Run)
```

`mcp` não escreve em nenhuma tabela — cada tool delega a persistência para
o módulo dono (`analysis_runs`/`analysis_results` via `analysis/runner`,
`strategist_decisions` via `strategist/runner`, `backtest_runs`/etc via
`simulation/runner`, `risk_state`/`risk_decisions` via
`risk-engine/storage` diretamente). O único dado que `mcp` lê por conta
própria é `candles`, para a tool de preço.

## Ferramentas MCP

Cada tool recebe parâmetros nomeados (schema JSON gerado pelo SDK a partir
dos tipos Go) e devolve um resultado estruturado — nunca texto livre
formatado à mão, para o agente de LLM que chama a tool poder ler o
resultado de forma confiável.

1. **`run_analysis`** — parâmetros: `assets` (lista de símbolos,
   obrigatório), `timeframe` (default `"1h"`), `agents` (lista, default
   todos os quatro), `asset_names` (mapa símbolo→nome, opcional, mesmo uso
   do sub-projeto 4). Chama `analysis/runner.Run`. Retorna
   `analysis_run_id` e, por ativo/agente, a narrativa e os indicadores
   principais.
2. **`run_strategist`** — parâmetros: `analysis_run_id` (obrigatório),
   `assets` (obrigatório), `timeframe` (default `"1h"`), `cash`
   (obrigatório), `positions` (mapa símbolo→quantidade, opcional),
   `daily_loss`/`weekly_loss`/`drawdown` (frações, default 0),
   `consecutive_losses` (default 0) — mesmos campos manuais de portfólio
   do sub-projeto 5, mesmo caráter de stopgap até o sub-projeto 8. Chama
   `strategist/runner.Run`. Retorna, por ativo, a decisão (side,
   confidence, sizing, rationale) e o veredito do risk-engine (aprovada,
   rejeitada com motivos, ou `null` se `risk.Evaluate` falhou/não foi
   chamado por ser `hold`).
3. **`run_backtest`** — parâmetros: `period_start`/`period_end` (RFC3339,
   obrigatórios), `timeframes` (lista, obrigatório), `driving_timeframe`
   (obrigatório), `assets` (obrigatório), `initial_cash` (default 10000),
   `fee_pct` (default 0.001), `ma_short_period`/`ma_long_period` (defaults
   10/30) — mesmos parâmetros da CLI `cmd/backtest`, a única estratégia
   disponível continua sendo `MovingAverageCrossStrategy` (nenhuma tool
   nova de estratégia nesta fase). Chama `simulation/runner`. Retorna
   `backtest_run_id` e as métricas finais (retorno, drawdown, Sharpe,
   Sortino, win rate, contagem de trades).
4. **`get_risk_state`** — sem parâmetros. Chama
   `risk-engine/storage.GetState(ctx, nil)` +
   `risk-engine/storage.GetLimits(ctx)` (sempre a linha ao vivo,
   `run_id IS NULL` — nunca estado de um backtest). Retorna status, motivo,
   data da última mudança, e todos os limites configurados.
5. **`set_risk_state`** — parâmetros: `status` (enum
   `normal`/`paused`/`kill_switch`, obrigatório), `reason` (obrigatório).
   Chama `risk-engine/storage.SetState(ctx, nil, status, reason)`
   diretamente — sem tool separada para "resetar", já que
   `storage.Reset` já é, no próprio código do risk-engine, só
   `SetState(..., StatusNormal, reason)`. Retorna o novo estado
   confirmado (releitura via `GetState` após o `SetState`).
6. **`get_latest_price`** — parâmetros: `asset` (obrigatório), `timeframe`
   (default `"1h"`), `exchange` (default `risk.ReferenceExchange`, hoje
   `"binance"`). Lê `candles` diretamente (leitura fina própria, mesmo
   padrão dos outros módulos). Retorna o preço de fechamento do candle mais
   recente, ou "não encontrado" se não houver dado coletado para aquele
   ativo/timeframe/exchange.

## Tratamento de erros

- **Erro de validação de parâmetros** (ex. `run_strategist` sem `cash`,
  `set_risk_state` com `status` fora do enum): a tool retorna um erro MCP
  claro antes de chamar qualquer biblioteca — mesma responsabilidade que
  cada CLI já tem hoje para suas próprias flags, só que validado pelo
  schema/tipo Go em vez de parsing de string de flag.
- **Erro dentro de um `runner.Run`** (ex. banco indisponível, LLM falhou
  para todos os ativos): propagado como erro da tool MCP, com a mensagem
  de erro original do módulo — a camada MCP não interpreta nem esconde o
  erro, só repassa.
- **Falha parcial dentro de uma execução** (ex. um ativo específico falhou
  em `run_analysis` mas outros tiveram sucesso): não é um erro da tool —
  cada `runner.Run` já trata isso internamente (política já estabelecida
  nos sub-projetos 4/5: falha isolada não aborta a execução inteira) e o
  resultado estruturado da tool reflete isso por ativo.
- **`set_risk_state` para `kill_switch`**: nenhuma confirmação adicional
  além da que o próprio cliente MCP (Claude Code, Claude Desktop) já pede
  antes de executar uma tool — este sub-projeto não adiciona uma segunda
  camada de confirmação própria.

## Testes

Mesma política de rigor reduzido, com a mesma divisão de trabalho dos
sub-projetos 4-5: testes diretos nesta implementação só para lógica com
risco real de erro silencioso (montagem dos parâmetros de cada tool,
conversão do resultado de cada `runner.Run` para o formato de retorno da
tool, os três refactors preservando o comportamento das funções movidas —
o jeito mais direto de garantir isso é reexecutar os testes de integração
já existentes de `analysis`/`strategist` após mover `Run`, já que eles
exercitam a função pelo nome exportado). Checklist de gaps de cobertura
mais pontuais, no mesmo formato dos sub-projetos 4/5, para o Codex escrever
como TDD depois.

Sem teste de integração real do protocolo MCP em si (conectar um cliente
MCP de verdade) — o SDK oficial é a fronteira confiável para o transporte;
os testes deste sub-projeto validam a lógica de cada tool chamando as
funções Go diretamente, não via stdio/JSON-RPC.

## Critério de conclusão desta fase

A camada MCP está pronta quando:

- `analysis/runner.Run`, `strategist/runner.Run`, e `simulation/runner`
  existem como pacotes públicos, `cmd/analysis`/`cmd/strategist`/
  `cmd/backtest` continuam funcionando exatamente como antes (mesmos
  testes de integração existentes passando sem alteração de expectativa),
  e cada um importa o pacote novo em vez de conter a lógica movida.
- Um servidor MCP (`cmd/mcp-server`) sobe via stdio e expõe as seis tools
  listadas, cada uma com schema de parâmetros tipado (não texto livre).
- `run_analysis`/`run_strategist`/`run_backtest` produzem exatamente os
  mesmos efeitos (linhas no banco, comportamento) que rodar a CLI
  equivalente — a tool é uma chamada de biblioteca, não uma reimplementação.
- `set_risk_state`/`get_risk_state` operam sempre sobre a linha ao vivo do
  risk-engine (`run_id IS NULL`), nunca sobre o estado de um backtest.
