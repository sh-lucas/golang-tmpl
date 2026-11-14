# Go HTTP template

Exemplo pequeno de API em Go 1.27 usando `net/http`, `encoding/json/v2`, SQLite e queries geradas pelo sqlc.

## Desenvolvimento

Instale as ferramentas de desenvolvimento:

Instale também o [Just](https://just.systems/) conforme o método disponível no seu sistema.

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go install github.com/air-verse/air@v1.63.0
go install github.com/swaggo/swag/cmd/swag@v1.16.4
go install github.com/evilmartians/lefthook/v2@v1.13.6
lefthook install
```

Depois execute:

```sh
just run
```

O Air executa `sqlc generate` antes de cada build. Copie `.env.example` para configurar `DATABASE_ROOT`, `SERVER_PORT`, `JWT_SECRET` e `DATABASE_ACCESS_KEY` obrigatórios. `DATABASE_ROOT` contém o SQLite principal em `sqlite.db` e os blobs grandes em `large_blobs/`. Ao executar receitas do Just, o `.env` é carregado automaticamente; executando o binário diretamente, a aplicação lê apenas variáveis exportadas pelo ambiente.

## Autenticação de exemplo

- `POST /admins`: cria o primeiro admin sem autenticação; os próximos exigem Bearer token.
- `POST /auth/login`: retorna um token de sessão válido por 24 horas.
- `GET /admins/me`: retorna o admin autenticado.
- `GET /health`: verifica o processo HTTP.
- `/libsql/`: expõe o mesmo SQLite pelo protocolo HTTP do libSQL. Use o token configurado em `DATABASE_ACCESS_KEY` como Bearer token; no DBeaver, informe-o como senha e deixe o usuário vazio, com a URL `http://localhost:3000/libsql`.

Criação e login recebem `{"email":"admin@example.com","password":"correct horse battery staple"}`.

Cada domínio vive em `internal/features/<feature>`. O SQL-fonte fica junto da feature e gera, pelo `sqlc.yaml`, o pacote `<feature>/internal/queries`, que só a própria feature consegue importar. Recomendamos que cada tabela tenha uma feature dona, a única que escreve nela; isso é convenção, porque outro `.sql` ainda pode escrever na tabela. Crie features com `just mkhandler <nome>`. As migrations são aplicadas automaticamente.

O teste `internal/architecture` falha se uma feature importar outra ou se `common/` importar `internal/`. A CI executa `just check` e falha se o código gerado ou a formatação não estiverem commitados.

O pre-commit formata os arquivos staged, regenera sqlc e Swagger, atualiza módulos e adiciona os resultados ao próprio commit com `stage_fixed`. Depois executa build, vet e testes através de `just check`.
