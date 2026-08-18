# Handoff — Sub-projeto 4 (Agentes de Análise), execução em andamento

Escrito em 2026-08-17 pela sessão Claude que fez o brainstorming, o spec, o
plano e começou a execução via `superpowers:subagent-driven-development`.
Objetivo deste arquivo: permitir que qualquer agente (Claude, Codex, ou
humano) retome exatamente daqui, mesmo sem acesso à skill/ferramentas de SDD
usadas por esta sessão.

## Onde as coisas estão

- **Branch:** `analysis-agents`
- **Worktree:** `.worktrees/analysis-agents/` dentro do checkout principal
  (`C:\Users\Usuario\Documents\investment-platform`)
- A branch foi criada a partir do `master` **local** (commit `7b2bae4`), que
  já contém o spec e o plano deste sub-projeto. **`origin/master` está
  atrasado** (em `1386677`, 2 commits antes) — isso é anterior a este
  sub-projeto, não uma consequência dele. Não dar push sem confirmar com o
  usuário, como sempre.
- **Spec:** `docs/superpowers/specs/2026-08-17-analysis-agents-design.md`
- **Plano:** `docs/superpowers/plans/2026-08-17-analysis-agents.md` — 11
  tarefas, cada uma com o código completo já escrito (sem placeholders). Este
  plano é **autossuficiente**: qualquer agente pode implementar as tarefas
  restantes lendo o plano diretamente, mesmo sem replicar o processo de
  subagents descrito abaixo.

## Processo em uso (por esta sessão Claude)

Skill `superpowers:subagent-driven-development`: um subagent implementador
por tarefa, revisão de tarefa (spec + qualidade) depois de cada uma, revisão
final de todo o branch no final. Ledger de progresso em
`.worktrees/analysis-agents/.superpowers/sdd/2026-08-17-analysis-agents/progress.md`
— **esse arquivo está no `.gitignore` (é scratch local), não sobrevive a um
clone novo nem a remoção do worktree.** Este documento é o resumo durável do
que importa dele.

**Se quem retomar não usa essa skill (ex: Codex):** não precisa replicá-la.
Basta seguir o plano tarefa por tarefa — cada tarefa já tem os arquivos a
criar/editar, o código completo, os comandos de teste e o comando de commit.

## Status (atualizado em 2026-08-18)

- ✅ Tarefas 1-6 implementadas, revisadas e aprovadas sem findings
  Critical/Important — commits `2cdacc0`..`62ba5ce` na branch
  `analysis-agents`.
- ❌ **Tarefa 7 (cliente LLM) foi despachada mas o subagent implementador foi
  encerrado por limite de sessão antes de commitar qualquer código.** Nenhum
  commit da Tarefa 7 existe na branch — worktree está limpo, começa do zero.
- ⬜ Tarefas 8 a 11 — não iniciadas.

**Lista de tasks objetiva para retomar (recomendado para quem não usa a
skill de SDD, ex: Codex):**
`docs/superpowers/plans/2026-08-17-analysis-agents-CODEX-TASKS.md`

## Decisão já tomada (ruling) — relevante para as Tarefas 7, 8, 9, 10

`anthropic-sdk-go@latest` resolve para uma versão que exige Go 1.24. O
container de dev deste módulo (`golang:1.22`, mesma imagem de
`market-data`/`risk-engine`/`simulation`) tem `GOTOOLCHAIN=local`, que dá
erro em vez de trocar de toolchain automaticamente.

**Decisão:** fixar `github.com/anthropics/anthropic-sdk-go@v1.9.0` (última
versão compatível com Go 1.22/1.21), manter `go.mod` em `go 1.22`, sem
alterar `GOTOOLCHAIN` no `docker-compose.yml`. Verificado por compilação de
teste que a API usada pelo plano (Tarefa 7: `anthropic.NewClient()`,
`MessageNewParams{Model, MaxTokens, System, Messages}`,
`NewUserMessage`/`NewTextBlock`, leitura de `resp.Content` via
`block.AsAny().(anthropic.TextBlock)`) compila normalmente em v1.9.0.

