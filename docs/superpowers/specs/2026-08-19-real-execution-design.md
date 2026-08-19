# Ambiente Real (Execução em Exchanges/Corretoras)

Status: Aprovado para planejamento de implementação
Data: 2026-08-19
Sub-projeto 8 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o oitavo sub-projeto da plataforma de investimentos autônomos. O
`strategist` (sub-projeto 5) já decide buy/sell/hold via LLM e valida a
proposta através do `risk.Evaluate`, mas hoje isso é só teoria: nenhuma
ordem é colocada em lugar nenhum, e o portfolio (`cash`/`positions`) é
informado manualmente via flags de CLI a cada execução — um placeholder
explicitamente documentado como provisório até este sub-projeto.

Este sub-projeto adiciona execução real de ordens contra a testnet de
futuros da Binance, e substitui o portfolio informado manualmente por uma
consulta real à conta na exchange. O objetivo desta fase é validar toda a
cadeia decisão → risco → execução contra uma API real, sem capital de
verdade em jogo — migração para produção com dinheiro real fica para uma
fase futura, fora deste sub-projeto.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. Agentes de análise *(concluído)*
5. Agente estrategista + motor de decisão *(concluído)*
6. Camada MCP *(concluído)*
7. Integração multi-LLM (Codex + Claude) *(concluído)*
8. **Ambiente real (execução em exchanges/corretoras)** ← este documento
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Testnet de futuros da Binance**, não produção com dinheiro real.
  Migração para produção é uma decisão explicitamente adiada — o design
  mantém a URL base da exchange como uma constante fácil de trocar depois,
  mas não constrói um toggle configurável agora (YAGNI).
- **Novo módulo Go `execution`**, seguindo a convenção já estabelecida no
  repositório (cada capacidade é seu próprio módulo: `market-data`,
  `risk-engine`, `simulation`, `analysis`, `strategist`, `mcp`). Expõe um
  pacote público com duas operações: `FetchPortfolio` (consulta saldo e
  posições reais na conta da testnet) e `Execute` (coloca uma limit order,
  acompanha até preencher ou expirar, cancela se não preencher).
- **Cliente autenticado da Binance** (`execution/internal/binanceclient`):
  assinatura HMAC-SHA256 por requisição, header `X-MBX-APIKEY` — um padrão
  novo neste repositório (os clientes existentes em `market-data` só leem
  endpoints públicos, sem autenticação). Credenciais via variáveis de
  ambiente `BINANCE_API_KEY`/`BINANCE_API_SECRET`, mesmo padrão já usado
  em todo o repositório para outras integrações externas.
- **`strategist` importa `execution` diretamente** (import Go normal entre
  módulos — `execution`'s pacote público, não há nenhum problema de
  visibilidade de `internal/` aqui, ao contrário do que aconteceu no
  sub-projeto 6 com o `mcp`, porque o pacote que `strategist` precisa já
  nasce público). O fluxo fica encadeado numa única chamada: `strategist`
  busca o portfolio real, decide, valida com `risk.Evaluate` e, se
  aprovado, executa — sem passo manual intermediário.
- **Isso muda o comportamento observável do `mcp`, e exige uma pequena
  atualização de compatibilidade no código dele** (correção feita durante
  o planejamento desta fase — o rascunho original deste spec afirmava
  "sem tocar no código do mcp", o que está errado: ver a lição equivalente
  do sub-projeto 7 sobre manifests/imports transitivos, registrada em
  memória — aqui o problema é a própria assinatura de `RunWithDSN`, não só
  o `go.mod`). Como `run_strategist` chama `strategist.Run`/`RunWithDSN`,
  toda chamada futura a essa tool MCP passa a também executar ordens
  reais na testnet, não só persistir a decisão como hoje — comportamento
  esperado, confirmado pelo usuário (ainda é testnet, sem risco de
  capital). Mas como `cash`/`positions`/`timeframe` deixam de fazer
  sentido como parâmetros de `RunWithDSN` (portfolio real substitui os
  dois primeiros; preço de sizing passa a ser sempre `1m`), a assinatura
  de `RunWithDSN` muda, e `mcp/internal/tools/strategist.go` — que chama
  essa função e expõe esses três campos em `RunStrategistArgs` — precisa
  ser atualizado para compilar. `mcp/go.mod` também precisa de `go mod
  tidy` para declarar `execution` como dependência transitiva nova (via
  `strategist/runner` → `execution/executor`), e `mcp/docker-compose.yml`
  precisa montar `../execution:/execution` como já faz para os outros
  módulos irmãos.
