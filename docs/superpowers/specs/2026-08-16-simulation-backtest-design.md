# Ambiente de Simulação / Backtest

Status: Aprovado para planejamento de implementação
Data: 2026-08-16
Sub-projeto 3 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o terceiro sub-projeto da plataforma de investimentos autônomos.
O sub-projeto 1 (Fundação de Dados de Mercado, concluído) coleta
candles, funding rate, open interest, liquidações e notícias de
Binance, Bybit e OKX em um TimescaleDB compartilhado. O sub-projeto 2
(Motor de Controle de Risco, concluído) é o módulo Go `risk-engine`
(pacotes públicos `risk` e `storage`), que valida uma operação
proposta contra limites de concentração, perdas e qualidade de dado,
consultando o mesmo TimescaleDB — mas não decide *o que* comprar ou
vender, e não tinha, até agora, nenhum consumidor real além dos seus
próprios testes.

Este sub-projeto constrói o **ambiente de simulação/backtest**: a peça
que roda uma estratégia contra dados históricos reais, simula a
execução de ordens e evolui uma carteira simulada — chamando o
`risk-engine` de verdade antes de cada operação simulada, exatamente
como o sub-projeto 8 (ambiente real) vai fazer depois. É a primeira
vez que o `risk-engine` é exercitado ponta a ponta por um consumidor
externo, e o objetivo explícito é validar o próprio motor de risco em
condições realistas antes de haver dinheiro de verdade envolvido.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. **Ambiente de simulação / backtest** ← este documento
4. Agentes de análise
5. Agente estrategista + motor de decisão
6. Camada MCP
7. Integração multi-LLM (Codex + Claude)
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Módulo Go separado** (`simulation/`, `go.mod` próprio), no mesmo
  repositório, seguindo a mesma estrutura dos sub-projetos anteriores
  (`cmd/`, `internal/`, `migrations/`). Depende do módulo `risk-engine`
  via `replace` local no `go.mod` (não publicado em nenhum registro).
  Lê as tabelas de mercado do `market-data` e chama a API pública do
  `risk-engine`; escreve apenas nas suas próprias tabelas.
- **Interface `Strategy`** como mecanismo principal de decisão nesta
  fase (os sub-projetos 4/5 vão implementá-la de verdade depois). Uma
  implementação de exemplo (`FixedOperationsStrategy`, que repete uma
  lista pré-definida de operações) cobre o caso de replay e serve de
  base para os testes de integração deste próprio sub-projeto. Uma ou
  duas estratégias simples adicionais (ex. média móvel) validam o
  laço de ponta a ponta com uma decisão de verdade, não só replay.
- **Multi-timeframe simultâneo**: o relógio da simulação avança no
  timeframe mais fino entre os configurados para aquela execução (o
  "timeframe condutor"); a cada passo, a `Strategy` recebe acesso a
  candles de todos os timeframes configurados, cortados no instante
  simulado atual (sem dado futuro).
- **Execução na abertura do próximo candle** do timeframe condutor
  (nunca no candle que gerou o sinal — evita viés de lookahead), com
  taxa percentual configurável por execução, aplicada sobre o valor de
  cada operação.
- **Escopo de ativos igual ao sub-projeto 1** (qualquer ativo/exchange
  já coletado está disponível; carteiras multi-ativo).
- **Estado da carteira simulada é persistido** no TimescaleDB
  (posições, caixa, histórico de trades, curva de patrimônio) — ao
  contrário do `risk-engine`, que recebe isso como entrada, aqui é
  este sub-projeto que rastreia e persiste de verdade, estabelecendo o
  padrão que o sub-projeto 8 também vai seguir.
