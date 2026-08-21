# Validação Quantitativa e Auditoria de Performance

Status: Aprovado para planejamento de implementação
Data: 2026-08-20
Sub-projeto 11 da plataforma pessoal de investimentos autônomos.

## Objetivo

Adicionar uma camada **observacional** de pesquisa e auditoria: registrar
hipóteses e tentativas de backtest de forma reproduzível, medir a trajetória
de patrimônio e derivar métricas de execução já suportadas pelos dados. A
camada não gera sinais, não recomenda alocação e não pode chamar os caminhos
de `strategist`, `execution`, `mcp` ou `risk-engine`.

O objetivo é reduzir falsa confiança em um resultado histórico e tornar
explícitas as limitações dos dados/executabilidade antes que uma estratégia
seja considerada para operação em paper ou testnet.

## Escopo da primeira fase

- Novo módulo Go `validation`, com migration e CLI sob demanda.
- Registro imutável de hipótese, versão, universo, período, custos,
  parâmetros, métricas primárias e plano de splits temporais.
- Auditoria de um `backtest_run_id` existente, persistindo:
  configuração, fingerprint do dataset, commit Git opcional, métricas e
  findings de qualidade.
- Métricas derivadas, somente de leitura, sobre curvas de equity existentes:
  máximo drawdown, drawdown corrente, duração de recuperação e tempo sob a
  máxima histórica.
- Métricas de execução derivadas de dados reais existentes: slippage realizado
  em basis points para fills que tenham preço solicitado e preço médio de fill.
- Métrica de turnover de backtest calculada contra equity médio, quando a
  curva e os trades forem suficientes.
- Splits temporais explícitos (`train`, `validation`, `holdout`) e validação
  de que não se sobrepõem. O módulo registra embargo/purga declarados; não
  inventa uma política estatística onde ela não foi especificada.
- Endpoint read-only no `web-api` para consultar relatórios finalizados e
  página de dashboard apenas após existir uma necessidade concreta de leitura.

## Fora de escopo

- Alterar sizing, risco, prompts, sinais, ordens, paper trading ou Binance.
- Usar retorno histórico, Sharpe ou LLM para aprovar automaticamente uma
  estratégia para execução.
- Otimização automática de parâmetros, seleção do melhor backtest ou ML.
- Taxa de acerto por decisão, P&L por lote e funding P&L: os dados atuais não
  possuem fechamento/lot matching nem ledger de pagamentos de funding.
- Spread, profundidade de livro e impacto de mercado: o projeto ainda coleta
  OHLCV, não bid/ask ou order book. Slippage real é reportado quando houver
  fill, mas não modelado como certeza no backtest.
- Classificações preditivas de ciclo, sentimento ou regime. Regimes futuros
  devem ser alertas transparentes e validados separadamente.

## Princípios e guardrails

1. **Hipótese antes do resultado.** Uma run não inicia sem descrição
   falsificável, universo, horizonte, custos, métricas e split declarados.
2. **Disponibilidade temporal.** Inputs posteriores ao instante avaliado
   invalidam a run; o módulo nunca trata isso como resultado parcial.
3. **Evidência não é recomendação.** Status possíveis: `completed`,
   `inconclusive`, `invalid` ou `failed`; nenhum significa "operar".
4. **Configuração reproduzível.** A configuração canônica é serializada,
   recebe SHA-256 e é preservada com contagens/fingerprint de dados.
5. **Tentativas visíveis.** Variantes vinculadas à mesma hipótese são
   registradas; não se apresenta apenas a melhor tentativa isolada.
6. **Execução honesta.** Custos/fill assumptions ausentes produzem finding
   explícito; nunca são preenchidos com zero silenciosamente.
7. **Sem acoplamento decisório.** O módulo só lê tabelas de outros módulos e
   escreve suas próprias tabelas.

## Modelo de dados

`validation` é dono das tabelas abaixo. IDs são `TEXT` gerados por
`uuid.NewString()`.

- `validation_hypotheses`: contrato pré-registrado e versionado da hipótese.
- `validation_runs`: uma auditoria/execução, com status, configuração JSON,
  hash, referência textual opcional ao backtest e erro.
- `validation_splits`: janelas temporais declaradas e embargo em minutos.
- `validation_metrics`: nome, valor, segmento e unidade de cada métrica.
- `validation_findings`: severidade, regra, evidência e mensagem.
- `validation_attempts`: vínculo de variantes/tentativas da mesma hipótese.

Não há foreign keys para tabelas de outros módulos: referências como
`backtest_run_id` permanecem textos auditáveis para preservar propriedade de
schema e permitir auditoria de registros históricos.

## Métricas e limitações

Para curvas de equity, o cálculo usa máximas históricas e timestamps:

- `max_drawdown_pct`;
- `current_drawdown_pct`;
- `max_recovery_duration_seconds` para episódios recuperados;
- `current_time_under_water_seconds` se o último episódio ainda estiver
  aberto;
- `time_under_water_seconds` acumulado em episódios recuperados.

Para execução real, `realized_slippage_bps` é calculado apenas se quantidade
preenchida e preços solicitado/médio forem válidos. O sinal é normalizado por
lado: fill acima do solicitado piora compra; fill abaixo do solicitado piora
venda. Fill parcial é identificado como limitação, não convertido em P&L.

`turnover_pct` de backtest é notional absoluto de fills dividido pelo equity
médio disponível na curva. Se não houver equity suficiente, é `inconclusive`.

Funding é exposto apenas como contexto de mercado já existente; nenhum P&L de
funding é estimado sem um ledger confiável.

## Critérios de conclusão

- Uma hipótese inválida (campos obrigatórios/splits inválidos) não gera run.
- Auditoria de backtest persiste config/hash, métricas e findings sem alterar
  qualquer tabela do `simulation`.
- Curvas conhecidas produzem métricas exatas de drawdown e recuperação.
- Slippage de buy/sell é normalizado corretamente e dados insuficientes são
  explicitamente marcados.
- Uma run com custo ou preço executável ausente fica `inconclusive`/`invalid`,
  nunca "aprovada" silenciosamente.
- Testes não chamam APIs externas e todo o módulo continua sem caminho de
  escrita para risco/estratégia/execução.
