# Comitê de Pesquisa Multiagente e Ranking de Oportunidades

Status: Rascunho para revisão
Data: 2026-08-20
Sub-projeto 12 da plataforma pessoal de investimentos autônomos.

## Objetivo

Hoje `strategist.Decide` avalia cada ativo isoladamente: recebe as
narrativas técnica/derivativos/notícias/risco de **um** ativo e decide
buy/sell/hold para ele, sem nenhuma noção de que outros ativos também
foram analisados no mesmo ciclo. Não existe comparação relativa ("BTC
está com contexto melhor que ETH agora"), nem um papel que sintetize o
cenário macro do ciclo, nem um registro auditável de por que um ativo foi
priorizado sobre outro.

Este sub-projeto adiciona um **comitê de pesquisa multiagente**: papéis
especializados (cada um com sua própria narrativa e evidência, como um
analista técnico, um analista de notícias/macro e um "chefe de mesa" que
sintetiza) que produzem, ao final de cada ciclo de análise, uma
comparação estruturada entre todos os ativos analisados — tese, confiança
e evidência por ativo — mais uma ordenação determinística e reproduzível
de oportunidades. O objetivo é maximizar a qualidade **mensurável** da
decisão (mais contexto, mais evidência, comparação explícita), nunca
prometer previsão de mercado.

Este é o papel do **estrategista**, não da camada de validação
(sub-projeto 11): o comitê gera tese e prioridade; o `risk-engine`
continua sendo o único guardrail determinístico antes de qualquer
execução; a `validation` continua sendo o auditor a posteriori que mede
se as teses do comitê foram, na prática, boas.

## Escopo da primeira fase

- Dois novos tipos de agente no módulo `analysis`, seguindo o padrão já
  existente (`technical`, `derivatives`, `news`, `risk_context`):
  - `macro`: roda uma vez por ciclo (não por ativo), como `risk_context`
    hoje — sintetiza o regime de mercado do ciclo (volatilidade agregada,
    dispersão entre os ativos analisados, tom geral das notícias do
    universo) a partir dos dados já coletados nesse mesmo ciclo.
  - `committee`: roda uma vez por ciclo, **depois** de todos os agentes
    por ativo e do `macro`/`risk_context` terem terminado. Recebe as
    narrativas de todos os ativos do ciclo e devolve, por ativo, uma tese
    estruturada (bull/bear/neutro), confiança, `opportunity_score`
    qualitativo (0–1) e as evidências citadas (quais pontos das
    narrativas sustentam a tese).
- Uma nova tabela `analysis_rankings`, dona do `analysis`, com a
  ordenação **determinística** calculada em Go (não pelo LLM) a partir do
  `opportunity_score` do comitê combinado com sinais já existentes no
  projeto: idade do dado, liquidez e volatilidade (mesmos conceitos que
  `risk-engine` já usa como limites). Ver "Ranking determinístico"
  abaixo.
- `strategist` ganha um novo caminho de decisão que **lê** o ranking do
  ciclo e o inclui como contexto adicional no prompt — usado
  exclusivamente por `run_paper_strategist` (execução simulada). O
  caminho real (`run_strategist`/`RunWithDSN`) permanece **inalterado**
  nesta fase.
- Universo de ativos por ciclo continua pequeno e configurável (a mesma
  lista que o sistema já acompanha hoje, tipicamente 5–10 ativos) — o
  comitê não aumenta o número de ativos analisados, só compara os que já
  são.
- Persistência completa de tese, evidência e ranking para auditoria
  posterior pela `validation`.

## Fora de escopo (nesta fase)

- Alterar `run_strategist` real, sizing real, `risk-engine` ou execução
  real/testnet. O ranking só influencia decisões via
  `run_paper_strategist`.
- Promoção automática de paper para real. A transição depende de revisão
  humana explícita e de evidência da `validation` (sub-projeto 11) de que
  o ranking melhora resultado ajustado a risco — não é parte deste
  sub-projeto.
- Aumentar o universo de ativos analisados por ciclo ou adicionar novas
  fontes de dados (a ingestão de notícias já existente em
  `analysis/internal/news` é reaproveitada como está).
- Suporte a outras classes de ativo (ações, forex, etc.). O desenho evita
  acoplar nomes/tabelas a "cripto" onde for barato não fazer isso, mas
  nenhum adaptador de classe de ativo é construído agora — ver
  "Extensibilidade futura".
- Qualquer novo agente de execução, contabilidade de PnL realizado ou
  papel que decida sozinho sem passar pelo `risk-engine`.
- Voto livre entre múltiplos LLMs. O comitê é uma sequência de papéis
  determinística (mesma ordem, mesmos inputs, mesmo formato de saída
  auditável) — não uma votação onde modelos podem divergir de forma não
  reprodutível.

## Papéis do comitê

Mantendo o padrão de `agents.Output{Indicators, Narrative, Err}` já usado
por todo agente em `analysis`:

| Papel | Escopo | Quando roda | Já existe? |
|---|---|---|---|
| Analista técnico | por ativo | a cada ativo | sim (`agents.Technical`) |
| Analista de derivativos | por ativo | a cada ativo | sim (`agents.Derivatives`) |
| Analista de notícias | por ativo | a cada ativo | sim (`agents.News`) |
| Contexto de risco | uma vez/ciclo | após os agentes por ativo | sim (`agents.RiskContext`) |
| **Analista de regime (macro)** | uma vez/ciclo | após os agentes por ativo | novo |
| **Chefe de mesa (committee)** | uma vez/ciclo, mas produz saída por ativo | último, após todos os anteriores | novo |

O "chefe de mesa" não inventa dado novo — ele lê as narrativas já
produzidas pelos outros papéis (mesma limitação que `strategist.Decide`
já tem hoje: sem as três narrativas por ativo, o ativo é pulado) e as
compara. Divergência entre papéis (ex.: técnico otimista, notícia
negativa) não é escondida — vira parte da evidência citada na tese, para
a `validation` poder auditar depois se o comitê pesou bem o conflito.

## Princípios e guardrails

1. **Comparação, não previsão.** O comitê nunca declara "vai subir" —
   declara "este ativo tem, agora, evidência relativamente mais forte que
   os outros analisados", com a evidência explícita.
2. **Ranking é código, tese é LLM.** A ordenação final usada pelo
   `strategist` é uma fórmula determinística em Go, testável e
   reproduzível a partir dos dados persistidos — nunca "a ordem que o LLM
   listou". O LLM contribui um score qualitativo, não a posição final.
3. **Sem promoção silenciosa.** Nada neste sub-projeto libera execução
   real, muda sizing real ou altera limite de risco. Só o caminho
   simulado (`run_paper_strategist`) consome o ranking.
4. **Evidência auditável.** Toda tese do comitê referencia de qual
   narrativa/indicador ela veio. Sem isso, a `validation` (sub-projeto 11)
   não consegue depois avaliar se o comitê usou dado obsoleto ou
   insuficiente.
5. **Falha isolada, como hoje.** Se `macro` ou `committee` falharem, o
   ciclo de análise não é descartado — mesmo comportamento que os agentes
   atuais já têm em `analysis/runner.Run` (falha vira log, não aborta o
   run, a menos que nenhum agente tenha tido sucesso).
6. **Sem acoplamento de escrita.** `analysis` continua dono de suas
   próprias tabelas; `strategist` só lê `analysis_rankings` — mesmo
   padrão de leitura direta já usado no projeto (ex.:
   `strategist/internal/storage.LatestPrice` lendo `candles` do
   `market-data`).

## Fluxo de dados

```
run_analysis (ciclo)
  ├─ technical / derivatives / news   (por ativo, já existe)
  ├─ risk_context                     (uma vez/ciclo, já existe)
  ├─ macro                            (uma vez/ciclo, novo)
  └─ committee                        (uma vez/ciclo, novo — roda por último)
       │  lê todas as narrativas do ciclo, produz tese+evidência por ativo
       ▼
  analysis_rankings (novo, dono: analysis)
       │  Go calcula composite_score = f(opportunity_score do LLM,
       │  frescor do dado, liquidez, volatilidade) — determinístico
       ▼
run_paper_strategist (mcp, já existe — sub-projeto da simulação)
       │  lê analysis_rankings do run, inclui como contexto extra no
       │  prompt de Decide; run_strategist real não muda
       ▼
strategist_decisions (já existe) — decisões simuladas, mesmas tabelas/
                                     filtro de paper_decision_ids já
                                     construídos no sub-projeto anterior
```

## Modelo de dados

`analysis` continua dono de suas tabelas; a única tabela nova é:

- `analysis_rankings`: `run_id`, `asset`, `rank` (posição, 1 = melhor),
  `composite_score` (float determinístico), `opportunity_score_raw`
  (float, saída crua do LLM antes da combinação), `thesis` (bull/bear/
  neutro), `confidence`, `evidence` (JSON com as citações que
  sustentaram a tese), `computed_at`.

O resultado textual do papel `committee` em si (a narrativa longa, por
ativo) é persistido em `analysis_results` normalmente, com
`agent_type = 'committee'` — reaproveita a tabela e o schema que já
existem, sem tabela nova para isso. `analysis_rankings` guarda só os
campos estruturados necessários para o cálculo determinístico e para o
`strategist` ler sem precisar reprocessar texto.

Não há foreign key para `strategist_decisions`: `strategist` lê
`analysis_rankings` por `run_id`/`asset`, mesmo padrão de leitura direta
entre módulos já estabelecido no projeto.

## Ranking determinístico

`composite_score` é calculado em Go, em `analysis`, logo após o papel
`committee` terminar — nunca no prompt, nunca pelo LLM. Fórmula da
primeira versão (simples, auditável, com constantes nomeadas — ajustável
depois de dados reais da `validation`):

```
composite_score =
    opportunity_score_raw
    * freshness_factor(idade_do_dado, max_data_age_minutes)
    * liquidity_factor(liquidez, min_liquidity)
    * volatility_factor(volatilidade, max_volatility)
```

Onde `freshness_factor`, `liquidity_factor` e `volatility_factor` são
multiplicadores em `(0, 1]` que penalizam dado velho, liquidez baixa ou
volatilidade acima do limite — usando exatamente os mesmos limiares que
`risk-engine` já expõe via `GetLimits` (`max_data_age_minutes`,
`min_liquidity`, `max_volatility`), para não inventar um segundo conjunto
de números divergente do que o risco já usa. `rank` é a ordenação
decrescente de `composite_score` entre os ativos do ciclo — em caso de
empate exato, desempate por `asset` (ordem alfabética), para o resultado
ser 100% reproduzível a partir dos mesmos inputs.

Esta função fica isolada e coberta por teste direto (dado um conjunto de
scores/idades/liquidezes conhecido, o ranking esperado é exato) — é
exatamente o tipo de lógica que a política de teste reduzida do projeto
ainda pede cobertura, por ser lógica de negócio real, não meramente
integração.

## Mudanças no strategist (somente paper)

`strategist/runner` ganha uma nova função pública, ao lado de
`RunWithExecutor` — não uma alteração de `Run`/`Decide` existentes:

- Lê `analysis_rankings` do `run_id` (leitura direta, mesmo padrão de
  `LatestPrice`).
- Para cada ativo, acrescenta ao prompt já construído por `buildPrompt`
  uma seção `[ranking]` com o `rank`, `composite_score` e `thesis` do
  ativo **e** um resumo do ranking dos demais ativos do ciclo (para dar
  ao LLM a mesma comparação relativa que o comitê já fez) — o prompt
  ganha contexto, a estrutura de decisão (`Decide`, sizing clamp,
  `risk.Evaluate`, execução) não muda em nada.
- É chamada exclusivamente pelo novo caminho usado por
  `run_paper_strategist` no `mcp`. `run_strategist` (real) continua
  chamando o `Run` de sempre, sem ranking, até que a `validation` e uma
  decisão humana explícita aprovem a promoção — isso é assunto de um
  sub-projeto futuro, não desta spec.

## Extensibilidade futura (fora de escopo agora)

O projeto pretende cobrir outras classes de ativo além de cripto. Nesta
fase, nenhum adaptador é construído, mas o desenho evita alguns
acoplamentos caros de desfazer depois:

- `agents.Output` e as tabelas de `analysis` já são genéricas por
  `asset string` — nenhuma mudança de schema é necessária para outra
  classe de ativo aceitar as mesmas tabelas.
- `freshness_factor`/`liquidity_factor`/`volatility_factor` usam os
  limites já configuráveis do `risk-engine`, que também não são
  cripto-específicos.
- O ponto que precisaria de um adaptador de verdade (fonte de
  candles/derivativos por classe de ativo) já é isolado hoje em
  `market-data` — fora do escopo deste sub-projeto, mas é o lugar certo
  para essa extensão quando for necessária.

Nada disso é construído agora — é só o motivo de este desenho não
hardcodar suposições cripto-específicas onde já não custava nada evitar.

## Critérios de conclusão

- `macro` e `committee` seguem o mesmo contrato de falha isolada dos
  agentes existentes: falha de um não aborta o ciclo todo.
- `analysis_rankings` é 100% recalculável a partir de
  `analysis_results` + limites do `risk-engine` do momento — sem estado
  escondido.
- Ranking com empate produz ordem determinística (desempate por
  `asset`), testado diretamente.
- `run_strategist` real não muda de comportamento nem de prompt nesta
  fase — teste de regressão garante isso.
- `run_paper_strategist` com ranking disponível inclui a seção
  `[ranking]` no prompt; sem `analysis_rankings` para o run, cai de volta
  no prompt de hoje sem ranking (não quebra, só fica sem o contexto
  extra).
- Nenhuma tabela de `risk-engine`, `execution` ou `strategist_decisions`
  (schema) muda nesta fase.
