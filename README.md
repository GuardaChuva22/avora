# Avora

Ecossistema agêntico pessoal e privado, distribuído entre Windows, VPS e nuvens oficiais de provedores.

**Status:** bootstrap de arquitetura e protótipo local, não produto funcional.

## Componentes
- `cmd/avora-server`: API inicial em Go (VPS/control plane).
- `internal/scheduler`: política de roteamento, testada e sem execução de tarefas.
- `apps/desktop`: futuro app Windows C++/Qt 6 + runtime local Go.
- `apps/mobile`: futuro APK Android Kotlin/Compose.
- `docs`: arquitetura, roadmap e matriz de 263 requisitos obrigatórios.

## Experimente
Go 1.23+:
```sh
go test ./...
go run ./cmd/avora-server
curl http://127.0.0.1:8080/healthz
```

A API atual NÃO executa comandos, não usa credenciais e não conecta IA. Ela escuta em loopback por padrão.

O motor (agente Avora/Codex/Claude) e o destino (Windows/cloud oficial/VPS) são escolhas independentes. PC online tem prioridade, sem pressupor que projetos locais estejam disponíveis na nuvem. Aprovações, isolamento, auditoria e controle de custo serão obrigatórios.

Documentos fundamentais: [catálogo completo — 263 requisitos](docs/requirements.md), [42 requisitos complementares](docs/requirements-complementares.md), [arquitetura](docs/architecture.md) e [roadmap](docs/roadmap.md).