- **Ordens do tipo limit**, no preço de mercado atual (mesmo preço usado
  para o sizing da decisão) — não market order. Acompanhamento por
  polling do status a cada ~2s até um timeout fixo de 30s; se não
  preencher totalmente até lá, cancela a ordem e persiste a quantidade que
  de fato preencheu (pode ser zero) — isso nunca é tratado como erro, é um
  resultado válido.
- **`newClientOrderId` para idempotência**: toda ordem colocada carrega um
  ID de cliente determinístico e único (derivado do ID da decisão
  persistida), para que um retry acidental nunca resulte em ordem
  duplicada na exchange.
- **Duas lacunas do `strategist` (sub-projeto 5), pendentes justamente
  para esta fase, corrigidas agora**:
  1. **Clamp de sizing de venda**: hoje `outcome.Quantity` para uma venda é
     calculado só a partir de `sizing_pct * portfolio_value / price`, sem
     nunca ser limitado à quantidade realmente em posição — um LLM
     poderia propor vender mais do que existe. Com portfolio real vindo
     de `FetchPortfolio`, a quantidade de venda passa a ser limitada a
     `min(sizing_pct * portfolio_value / price, posição_atual_do_ativo)`.
  2. **Consistência de timeframe entre sizing e risk-engine**: o preço
     usado para sizing hoje vem de `-timeframe` (default `1h`), enquanto
     as checagens de qualidade do risk-engine (`data_freshness`,
     `volatility`, `liquidity`, em `risk-engine/risk/quality.go`) leem
     candles fixos de `1m` (hardcoded no SQL de
     `risk-engine/storage/marketdata.go`). Esse descompasso significa que
     o risk-engine pode aprovar uma operação como "dado fresco" enquanto o
     preço usado pra dimensionar a ordem já está desatualizado em até uma
     hora. Fix: o preço usado para sizing (e para valorar posições
     existentes em `FetchPortfolio`) passa a vir sempre de candles `1m`,
     igual ao risk-engine — a flag `-timeframe` do CLI do `strategist` é
     removida (deixa de fazer sentido ter um timeframe configurável para
     isso).

### Fora de escopo (explicitamente adiado)

- **Produção com dinheiro real** — fase futura. A constante de URL base
  da exchange fica isolada para facilitar a troca depois, mas nenhum
  mecanismo de configuração/toggle é construído agora.
- **Kill switch dedicado de execução** — decisão explícita do usuário:
  estamos em testnet, sem capital real, e o risk-engine já bloqueia
  operações fora dos limites antes de chegar na execução. Um interruptor
  de emergência específico da execução (ex: `EXECUTION_ENABLED=false`)
  fica para quando migrar para produção real.
- **Outras exchanges além da Binance** — Bybit e OKX já têm clientes
  read-only em `market-data`, mas autenticação/execução para eles fica
  para uma fase futura se necessário.
- **Market orders ou outros tipos de ordem** (stop-loss, take-profit,
  ordens condicionais) — só limit order nesta fase.
- **Flag `-execute` para tornar a execução opcional** — cogitada durante o
  brainstorm, descartada: o usuário confirmou que a execução deve sempre
  acontecer junto com a decisão, sem toggle.
- **Reconciliação de posições fora do fluxo de decisão** (ex: um processo
  periódico que audita se o portfolio local bate com o real independente
  de rodar o `strategist`) — cada chamada já busca o portfolio real do
  zero via `FetchPortfolio`, então não há estado local para divergir.

## Arquitetura

```text
execution/                          (novo módulo Go, go.mod próprio)
├── internal/binanceclient/
│   ├── client.go                   # assinatura HMAC-SHA256, base URL da testnet
│   ├── account.go                  # GetAccount() → saldo + posições abertas
│   └── orders.go                   # PlaceLimitOrder, GetOrderStatus, CancelOrder
├── internal/storage/                # tabela `executions` (auditoria)
├── execution/                       # pacote público
│   ├── portfolio.go                # FetchPortfolio(ctx) → risk.PortfolioState, valor total
│   └── execute.go                  # Execute(ctx, ...) → Outcome (preenchido/parcial/cancelado)
├── cmd/execution/                   # CLI standalone (debug manual, não faz parte do pipeline automático)
└── docker-compose.yml

strategist/                          (módulo existente, modificado)
├── cmd/strategist/main.go           # remove -cash/-positions/-timeframe, chama execution.FetchPortfolio
├── runner/runner.go                 # idem, e RunWithDSN passa a construir o cliente de execution internamente
└── internal/strategist/decide.go    # clamp de venda; após risk.Evaluate aprovar, chama execution.Execute
```

