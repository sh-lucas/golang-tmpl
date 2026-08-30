# Fluxo de desenvolvimento

## Planejamento

- Inspecione código, configuração e testes antes de alterar.
- Apresente um plano e aguarde aprovação antes de implementar features.
- Não amplie o escopo nem execute operações Git sem autorização explícita.

## Arquitetura

- Use Go 1.27, generics, `net/http` e `encoding/json/v2`; evite frameworks.
- Organize cada domínio em `internal/features/<feature>`.
- Cada feature começa em `<feature>.go`: ele declara as rotas, mantém os comentários OpenAPI e contém as funções principais do endpoint. O `handler` permanece explícito e executa cada operação; os demais arquivos usam esse handler para responsabilidades específicas, como autenticação.
- Nomeie arquivos complementares como `<feature>_queries.sql`, `<feature>_test.go` e `<feature>_<responsabilidade>.go`.
- Mantenha o SQL-fonte junto da feature, enumere-o no `sqlc.yaml` e importe somente o pacote gerado `queries/`.
- Prefira funções diretas, dependências explícitas e stdlib. Não crie abstrações sem necessidade atual.

## TDD

- Escreva primeiro um teste de comportamento e confirme que ele falha pelo motivo esperado.
- Implemente somente o necessário para fazê-lo passar; refatore apenas com a suíte verde.
- Teste fluxos HTTP completos com `httptest` e SQLite `:memory:` real, executando todas as migrations.
- Não use mocks de banco. Mantenha testes ao lado da feature e evite casos repetitivos.

## Validação

- Após alterar SQL, execute `sqlc generate`; após alterar rotas, gere o Swagger.
- Antes de entregar, execute `gofmt`, `go vet ./...`, `go test ./...` e `go build ./cmd/backend`.
- Informe exatamente as verificações executadas e seus resultados.
