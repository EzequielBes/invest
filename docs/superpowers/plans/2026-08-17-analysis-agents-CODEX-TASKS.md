# Tasks pendentes para o Codex — Sub-projeto 4 (Agentes de Análise)

**Atualizado em 2026-08-18: Tasks 7-11 concluídas e commitadas** (commits
`cb73f99`, `1f4a35b`, `de196c2` na branch `analysis-agents`). `go build ./...`
e `go test ./... -count=1` passam inteiros dentro do container
`analysis-dev`. Este arquivo não tem mais tasks pendentes de implementação —
mantido só como histórico de onde o handoff parou.

**O que falta agora não é implementação, é cobertura de teste adicional**
(TDD, para o Codex escrever): ver
`docs/superpowers/plans/2026-08-18-analysis-agents-TEST-CHECKLIST.md` — o
maior gap é `internal/agents/derivatives.go`, sem nenhum teste hoje.

Ver também `2026-08-17-analysis-agents-HANDOFF.md` para o contexto geral do
sub-projeto (ainda válido, exceto a seção de status que este arquivo
substitui).
