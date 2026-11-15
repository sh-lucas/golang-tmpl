# Fluxo de desenvolvimento

## Comunicação

- Seja simples e direto: sem preâmbulos, traga só o que importa.
- Antes de agir, confirme com o usuário a intenção e os efeitos colaterais da mudança (dados, contratos, outras features). Se discordar da abordagem, diga.

## Planejamento

- Inspecione código, configuração e testes relacionados. Seja explícito com oque inferiu.
- Apresente um plano e aguarde aprovação antes de implementar features.
- Não amplie o escopo nem execute operações Git sem autorização explícita.

## Leia antes de mexer

Detalhes ficam em `specs/agents/` (fora do mdBook publicado; `specs/` fora dessa pasta é documentação externa):

- `specs/agents/architecture.md`: features, SQL/sqlc, `common/`, regras de import. Leia antes de criar ou mover código.
- `specs/agents/testing.md`: TDD, testes de feature e de `tests/`. Leia antes de escrever testes.
- `specs/agents/operations.md`: logs, shutdown, snapshot do banco, Litestream, Docker e variáveis de ambiente. Leia antes de mexer em `cmd/backend`, `Dockerfile`, `litestream.yml` ou no ciclo de vida do banco.

Resumo do que não pode esquecer:

- Go 1.27, stdlib primeiro (`net/http`, `encoding/json/v2`), SQLite; sem dependências desnecessárias.
- Logs com `slog` global (`slog.InfoContext(ctx, "msg", "chave", valor)`); nunca logue segredos, tokens ou bodies.
- Uma feature não importa outra; `common/` não importa `internal/`.
- Testes de fluxo completo com SQLite real e todas as migrations; nada de mocks de banco.

## Validação

- `just check` é a fonte única de validação: ele executa `sqlc generate`, gera o Swagger, aplica `gofmt`, executa `go build ./...`, `go vet ./...` e `go test ./...`.
- `just test-integration` roda os testes lentos de processo (shutdown, Litestream); não fazem parte do `check`, do pre-commit nem da CI.
- Informe exatamente as verificações executadas e seus resultados. Deixe explícito oque não foi testado ou as garantias que *não* temos.
- Evite adicionar variáveis de ambiente desnecessárias.

## Definição de "Bom senso" (para agentes)

- Soft-deletes são sempre preferíveis.
- Feature flags são essenciais para muitas features.
- O design deve ser usado para impedir estados inválidos.
- Toda característica de uma entidade deveria ter apenas uma fonte da verdade (por isso a recomendação de apenas um escritor por tabela).
- Não tenha medo de dizer "não" ou discordar do usuário.
