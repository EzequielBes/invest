# Motor de Controle de Risco (Risk Engine)

Status: Aprovado para planejamento de implementação
Data: 2026-08-16
Sub-projeto 2 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o segundo sub-projeto da plataforma de investimentos autônomos.
O sub-projeto 1 (Fundação de Dados de Mercado, já concluído e mergeado
em `master`) coleta candles, funding rate, open interest, liquidações
e notícias de Binance, Bybit e OKX em um TimescaleDB compartilhado —
mas apenas dados públicos de mercado, sem qualquer noção de carteira,
posições ou execução real.

Este sub-projeto constrói o **motor de controle de risco**: a peça que
decide se uma operação proposta pode ou não ser realizada, com base em
limites configuráveis. Ele é independente de qualquer agente de
decisão — os agentes de análise, o motor de estratégia e o ambiente de
execução são sub-projetos futuros que ainda não existem. O motor de
risco não decide *o que* comprar ou vender; ele valida se uma operação
já proposta por outra parte do sistema respeita os limites
estabelecidos.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. **Motor de controle de risco** ← este documento
3. Ambiente de simulação / backtest
4. Agentes de análise
5. Agente estrategista + motor de decisão
6. Camada MCP
7. Integração multi-LLM (Codex + Claude)
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Módulo Go separado** (`risk-engine/`, `go.mod` próprio), no mesmo
  repositório `investment-platform`, seguindo a mesma estrutura do
  sub-projeto 1 (`cmd/`, `internal/`, `migrations/`). Conecta-se ao
  **mesmo** TimescaleDB já usado pelo sub-projeto 1 — não há banco
  separado.
- **Estado da carteira é entrada, não persistência.** O motor recebe
  posições atuais, caixa e métricas de perda (diária, semanal,
  drawdown, prejuízos consecutivos) como parâmetro de cada chamada.
  Rastrear e persistir a carteira de verdade é responsabilidade de um
  sub-projeto futuro (simulação ou ambiente real).
- **Regras nesta fase** (apenas cripto, consistente com o escopo do
  sub-projeto 1):
  - Concentração: percentual máximo por ativo, percentual máximo total
    em cripto, valor máximo por operação.
  - Perdas: perda máxima diária, perda máxima semanal, drawdown
    máximo, limite de prejuízos consecutivos.
  - Qualidade do ativo/dado: volatilidade máxima, liquidez mínima,
    idade máxima aceitável do dado de mercado mais recente (bloqueio
    por dado desatualizado).
- **Limites de frequência (quantidade máxima de operações num
  período)** ficam fora desta fase — adicionados quando houver um
  executor real gerando operações em sequência.
- **Mecanismos de proteção emitem sinal, não executam.** Pausa
  automática e kill switch mudam o estado operacional persistido
  (`risk_state`) e ficam registrados; cancelar ordens ou fechar
  posições de verdade é trabalho do sub-projeto 8 (Ambiente real), que
  vai consultar esse estado antes de agir.
- **Sem API/serviço nesta fase.** Mesmo padrão do sub-projeto 1: é uma
  biblioteca Go bem testada que sub-projetos futuros importam
  diretamente. Uma API (HTTP ou MCP) pode vir depois, quando houver um
  consumidor remoto de verdade.
- **Limites configuráveis em runtime**, guardados em tabela no banco
  (não em arquivo de configuração), para permitir ajuste sem reiniciar
  o processo.

### Fora de escopo (explicitamente adiado)

- Rastreamento/persistência de posições e caixa reais.
- Limites de frequência de operações.
- Percentual máximo por setor e exposição cambial máxima — fazem
  sentido quando houver ações e múltiplas moedas, não nesta fase
  cripto-only.
- Execução de fato de qualquer mecanismo de proteção (cancelar ordem,
  fechar posição) — o motor só decide e sinaliza.
- Qualquer API externa (HTTP, MCP) para consumir o motor.

## Arquitetura

```text
risk-engine/
├── go.mod
├── cmd/
│   └── (sem binário nesta fase — biblioteca pura; ver nota abaixo)
├── internal/
│   ├── risk/
│   │   ├── types.go        (PortfolioState, ProposedOperation, Decision, Rule)
│   │   ├── evaluate.go     (Evaluate — orquestra a avaliação)
│   │   ├── concentration.go
│   │   ├── losses.go
│   │   └── quality.go
│   └── storage/
│       ├── db.go
│       ├── limits.go       (leitura/escrita de risk_limits)
│       ├── state.go        (leitura/escrita de risk_state)
│       ├── decisions.go    (escrita em risk_decisions)
│       └── marketdata.go   (leitura read-only de candles/etc.)
├── migrations/
│   └── 001_init.sql
└── docker-compose.yml       (serviço "go" apontando para o TimescaleDB
                               do sub-projeto 1 via rede Docker compartilhada)
```

Nota sobre `cmd/`: como não há API nesta fase, não existe um binário
principal executável ainda — `internal/risk` e `internal/storage` são
consumidos como biblioteca. Um `cmd/` pode ser adicionado em fase
futura se este sub-projeto precisar rodar como processo próprio (por
exemplo, para expor uma API).

O `docker-compose.yml` deste módulo **não** sobe um novo TimescaleDB —
ele referencia a rede Docker já criada pelo `docker-compose.yml` do
sub-projeto 1 (`market-data_default`, como rede `external: true`) e
conecta ao container `timescaledb` existente por nome de rede. Isso
evita duplicar a instância de banco.

## Modelo de dados

Três tabelas novas no mesmo TimescaleDB, de propriedade deste
sub-projeto (o sub-projeto 1 continua dono de `candles`,
`funding_rates`, `open_interest`, `liquidations`, `news_items`,
`collector_runs`, que este motor apenas lê).

