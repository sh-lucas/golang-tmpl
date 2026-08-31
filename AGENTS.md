# Fluxo de desenvolvimento

## Planejamento

- Inspecione código, configuração e testes antes de alterar.
- Apresente um plano e aguarde aprovação antes de implementar features.
- Não amplie o escopo nem execute operações Git sem autorização explícita.

## Delegação ─ Recomendação forte para operação no dia-a-dia.

- Para cada change request, delegue o ciclo de investigar, entender, implementar, testar e corrigir para exatamente um subagente pequeno. `gpt-5.6-luna (high)` e Claude Sonnet 5 são apenas exemplos adequados; evite agentes de geração anterior.
- O agente principal (quando 5.6 Terra/Sol, ou Opus/Sonnet) para revisar o resultado, conferir escopo e entregar. Ele não deve repetir o ciclo de pesquisa e correções do subagente.
- Após a revisão, o agente principal executa `just check` apenas uma vez como validação final. O subagente absorve as demais tool calls e tentativas necessárias para chegar a esse ponto.

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

- `just check` é a fonte única de validação: ele executa `sqlc generate`, gera o Swagger, aplica `gofmt`, executa `go vet ./...` e `go test ./...`.
- Antes de entregar, o subagente usa esse alvo durante as iterações; o agente principal executa `just check` somente uma vez como validação final. Não é necessário executar `go build`.
- Informe exatamente as verificações executadas e seus resultados.