**Consequência prática:** como a Tarefa 1 é só scaffold (não importa
`uuid`/`anthropic-sdk-go`/`risk-engine` ainda), `go mod tidy` removeu essas
dependências do `require` de `go.mod` — só a linha
`replace risk-engine => ../risk-engine` ficou, sem `require` correspondente
(isso é esperado, não é bug). **Ao implementar as tarefas que de fato
importam esses pacotes:**

- **Tarefa 7** (`internal/llm/client.go`, importa `anthropic-sdk-go`): rodar
  `go get github.com/anthropics/anthropic-sdk-go@v1.9.0` **explicitamente
  com a versão** — nunca um `go get` sem versão (resolveria para a mais
  recente de novo e reintroduziria o problema do Go 1.24).
- **Tarefa 10** (`cmd/analysis/main.go`, importa `github.com/google/uuid`):
  fixar `@v1.6.0` explicitamente (mesma versão que `simulation/go.mod` usa),
  por consistência.
- **Tarefas 8/9** (importam `risk-engine/risk` e `risk-engine/storage`): o
  `replace` já existe; `go build`/`go mod tidy` deve re-adicionar
  `require risk-engine v0.0.0` sozinho ao primeiro import — não deve
  precisar de `go get` explícito, mas vale conferir se algo der errado.

## Convenção do repo: colisão de nome de projeto no docker compose

Sempre prefixar **todo** comando `docker compose` com
`COMPOSE_PROJECT_NAME=analysis-dev` (ou outro nome específico do worktree em
uso) — nunca `docker compose exec -e COMPOSE_PROJECT_NAME=... go ...` (isso
passa a variável para dentro do container, não define o nome do projeto no
host; é um erro que já apareceu e foi corrigido no texto do plano). O plano
em `docs/superpowers/plans/2026-08-17-analysis-agents.md` já foi escrito com
essa convenção aplicada em todo comando de toda tarefa — copiar os comandos
do plano literalmente é suficiente.

## Próximos passos para retomar

1. **Checar o resultado da revisão da Tarefa 1** (pode já ter chegado). Se
   aprovada sem findings: marcar completa e seguir para a Tarefa 2. Se
   houver findings Critical/Important: corrigir (reabrindo o mesmo
   implementador ou implementando a correção diretamente) e reverificar
   antes de prosseguir.
2. **Tarefas 2 a 11, em ordem:** cada uma no plano já tem os arquivos, o
   código completo, os comandos de build/teste e o comando de commit. Seguir
   a ordem — cada tarefa depende de tipos/interfaces definidos nas
   anteriores (ver a seção "Interfaces" de cada tarefa no plano).
3. **Depois da Tarefa 11:** revisão final de todo o branch (comparar contra
   o spec inteiro, não só tarefa por tarefa), corrigir o que aparecer,
   rodar a suíte completa (`go test ./... -v` dentro do container) uma
   última vez.
4. **Ao terminar:** usar (ou seguir manualmente) o fluxo de
   `superpowers:finishing-a-development-branch` — rodar os testes, e então
   escolher entre merge local para `master`, push + PR, ou manter a branch
   como está, conforme o usuário preferir. Não fazer merge/push sem
   confirmar com o usuário.
5. **Depois de mergeado:** atualizar a memória do projeto (arquivo
   `investment_platform_progress.md` na memória do Claude, se for o Claude
   retomando) marcando o sub-projeto 4 como completo, e perguntar ao
   usuário se deseja dar push para `origin/master` (que já estava atrasado
   antes deste sub-projeto começar).

## Arquivos-chave

- Plano completo (código de todas as 11 tarefas):
  `docs/superpowers/plans/2026-08-17-analysis-agents.md`
- Spec/design: `docs/superpowers/specs/2026-08-17-analysis-agents-design.md`
- Este handoff: `docs/superpowers/plans/2026-08-17-analysis-agents-HANDOFF.md`
- Ledger local (não versionado, só existe neste worktree enquanto ele
  existir):
  `.superpowers/sdd/2026-08-17-analysis-agents/progress.md`
