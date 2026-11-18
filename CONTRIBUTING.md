# Como contribuir

Que bom que você quer ajudar. O Avora é um time de agentes que trabalha com você, sozinho ou em grupo.

## Antes de começar
- Leia o [plano do ecossistema](docs/plano-ecossistema.md), o [fluxo das telas](docs/fluxo-telas.md) e as [decisões](docs/decisions.md). Cada mudança grande vira uma linha nova nas decisões.
- Abra uma issue dizendo o que você quer fazer, para ninguém trabalhar na mesma coisa.

## Preparar o ambiente
- **Servidor:** Go 1.26 ou mais novo. Rode `go test ./...` antes de mandar qualquer mudança.
- **App de desktop (Windows):** Qt 6 com MSVC e o Visual Studio Build Tools. Para compilar, rode `powershell -ExecutionPolicy Bypass -File desktop\build.ps1`.
- **Servidor local para testar o app:** defina `AVORA_ADMIN_TOKEN` com 32 caracteres ou mais e rode `go run ./cmd/avora-server`. Sem `AVORA_DATABASE_URL`, os dados ficam só na memória.

## Regras do código
- **Nenhum valor fixo.** Endereço, porta, modelo, pasta e fuso vêm da configuração ou da tela.
- **Interface:**
  - texto em português do Brasil, com frases simples;
  - cores, fontes, raios e espaçamentos vêm do [`Theme.qml`](desktop/qml/Theme.qml);
  - use os componentes que já existem (`Card`, `Sheet`, `Pill`, `PrimaryButton`, `GhostButton`, `Field`...).
- **Banco:** toda mudança no `Store` existe nas duas implementações, memória e Postgres, com uma migração nova em `internal/store/migrations`.
- **Comentários:** em inglês, curtos, só quando explicam o porquê.

## Mandando a mudança
1. Faça um fork e crie uma branch.
2. Rode os testes do Go e compile o app, se você mexeu nele.
3. Abra um pull request explicando o que mudou e por quê, com um print se mexeu na tela.
