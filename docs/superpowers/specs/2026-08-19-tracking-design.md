# Acompanhamento e Aprendizado (Patrimônio Real ao Longo do Tempo)

Status: Aprovado para planejamento de implementação
Data: 2026-08-19
Sub-projeto 10 de 10 da plataforma pessoal de investimentos autônomos —
o último.

## Contexto e escopo

Este é o décimo e último sub-projeto da plataforma de investimentos
autônomos. Hoje o sistema executa ordens reais (sub-projeto 8) e mostra
um dashboard de acompanhamento (sub-projeto 9), mas não existe nenhum
registro histórico de como o patrimônio real evolui — o portfolio real
só é consultado no momento de cada decisão do `strategist`
(`execution/executor.FetchPortfolio`), nunca persistido por si só. Este
sub-projeto adiciona esse registro: um snapshot periódico do patrimônio
real (cash + posições valoradas), visualizável como uma curva de
patrimônio no dashboard já existente.

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. Agentes de análise *(concluído)*
5. Agente estrategista + motor de decisão *(concluído)*
6. Camada MCP *(concluído)*
7. Integração multi-LLM (Codex + Claude) *(concluído)*
8. Ambiente real (execução em exchanges/corretoras) *(concluído)*
9. Frontend *(concluído)*
10. **Acompanhamento e aprendizado** ← este documento

### Escopo desta fase

- **Novo módulo Go `tracking`**, seguindo a convenção já estabelecida do
  repositório. Contém um processo de longa duração (`cmd/tracker`, mesmo
  padrão do scheduler de coleta ao vivo do `market-data`) que, a cada
  intervalo fixo (15 minutos, configurável via variável de ambiente),
  consulta o patrimônio real na Binance testnet e persiste um snapshot.
- **Reaproveita `execution/executor`** (import Go direto, pacote já
  público) para consultar `FetchPortfolio` — sem reconstruir
  autenticação/assinatura HMAC do zero. Precificação das posições via
  candle `1m` mais recente, mesma lógica já usada por
  `strategist.buildPortfolio` (reimplementada localmente em `tracking`,
  não compartilhada — mesma filosofia de pequena duplicação já usada em
  outros lugares do repositório, ex. `strategist/internal/storage.LatestPrice`
  lendo a tabela `candles` de `market-data` sem depender daquele módulo).
- **Nova tabela `equity_snapshots`** (`ts`, `cash`, `positions_value`,
  `total_equity`) — mesma forma de `backtest_equity_curve`, o que permite
  reaproveitar o componente de gráfico de curva de equity já construído
  no sub-projeto 9 para o backtest.
- **`web-api` ganha um endpoint novo**, `GET /api/equity-snapshots`, lendo
  a tabela nova diretamente via SQL — mesmo padrão já usado pelos outros
  5 endpoints existentes (leitura pura, sem import Go de outro módulo).
- **`frontend` ganha uma aba nova**, "Patrimônio", reaproveitando o
  componente `EquityCurveChart` já existente (construído para a aba de
  Backtests no sub-projeto 9).
- **Escopo explicitamente reduzido, por decisão do usuário**: só a curva
  de patrimônio total. Nenhuma métrica de "taxa de acerto" por decisão
  (comparar preço na decisão vs. preço N horas depois) — cogitada no
  brainstorm, descartada para esta fase. Nenhum loop de feedback que
  ajuste o prompt do LLM com base em performance histórica — "aprendizado"
  nesta fase é só visibilidade histórica de performance, não um sistema
  que se auto-ajusta.

### Fora de escopo (explicitamente adiado)

- **Taxa de acerto por decisão** (comparação de preço de entrada vs.
  preço futuro) — cogitada, descartada para esta fase.
- **Loop de feedback para o LLM** (ajustar prompts/contexto do
  `strategist` com base em performance histórica) — "aprendizado" real no
  sentido de auto-ajuste do sistema; fora de escopo desta fase, que é só
  acompanhamento passivo.
- **Alertas/monitoramento operacional** (falhas de módulos, kill_switch
  do risk-engine, ordens travadas) — cogitado no brainstorm inicial,
  descartado em favor do foco em performance financeira real.
- **P&L por posição individual / matching de compra-venda** — o snapshot
  é do patrimônio total, não tenta reconstruir o P&L de cada trade
  fechado.
