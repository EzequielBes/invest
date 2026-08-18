# Agente Estrategista + Motor de Decisão

Status: Aprovado para planejamento de implementação
Data: 2026-08-18
Sub-projeto 5 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o quinto sub-projeto da plataforma de investimentos autônomos. Os
sub-projetos 1-4 (todos concluídos) constroem, nesta ordem: coleta de dados
de mercado (`market-data`), validação mecânica de operações contra limites
de risco (`risk-engine`), um ambiente de backtest que roda estratégias
determinísticas contra o `risk-engine` (`simulation`), e agentes que
transformam dados brutos em indicadores estruturados + narrativa em
linguagem natural, por domínio (`analysis`).

Nenhum desses sub-projetos decide o que fazer. O `analysis` explicitamente
não recomenda ações (ver seu spec, "Fora de escopo"); o `risk-engine` só
aprova ou rejeita uma operação já proposta; o `simulation` só teve
estratégias determinísticas de exemplo (cruzamento de médias), não uma
decisão inteligente. Este sub-projeto constrói a primeira peça que **decide**:
lê os quatro outputs de análise de um ativo, pede a um LLM (Claude) uma
decisão estruturada (comprar/vender/manter, com tamanho e justificativa), e
submete essa proposta ao `risk-engine` para validação — sem executar nada de
verdade, já que não há corretora integrada ainda (sub-projeto 8).

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. Agentes de análise *(concluído)*
5. **Agente estrategista + motor de decisão** ← este documento
6. Camada MCP
7. Integração multi-LLM (Codex + Claude)
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Módulo Go separado** (`strategist/`, `go.mod` próprio), mesma estrutura
  dos sub-projetos anteriores (`cmd/`, `internal/`, `migrations/`). Depende
  de `risk-engine` via `replace` local (só para `risk.Evaluate`,
  `risk.ProposedOperation`, `risk.PortfolioState`, `risk.EvalOptions` — não
  importa `analysis` nem `simulation`; lê `analysis_results` pela mesma
  tabela compartilhada no TimescaleDB, sem depender do código Go daquele
  módulo).
- **Um agente estrategista por ativo**, consolidando os três outputs de
  análise por ativo (`technical`, `derivatives`, `news`) de um
  `analysis_run_id` já existente, mais o `risk_context` (compartilhado, uma
  vez por execução, não por ativo), num único prompt.
- **Decisão estruturada via tool use do Claude**: `side`
  (`buy`/`sell`/`hold`), `confidence` (0.0-1.0), `sizing_pct` (fração do
  valor total do portfólio a alocar, ignorado se `hold`), `rationale`
  (texto curto explicando a decisão). Tool use em vez de parsing de JSON em
  texto livre — resposta estruturada garantida pelo SDK, sem parser frágil
  nem *retry* de reformatação.
- **Conversão da decisão em proposta de operação**: para `buy`/`sell`,
  `Quantity = sizing_pct * valor_total_do_portfólio / preço_atual` (preço =
  close do candle mais recente do timeframe informado), formando um
  `risk.ProposedOperation`. Para `hold`, nenhuma proposta é gerada.
- **Validação via `risk.Evaluate`** contra um `risk.PortfolioState`
  fornecido pelo usuário (ver "Portfólio manual" abaixo) — reaproveita o
  motor de risco já existente, o estrategista não reimplementa checagem de
  limites.
- **Persistência da decisão** (aprovada ou não, `hold` incluso) numa tabela
  própria, ligando de volta ao `analysis_run_id` que a originou.
- **Portfólio manual via flags/CLI**: como não há execução real (sub-projeto
  8) para rastrear posições automaticamente, o usuário informa o portfólio
  atual (caixa e posições) a cada execução via flags. Isso é
  deliberadamente um stopgap — fica explícito no código e nesta spec, não
  escondido atrás de uma abstração que sugira persistência automática.
- **Execução via binário CLI sob demanda** (`cmd/strategist`), mesmo padrão
  de `cmd/analysis`/`cmd/backtest`/`cmd/market-data` — sem agendamento
  embutido.

### Fora de escopo (explicitamente adiado)

- **Execução real de ordens** — o resultado desta fase é sempre um registro
  de decisão (aprovada ou rejeitada pelo risk-engine), nunca uma chamada a
  uma exchange. Sub-projeto 8.
