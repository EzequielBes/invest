# Fundação de Dados e Histórico (Market Data Foundation)

Status: Aprovado para planejamento de implementação
Data: 2026-08-15
Sub-projeto 1 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Esta é a primeira peça de uma plataforma pessoal de investimentos
autônomos (uso exclusivamente pessoal, sem terceiros). A plataforma
completa terá agentes de análise, motor de estratégia, controle de
risco, execução em ambiente real e simulado, integração multi-LLM e
frontend — mas nenhuma dessas peças pode existir sem uma fundação de
dados confiável. Este documento cobre **apenas** essa fundação.

Decomposição completa da plataforma (para referência; cada item é seu
próprio ciclo spec → plano → implementação):

1. **Fundação de dados e histórico** ← este documento
2. Motor de controle de risco
3. Ambiente de simulação / backtest
4. Agentes de análise (técnico, fundamentalista, notícias, macro, cripto, carteira)
5. Agente estrategista + motor de decisão
6. Camada MCP
7. Integração multi-LLM (Codex + Claude)
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Mercado:** apenas criptomoedas. Ações, renda fixa, ETFs e mercados
  internacionais ficam para fases futuras.
- **Exchanges:** Binance, Bybit, OKX.
- **Ativos:** lista curada dos ~20-30 ativos de maior liquidez/market
  cap, configurável.
- **Dados coletados:** candles OHLCV em múltiplos timeframes (1m, 1h,
  1d), funding rate, open interest, liquidações, e notícias brutas
  (RSS de CoinDesk e Cointelegraph).
- **Histórico:** backfill de 1-2 anos por ativo/exchange na primeira
  execução.
- **Ambiente de execução:** máquina local / home server pessoal.

### Fora de escopo (explicitamente adiado)

- Classificação/interpretação de notícias (sentimento, tipo de evento,
  confiabilidade) — isso é do futuro agente de notícias (sub-projeto 4).
- Qualquer lógica de decisão, risco ou negociação — esta fundação
  **só coleta e armazena**, não interpreta nem decide.
- API própria de consulta — consumidores leem o banco diretamente por
  enquanto (ver "Acesso aos dados" abaixo).
- Outras exchanges além de Binance/Bybit/OKX, e outros mercados além
  de cripto.

## Arquitetura

Um único serviço Go (`market-data`), rodando continuamente na máquina
local, organizado em módulos internos com responsabilidades isoladas:

```text
market-data/
├── collectors/
│   ├── binance/    (implementa a interface Collector)
│   ├── bybit/      (implementa a interface Collector)
│   ├── okx/        (implementa a interface Collector)
│   └── news/        (poller de RSS)
├── scheduler/       (orquestra backfill inicial + coleta contínua)
├── storage/         (camada de acesso ao Postgres/TimescaleDB)
└── main.go
```

Cada coletor de exchange implementa a mesma interface:

```go
type Collector interface {
    FetchCandles(ctx context.Context, symbol string, timeframe Timeframe, from, to time.Time) ([]Candle, error)
    StreamLive(ctx context.Context, symbols []string) (<-chan Candle, error)
    FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]FundingRate, error)
    FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]OpenInterest, error)
    FetchLiquidations(ctx context.Context, symbols []string) (<-chan Liquidation, error)
}
```

Essa interface comum isola as particularidades de cada API de exchange
(autenticação, formatos, limites de taxa) atrás de um contrato único,
para que o scheduler e a camada de armazenamento não precisem saber
qual exchange está por trás dos dados.

O **scheduler**:
- Na primeira execução por ativo/exchange, dispara backfill histórico
  via REST (candles, funding, OI) cobrindo 1-2 anos.
- Depois disso, mantém WebSockets abertos para candles/liquidações em
  tempo real, e faz polling periódico para funding/open interest
  (dados que nem toda exchange expõe via stream).
- Ao reiniciar, consulta `collector_runs` para detectar lacunas
  (períodos sem dados) e dispara backfill pontual para preenchê-las.

