# Testes

## TDD

- Escreva primeiro um teste de comportamento e confirme que ele falha pelo motivo esperado.
- Implemente somente o necessário para fazê-lo passar; refatore apenas com a suíte verde.
- Aprofunde o teste cobrindo todas as invariantes importantes antes de dar uma feature como feita.
- Teste fluxos HTTP completos com `httptest` e SQLite real (`:memory:` ou arquivo em `t.TempDir()`), executando todas as migrations.
- Não use mocks de banco. Mantenha testes ao lado da feature e evite casos repetitivos.
- Não se faz teste unitário para CRUD. Teste fluxos completos, incluindo o armazenamento, conflitos, side-effects e consequências em testes longos e profundos.

## Onde ficam

- Testes de uma feature: na pasta da feature.
- Testes cross-feature e de processo: em `tests/`.
  - `migrations_test.go`: nomes, instruções proibidas, aplicação limpa e idempotente. Rápido; roda no `just check`.
  - Arquivos com `//go:build integration` compilam `cmd/backend` uma vez (`TestMain`) e rodam o binário de verdade, sem importar features. São lentos e **não** rodam no `just check`, no pre-commit nem na CI; rode com `just test-integration` ao mexer em `cmd/backend`, shutdown, snapshot ou Litestream. O `go vet -tags integration` do `check` garante que continuem compilando.
    - `shutdown_test.go`: drenagem de requisições, limite de tempo, segundo sinal, conexões nunca usadas, banco sem `-wal`/`-shm` ao sair.
    - `litestream_test.go`: o mesmo comando do bloco opcional do `Dockerfile`, com réplica `file://`. É pulado se `litestream` não estiver no `PATH`.
- Para esperar algo de forma determinística, use sinais do próprio protocolo (ex.: `Expect: 100-continue` confirma que o handler começou) em vez de `sleep`.