- **Tracking automático de portfólio** (ler posições/caixa reais de uma
  corretora) — o portfólio é informado manualmente a cada execução nesta
  fase. Sub-projeto 8 (ou 10, dependendo de como evoluir).
- **Backtest do agente estrategista** — rodar o estrategista contra
  histórico exigiria uma chamada ao LLM por candle simulado (caro, lento,
  não-determinístico, quebra a reprodutibilidade que o `simulation` foi
  desenhado para ter). Fica fora de escopo agora; pode voltar como um
  sub-projeto dedicado no futuro se fizer sentido.
- **Disparo automático da análise (sub-projeto 4)** — o estrategista só lê
  um `analysis_run_id` já existente; não roda `cmd/analysis` internamente.
  Fluxo de duas etapas, acoplamento fraco entre os dois módulos.
- **Múltiplos provedores de LLM ou comparação entre eles** — sub-projeto 7.
- **Camada MCP ou API HTTP** — só CLI nesta fase, mesma decisão dos
  sub-projetos anteriores. MCP é o sub-projeto 6.
- **Sizing sofisticado** (Kelly criterion, volatility targeting, etc.) — o
  Claude sugere um `sizing_pct` direto na decisão estruturada; o
  `risk-engine` já impõe os limites reais (`MaxPctPerAsset`,
  `MaxValuePerTrade`); nenhuma lógica de dimensionamento adicional em código
  Go nesta fase.
- **Retry customizado nas chamadas ao Claude** — mesma decisão do
  sub-projeto 4: o SDK já reenvia erros de rede/429/5xx automaticamente.

## Arquitetura

```text
strategist/
├── go.mod                    (module strategist; require risk-engine, replace ../risk-engine)
├── docker-compose.yml        (golang:1.22, rede market-data_default)
├── cmd/
│   └── strategist/
│       └── main.go           (CLI: -run-id, -assets, -timeframe, -cash, -positions,
│                              -daily-loss, -weekly-loss, -drawdown, -consecutive-losses)
├── internal/
│   ├── storage/
│   │   ├── db.go             (pool de conexão)
│   │   ├── analysisdata.go   (leitura: analysis_results por run_id/agent_type/asset — tabela do analysis)
│   │   ├── marketdata.go     (leitura: candle mais recente, para o preço atual — tabela do market-data)
│   │   └── decisions.go      (escrita: SaveDecision)
│   ├── llm/
│   │   └── client.go         (wrapper fino sobre o SDK Anthropic, com tool use —
│   │                          Decide(ctx, systemPrompt, userPrompt) (Decision, error))
│   └── strategist/
│       └── decide.go         (orquestra: lê analysis_results -> monta prompt -> llm.Decide ->
│                               converte para risk.ProposedOperation -> risk.Evaluate -> persiste)
└── migrations/
    └── 001_init.sql
```

`strategist` lê `analysis_results` (do `analysis`) e candles (do
`market-data`), e usa `risk-engine/risk` como biblioteca (via `replace`) —
não escreve em nenhuma tabela de outro módulo. Escreve só na sua própria
tabela nova (`strategist_decisions`); `risk_decisions` já é escrito
automaticamente por `risk.Evaluate` (ver "Modelo de dados").

## Modelo de dados

```sql
CREATE TABLE IF NOT EXISTS strategist_decisions (
    id                TEXT PRIMARY KEY,
    analysis_run_id   TEXT NOT NULL,
    asset             TEXT NOT NULL,
    side              TEXT NOT NULL,               -- 'buy' | 'sell' | 'hold'
    confidence        DOUBLE PRECISION NOT NULL,
    sizing_pct        DOUBLE PRECISION NOT NULL DEFAULT 0,
    rationale         TEXT NOT NULL DEFAULT '',
    proposed_quantity DOUBLE PRECISION NOT NULL DEFAULT 0,
    proposed_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_allowed      BOOLEAN,                      -- NULL quando side='hold' (risk.Evaluate não é chamado)
    risk_reasons      JSONB NOT NULL DEFAULT '[]',
    created_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS strategist_decisions_run_id ON strategist_decisions (analysis_run_id);
```