O **coletor de notícias** faz polling dos feeds RSS configurados
(CoinDesk, Cointelegraph) em intervalo fixo, armazenando cada item
como está — sem classificação.

## Banco de dados

**PostgreSQL com a extensão TimescaleDB.**

Motivo da escolha em vez de SQLite ou Postgres puro:
- TimescaleDB é feito para séries temporais: hypertables particionadas
  por tempo, agregação contínua (derivar candles de 1h a partir dos de
  1m) e políticas de retenção, sem implementar isso manualmente.
- Suporta múltiplos leitores concorrentes com segurança — Go escreve,
  Python (backtests futuros) e a futura camada MCP vão ler. SQLite
  trava em concorrência de leitura/escrita simultânea, o que se torna
  um problema assim que outras partes do sistema passarem a consultar
  os dados.
- Continua leve o suficiente para uso pessoal: um container Docker
  (Postgres + extensão Timescale) rodando localmente.

### Modelo de dados

| Tabela | Colunas principais | Observação |
|---|---|---|
| `assets` | `exchange, symbol, tracked_since` | catálogo de ativos rastreados |
| `candles` | `exchange, symbol, timeframe, timestamp, open, high, low, close, volume` | hypertable particionada por tempo |
| `funding_rates` | `exchange, symbol, timestamp, rate` | |
| `open_interest` | `exchange, symbol, timestamp, value` | |
| `liquidations` | `exchange, symbol, timestamp, side, price, quantity` | |
| `news_items` | `source, published_at, collected_at, title, body, url` | bruto, sem classificação |
| `collector_runs` | `collector, symbol, started_at, finished_at, status, error` | log de execuções; usado para detectar lacunas |

### Acesso aos dados

Sem API própria por enquanto (YAGNI). Python (backtests futuros) e a
futura camada MCP leem diretamente do Postgres via SQL/views. Se isso
virar gargalo de performance ou exigir controle de acesso mais fino
mais adiante, uma API interna pode ser adicionada depois sem alterar o
schema.

## Tratamento de erros e resiliência

- **Reconexão de WebSocket:** ao cair (queda de rede, restart da
  exchange), reconectar com backoff exponencial e usar REST para
  preencher a lacuna criada pela desconexão.
- **Detecção de lacunas:** `collector_runs` permite identificar
  períodos sem dados (ex: serviço ficou desligado) e disparar backfill
  pontual ao reiniciar.
- **Isolamento de falhas:** cada coletor roda em sua própria goroutine
  com seu próprio `recover`/log. Uma exchange falhando não derruba as
  outras nem o coletor de notícias.
- **Rate limiting:** um limitador por exchange, respeitando os limites
  de cada API (diferentes entre Binance/Bybit/OKX). Isso já importa
  nesta fase porque as fases futuras (motor de risco, agentes)
  dependem de dados não atrasados.
- Esta fundação **não decide nada** — não há lógica de "dado
  suspeito" além de logar lacunas. Validação de qualidade de dado
  para fins de decisão é responsabilidade de fases futuras (motor de
  risco, agentes).

## Testes

- Testes unitários por coletor, usando respostas de API gravadas
  (fixtures) por exchange — sem bater na API real durante os testes.
- Teste de integração do backfill, validando consistência entre
  candles de 1m/1h/1d.
- Teste de recuperação de lacuna: simula o serviço ficando offline por
  um período e verifica que o backfill pontual preenche corretamente
  ao reiniciar.

## Critério de conclusão desta fase

A fundação está pronta quando:
- O serviço roda continuamente na máquina local coletando candles
  (1m/1h/1d), funding, open interest e liquidações de Binance, Bybit e
  OKX para a lista curada de ativos.
- O backfill de 1-2 anos foi executado com sucesso para todos os
  ativos/exchanges configurados.
- O coletor de notícias está armazenando itens de CoinDesk e
  Cointelegraph.
- Uma consulta SQL simples (ou script Python) consegue ler candles,
  funding, liquidações e notícias diretamente do banco.
- Uma interrupção simulada do serviço é seguida de recuperação
  automática das lacunas ao reiniciar.
