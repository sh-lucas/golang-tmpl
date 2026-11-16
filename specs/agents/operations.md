# Operação

## Logs

- `cmd/backend` configura o `slog` global com JSON em stderr, uma linha por evento. Coletores (Loki via Alloy/Promtail, driver do Docker) leem isso direto; o app não envia logs para lugar nenhum.
- Nos handlers use `slog.InfoContext(r.Context(), "mensagem", "chave", valor)` (ou `Warn`/`Error`). `log.Printf` também funciona e passa pelo mesmo handler, mas prefira atributos estruturados.
- Nunca logue segredos, tokens, o header `Authorization` ou bodies.

## Shutdown

- SIGINT/SIGTERM: para de aceitar conexões, fecha conexões que nunca enviaram nada (preconnect de navegador) e espera até 5s (`drainTimeout`) as requisições em andamento; depois disso força o fechamento. Um segundo sinal mata na hora.
- Em seguida fecha as sessões libSQL e o banco. Fechar a última conexão faz o checkpoint e remove `-wal`/`-shm`. Não há checkpoint explícito de propósito: com o Litestream rodando ele é o dono do WAL, e um `wal_checkpoint(TRUNCATE)` do app perde frames ainda não replicados ou trava esperando o leitor dele. Com Litestream, o `-wal` pode ficar no volume; isso é seguro.
- O orçamento total cabe nos 10s padrão do `docker stop`. O `kill_delay` do Air (`.air.toml`) é maior que esse orçamento para o `just run` nunca matar um shutdown limpo.

## Snapshot do banco (`/libsql/snapshot`)

Mesmo Bearer token do `/libsql` (`DATABASE_ACCESS_KEY`), que já dá controle total do banco.

- `GET`: cópia consistente e autocontida de `sqlite.db` (`VACUUM INTO`), sem precisar parar escritas.
- `PUT` (body = arquivo SQLite, até 4 GiB): um restore por vez (409 se outro estiver rodando). O upload vai para um arquivo temporário e só é aceito se:
  - for SQLite íntegro (`integrity_check`);
  - tiver `schema_migrations` com pelo menos uma migration, todas conhecidas por este binário (rejeita banco de outra aplicação, vazio ou de versão mais nova);
  - as migrations pendentes aplicarem nele sem erro e `foreign_key_check` ficar limpo.
  
  Só então o banco atual é copiado para `DATABASE_ROOT/backups/pre-restore-*.db` (desfazer; nunca é apagado automaticamente) e o upload é copiado por cima com a API de backup do SQLite, numa única transação: o pool continua válido, outras conexões veem o banco antigo ou o novo, nunca um meio-termo, e o Litestream replica o restore como uma escrita normal. Qualquer falha antes disso deixa o banco intacto.
- Escritas que caem entre a cópia `pre-restore` e o restore (milissegundos) não ficam no backup; o restore as sobrescreve de qualquer forma.
- Espaço em disco: um restore usa cerca de 3x o tamanho do banco (upload, cópia `pre-restore` e o banco).
- `large_blobs/` não entra no snapshot nem no restore.

## Litestream (opcional, desligado)

- Ative descomentando as três linhas no fim do `Dockerfile`. Configuração em `litestream.yml`; só variáveis de runtime: `LITESTREAM_REPLICA_URL` (com esquema, ex.: `s3://bucket/caminho`), `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY`. Nunca coloque credenciais no arquivo ou na imagem.
- O container roda `litestream replicate -exec /backend`: o Litestream repassa o SIGTERM ao app, espera ele sair e faz o sync final.
- Restore automático: com o volume vazio (sem `sqlite.db`), restaura o último backup antes de subir o app. Um banco existente nunca é sobrescrito. Só "nenhum backup ainda" sobe vazio; URL ausente/vazia, credencial errada ou réplica ilegível impedem a subida em vez de criar um banco vazio por cima do backup.
- Garantias que *não* temos:
  - Um banco novo só é monitorado no primeiro tick do Litestream (~1s); escritas feitas e encerradas antes disso no primeiro deploy não são replicadas.
  - Duas instâncias com a mesma `LITESTREAM_REPLICA_URL` corrompem o histórico da réplica; use uma URL por ambiente.
  - `large_blobs/` não é replicado.
  - Sem esquema na URL (`bucket/caminho`), o Litestream trata como diretório local.
  - `libsql-handler` v1.3.1 roda `wal_checkpoint(TRUNCATE)` 1s depois de cada uso do `/libsql`, sem opção para desligar. Com Litestream, isso trava a próxima escrita por ~5s (medido; sem perda de dados). Pendente: opção na lib para desligar esse checkpoint.

## Docker

- Imagem final `scratch` com `/backend`, o mdBook e os certificados de CA (HTTPS de saída). Não há shell; para inspecionar o banco use `/libsql` ou `/libsql/snapshot`.
**Jamais** teste imagens OCI com o litstream ligado ou apontando para o object storage remoto.