`id` é `uuid.NewString()` gerado em Go, armazenado como `TEXT` — mesma
convenção de `analysis`/`simulation`. Sem FK para `analysis_runs` (tabela de
outro módulo) nem para `risk_decisions` — ligação é por `analysis_run_id` +
`asset` + proximidade de `created_at`, consistente com a filosofia de
"analysis só lê, nunca ganha FK de fora" já estabelecida.

`risk_decisions` (tabela do `risk-engine`, já existente) continua sendo
escrita automaticamente dentro de `risk.Evaluate` para toda proposta
`buy`/`sell` — é o registro mecânico da checagem de limites.
`strategist_decisions` é o registro do *raciocínio* (LLM) que originou (ou
não) essa proposta, incluindo os casos `hold` que nunca chegam ao
risk-engine.

## Decisão estruturada do LLM

- `internal/llm/client.go` expõe `Decide(ctx context.Context, systemPrompt,
  userPrompt string) (Decision, error)`, onde `Decision{Side, Confidence,
  SizingPct, Rationale}`.
- Implementado com **tool use** do SDK Anthropic (`anthropic-sdk-go@v1.9.0`,
  já pinado pelo sub-projeto 4 por incompatibilidade de toolchain — mesma
  versão, mesma restrição): uma única tool `record_decision` com schema
  JSON exigindo os quatro campos, `tool_choice` forçando essa tool (não
  deixa o modelo responder em texto livre). Resposta é lida do
  `tool_use` block, não de `TextBlock` — divergência importante do wrapper
  do sub-projeto 4, que só lida com texto.
- Modelo: `claude-sonnet-5`, `max_tokens: 512`, sem `thinking` (decisão
  direta, não precisa de raciocínio estendido exposto — o `rationale`
  já é o resumo do porquê).
