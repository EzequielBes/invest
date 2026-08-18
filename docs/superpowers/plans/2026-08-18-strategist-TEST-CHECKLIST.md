# Checklist de testes pendentes — Sub-projeto 5 (Agente Estrategista)

Escrito em 2026-08-18, ao final da implementação (todas as 8 tasks do plano
`2026-08-18-strategist.md` completas, revisão final do branch limpa,
1 finding Important corrigido — `buildPortfolio` agora rejeita portfólio
com valor total zero). Esta lista é o que falta **além** disso: gaps de
cobertura em lógica com risco real de erro silencioso, não uma exigência de
cobrir toda função/branch. Mesmo formato/uso de
`docs/superpowers/plans/2026-08-18-analysis-agents-TEST-CHECKLIST.md`
(sub-projeto 4): fica para o Codex escrever como TDD; se achar um bug real,
volta para o Claude corrigir.

## Como rodar / convenções

- `COMPOSE_PROJECT_NAME=strategist-dev docker compose -f strategist/docker-compose.yml exec go go test ./... -v`
- Testes de integração usam `TEST_DATABASE_URL` (já setado no container) e
  pulam (`t.Skip`) se não estiver setado.
- Seed de dados de outras tabelas (compartilhadas com `analysis`/
  `market-data`): sempre símbolos claramente falsos (`TESTASSET*`, nunca
  `BTC`/`ETH`/ativo real), com `t.Cleanup` limpando tudo que foi inserido —
  ver `seedAnalysisRun`/`seedCandle` em `cmd/strategist/main_test.go` para o
  padrão já estabelecido.
- LLM fake: `fakeLLMClient` em `cmd/strategist/main_test.go`, nunca bater na
  API real da Anthropic num teste automatizado.

## `internal/strategist/decide.go` — caminho `RiskErr` nunca exercitado

**Maior gap desta lista.** `Decide` tem três desfechos possíveis para
buy/sell: `risk.Evaluate` aprova, `risk.Evaluate` rejeita (ambos setam
`Outcome.Risk`), ou `risk.Evaluate` retorna um erro de verdade (seta
`Outcome.RiskErr`, não o `error` de retorno de `Decide` — é assim que o
chamador ainda consegue persistir a decisão do LLM mesmo quando a validação
de risco falhou). Esse terceiro caminho nunca foi exercitado em nenhum
teste do módulo — nem em `decide_test.go` (usa `riskStore=nil`, só cobre
`hold`/falha de LLM) nem em `main_test.go` (usa um `riskStore` real que
sempre *responde*, nunca falha).

- [ ] **`risk.Evaluate` retornando erro real.** Precisa de um jeito de
      forçar isso — mais fácil via `internal/strategist` com um
      `*riskstorage.Store` real mas apontando para um DSN inválido/fechado
      (não dá para mockar, `risk.Evaluate` recebe `*storage.Store`
      concreto). Alternativa: fechar o pool (`riskStore.Close()`) antes de
      chamar `Decide`, forçando erro de conexão. Esperar:
      `outcome.Risk == nil`, `outcome.RiskErr != nil`,
      `err == nil` (o retorno de `Decide` em si, não `RiskErr`) —
      e que o chamador (`Run`) ainda persista a decisão com
      `risk_allowed = NULL` (já coberto no nível de `main.go`'s `save()`,
      mas nunca disparado ponta a ponta por esse gatilho específico).

## `cmd/strategist` — caminho "aprovado" nunca exercitado

Todos os testes de integração atuais seedam candles no timeframe `1h`,
mas as checagens de qualidade do risk-engine (`checkDataFreshness`,
`checkVolatility`, `checkLiquidity`) leem candles no timeframe `1m` — ou
seja, toda proposta testada hoje é **rejeitada** por dados de qualidade
insuficiente, nunca aprovada. Isso é inofensivo pros testes atuais (eles só
verificam que uma decisão *existe* e foi persistida, não qual o veredito),
mas significa que o branch `if outcome.Risk.Allowed { status = "approved" }`
em `report()` (`cmd/strategist/main.go`) nunca rodou em teste nenhum.

- [ ] **Cenário de aprovação real.** Seedar candles de `TESTASSET*` também
      no timeframe `1m` (60+ candles, com volume/preço suficientes para
      passar `checkVolatility`/`checkLiquidity`) além do `1h` já usado para
      o preço, e limites de risco (`risk_limits`, já seedados pela migration
      do `risk-engine`) suficientemente folgados. Esperar
      `d.RiskAllowed != nil && *d.RiskAllowed == true` — cobre o branch
      "approved" de `report()` e confirma que o caminho de sucesso real do
      `risk.Evaluate` (não só "não deu erro") está correto ponta a ponta.

## `cmd/strategist/flags_test.go` — borda de `-positions`

- [ ] **Entrada só com espaços em branco** (ex. `" : "`). Já tratado
      corretamente pelo guard `symbol == "" || qtyStr == ""` em
      `parsePositions` (confirmado na revisão final), só não tem teste
      dedicado. Baixa prioridade — a borda real (`"BTC"` sem `:`) já é
      testada e cobre o mesmo guard.

## `internal/storage/analysisdata.go` — determinismo de `risk_context`

- [ ] **Duas linhas `risk_context` no mesmo run.** `ResultsForRun` não tem
      `ORDER BY`, e `cmd/strategist/main.go` fica com a última linha
      `risk_context` iterada, não necessariamente a mais recente. Hoje o
      `analysis` só grava uma linha `risk_context` por run, então isso é
      teórico — mas se algum dia deixar de ser, o comportamento vira
      não-determinístico. Um teste que seeda duas linhas `risk_context`
      pro mesmo `run_id` e confirma qual delas "vence" (ou força a adoção
      de `ORDER BY created_at` em `ResultsForRun`) resolveria isso na
      raiz. Baixa prioridade, mas fácil de esquecer depois.

## Fora desta lista (observações da revisão final, não são gaps de teste)

- **Sizing de `sell` sem clamp para a quantidade realmente possuída** — a
  mesma fórmula (`sizing_pct * portfolioValue / price`) é usada pra
  buy e sell, sem checar se o portfólio de fato tem aquele ativo pra
  vender. Inofensivo nesta fase (só grava a decisão, nunca executa), mas
  **precisa ser fechado antes do sub-projeto 8** (execução real) —
  registrado aqui como lembrete, não é algo pra testar agora, é um gap de
  design a resolver quando a execução de verdade entrar em cena.
- **`-timeframe` (preço/sizing) vs. timeframe fixo `1m` das checagens de
  qualidade do risk-engine** — hoje esse descompasso é protetor (ativos
  pouco cobertos são automaticamente rejeitados), mas é uma inconsistência
  latente entre duas fontes de preço diferentes alimentando uma única
  decisão. Vale revisitar no sub-projeto 8, não é um bug deste branch.
- CRUD puro de storage (`internal/storage/*.go`, exceto os gaps acima) —
  wrappers finos sobre SQL, sem lógica de decisão, intencionalmente sem
  teste dedicado por política de rigor reduzido.
- Testes de integração reais contra a API do Claude — custo e
  não-determinismo, mesma decisão do sub-projeto 4.
