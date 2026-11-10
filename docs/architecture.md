# Avora — arquitetura inicial

## Decisões
- **Pessoal:** um operador, sem SaaS ou plano de distribuição.
- **Control plane VPS (Go):** sessões, mensagens, histórico, memória, presença, políticas, filas e logs.
- **Runtime Windows (Go):** agente local completo com ferramentas, projetos, terminal, navegador, arquivos, Codex CLI/App Server e Claude Code CLI/SDK. **Não implementado ainda.**
- **Desktop Windows (C++20/Qt 6):** interface local e remota. **Planejado.**
- **Android (Kotlin/Jetpack Compose):** APK pessoal, chat, voz, notificações, ações e aprovações. **Planejado.**
- **Cloud de provedores:** conectores separados das CLIs executadas em VPS/Windows.

## Seleção
O motor e o alvo são independentes. Motores: native, codex, claude. Alvos: windows, provider_cloud, vps. Para tarefas complexas de desenvolvimento, priorizar PC ligado. Com PC desligado, cloud oficial compatível; depois VPS quando tiver runtime, autorização e credenciais disponíveis. O agente nativo pode ser o motor preferível para tarefas simples.

O fallback de projetos só funciona com arquivos acessíveis no destino e consentimento para sincronização. Nunca migrar silenciosamente credenciais, repositórios privados ou processos em andamento.

## Segurança obrigatória antes de execução real
Identidade de dispositivos; HTTPS/WSS; sessões autenticadas; permissões por tarefa; cofre de credenciais; isolamento; limites de custo; aprovações para efeitos sensíveis; logs de auditoria; revogação; proteção a prompt injection.

## Contratos projetados
- Device: id, identity, capabilities, last_seen, online.
- Job: id, conversation_id, engine, target, project_ref, permission_scope, status.
- Execution: job_id, events, logs, artifacts, exit_code, timestamps.
- Approval: job_id, action, scope, expiration, decision.

**Estado atual:** API Go local de saúde/capacidades, roteamento puro e testes. Nenhuma execução real, autenticação ou memória.
