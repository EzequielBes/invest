# Agentes de Análise

Status: Aprovado para planejamento de implementação
Data: 2026-08-17
Sub-projeto 4 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o quarto sub-projeto da plataforma de investimentos autônomos.
O sub-projeto 1 (Fundação de Dados de Mercado, concluído) coleta
candles, funding rate, open interest, liquidações e notícias em um
TimescaleDB compartilhado. O sub-projeto 2 (Motor de Controle de
Risco, concluído) valida operações propostas contra limites e expõe
`risk_state`/`risk_limits`. O sub-projeto 3 (Ambiente de
Simulação/Backtest, concluído) valida o motor de risco rodando
estratégias contra dados históricos.

Nenhum desses sub-projetos até agora produz uma leitura interpretativa
dos dados coletados — só dados brutos (mercado) e decisões mecânicas
(risco). Este sub-projeto constrói os **agentes de análise**: um
conjunto de rotinas que lê os dados já coletados, calcula indicadores
estruturados por domínio, e usa um LLM (Claude) para gerar uma
narrativa curta em linguagem natural resumindo o que aqueles
indicadores significam. É a primeira peça da plataforma que produz uma
interpretação legível por humano (e, no sub-projeto 5, consumível por
um agente de decisão) em vez de só números ou aprovações/rejeições.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. **Agentes de análise** ← este documento
5. Agente estrategista + motor de decisão
6. Camada MCP
7. Integração multi-LLM (Codex + Claude)
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Módulo Go separado** (`analysis/`, `go.mod` próprio), seguindo a
  mesma estrutura dos sub-projetos anteriores (`cmd/`, `internal/`,
  `migrations/`). Depende de `risk-engine` via `replace` local no
  `go.mod` (só para reusar a constante `risk.ReferenceExchange` e os
  tipos de leitura de `risk_state`/`risk_limits` — não chama
  `risk.Evaluate`, não decide operações). Lê as tabelas de mercado do
  `market-data` e as tabelas de risco do `risk-engine`; escreve apenas
  nas suas próprias tabelas.
- **Quatro agentes de análise**, cada um cobrindo um domínio: técnico
  (candles), derivativos (funding rate / open interest / liquidações),
  notícias/sentimento, e risco/contexto de portfólio.
- **Saída dupla por agente**: indicadores estruturados (calculados em
  código, sem LLM) **e** uma narrativa textual curta gerada por Claude
  a partir desses indicadores.
- **Execução via binário CLI sob demanda** (`cmd/analysis`), mesmo
  padrão do `cmd/market-data` e `cmd/backtest` — sem agendamento
  embutido (cron fica fora de escopo, é responsabilidade externa).
- **Escopo de ativos**: qualquer ativo já coletado pelo `market-data`
  na exchange de referência (`risk.ReferenceExchange`, atualmente
  `"binance"`); lista padrão de execução vem de flag CLI (`-assets`),
  sem default automático a partir da tabela `assets` nesta fase (evita
  acoplamento a decidir "quais ativos analisar" — isso é uma decisão
  do sub-projeto 5).
- **Um único provedor de LLM**: Claude via API Anthropic
  (`claude-sonnet-5`, sem `thinking`), lido de `ANTHROPIC_API_KEY`.
  Multi-LLM é o sub-projeto 7.

### Fora de escopo (explicitamente adiado)

- Qualquer decisão de compra/venda — isso é o sub-projeto 5. Os
  agentes de análise só descrevem o estado atual, nunca recomendam uma
  ação de trading.
- Agendamento automático (cron, worker contínuo) — a CLI roda sob
  demanda; disparo periódico é responsabilidade externa (ex. cron do
  SO), fora deste sub-projeto.
- Múltiplos provedores de LLM ou comparação entre eles — sub-projeto
  7.
- Classificação de sentimento como uma chamada de LLM separada — o
  agente de notícias não faz uma segunda chamada para "classificar"
  sentimento; a leitura de sentimento, se houver, sai dentro da mesma
  narrativa gerada a partir dos artigos encontrados.
- Histórico de `risk_decisions` no agente de risco/contexto — só o
  estado atual (`risk_state`) e os limites atuais (`risk_limits`).