`strategist` importa o pacote público `execution/execution` como uma
dependência Go normal — não há nenhuma restrição de `internal/` a
contornar aqui, ao contrário do padrão `RunWithDSN` usado pelo `mcp` no
sub-projeto 6 (aquele padrão existe porque `mcp` precisava alcançar lógica
que vivia em pacotes `internal/` de outros módulos; aqui `execution`
nasce com uma API pública porque é exatamente essa a interface que
`strategist` precisa consumir).

## Fluxo de dados

```text
strategist.Run
  → execution.FetchPortfolio(ctx)              [substitui -cash/-positions]
  → para cada asset:
      preço = candle 1m mais recente            [substitui -timeframe]
      strategist.Decide(..., portfolio, preço)
        → risk.Evaluate(...)
        → se aprovado e side != "hold":
            quantidade = clamp(sizing, posição atual, se side == "sell")
            execution.Execute(ctx, asset, side, quantidade, preço)
        → persiste Decision + resultado da execução
```

## Tratamento de erros

- **`FetchPortfolio` falha**: erro fatal do run inteiro — sem portfolio
  real não há como dimensionar com segurança nenhuma decisão, então isso
  não é uma falha isolada por ativo (diferente da política já existente
  para falha de `risk.Evaluate` por ativo).
- **`execution.Execute` falha por erro de rede/API** (não por timeout de
  preenchimento): mesma política já usada para falha de `risk.Evaluate` —
  erro isolado por ativo, registrado junto da decisão persistida, o run
  continua para os demais ativos.
- **Timeout sem preenchimento total**: não é erro. A ordem é cancelada, e
  a quantidade efetivamente preenchida até o timeout (inclusive zero) é
  persistida como o resultado real da execução.
- **Preenchimento parcial**: a quantidade parcial já executada permanece
  válida (não é revertida) — a operação persistida reflete o que
  realmente aconteceu na exchange, que pode ser menor que o que o
  risk-engine aprovou (aprovação cobre o teto, nunca o piso; um
  preenchimento parcial é sempre menos exposição, nunca mais, então não
  precisa de nova checagem de risco).

## Testes

Mesma política de rigor reduzido dos sub-projetos 4-10: teste direto onde
há lógica real de decisão — a assinatura HMAC-SHA256 (contra um valor
determinístico conhecido), o clamp de sizing de venda contra a posição
real, e a lógica de polling/timeout/cancelamento (com um cliente Binance
fake, sem chamar a testnet de verdade nos testes automatizados). Sem
teste de integração contra a API real da Binance nos testes automatizados
— mesma política já usada para os provedores de LLM; uma validação manual
end-to-end contra a testnet real fica para o handoff final da
implementação, como feito no sub-projeto 7.

## Critério de conclusão desta fase

A execução real está pronta quando:

- `strategist` busca portfolio real da testnet da Binance via
  `execution.FetchPortfolio`, sem nenhuma flag manual de `-cash`/
  `-positions`.
- O preço usado para sizing e para valorar posições existentes vem sempre
  de candles `1m`, consistente com as checagens de qualidade do
  risk-engine.
- Uma decisão de venda nunca propõe uma quantidade maior do que a
  posição real detida no ativo.
- Uma decisão aprovada pelo risk-engine (`side != "hold"`) resulta em uma
  limit order real colocada na testnet da Binance, com acompanhamento até
  preenchimento ou timeout/cancelamento, e o resultado real (preenchido/
  parcial/cancelado) é persistido junto da decisão.
- Um retry acidental da mesma decisão nunca resulta em ordem duplicada na
  exchange (via `newClientOrderId` determinístico).
- `run_strategist` via MCP passa a executar ordens reais na testnet como
  parte do mesmo fluxo, com `mcp/internal/tools/strategist.go` atualizado
  para a nova assinatura de `RunWithDSN` (sem `cash`/`positions`/
  `timeframe`) e `mcp/go.mod`/`docker-compose.yml` atualizados para a
  nova dependência transitiva em `execution`.
