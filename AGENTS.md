# Fluxo de desenvolvimento

## Comunicação

- Seja simples e direto: sem preâmbulos, traga só o que importa.
- Antes de agir, confirme com o usuário a intenção e os efeitos colaterais da mudança (dados, contratos, outras features). Se discordar da abordagem, diga.

## Planejamento

- Inspecione código, configuração e testes relacionados. Seja explícito com oque inferiu.
- Apresente um plano e aguarde aprovação antes de implementar features.
- Não amplie o escopo nem execute operações Git sem autorização explícita.

## Arquitetura

- Go 1.27, generics em métodos, prefira `net/http` e `encoding/json/v2`; evite instalar dependências desnecessárias. SQLite remove overhead e latência de rede e facilita testes de integração.
- Cada feature começa em `<feature>.go`: ele declara as rotas, mantém os comentários OpenAPI e contém as funções principais do endpoint. O `handler` permanece explícito e executa cada operação; os demais arquivos usam esse handler para responsabilidades específicas, como autenticação.
- Nomeie arquivos complementares como `<feature>_queries.sql`, `<feature>_test.go` e `<feature>_<responsabilidade>.go`.
- Crie features com `just mkhandler <nome>`. O SQL-fonte fica junto da feature e gera `<feature>/internal/queries`, importável só pela própria feature. Escreva o SQL, inclusive transformações de dados, em vez de queries cruas em Go.
- Serviços e integrações externas ficam em `<feature>/internal/<repositório>/`; as queries geradas (`internal/queries`) são um desses repositórios.
- Recomendação: cada tabela tem uma feature dona, a única que escreve nela. O `internal/` só impede importar as queries de outro dono; nada impede outro `.sql` de escrever na tabela, então respeite isso por convenção.
- Uma feature não importa outra, e `common/` não importa `internal/` (`internal/architecture` garante). Código compartilhado vai para `common/`. Para relaxar uma regra, altere esse teste num PR próprio.
- Prefira funções diretas, dependências explícitas e stdlib. Não crie abstrações sem necessidade.

## TDD

- Escreva primeiro um teste de comportamento e confirme que ele falha pelo motivo esperado.
- Implemente somente o necessário para fazê-lo passar; refatore apenas com a suíte verde.
- Aprofunde o teste cobrindo todas as invariantes importantes antes de dar uma feature como feita.
- Teste fluxos HTTP completos com `httptest` e SQLite `:memory:` real, executando todas as migrations.
- Não use mocks de banco. Mantenha testes ao lado da feature e evite casos repetitivos.
- Testes feature-wide na mesma pasta da feature, testes cross-feature em `tests/`.
- Não se faz teste unitário para CRUD. Teste fluxos completos, incluindo o armazenamento, conflitos, side-effects e consequências em testes longos e profundos.
- Dirty migrations: migrações com seeds *são* permitidas, e recomendadas para testar persistência entre migrations.

## Validação

- `just check` é a fonte única de validação: ele executa `sqlc generate`, gera o Swagger, aplica `gofmt`, executa `go build ./...`, `go vet ./...` e `go test ./...`.
- Informe exatamente as verificações executadas e seus resultados. Deixe explícito oque não foi testado ou as garantias que *não* temos.
- Evite adicionar variáveis de ambiente desnecessárias.

## Definição de "Bom senso" (para agentes)

- Soft-deletes são sempre preferíveis.
- Feature flags são essenciais para muitas features.
- O design deve ser usado para impedir estados inválidos.
- Toda característica de uma entidade deveria ter apenas uma fonte da verdade (por isso a recomendação de apenas um escritor por tabela).
- Não tenha medo de dizer "não" ou discordar do usuário.
