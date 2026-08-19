# Integração Multi-LLM (Codex + Claude)

Status: Aprovado para planejamento de implementação
Data: 2026-08-19
Sub-projeto 7 de 10 da plataforma pessoal de investimentos autônomos.

## Contexto e escopo

Este é o sétimo sub-projeto da plataforma de investimentos autônomos. Os
sub-projetos 4 (`analysis`) e 5 (`strategist`) já chamam um LLM — hoje
exclusivamente Claude, via um `internal/llm` próprio em cada módulo
(`analysis` narra indicadores, `strategist` decide buy/sell/hold via tool
use). Ambos os módulos exigem `ANTHROPIC_API_KEY` e falham se ela não
estiver setada.

Este sub-projeto adiciona a API da OpenAI como segundo provedor possível,
com seleção automática por disponibilidade: a plataforma deve funcionar
com qualquer um dos dois provedores sozinho (só Claude assinado, ou só
OpenAI), e — quando os dois estão disponíveis — usar um como principal
com *fallback* automático para o outro quando a chamada ao principal
falhar. O objetivo não é rodar os dois em paralelo por chamada (isso seria
o dobro do custo/latência para todo narrativa/decisão, sem necessidade
clara) — é resiliência: mais chance de uma chamada individual ter sucesso,
sem abrir mão de consistência (o mesmo provedor é usado por padrão,
enquanto ele estiver saudável).

Decomposição completa da plataforma (para referência):

1. Fundação de dados e histórico *(concluído)*
2. Motor de controle de risco *(concluído)*
3. Ambiente de simulação / backtest *(concluído)*
4. Agentes de análise *(concluído)*
5. Agente estrategista + motor de decisão *(concluído)*
6. Camada MCP *(concluído)*
7. **Integração multi-LLM (Codex + Claude)** ← este documento
8. Ambiente real (execução em exchanges/corretoras)
9. Frontend
10. Acompanhamento e aprendizado

### Escopo desta fase

- **Só os módulos `analysis` e `strategist`** — os únicos dois que chamam
  um LLM hoje. `market-data`/`risk-engine`/`simulation`/`mcp` não são
  tocados (o `mcp` continua chamando `analysis`/`strategist` via
  `RunWithDSN`, que por sua vez constrói o cliente LLM internamente — a
  troca de provedor fica transparente para o `mcp`, nenhuma mudança
  necessária lá).
- **Um cliente OpenAI novo por módulo**, implementando a mesma interface
  já existente em cada um (`analysis`: `Summarize(ctx, systemPrompt,
  userPrompt string) (string, error)`; `strategist`: `Decide(ctx,
  systemPrompt, userPrompt string) (Decision, error)`). SDK oficial
  (`github.com/openai/openai-go`) — verificado que a versão mais recente
  resolve em Go 1.22 sem forçar bump de toolchain (diferente do que
  aconteceu com `anthropic-sdk-go` no sub-projeto 4 e o SDK do MCP no
  sub-projeto 6), então **nenhuma mudança na versão do Go dos dois
  módulos** é necessária.
- **Modelo**: um modelo de chat de propósito geral da OpenAI (ex.
  `gpt-5`), não o Codex especializado em geração de código — a tarefa
  aqui é narrar indicadores e decidir compra/venda, não escrever código.
  Mesmo padrão dos clientes existentes: uma constante de string fixa no
  arquivo (`model = "claude-sonnet-5"` hoje; `model = "gpt-5"` no cliente
  novo), não configurável nesta fase.
- **`strategist`'s `Decide` estruturado** usa tool calling da OpenAI
  (`ChatCompletionNewParams.Tools` + `ToolChoice` forçando a função), o
  equivalente funcional ao tool use forçado que o cliente Anthropic já usa
  — mesmo contrato de saída (`Decision{Side, Confidence, SizingPct,
  Rationale}`), implementação diferente por trás da mesma interface.
