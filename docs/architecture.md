# Avora — Arquitetura

Visão técnica da [spec](spec.md). Decisões e motivos em [decisions.md](decisions.md).

## Componentes

| Componente | Linguagem | Onde roda | Papel |
|---|---|---|---|
| `avora-server` | Go | VPS | Control plane: conversas, jobs, fila, roteador, portão de políticas, log de eventos, heartbeat, rotinas, API dos apps |
| `avora-runtime` | Go | VPS e PC | Nó: heartbeat, anúncio de capacidades, execução de jobs, adaptadores de motor |
| Postgres (`avora`) | — | VPS | Estado durável e log de eventos (pgvector pra busca semântica, Fase 8) |
| `omniarch-mcp` | a definir | VPS | Adaptador MCP sobre a API REST do Omniarch |
| ntfy | — | VPS | Push pro Android (em aberto) |
| Apps | a definir | Windows, Android | Chat, Atividade, Jobs, aprovações, configurações |

## Fluxo de um job

```
app/rotina/heartbeat/gatilho
        │
        ▼
   avora-server ── roteador (nó + motor + motivo) ──► evento job.routed
        │
        ▼
   fila durável ──► avora-runtime (claim)
                         │
                         ▼
                 adaptador de motor (Claude Code / Codex / nativo)
                         │  cada chamada de ferramenta
                         ▼
                 portão de políticas ──► evento tool.requested
                    │        │
              permitido   precisa aprovação ──► push ──► você decide
                    │
                    ▼
                 execução ──► evento tool.completed ──► stream pro app
```

## Contratos principais

- **Node:** id, nome, tipo (`vps`, `windows`, `android`), hash do token, capacidades, last_seen, revogado_em.
- **Agent:** id, versão, nome, cargo, persona, motor, modelo, esforço, ferramentas, política, responsabilidades.
- **Job:** id, conversa, agente+versão, origem (`user`, `routine`, `heartbeat`, `trigger`, `agent`), nó, motor, status, prazo, custo.
- **Event:** id sequencial, job, tipo, momento, ator, payload (segredos redigidos). Append-only.
- **Approval:** job, ação, escopo, expira_em, decisão, decidido_por.

## Regras de segurança que não mudam

- Nenhuma porta pública: tudo via Tailscale; servidor escuta em loopback e é publicado com `tailscale serve`.
- Tokens guardados só como hash; revogação imediata.
- Nenhuma ação fora do portão de políticas; sem regra, nada é permitido.
- Segredos nunca entram no contexto do modelo nem nos logs.
- Jobs sem humano presente não executam ações 🔒.
- Credenciais do PC não são copiadas pra VPS; cada nó usa a própria autenticação oficial.

## Estado atual

Fase 1 em produção na VPS: Postgres, log de eventos, jobs duráveis e nós pareados; ações ainda limitadas a `ping` e `diagnostics`. Ver [operations.md](operations.md).