- **Extensão retrocompatível do `risk-engine`**: parâmetro `AsOf`
  (recorte de tempo, para as consultas de qualidade de dado não verem
  candles "do futuro" em relação ao instante simulado) e isolamento de
  estado por `RunID` (para `risk_state`/`risk_decisions` de uma
  execução de backtest não colidirem entre execuções nem com a
  operação ao vivo). Ambos opcionais — `nil`/ausente preserva o
  comportamento atual do `risk-engine` para uso ao vivo.
- **Métricas completas ao final de cada execução**: retorno total,
  drawdown máximo, número de trades, win rate, retorno médio por
  trade, Sharpe ratio, Sortino ratio, volatilidade anualizada.
- **Execução via binário CLI** (`cmd/backtest`), recebendo período,
  timeframes, ativos, estratégia, capital inicial e taxa via flags —
  mesmo padrão do `cmd/market-data` do sub-projeto 1.

### Fora de escopo (explicitamente adiado)

- Comparação de múltiplas estratégias numa única execução (cada
  execução testa uma `Strategy`; comparar resultados entre execuções é
  responsabilidade de quem lê `backtest_results` depois — futuramente
  o frontend, sub-projeto 9).
- Retomar uma execução de backtest interrompida — cada execução é
  stateless do início ao fim; se falhar, roda de novo.
- Qualquer forma de execução ao vivo ou paper trading contra exchanges
  reais — isso é o sub-projeto 8. Este sub-projeto só repete o
  passado.
- Qualquer API externa (HTTP, MCP) para disparar backtests — só CLI
  nesta fase, mesma decisão já tomada nos sub-projetos 1 e 2.
- Mudar `risk_limits` por execução de backtest — cada execução testa
  contra os limites reais atualmente configurados (tabela
  compartilhada, sem mudança de schema aqui). Testar hipóteses de
  configuração de limites diferentes fica para uma fase futura, se
  vier a ser necessário.

## Arquitetura

```text
simulation/
├── go.mod                  (module simulation; require risk-engine, replace ../risk-engine)
├── cmd/
│   └── backtest/
│       └── main.go         (CLI: período, timeframes, ativos, taxa, capital inicial, estratégia)
├── internal/
│   ├── engine/
│   │   ├── clock.go        (avança candle a candle no timeframe condutor)
│   │   ├── run.go          (orquestra: mark-to-market → Strategy.Decide → risk.Evaluate → fill)
│   │   └── fill.go         (aplica execução na abertura do próximo candle + taxa)
│   ├── strategy/
│   │   ├── strategy.go     (interface Strategy, FixedOperationsStrategy)
│   │   └── examples/       (1-2 estratégias simples de exemplo, ex. média móvel)
│   ├── marketview/
│   │   └── marketview.go   (acesso multi-timeframe "as of" instante simulado)
│   ├── portfolio/
│   │   └── portfolio.go    (estado da carteira simulada: posições, caixa, perdas, drawdown)
│   ├── metrics/
│   │   └── metrics.go      (retorno, drawdown, win rate, Sharpe, Sortino, volatilidade)
│   └── storage/
│       └── runs.go         (persiste backtest_runs, backtest_trades, backtest_equity_curve, backtest_results)
└── migrations/
    └── 001_init.sql

risk-engine/
├── migrations/
│   └── 002_run_scoped_state.sql   (adiciona run_id a risk_state e risk_decisions)
├── risk/
│   ├── evaluate.go          (Evaluate ganha EvalOptions{AsOf, RunID}, opcional)
│   └── quality.go           (checks passam a aceitar AsOf)
└── storage/
    ├── state.go              (GetState/SetState/SetStateIfNormal/Reset ganham runID *uuid.UUID)
    └── marketdata.go          (LatestCandle/RecentCandles ganham asOf *time.Time)
```

`simulation` só lê as tabelas do `market-data` (candles) e escreve
apenas nas suas próprias quatro tabelas novas. A extensão do
`risk-engine` é feita nesse próprio módulo (nova migração, nova versão
das assinaturas existentes) — não duplicada dentro de `simulation`.

## Modelo de dados