- Qualquer API HTTP ou camada MCP para disparar análises — só CLI
  nesta fase (mesma decisão dos sub-projetos 1–3; MCP é o sub-projeto
  6).
- Retry/backoff customizado nas chamadas ao Claude — o SDK Anthropic
  já reenvia automaticamente erros de rede/429/5xx; não há lógica de
  retry adicional neste sub-projeto.

## Arquitetura

```text
analysis/
├── go.mod                    (module analysis; require risk-engine, replace ../risk-engine)
├── docker-compose.yml        (golang:1.22, rede market-data_default)
├── cmd/
│   └── analysis/
│       └── main.go           (CLI: -assets, -timeframe, -agents)
├── internal/
│   ├── storage/
│   │   ├── db.go             (pool de conexão)
│   │   ├── marketdata.go     (leitura: candles, funding_rates, open_interest,
│   │   │                      liquidations, news_items — todas do market-data)
│   │   ├── riskdata.go       (leitura: risk_state linha ao vivo, risk_limits — do risk-engine)
│   │   └── runs.go           (escrita: CreateRun, SaveResult, FinishRun)
│   ├── indicators/
│   │   └── technical.go      (SMA curta/longa, tendência, RSI, volatilidade, volume relativo)
│   ├── derivatives/
│   │   └── signals.go        (funding extremo, variação de OI, cascata de liquidação)
│   ├── news/
│   │   └── search.go         (busca por palavra-chave em news_items por ativo, janela de tempo)
│   ├── llm/
│   │   └── client.go         (wrapper fino sobre o SDK Anthropic — Summarize(ctx, systemPrompt, userPrompt) (string, error))
│   └── agents/
│       ├── technical.go      (orquestra: coleta candles -> indicators.Compute -> prompt -> llm.Summarize)
│       ├── derivatives.go    (orquestra: coleta funding/OI/liquidations -> derivatives.Compute -> prompt -> llm.Summarize)
│       ├── news.go           (orquestra: news.Search -> prompt -> llm.Summarize)
│       └── riskcontext.go    (orquestra: storage.riskdata -> prompt -> llm.Summarize)
└── migrations/
    └── 001_init.sql
```

`analysis` só lê as tabelas do `market-data` e do `risk-engine`;
escreve apenas nas suas próprias duas tabelas novas (`analysis_runs`,
`analysis_results`). Não há extensão de schema em nenhum módulo
existente — leitura pura.

## Modelo de dados

**Tabelas novas, de propriedade do `analysis` (schema unificado —
indicadores em JSONB em vez de uma tabela por domínio, já que o
consumo primário é o LLM e, depois, o sub-projeto 5, não queries SQL
estruturadas sobre campos específicos):**

```sql
CREATE TABLE IF NOT EXISTS analysis_runs (
    id          TEXT PRIMARY KEY,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    timeframe   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    error       TEXT
);

CREATE TABLE IF NOT EXISTS analysis_results (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES analysis_runs(id),
    agent_type  TEXT NOT NULL,
    asset       TEXT NOT NULL DEFAULT '',
    indicators  JSONB NOT NULL,
    narrative   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS analysis_results_run_id ON analysis_results (run_id);
```

`id` em ambas as tabelas é `uuid.NewString()` gerado em Go e
armazenado como `TEXT` — mesma convenção já estabelecida em
`simulation` (`backtest_runs.id`), evita complexidade de codec UUID no
pgx e dependência de `pgcrypto`.

`agent_type IN ('technical', 'derivatives', 'news', 'risk_context')`.
`asset` fica vazio (`''`) para `risk_context`, que é uma leitura de
portfólio, não por ativo. `status IN ('running', 'completed',
'failed')` em `analysis_runs`; `'failed'` só quando **todos** os
agentes falharam ao gerar a narrativa (ver Tratamento de erros).

## Indicadores por agente

Todos os cálculos abaixo são feitos em código Go puro, sem chamar o
LLM — o LLM só recebe o resultado já calculado e o narra.

**Técnico** (`internal/indicators/technical.go`), por ativo, sobre
candles do timeframe da execução (`risk.ReferenceExchange`):

