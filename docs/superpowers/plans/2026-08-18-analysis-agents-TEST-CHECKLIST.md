# Checklist de testes pendentes — Sub-projeto 4 (Agentes de Análise)

Escrito em 2026-08-18. Tasks 7-11 do plano (`2026-08-17-analysis-agents.md`)
estão implementadas, commitadas na branch `analysis-agents`, e a suíte atual
passa (`go test ./... -count=1` dentro do container `analysis-dev`). Esta
lista é o que falta **além** disso: gaps de cobertura em lógica com risco
real de erro silencioso, não uma exigência de cobrir toda função/branch.

**Fluxo combinado com o usuário:** estes testes ficam para o Codex escrever
como TDD (teste primeiro, depois qualquer ajuste de código que o teste
revelar necessário). Se o teste encontrar um bug real, não corrigir
silenciosamente — trazer de volta para o Claude corrigir, junto com o
teste que falhou e por quê.

## Como rodar / convenções

- `COMPOSE_PROJECT_NAME=analysis-dev docker compose -f analysis/docker-compose.yml exec go go test ./... -v`
- Testes de integração (banco real) usam `TEST_DATABASE_URL` (já setado no
  container) e pulam (`t.Skip`) se não estiver setado — ver `testStores` em
  `analysis/cmd/analysis/main_test.go`.
- Para popular dados de mercado (candles, funding_rates, open_interest,
  liquidations, news_items — tabelas do `market-data`, lidas mas não
  gravadas por `analysis`), fazer `INSERT` direto na tabela dentro do teste,
  seguindo o padrão `seedCandles` em
  `simulation/internal/storage/candles_test.go`. Não existe helper
  exportado de seed — cada pacote de teste escreve o seu.
- LLM fake: seguir o padrão `fakeLLMClient`/`selectiveFakeLLMClient` em
  `analysis/cmd/analysis/main_test.go` — nunca bater na API real da
  Anthropic num teste automatizado.
- Rigor reduzido (preferência já registrada do usuário): testar só onde há
  lógica real, não todo branch/wrapper.

## `internal/agents/technical.go` — Technical

- [ ] **Caminho de dados suficientes (>=51 candles).** Seedar 51+ candles
      com uma série conhecida, rodar `Technical(...)` com um `llm.Client`
      fake, esperar `Output.Indicators` do tipo `indicators.Technical`
      (não `PartialTechnical`), `Narrative` = texto do fake, `Err` nil.
      Hoje só o caminho de dados insuficientes é exercitado (via
      `main_test.go`, indiretamente).

## `internal/agents/derivatives.go` — Derivatives

**Sem nenhuma cobertura hoje, nem unitária nem de integração — é o maior
gap desta lista.**

- [ ] **Funding extremo + cascata de liquidação.** Seedar
      `funding_rates` com taxa > 0.1%, `open_interest` (agora e 24h atrás,
      variação perceptível), `liquidations` na última hora somando >
      $1.000.000. Esperar `Output.Indicators.(derivatives.Signals)` com
      `FundingExtreme=true`, `LiquidationCascade=true`, e que o
      `userPrompt` passado ao LLM fake contenha esses valores formatados
      (ex. capturar o prompt no fake e checar substring).
- [ ] **Caso normal (smoke test).** Seedar valores dentro da faixa normal,
      confirmar que não há erro/panic e que o resultado é persistível —
      cobertura de plumbing, não precisa ser exaustivo (o cálculo em si já
      tem teste unitário em `internal/derivatives/signals_test.go`).

## `internal/agents/news.go` — News

- [ ] **Caminho de artigos encontrados.** Hoje só o caminho de falha do
      LLM é exercitado (`main_test.go`, agente `news` no teste
      `TestRun_PartialLLMFailureStillCompletes`, mas sem notícia seedada).
      Seedar 1-2 `news_items` mencionando o ativo por símbolo ou nome,
      esperar `result.ArticleCount > 0`, prompt listando os títulos,
      `Narrative` não vazia.
- [ ] **Borda da janela de 24h.** Seedar uma notícia às ~25h atrás
      (fora da janela) e uma às ~23h atrás (dentro) — esperar que só a de
      23h apareça em `RecentNews`/`result.Articles`. É o tipo de bug de
      off-by-one que passa silenciosamente sem teste.

## `internal/agents/riskcontext.go` — RiskContext

Já coberto ponta-a-ponta por `main_test.go` contra o `risk_state` seedado
pela própria migration do `risk-engine`. Baixa prioridade — só vale
adicionar se for fácil simular um `risk_state` em status diferente do
default (ex. `"halted"`) e confirmar que o prompt reflete o status
corretamente.

## `internal/indicators/technical.go` — valores exatos

- [ ] **Um caso de RSI/SMA com resultado calculado à mão.** Os testes
      atuais (`TestCompute_UptrendBullish`, `TestCompute_FlatIsNeutral`)
      checam direção (`> 50`, `bullish`, etc.), não o valor exato — o spec
      original pedia "resultado esperado calculado à mão" para RSI/SMA/
      volatilidade/volume relativo. Um erro de fórmula (ex. RSI com
      método errado de suavização) pode passar despercebido só com checks
      direcionais. Adicionar um caso pequeno (ex. 15-20 candles) com RSI
      calculado à mão externamente e comparado com tolerância
      (`math.Abs(got-want) < 0.01`).

## `cmd/analysis/main.go` — validação de flags

- [ ] **Nome de agente inválido em `-agents` é rejeitado antes de tocar o
      banco.** Chamar `run(ctx, "BTC", "", "1h", "technical,nonsense")`
      (sem `DATABASE_URL` setado) e confirmar que o erro é sobre o agente
      desconhecido, não sobre conexão de banco — prova que a validação
      roda antes da tentativa de conexão.

## Fora desta lista (intencional, não é gap)

- CRUD puro de storage (`internal/storage/*.go`, exceto o que já é
  exercitado pelos testes de integração acima) — wrappers finos sobre
  SQL, sem lógica de decisão.
- `cmd/analysis/main()` em si (parsing de flags do pacote `flag`, log
  fatal) — já teria que rodar o binário de verdade; `run()`/`Run()`
  exportados são o ponto de teste, não `main()`.
- Testes de integração reais contra a API do Claude — custo e
  não-determinismo, já descartado no spec original.