**Tabelas novas, de propriedade do `simulation`:**

| Tabela | Colunas principais | Observação |
|---|---|---|
| `backtest_runs` | `id UUID PK, strategy_name TEXT, period_start TIMESTAMPTZ, period_end TIMESTAMPTZ, timeframes TEXT[], driving_timeframe TEXT, initial_cash DOUBLE PRECISION, fee_pct DOUBLE PRECISION, status TEXT, error TEXT, started_at TIMESTAMPTZ, ended_at TIMESTAMPTZ` | Uma linha por execução; `status IN ('running','completed','failed')` |
| `backtest_trades` | `id BIGSERIAL PK, run_id UUID FK, ts TIMESTAMPTZ, asset TEXT, side TEXT, quantity DOUBLE PRECISION, price DOUBLE PRECISION, fee DOUBLE PRECISION, allowed BOOLEAN, reject_reason TEXT` | Toda operação proposta pela `Strategy`, aprovada ou não |
| `backtest_equity_curve` | `id BIGSERIAL PK, run_id UUID FK, ts TIMESTAMPTZ, cash DOUBLE PRECISION, positions_value DOUBLE PRECISION, total_equity DOUBLE PRECISION` | Uma linha por candle do timeframe condutor |
| `backtest_results` | `run_id UUID PK FK, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio, annualized_volatility_pct, win_rate_pct DOUBLE PRECISION, total_trades INT, avg_trade_pct DOUBLE PRECISION` | Métricas finais, gravadas quando `status='completed'` |

`id UUID PRIMARY KEY DEFAULT gen_random_uuid()` em `backtest_runs`
(requer `CREATE EXTENSION IF NOT EXISTS pgcrypto` na migração, se ainda
não habilitada no TimescaleDB compartilhado).

**Extensão no `risk-engine`** (`migrations/002_run_scoped_state.sql`):

- `risk_state`: adiciona `run_id UUID NULL`; remove a constraint
  `CHECK (id=1)`; `id` passa a ser preenchido por
  `gen_random_uuid()` para linhas de backtest, mantendo `id=1` fixo
  só para a linha ao vivo (`run_id IS NULL`). Índice único parcial
  garante uma linha só por `run_id` não-nulo, e uma linha só com
  `run_id IS NULL`.
- `risk_decisions`: adiciona `run_id UUID NULL` (sem mudança de
  constraint — já é append-only). `NULL` = decisão de operação ao
  vivo; preenchido = decisão de uma execução de backtest específica.
- `risk_limits` não muda.

## Lógica de simulação

**Interfaces centrais.** `Strategy` e `MarketView` são declaradas em
`strategy/strategy.go` (o pacote que as consome); `marketview/` fornece
a implementação concreta de `MarketView` (satisfação estrutural, sem
`strategy` importar `marketview`); `PortfolioSnapshot` é declarada em
`portfolio/portfolio.go` e importada por `strategy`:

```go
// strategy/strategy.go
type Strategy interface {
    Decide(ctx context.Context, view MarketView, portfolio portfolio.Snapshot) ([]risk.ProposedOperation, error)
}

type MarketView interface {
    // Candles fechados até o instante simulado atual (mais recente por último).
    // tf é um dos timeframes configurados para a execução.
    Candles(ctx context.Context, tf, asset string, n int) ([]storage.Candle, error)
    Now() time.Time
}

// portfolio/portfolio.go
type Snapshot struct {
    Positions         map[string]risk.Position
    Cash              float64
    DailyLoss         float64
    WeeklyLoss        float64
    Drawdown          float64
    ConsecutiveLosses int
}
```

`FixedOperationsStrategy` (lista pré-definida de
`{ts, asset, side, quantity}`) implementa `Strategy`: a cada chamada de
`Decide`, devolve as operações cujo `ts` caiu dentro da janela do
candle atual do timeframe condutor. Cobre o caso de replay sem exigir
um mecanismo separado do resto do motor.