| Tabela | Colunas principais | Observação |
|---|---|---|
| `risk_limits` | `id, max_pct_per_asset, max_pct_crypto_total, max_value_per_trade, max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses, max_volatility, min_liquidity, max_data_age_minutes, updated_at` | Linha única (id fixo), atualizável em runtime |
| `risk_state` | `id, status, reason, changed_at` | `status` ∈ `normal, paused, kill_switch`; linha única — `reason`/`changed_at` sempre refletem a transição mais recente (automática por violação de perda, ou manual via `Reset`). Não guarda histórico de transições anteriores; se isso vier a ser necessário, adiciona-se uma tabela de histórico depois sem quebrar o schema atual |
| `risk_decisions` | `id, ts, asset, side, quantity, value, allowed, reasons (jsonb), rules_checked (jsonb)` | Append-only; log de auditoria de cada operação avaliada |

`risk_limits` e `risk_state` como linha única (não uma linha por
"versão") mantêm a leitura simples — o histórico de mudança de limites
não é um requisito desta fase; se vier a ser necessário, adiciona-se
depois sem quebrar o schema atual.

## Lógica de decisão

```go
func Evaluate(ctx context.Context, portfolio PortfolioState, proposed ProposedOperation) (Decision, error)
```

- **`PortfolioState`** (entrada): posições atuais (`map[string]Position`
  — ativo, quantidade, valor), caixa disponível, perda acumulada hoje,
  perda acumulada na semana, drawdown atual, número de prejuízos
  consecutivos recentes.
- **`ProposedOperation`** (entrada): ativo (símbolo canônico, ex.
  `"BTC"`), lado (compra/venda), quantidade, valor estimado.
- **`Decision`** (saída): `Allowed bool`, `Reasons []string` (motivo de
  cada rejeição, vazio se aprovado), `RulesChecked []RuleResult` (cada
  regra avaliada, com valor medido vs. limite).

Ordem de avaliação dentro de `Evaluate`:

1. **Estado operacional primeiro.** Lê `risk_state`. Se `paused` ou
   `kill_switch`, rejeita imediatamente com esse motivo, sem avaliar
   mais nada.
2. **Concentração** (usa apenas `PortfolioState` — sem I/O): % do
   ativo na carteira após a operação, % total em cripto, valor da
   operação. Viola → rejeita só esta operação.
3. **Perdas** (usa apenas `PortfolioState` — sem I/O): perda diária,
   perda semanal, drawdown, prejuízos consecutivos. Viola → rejeita a
   operação **e** transaciona a atualização de `risk_state` para
   `paused` na mesma transação de banco que grava a decisão — essas
   regras indicam sofrimento no nível da carteira inteira, não de um
   ativo isolado, e por isso disparam a pausa automática que a spec
   pede.
4. **Qualidade do ativo/dado** (consulta o TimescaleDB): volatilidade
   recente (calculada a partir dos candles mais recentes), liquidez
   (volume recente), idade do candle mais recente para aquele ativo.
   Falha ao consultar (banco indisponível, sem dado para o ativo) ⇒
   modo seguro: trata como violação e rejeita — nunca assume que está
   tudo bem na ausência de dado.
5. **Registro.** Toda avaliação (aprovada ou não) é gravada em
   `risk_decisions`, com todas as regras checadas e o motivo de cada
   uma — é o rastro de auditoria que a spec pede ("explicar não apenas
   o que fez, mas por que").

## Tratamento de erros e resiliência

- **Modo seguro por padrão**: qualquer falha ao obter dado necessário
  para uma regra (banco fora do ar, candle ausente, dado
  desatualizado) resulta em rejeição da operação, nunca em aprovação
  silenciosa.
- **Transição de estado é atômica**: mudar `risk_state` e gravar a
  decisão correspondente em `risk_decisions` acontece na mesma
  transação — nunca fica um estado pausado sem o registro do motivo,
  nem um registro de motivo sem a pausa de fato aplicada.
- O motor **nunca escreve** em `PortfolioState` nem em qualquer tabela
  do sub-projeto 1 — é somente leitura de mercado, e leitura/escrita
  apenas das suas três tabelas próprias.
- Reabrir de `paused`/`kill_switch` para `normal` é uma ação manual
  (uma função exposta, ex. `Reset(ctx, reason string) error`) — o
  motor nunca se despausa sozinho.

## Testes

- Testes unitários por categoria de regra (concentração, perdas) com
  `PortfolioState` injetado — funções puras, sem banco.
- Testes de integração para as regras de qualidade de ativo/dado e
  para persistência de estado/log, contra um TimescaleDB real: candles
  conhecidos inseridos no teste, depois verifica a decisão resultante.
- Teste específico do gatilho automático: violar uma regra de perda
  resulta em `risk_state = paused`, verificável numa chamada seguinte.
- Teste do modo seguro: simular ausência de dado de mercado para um
  ativo e confirmar que a operação é rejeitada, não aprovada.

## Critério de conclusão desta fase

O motor de risco está pronto quando:

- `Evaluate` aplica corretamente todas as regras de concentração,
  perdas e qualidade de ativo/dado listadas neste documento, contra
  cenários de teste cobrindo aprovação e cada tipo de rejeição.
- Uma violação de regra de perda transiciona `risk_state` para
  `paused` de forma atômica e auditável.
- Toda chamada a `Evaluate` produz uma linha em `risk_decisions` com
  motivo claro, aprovada ou não.
- Ausência ou indisponibilidade de dado de mercado necessário resulta
  em rejeição, nunca em aprovação.
- O módulo roda testes de integração contra o TimescaleDB real do
  sub-projeto 1, sem duplicar a instância de banco.