- `systemPrompt` fixo definindo o papel ("Você é um estrategista de
  investimentos em criptomoedas..." + instrução explícita de nunca
  recomendar `sizing_pct` acima de um teto razoável — sugestão inicial
  25%, mesmo espírito do limiar de cascata de liquidação do sub-projeto 4:
  valor razoável, ajustável depois, não configurável nesta fase. É um
  *soft guardrail* no prompt (o modelo pode ignorá-lo); o `risk-engine` é
  o guardrail real e vinculante via `MaxPctPerAsset`/`MaxValuePerTrade`.
- `confidence` é registrado em `strategist_decisions` para consulta e para
  um eventual sub-projeto 10 (acompanhamento/aprendizado) usar
  historicamente — **não** é usado para filtrar ou auto-rejeitar decisões
  nesta fase; toda decisão `buy`/`sell`, independente da confiança
  reportada, é submetida ao `risk.Evaluate` do mesmo jeito.
- `userPrompt` monta as narrativas + indicadores dos três agentes por ativo
  (`technical`, `derivatives`, `news`) e do `risk_context` compartilhado,
  formatados como texto legível — mesmo padrão do sub-projeto 4.
- Se a tool call vier ausente, malformada, ou com `side` fora do enum
  esperado: tratado como falha de decisão para aquele ativo (ver
  "Tratamento de erros") — nunca um `hold` implícito.

## CLI (`cmd/strategist/main.go`)

Flags:

- `-run-id` (obrigatória): `analysis_run_id` já existente (gerado por
  `cmd/analysis`).
- `-assets` (obrigatória): símbolos separados por vírgula — subconjunto (ou
  igual) dos ativos analisados naquele run.
- `-timeframe` (default `"1h"`): usado para buscar o candle mais recente
  (preço atual) de cada ativo.
- `-cash` (obrigatória): caixa disponível em USD.
- `-positions` (default vazio): posições atuais, formato
  `SYMBOL:quantidade` separado por vírgula (ex.
  `-positions=BTC:0.5,ETH:2`) — usado para montar o `risk.PortfolioState`.
  Ausente = sem posições abertas. O `Value` de cada posição é calculado
  buscando o preço atual do símbolo (mesma fonte que `-timeframe` usa para
  o ativo decidido) — se um símbolo em `-positions` não tiver dado de
  preço, é um erro fatal para a execução inteira (uma avaliação de risco
  sobre um portfólio com valor errado é pior que não avaliar).
- `-daily-loss`, `-weekly-loss`, `-drawdown` (default `0`, fração — ex.
  `0.02` = 2%), `-consecutive-losses` (default `0`): completam os campos
  de `risk.PortfolioState` que `risk.Evaluate` usa para os limites de
  perda (`checkDailyLoss`/`checkWeeklyLoss`/`checkDrawdown`/
  `checkConsecutiveLosses`) — mesmo espírito manual de `-cash`/
  `-positions`, já que não há rastreamento automático de perdas nesta
  fase. Omitidos = sem perdas registradas (limites de perda efetivamente
  inertes até haver rastreamento automático).

Fluxo:

1. Valida flags (run-id/assets/cash obrigatórios; positions bem-formado se
   presente).
2. Lê `risk_context` do run (compartilhado) e, para cada ativo, os três
   resultados de agente (`technical`/`derivatives`/`news`) daquele
   `run-id`.
3. Para cada ativo com dados suficientes: monta o prompt, chama
   `llm.Decide`, converte para `risk.ProposedOperation` se não for `hold`,
   chama `risk.Evaluate` com o `PortfolioState` das flags, persiste em
   `strategist_decisions`.
4. Imprime um resumo por ativo: decisão, tamanho proposto, se foi aprovada
   pelo risk-engine e por quê (ou "hold: <rationale>").

## Tratamento de erros e resiliência

- **Ativo sem os três outputs de análise no run** (ex. `cmd/analysis` foi
  rodado só com `-agents=technical`): pula o ativo, loga o motivo, segue
  para os próximos — mesma política do sub-projeto 4.
- **Falha na chamada ao LLM ou tool call ausente/malformada**: pula o
  ativo, loga o erro, **não persiste uma decisão implícita** — ausência de
  decisão é o resultado correto quando o LLM falhou, nunca `hold` por
  default (decidir "não decidir" silenciosamente como se fosse uma decisão
  real seria pior que simplesmente não registrar nada).
- **Falha em `risk.Evaluate`** (erro, não rejeição — rejeição é um
  `Decision{Allowed: false}` válido): pula o ativo, loga, a decisão do LLM
  ainda é persistida com `risk_allowed = NULL` e uma nota no log
  (a proposta existiu, só não foi possível validá-la).
- **Falha ao gravar `strategist_decisions`**: loga e segue para o próximo
  ativo — ao contrário do `analysis`, aqui não há um "run" guarda-chuva
  para marcar como falho (cada ativo é independente, não há status agregado
  nesta fase).
- **`-positions` malformado**: erro de validação de flag, aborta antes de
  qualquer chamada de rede/banco (mesmo padrão de `-asset-names` no
  sub-projeto 4).

## Testes

Mesma política de rigor reduzido dos sub-projetos 4-10, com a divisão de
trabalho combinada a partir do sub-projeto 4: lógica de orquestração
complexa (o fluxo decisão → conversão em `ProposedOperation` → validação →
persistência, incluindo os caminhos de erro acima) ganha teste real escrito
nesta implementação; gaps de cobertura mais pontuais ficam num checklist
separado para o Codex escrever como TDD, no mesmo formato usado em
`docs/superpowers/plans/2026-08-18-analysis-agents-TEST-CHECKLIST.md`.

Pontos com lógica real que merecem teste direto: a conversão
`sizing_pct` → `Quantity`/`Value` (matemática com risco de erro de sinal ou
unidade); os caminhos de erro que decidem persistir vs. não persistir uma
decisão; e o fato de que `hold` nunca chama `risk.Evaluate`. Sem teste de
integração real contra a API do Claude (custo, não-determinismo) — o
wrapper `llm.Client` é a fronteira mockável, mesma decisão do sub-projeto 4.

## Critério de conclusão desta fase

O agente estrategista está pronto quando:

- Dado um `analysis_run_id` existente e um portfólio informado manualmente,
  `cmd/strategist` produz uma decisão estruturada (buy/sell/hold, tamanho,
  justificativa) por ativo solicitado, persistida em
  `strategist_decisions`.
- Toda proposta `buy`/`sell` é submetida ao `risk.Evaluate` real do
  sub-projeto 2 antes de ser considerada "aprovada" — nenhuma decisão
  ignora os limites de risco.
- Uma falha do LLM para um ativo específico não impede os demais ativos de
  serem decididos, e nunca produz um `hold` implícito.
- Nenhuma ordem real é enviada a lugar nenhum — o resultado desta fase é
  sempre um registro de decisão, nunca uma execução.