**O laço** (`engine/run.go`), por candle do timeframe condutor, do
início ao fim do período configurado:

1. Avança o relógio simulado para o **fechamento** desse candle
   (`ts_abertura + duração(timeframe)`).
2. Marca a carteira a mercado com o `close` desse candle por ativo,
   grava uma linha em `backtest_equity_curve` e recalcula `Drawdown`
   (pico de `total_equity` observado até agora vs. atual).
3. Executa qualquer fill pendente da rodada anterior, ao preço de
   **abertura** deste candle (não do candle que gerou o sinal),
   debitando `fee_pct` sobre o valor da operação; grava/atualiza a
   linha correspondente em `backtest_trades`.
4. Chama `Strategy.Decide` com o `MarketView` (todos os timeframes
   configurados, cortados no fechamento deste candle) e o
   `PortfolioSnapshot` atual.
5. Para cada operação proposta, chama
   `risk.Evaluate(ctx, store, portfolio, proposed, risk.EvalOptions{AsOf: &simTime, RunID: &runID})`.
   Grava uma linha em `backtest_trades` de qualquer forma (aprovada ou
   não, com `reject_reason` preenchido se rejeitada). Se aprovada,
   enfileira o fill para o passo 3 da próxima iteração.
6. Repete até `ts_fechamento > period_end`.
7. Ao final: `status='completed'`, calcula e grava `backtest_results`
   a partir de `backtest_equity_curve` e `backtest_trades`.

**Cálculo das métricas de perda que alimentam o `PortfolioSnapshot`**
(feito pelo pacote `portfolio`, a cada passo do laço — exatamente como
a spec do `risk-engine` já previa que fossem fornecidas pelo
chamador):

- `DailyLoss` / `WeeklyLoss`: variação percentual do `total_equity`
  desde o primeiro registro de `backtest_equity_curve` daquele dia/
  semana UTC corrente até o valor atual (perda = número positivo
  quando o equity caiu).
- `Drawdown`: `(pico_equity_até_agora - equity_atual) / pico_equity_até_agora`,
  nunca negativo.
- `ConsecutiveLosses`: contagem de trades fechados (fill completo,
  `allowed=true`) consecutivos com resultado negativo, na ordem
  cronológica de `backtest_trades`; reseta a zero no primeiro trade
  positivo.

**Métricas finais** (pacote `metrics`, sobre a série de retornos por
candle do timeframe condutor extraída de `backtest_equity_curve`, com
taxa livre de risco assumida como zero — padrão razoável para
backtests cripto de curto/médio prazo):

- Retorno total, drawdown máximo, win rate (% de `backtest_trades` com
  `allowed=true` e resultado positivo), trades totais, retorno médio
  por trade.
- Volatilidade anualizada: desvio padrão dos retornos por candle,
  multiplicado por `sqrt(períodos por ano do timeframe condutor)` (ex.
  `sqrt(365*24)` para candles de 1h).
- Sharpe ratio: média dos retornos por candle dividida pelo desvio
  padrão desses mesmos retornos, anualizado pelo mesmo fator acima.
- Sortino ratio: igual ao Sharpe, mas o denominador usa só o desvio
  padrão dos retornos negativos (downside deviation) em vez de todos os
  retornos.

## Tratamento de erros e resiliência

- **Sem detecção de gap por ativo**: o relógio avança em incrementos
  fixos do timeframe condutor, de `period_start` a `period_end`,
  independente de qual ativo tem candle naquele instante.
  `MarketView.Candles` devolve o que existir de fechado até ali — se
  um ativo tem um buraco de dados, a `Strategy` recebe menos histórico
  (ou nenhum) para aquele ativo naquele passo. É esperado, e faz parte
  do que este sub-projeto existe para testar.
