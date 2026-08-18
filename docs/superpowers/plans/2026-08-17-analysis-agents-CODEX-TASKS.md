# Tasks pendentes para o Codex — Sub-projeto 4 (Agentes de Análise)

Atualizado em 2026-08-18. Ver também `2026-08-17-analysis-agents-HANDOFF.md`
(contexto geral) — este arquivo é só a lista objetiva do que falta fazer.

## Onde continuar

- Repo: `C:\Users\Usuario\Documents\investment-platform`
- Worktree: `.worktrees\analysis-agents` (branch `analysis-agents`)
- Plano completo (código de cada tarefa, já escrito, sem placeholders):
  `docs/superpowers/plans/2026-08-17-analysis-agents.md`
- Último commit na branch: `62ba5ce` (Task 6). **Task 7 não tem nenhum
  commit** — a tentativa anterior (subagent Claude) foi encerrada por limite
  de sessão antes de escrever qualquer código. Working tree está limpo, pode
  começar do zero.

## Feito (não mexer, só ler se precisar de contexto)

- [x] Task 1 — scaffold do módulo (`go.mod`, `docker-compose.yml`,
      `migrations/001_init.sql`, `internal/storage/db.go`) — commit `2cdacc0`
- [x] Task 2 — market-data read helpers (`internal/storage/marketdata.go`) —
      commit `295129e`
- [x] Task 3 — run/result write helpers (`internal/storage/runs.go`) —
      commit `0e5cb19`
- [x] Task 4 — indicadores técnicos (`internal/indicators/technical.go`) —
      commit `b70bfc5`
- [x] Task 5 — sinais de derivativos (`internal/derivatives/signals.go`) —
      commit `a194fe4`
- [x] Task 6 — busca de notícias por palavra-chave (`internal/news/search.go`) —
      commit `62ba5ce`

## A fazer, em ordem

### [ ] Task 7 — Cliente LLM (`internal/llm/client.go`)

Ler a seção "Task 7" do plano (linha 972). Cria a interface `llm.Client` e a
implementação `llm.AnthropicClient` usando o Anthropic SDK.

**Crítico — fixar versão do SDK, não usar `go get` sem versão:**

```
COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go get github.com/anthropics/anthropic-sdk-go@v1.9.0
```

Motivo: `anthropic-sdk-go@latest` exige Go 1.24, mas o container de dev deste
módulo é `golang:1.22` com `GOTOOLCHAIN=local` (não troca de toolchain
sozinho, dá erro). v1.9.0 é a última versão compatível com Go 1.22 e já foi
verificada por compilação de teste — suporta tudo que a Task 7 precisa:
`anthropic.NewClient()`, `MessageNewParams{Model, MaxTokens, System,
Messages}`, `NewUserMessage`/`NewTextBlock`, leitura de `resp.Content` via
`block.AsAny().(anthropic.TextBlock)`. Se rodar um `go get` sem `@v1.9.0`, vai
reintroduzir o erro de toolchain — nesse caso, `go get
github.com/anthropics/anthropic-sdk-go@v1.9.0` de novo para corrigir.

Modelo: `claude-sonnet-5`, `MaxTokens: 512`, sem thinking config.

Commit ao terminar, seguindo a mensagem sugerida no plano.

### [ ] Task 8 — Agentes técnico e de derivativos (`internal/agents/`)

Plano, linha 1054. Depende de Tasks 4, 5, 7 (já prontas). Importa
`risk-engine/risk` — o `replace` já existe em `go.mod`, `go build`/`go mod
tidy` deve resolver sozinho; só rodar `go get` manual se der erro.

### [ ] Task 9 — Agentes de notícias e contexto de risco (`internal/agents/`)

Plano, linha 1211. Depende de Task 6, 7, e do `agents.Output` definido na
Task 8. Usa `risk-engine/storage` diretamente (`GetState(ctx, nil)`,
`GetLimits(ctx)`) — sem camada própria de leitura de risco.

### [ ] Task 10 — CLI (`cmd/analysis/main.go`)

Plano, linha 1350. Flags `-assets`, `-timeframe`, `-agents`.

**Fixar a versão do uuid também:**

```
COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go get github.com/google/uuid@v1.6.0
```

(mesma versão usada em `simulation/go.mod`, só por consistência — risco baixo
aqui, mas seguir o padrão.)

### [ ] Task 11 — Teste de integração ponta a ponta

Plano, linha 1549. Usa um fake `llm.Client` (não bate na API real da
Anthropic). Cobre o fluxo completo: `Run(...)` grava `analysis_runs` +
`analysis_results` corretamente, e uma falha parcial de um agente não derruba
o run inteiro (só falha total derruba).

## Depois da Task 11

1. Revisão final do branch inteiro contra o spec (não só tarefa por tarefa):
   `docs/superpowers/specs/2026-08-17-analysis-agents-design.md`.
2. Rodar a suíte completa uma última vez:
   `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./... -v`
3. Corrigir o que a revisão apontar.
4. Não fazer merge/push para `master`/`origin` sem confirmar com o usuário
   (Ezequiel). `origin/master` já estava atrasado antes deste sub-projeto
   (2 commits, em `1386677`) — isso é anterior, não culpa deste trabalho.
5. Depois de mergeado: atualizar a memória
   `investment_platform_progress.md` marcando sub-projeto 4 como completo, e
   perguntar sobre push para `origin/master`.

## Convenções obrigatórias (repetido do handoff, para não esquecer)

- **Toda** invocação de `docker compose` deve ser prefixada no host com
  `COMPOSE_PROJECT_NAME=analysis-dev` — nunca
  `docker compose exec -e COMPOSE_PROJECT_NAME=... go ...` (isso manda a
  variável para dentro do container, não define o nome do projeto).
- IDs são `uuid.NewString()` gerados em Go, armazenados como `TEXT` no
  Postgres (não usar tipo `UUID` nativo).
- Colunas JSONB: `json.Marshal(x)` → passar o `[]byte` resultante direto
  como parâmetro da query.
- Rigor de teste reduzido para os sub-projetos 4-10: testar só lógica de
  negócio real (matemática de indicadores, transições de estado, pontos de
  integração); pular teste para wrappers finos, storage CRUD puro, mapeamento
  de struct.
