# Arquitetura

- Go 1.27, generics em métodos, prefira `net/http` e `encoding/json/v2`; evite instalar dependências desnecessárias. SQLite remove overhead e latência de rede e facilita testes de integração.
- Cada feature começa em `<feature>.go`: ele declara as rotas, mantém os comentários OpenAPI e contém as funções principais do endpoint. O `handler` permanece explícito e executa cada operação; os demais arquivos usam esse handler para responsabilidades específicas, como autenticação.
- Endpoints usam `mug.Route(func(ctx, T) mug.Response)`. Campos de `T` sem tag especial vêm do body JSON; campos com `path:"id"`, `query:"limit"`, `header:"X-Key"` ou `cookie:"session"` vêm da requisição e precisam de `json:"-"` (o body nunca os preenche). Aceitam `string`, inteiros e `bool`; tag errada dá panic na subida. `validate` vale para todos, e erros aparecem como `"query.limit"`.
- Nomeie arquivos complementares como `<feature>_queries.sql`, `<feature>_test.go` e `<feature>_<responsabilidade>.go`.
- Crie features com `just mkhandler <nome>`. O SQL-fonte fica junto da feature e gera `<feature>/internal/queries`, importável só pela própria feature. Escreva o SQL, inclusive transformações de dados, em vez de queries cruas em Go.
- No SQL do sqlc, todo parâmetro que não vai direto para uma coluna (expressões, `||`, funções) usa `CAST(sqlc.arg(nome) AS TIPO)`: sem isso o Go gerado fica `Column1` e o tipo pode virar nulo. Numa query com `sqlc.arg`, nomeie todos os parâmetros: misturar com `?` gera índices errados (`?2` seguido de `?`) e quebra em runtime. Colunas nulas são geradas como ponteiros (`*string`), não `sql.Null*` (`emit_pointers_for_null_types`).
- Serviços e integrações externas ficam em `<feature>/internal/<repositório>/`; as queries geradas (`internal/queries`) são um desses repositórios.
- Recomendação: cada tabela tem uma feature dona, a única que escreve nela. O `internal/` só impede importar as queries de outro dono; nada impede outro `.sql` de escrever na tabela, então respeite isso por convenção.
- Uma feature não importa outra. Código compartilhado vai para `internal/<pacote>` (ex.: `blobs`, `queue`, `mug`), que recebe o `*sql.DB` por parâmetro e não importa `database/` (`tests/architecture_test.go` garante).
- `blobs` e `queue` guardam tudo no próprio SQLite: servem em produção, mas não substituem S3 ou RabbitMQ; confira se cobrem o caso antes de usar. Para relaxar uma regra, altere esse teste num PR próprio.
- Prefira funções diretas, dependências explícitas e stdlib. Não crie abstrações sem necessidade.

## Migrations

- Ficam em `database/migrations/NNN_snake_case.sql`, numeradas em sequência sem buracos, e rodam automaticamente na abertura do banco, cada uma na própria transação.
- Não use `BEGIN`/`COMMIT`/`PRAGMA`/`VACUUM` dentro delas; `tests/migrations_test.go` barra isso.
- Nunca edite uma migration já aplicada em produção; crie uma nova.
- `database/schema.sql` é o schema após todas as migrations, gerado pelo `just check` (`cmd/schema`); não edite à mão.
- Migrações com seeds (*dirty migrations*) são permitidas, e recomendadas para testar persistência entre migrations.