- **Seleção de provedor por disponibilidade**, uma função nova por módulo
  substituindo o `NewAnthropicClient()` atual nos 4 pontos de chamada
  (`cmd/analysis`, `analysis/runner`, `cmd/strategist`,
  `strategist/runner`):
  - Só `ANTHROPIC_API_KEY` setada → cliente Anthropic direto, sem camada
    extra.
  - Só `OPENAI_API_KEY` setada → cliente OpenAI direto.
  - Nenhuma das duas setada → erro claro na inicialização (nunca uma
    operação silenciosa sem LLM nenhum).
  - As duas setadas → um cliente de *fallback* que envolve os dois: toda
    chamada tenta primeiro o provedor principal (configurável via
    `LLM_PRIMARY_PROVIDER=anthropic|openai`, default `anthropic` se a
    variável não estiver setada ou tiver um valor inválido) e, **só se
    essa chamada específica retornar erro** (rede, limite de taxa
    esgotado após os retries automáticos do SDK, recusa do modelo,
    resposta malformada — tudo que já vira `error` hoje em ambos os
    clientes), tenta a mesma chamada no provedor secundário. Sem estado
    entre chamadas — a próxima chamada tenta o principal de novo, mesmo
    que a anterior tenha caído para o secundário. Se as duas chamadas
    falharem, retorna o erro do secundário (a última tentativa), não o do
    principal.

### Fora de escopo (explicitamente adiado)

- **Rodar os dois provedores em paralelo e comparar/exigir consenso** —
  cogitado durante o brainstorm, descartado: dobra custo e latência de
  toda chamada, e o usuário confirmou que não é esse o objetivo aqui.
  Pode voltar como ideia separada no futuro se fizer sentido, mas não faz
  parte deste sub-projeto.
- **Fallback "pegajoso"** (grudar no secundário pelo resto da execução
  depois da primeira falha do principal) — decisão explícita do usuário:
  fallback por chamada individual, sem estado. Mais simples, e falhas
  reais costumam ser transitórias (rate limit, timeout), então a próxima
  chamada geralmente volta a funcionar no principal.
- **Terceiro provedor ou mais** — só Anthropic e OpenAI nesta fase.
- **Configuração de qual modelo específico usar por provedor** — cada
  cliente tem seu modelo fixo em uma constante, mesmo padrão já
  estabelecido nos clientes Anthropic existentes.
- **Mudar a versão do Go de `analysis`/`strategist`** — confirmado
  desnecessário (ver acima).
- **Qualquer mudança no módulo `mcp`** — a troca de provedor acontece
  inteiramente dentro de `analysis`/`strategist`'s `internal/llm` e é
  invisível para quem chama `RunWithDSN`.

## Arquitetura

```text
analysis/internal/llm/
├── client.go          (Client interface, AnthropicClient — já existe)
├── openai_client.go    (novo: OpenAIClient, mesma interface Client)
└── select.go           (novo: NewClient() — detecção de disponibilidade,
                          FallbackClient quando os dois estão presentes)

strategist/internal/llm/
├── client.go          (Client interface, AnthropicClient, Decision — já existe)
├── openai_client.go    (novo: OpenAIClient, mesma interface Client)
└── select.go           (novo: NewClient() — mesma lógica de analysis/internal/llm/select.go)
```

A lógica de seleção/fallback é escrita duas vezes (uma por módulo) em vez
de extraída para um pacote compartilhado — mesma filosofia de pequena
duplicação já usada em outros lugares do repo (ex. cada módulo tem sua
própria cópia fina de leitura de `candles`) em vez de criar acoplamento
entre módulos por um punhado de linhas.

Os 4 pontos de chamada (`cmd/analysis/main.go`, `analysis/runner.go`,
`cmd/strategist/main.go`, `strategist/runner.go`) trocam
`llm.NewAnthropicClient()` por `llm.NewClient()`, sem qualquer outra
mudança de assinatura — `NewClient()` retorna a mesma interface `Client`
que `NewAnthropicClient()` já retornava (via `*AnthropicClient`,
`*OpenAIClient`, ou `*FallbackClient`, todos implementando `Client`).

