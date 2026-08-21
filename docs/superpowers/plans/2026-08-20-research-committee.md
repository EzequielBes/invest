# Comitê de Pesquisa e Ranking — Plano de Implementação

**Spec:** `docs/superpowers/specs/2026-08-20-research-committee-design.md`  
**Sub-projeto:** 12

## Entrega

1. Adicionar `macro` e `committee` ao `analysis/runner` em estágios fixos:
   agentes por ativo, `risk_context`, `macro`, `committee`.
2. Fazer o comitê devolver JSON validado por ativo; persistir sua narrativa,
   tese, score, confiança, evidências e os inputs de qualidade no
   `analysis_results`.
3. Criar `analysis_rankings` e calcular a ordenação em Go, com os limites
   atuais de frescor, liquidez e volatilidade do `risk-engine`; desempatar por
   símbolo.
4. Ler a tabela diretamente no `strategist`, acrescentando `[ranking]` apenas
   ao novo caminho público chamado por `run_paper_strategist`.
5. Não alterar `RunWithDSN`, `run_strategist`, sizing, schema de decisões nem
   execução real/testnet.

## Verificação

- Testes diretos do parser de saída não confiável do comitê e da fórmula de
  ranking/desempate.
- Regressão do `analysis/runner` para ordem fixa e falha isolada do comitê.
- Regressão do prompt sem ranking e teste da seção `[ranking]` no strategist.
- `go test ./...`, `go build ./...` e `go vet ./...` em `analysis`,
  `strategist` e `mcp`; aplicar e inspecionar a migration de `analysis`.
