# Avora

Ecossistema agêntico pessoal e privado, distribuído entre Windows, VPS e nuvens oficiais de provedores.

**Status:** protótipo autenticado Windows ↔ VPS, somente tarefas seguras de teste. Não é produto funcional.

## Componentes
- `cmd/avora-server`: control plane autenticado em Go (somente loopback).
- `cmd/avora-runtime`: worker Go (Windows/VPS), heartbeat e ações limitadas a `ping`/`diagnostics`.
- `internal/control`: API de presença, dispositivos, jobs e autenticação de token.
- `internal/scheduler`: política de roteamento para Windows/VPS.
- `apps/desktop`: futuro app Windows C++/Qt 6 + runtime local Go.
- `apps/mobile`: futuro APK Android Kotlin/Compose.
- `docs`: arquitetura, roadmap e matriz de 263 requisitos obrigatórios.

## Experimente
Requer Go 1.23+. Testes: `go test ./...`. Para configurar tokens, subir servidor e runtime e enviar um job seguro, siga [o tutorial da POC autenticada](docs/runtime-poc.md).

**Atenção:** não executa shell, IA, Codex nem Claude; ações de teste e dados temporários somente em memória. O control plane deve permanecer em loopback, acessível remotamente apenas por túnel SSH ou proxy TLS.

O motor (agente Avora/Codex/Claude) e o destino (Windows/cloud oficial/VPS) são escolhas independentes. PC online tem prioridade, sem pressupor que projetos locais estejam disponíveis na nuvem. Aprovações, isolamento, auditoria e controle de custo serão obrigatórios.

Documentos fundamentais: [catálogo completo — 263 requisitos](docs/requirements.md), [42 requisitos complementares](docs/requirements-complementares.md), [arquitetura](docs/architecture.md) e [roadmap](docs/roadmap.md).