## Detecção de disponibilidade e seleção

```text
func NewClient() (Client, error) {
    hasAnthropic := os.Getenv("ANTHROPIC_API_KEY") != ""
    hasOpenAI := os.Getenv("OPENAI_API_KEY") != ""

    switch {
    case hasAnthropic && hasOpenAI:
        primary, secondary := resolvePrimary()  // lê LLM_PRIMARY_PROVIDER
        return &FallbackClient{primary: primary, secondary: secondary}, nil
    case hasAnthropic:
        return NewAnthropicClient(), nil
    case hasOpenAI:
        return NewOpenAIClient(), nil
    default:
        return nil, fmt.Errorf("llm: no provider available (set ANTHROPIC_API_KEY and/or OPENAI_API_KEY)")
    }
}
```

(Pseudocódigo — a assinatura exata, incluindo se `NewClient()` retorna
`error` ou cai em `log.Fatal` no chamador, é decisão do plano de
implementação, não deste spec.)

`FallbackClient` implementa `Client` (`Summarize`/`Decide`, conforme o
módulo) chamando o principal e, em caso de erro, chamando o secundário com
os mesmos argumentos, retornando o resultado (ou erro) do secundário.

## Tratamento de erros

- **Só um provedor configurado, e ele falha**: o erro sobe normalmente,
  mesmo comportamento de hoje — o chamador (`agents.Technical`,
  `strategist.Decide`, etc.) já trata falha de LLM como uma falha
  isolada por ativo/agente, não aborta a execução inteira (política já
  estabelecida nos sub-projetos 4 e 5).
- **Os dois configurados, principal falha, secundário funciona**: sucesso
  transparente — o chamador não sabe (nem precisa saber) que houve
  fallback. Não há log estruturado do fallback nesta fase (YAGNI — se
  isso importar para depuração depois, é fácil adicionar um log simples
  em `FallbackClient`).
- **Os dois configurados, os dois falham**: retorna o erro do secundário
  (última tentativa), tratado pelo chamador como qualquer outra falha de
  LLM hoje.
- **Nenhum provedor configurado**: erro na inicialização do processo
  (CLI ou dentro do `RunWithDSN` que o `mcp` chama) — nunca uma execução
  silenciosa sem LLM nenhum.

## Testes

Mesma política de rigor reduzido dos sub-projetos 4-10: teste direto onde
há lógica real de decisão (a seleção de provedor por variável de ambiente,
e o comportamento de fallback do `FallbackClient` — principal com
sucesso não chama o secundário; principal falha e secundário funciona
retorna o resultado do secundário; os dois falham retorna o erro do
secundário). Sem teste de integração real contra a API da OpenAI (custo,
não-determinismo) — mesma decisão já tomada para a Anthropic nos
sub-projetos 4 e 5; o cliente OpenAI novo só precisa compilar e ser
exercitado por um fake `Client` nos testes de fallback, do mesmo jeito que
o `AnthropicClient` já é hoje.

## Critério de conclusão desta fase

A integração multi-LLM está pronta quando:

- `analysis` e `strategist` funcionam normalmente com só
  `ANTHROPIC_API_KEY` setada (comportamento de hoje, inalterado).
- Ambos também funcionam com só `OPENAI_API_KEY` setada (nenhuma
  variável da Anthropic presente) — mesmas capacidades, provedor
  diferente por trás da mesma interface.
- Com as duas setadas, uma falha do provedor principal em uma chamada
  específica não impede aquela chamada de ter sucesso via o secundário,
  e a chamada seguinte volta a tentar o principal primeiro.
- Nenhuma das duas setadas produz um erro claro na inicialização, nunca
  uma tentativa silenciosa de operar sem LLM.
- `LLM_PRIMARY_PROVIDER` troca corretamente qual provedor é tentado
  primeiro quando os dois estão disponíveis.