- **Modo seguro herdado do `risk-engine`**: dado de mercado ausente ou
  desatualizado (segundo o corte `AsOf`) para uma operação proposta já
  resulta em rejeição via os próprios checks de qualidade do
  `risk-engine` — o simulador não duplica essa lógica.
- **Falha de banco durante a execução aborta o backtest** sem
  resultado parcial silencioso: `backtest_runs.status` vai para
  `'failed'` com a mensagem de erro em `error`; fica `'running'`
  enquanto executa, `'completed'` ao terminar normalmente.
- **Validação de entrada na CLI**, antes de abrir conexão com o banco:
  `period_start < period_end`; `fee_pct >= 0`;
  `driving_timeframe` precisa estar entre os `timeframes` configurados.
- **Isolamento entre execuções**: como `risk_state`/`risk_decisions`
  agora são escopados por `run_id`, duas execuções concorrentes ou
  sequenciais sobre os mesmos ativos/período não interferem uma na
  outra, nem na operação ao vivo (`run_id IS NULL`) — garantia coberta
  por teste de integração explícito, não só assumida.

## Testes

- **Unitários (sem banco)**: cálculo de métricas (`metrics.go` —
  Sharpe, Sortino, drawdown máximo, win rate) contra séries de equity
  conhecidas; cálculo de `DailyLoss`/`WeeklyLoss`/`Drawdown`/
  `ConsecutiveLosses` (`portfolio.go`) contra sequências de trades
  conhecidas; `FixedOperationsStrategy` devolvendo as operações certas
  na janela certa de candle.
- **Integração (TimescaleDB real)**: candles conhecidos inseridos, roda
  um backtest completo com `FixedOperationsStrategy`, verifica
  `backtest_trades`/`backtest_equity_curve`/`backtest_results` batendo
  com o cálculo manual esperado.
- **Teste de não-lookahead**: insere candles com timestamp além do
  `period_end` simulado e confirma que nem o `MarketView` nem o
  `risk-engine` (via `AsOf`) os enxergam em nenhum ponto da execução.
- **Teste de isolamento de estado**: duas execuções de backtest com
  `run_id` diferentes sobre o mesmo ativo/período não vazam
  `risk_state`/`risk_decisions` uma para a outra, nem tocam a linha
  `run_id IS NULL` (ao vivo).
- **Teste do motor de risco real embutido**: uma sequência de trades
  que force violação de perda deve pausar aquele `run_id`
  especificamente (`risk_state` daquele run vira `paused`) e bloquear
  os trades seguintes da mesma execução — prova que o `Evaluate` de
  produção está sendo exercitado de verdade, não reimplementado à
  parte.
- **Regressão no `risk-engine`**: toda a suíte de testes existente do
  `risk-engine` (33 testes) continua passando sem alteração de
  comportamento quando `EvalOptions` é omitido — a extensão é aditiva,
  não muda o caminho ao vivo.

## Critério de conclusão desta fase

O ambiente de simulação está pronto quando:

- Uma execução de backtest completa roda do início ao fim contra dados
  reais do TimescaleDB, produzindo `backtest_trades`,
  `backtest_equity_curve` e `backtest_results` consistentes.
- O `risk-engine` real (via `Evaluate`, com `AsOf`/`RunID`) é chamado
  antes de cada operação simulada, e uma violação de regra pausa
  aquele `run_id` de forma isolada, sem afetar outras execuções nem a
  operação ao vivo.
- Nenhum candle com timestamp além do instante simulado corrente
  influencia qualquer decisão da `Strategy` ou do `risk-engine` durante
  a execução (não-lookahead comprovado por teste).
- As métricas finais (retorno, drawdown, Sharpe, Sortino, win rate,
  volatilidade) são calculadas corretamente a partir da curva de
  patrimônio e do log de trades.
- A suíte de testes existente do `risk-engine` continua passando sem
  mudança de comportamento no caminho ao vivo (`EvalOptions` omitido).