- **Qualquer mudança em `execution`, `strategist`, `risk-engine`,
  `analysis`, `simulation` ou `mcp`** — `tracking` só consome
  `execution/executor` (leitura, já público) e lê `candles` via SQL;
  nenhum desses módulos precisa de alteração. `web-api` ganha só um
  endpoint novo (leitura de uma tabela nova), sem alterar nenhum dos 6
  endpoints existentes.

## Arquitetura

```text
tracking/                        (novo módulo Go)
├── internal/storage/
│   ├── db.go                    # pgxpool.New(dsn), mesmo padrão de todo módulo
│   └── snapshots.go             # SaveSnapshot, RecentSnapshots
├── cmd/tracker/main.go          # loop contínuo (mesmo padrão do scheduler
│                                 # de coleta ao vivo do market-data):
│                                 # a cada SNAPSHOT_INTERVAL_MINUTES (default 15),
│                                 # FetchPortfolio → precifica via candle 1m →
│                                 # SaveSnapshot → dorme até o próximo ciclo
└── docker-compose.yml

web-api/internal/storage/equity.go   # novo arquivo: RecentEquitySnapshots
web-api/internal/api/equity.go       # novo arquivo: handler GET /api/equity-snapshots

frontend/src/pages/EquityPage.tsx    # nova aba, reaproveita EquityCurveChart
```

`tracking` importa `execution/executor` (dependência Go normal — pacote
público, mesmo padrão que `strategist` já usa para importar
`execution/executor` desde o sub-projeto 8). `tracking` não expõe nenhuma
API própria — só escreve na tabela `equity_snapshots`, que `web-api` lê
depois, exatamente como `web-api` já lê tabelas de outros 5 módulos sem
nenhum import Go entre eles.

## Fluxo de dados

```text
cmd/tracker (loop a cada 15min)
  → execClient.FetchPortfolio(ctx)         [cash, positions map[string]float64]
  → para cada posição: preço = candle 1m mais recente
  → positions_value = soma(quantidade * preço)
  → total_equity = cash + positions_value
  → SaveSnapshot(ts=now, cash, positions_value, total_equity)

GET /api/equity-snapshots?limit=N
  → SELECT ts, cash, positions_value, total_equity
    FROM equity_snapshots ORDER BY ts DESC LIMIT N

frontend "Patrimônio"
  → busca /api/equity-snapshots, reordena cronologicamente,
    renderiza com o EquityCurveChart já existente
```

## Tratamento de erros

- `FetchPortfolio` falha num ciclo do loop: registra o erro (stderr) e
  segue para o próximo ciclo — um ciclo perdido não é fatal, o próximo
  tenta de novo. Mesma filosofia de resiliência já usada no scheduler de
  coleta ao vivo do `market-data`.
- Preço de candle `1m` ausente para uma posição: mesma política já usada
  em `strategist.buildPortfolio` — esse ciclo específico falha (posição
  não pode ser valorada com segurança), logado e pulado, próximo ciclo
  tenta de novo.
- Erro ao salvar o snapshot: logado, ciclo seguinte tenta novamente — não
  derruba o processo.
- `web-api`: mesmo padrão já estabelecido nos outros 5 endpoints (erro de
  query vira 500 genérico, sem vazar detalhes de SQL).

## Testes

Mesma política de rigor reduzido dos sub-projetos 4-10: teste direto na
lógica de precificação/soma do snapshot e no storage (`SaveSnapshot`/
`RecentSnapshots`, contra `TEST_DATABASE_URL`), e no handler novo do
`web-api` (via fake, mesmo padrão dos outros 5). Sem teste de integração
contra a Binance real — mesma política já usada para `execution` no
sub-projeto 8 (o `FetchPortfolio` já é testado lá; aqui só a chamada e o
tratamento do resultado importam, testável com um fake do `executor.Client`).

## Critério de conclusão desta fase

O acompanhamento de patrimônio está pronto quando:

- `tracking`/`cmd/tracker` roda continuamente e persiste um snapshot real
  a cada ciclo (intervalo configurável, default 15 minutos).
- `GET /api/equity-snapshots` no `web-api` retorna os snapshots reais
  persistidos.
- O dashboard mostra uma aba "Patrimônio" com a curva de patrimônio real
  ao longo do tempo, usando dados reais (não os do backtest).
- Uma falha pontual (Binance indisponível, preço ausente) não derruba o
  processo `tracker` — ele continua tentando no próximo ciclo.