- `sma_short` (janela 20) e `sma_long` (janela 50).
- `trend`: `"bullish"` se `sma_short > sma_long`, `"bearish"` se
  `sma_short < sma_long`, `"neutral"` se aproximadamente iguais
  (diferença menor que 0.1%).
- `rsi` (RSI clássico, período 14, método de Wilder).
- `volatility`: desvio-padrão dos retornos percentuais dos últimos 20
  candles fechados.
- `relative_volume`: volume do candle mais recente dividido pela média
  de volume dos 20 candles anteriores.

Requer no mínimo 51 candles fechados (para a SMA longa de 50 mais um
ponto de referência); se não houver dados suficientes, o agente
técnico retorna indicadores parciais com os campos calculáveis
preenchidos e os demais omitidos do JSON, e a narrativa nota
explicitamente a limitação de dados.

**Derivativos** (`internal/derivatives/signals.go`), por ativo:

- `funding_rate`: taxa mais recente de `funding_rates`.
- `funding_extreme`: `true` se `|funding_rate| > 0.001` (0.1%).
- `oi_change_pct`: variação percentual do `open_interest` mais recente
  em relação ao valor de 24h atrás.
- `liquidation_volume_1h`: soma de `quantity * price` de
  `liquidations` na última hora.
- `liquidation_cascade`: `true` se `liquidation_volume_1h` exceder um
  limiar fixo (`$1,000,000` — valor razoável para cripto majors;
  ajustável depois se necessário, sem tornar isso configurável nesta
  fase).

**Notícias** (`internal/news/search.go`), por ativo:

- Busca em `news_items` (janela: últimas 24h por `published_at`) por
  ocorrência do nome ou símbolo do ativo (case-insensitive) em `title`
  OU `body`. O agente recebe o nome completo do ativo como parâmetro
  além do símbolo (ex. símbolo `"BTC"`, nome `"Bitcoin"`), já que
  notícias frequentemente usam o nome por extenso.
- Indicador estruturado: `article_count` e `articles` (lista de até 10
  itens mais recentes, cada um com `title`, `url`, `published_at`).
- Nenhuma classificação de sentimento é computada em código — a
  narrativa do LLM, ao ler os títulos/resumos, é o único lugar onde
  uma leitura de sentimento aparece.

**Risco/contexto** (`internal/agents/riskcontext.go`, via
`internal/storage/riskdata.go`), a nível de portfólio (não por
ativo, `asset = ''`):

- `risk_status`, `risk_reason`, `risk_changed_at`: linha ao vivo de
  `risk_state` (`run_id IS NULL`).
- `limits`: todos os campos de `risk_limits` (linha `id=1`).
- Sem consulta a `risk_decisions` — só o estado atual.

## Chamadas ao LLM

- `internal/llm/client.go` expõe um único método:
  `Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error)`,
  encapsulando `client.Messages.New` do SDK Anthropic Go
  (`model: "claude-sonnet-5"`, `max_tokens: 512`, sem `thinking` —
  tarefa de resumo curto, não precisa de raciocínio estendido).
- Uma chamada por agente por ativo (quatro chamadas por ativo por
  execução, já que `risk_context` roda uma vez por execução
  independente do número de ativos).
- Cada agente monta seu próprio `systemPrompt` (curto, fixo, definindo
  o papel do agente — ex. "Você é um analista técnico de
  criptomoedas...") e `userPrompt` (os indicadores calculados,
  formatados como texto legível, não JSON bruto — mais barato em
  tokens e mais natural para o modelo narrar).
- Chave de API lida de `ANTHROPIC_API_KEY` (o SDK lê automaticamente
  do ambiente; nenhuma flag de CLI para a chave).

## CLI (`cmd/analysis/main.go`)

Flags:

- `-assets` (obrigatória): símbolos separados por vírgula, na exchange
  de referência (mesmo padrão de `cmd/backtest`).
- `-timeframe` (default `"1h"`): timeframe usado pelo agente técnico
  para buscar candles.
- `-agents` (default `"technical,derivatives,news,risk_context"`):
  subconjunto de agentes a rodar nesta execução, separados por vírgula
  — permite rodar só um agente (ex. `-agents=news`) sem os outros.

Fluxo:

1. Valida flags (assets não vazio; agents é subconjunto válido).
2. Abre conexão com o banco, cria uma linha em `analysis_runs`
   (`status='running'`).
3. Para cada agente solicitado, para cada ativo aplicável (todos exceto
   `risk_context`, que roda uma vez): coleta dados, calcula
   indicadores, chama o LLM, grava uma linha em `analysis_results`.
4. Ao final: `status='completed'` se pelo menos um agente produziu
   narrativa com sucesso; `status='failed'` (com `error` preenchido)
   só se **todos** os agentes falharam ao chamar o LLM.
5. Imprime na saída padrão um resumo por agente/ativo (narrativa
   gerada ou "sem narrativa: <motivo>").

## Tratamento de erros e resiliência

- **Falha isolada por agente**: se a chamada ao LLM falhar para um
  agente/ativo específico (erro de rede, rate limit esgotado após os
  retries automáticos do SDK, resposta de recusa), o indicador
  estruturado ainda é salvo em `analysis_results` (já que foi
  calculado antes da chamada ao LLM), com `narrative=''`. O erro é
  logado (stderr) e a execução continua para os próximos
  agentes/ativos — uma falha de LLM não aborta a execução inteira.
- **Falha de coleta de dados** (ex. erro de query no banco): aborta
  aquele agente/ativo específico (mesma política acima — loga e
  segue), não a execução inteira.
- **Falha de banco ao gravar `analysis_runs`/`analysis_results`**: essa
  sim aborta a execução inteira (não há como reportar progresso sem
  banco) — `status='failed'` é uma melhor-esforço; se a própria escrita
  do status falhar, a CLI retorna código de saída não-zero e loga o
  erro real.
- **Dados insuficientes** (ex. menos de 51 candles para o agente
  técnico, nenhum artigo de notícia encontrado): não é erro — o agente
  roda normalmente com indicadores parciais/vazios e a narrativa
  reflete essa limitação (ex. "sem notícias recentes encontradas para
  este ativo").

## Testes

Alinhado com a preferência atual do usuário de reduzir o rigor de
testes nos sub-projetos 4–10: testes só nos pontos que têm lógica real
com risco de erro silencioso. Sem teste dedicado por indicador/branch
trivial.

- **Unitários (sem banco, sem LLM)**: cálculo de RSI, SMA, volatilidade
  e volume relativo (`indicators/technical.go`) contra séries de preço
  conhecidas com resultado esperado calculado à mão; detecção de
  `funding_extreme`/`liquidation_cascade`/`oi_change_pct`
  (`derivatives/signals.go`) contra casos conhecidos; busca por
  palavra-chave em notícias (`news/search.go`) — case-insensitive,
  match em `title` e em `body`, e um caso de não-match.
- **Integração (TimescaleDB real, sem chamar o Claude de verdade — usar
  um `llm.Client` fake/mock que devolve texto fixo)**: uma execução
  completa da CLI com um agente selecionado grava
  `analysis_runs`/`analysis_results` corretamente; uma falha simulada
  do `llm.Client` fake para um agente não impede os outros agentes de
  completarem e não marca `analysis_runs.status` como `'failed'`
  enquanto pelo menos um agente teve sucesso.
- Sem teste de integração real contra a API do Claude (custo, não
  determinístico) — o wrapper `llm.Client` é a fronteira mockável para
  todos os outros testes.

## Critério de conclusão desta fase

Os agentes de análise estão prontos quando:

- Uma execução da CLI (`cmd/analysis`) roda os quatro agentes contra
  ativos reais já coletados pelo `market-data`, produzindo indicadores
  estruturados corretos e uma narrativa gerada pelo Claude para cada
  agente/ativo, persistidos em `analysis_runs`/`analysis_results`.
- Uma falha de chamada ao LLM em um agente específico não interrompe
  os demais agentes nem marca a execução inteira como falha (a menos
  que todos os agentes falhem).
- O agente de risco/contexto reflete corretamente o estado ao vivo
  (`risk_state` com `run_id IS NULL`) e os limites atuais
  (`risk_limits`), sem consultar `risk_decisions`.
- O agente de notícias encontra corretamente artigos que mencionam o
  ativo por símbolo ou nome, dentro da janela de 24h.
