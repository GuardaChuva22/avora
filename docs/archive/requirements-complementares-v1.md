# Avora — Requisitos e decisões posteriores ao inventário

> **Fonte oficial complementar** ao [catálogo dos 263 requisitos originais](requirements.md). Estes itens vieram da conversa após a seleção integral do inventário. **Não substituem nem renumeram A01–Q15**. São requisitos de produto, comportamento e arquitetura, codificados como **AV-001 em diante**.

## Como interpretar
- **Decisão explícita:** direção declarada pelo proprietário durante a conversa.
- **Direção técnica / regra de segurança:** detalhamento proposto no projeto para tornar a intenção implementável; sujeito a revisão sem eliminar a capacidade original.
- **Estado de todos os itens:** planejado; soluções parciais não contam como entrega completa.
- **Modalidades remotas de Codex/Claude:** sempre verificar disponibilidade, termos e API da conta utilizada antes de implementar. Não pressupor paridade entre CLI local e nuvem oficial.

## Resumo de decisões
1. Uso **estritamente privado**, sem distribuição: Avora não é um SaaS.
2. **Go** no núcleo/servidor e no runtime; **C++/Qt 6** no desktop Windows; **Kotlin/Compose** no Android; Python pode complementar IA.
3. **Windows = ambiente local de IA completo**, com acesso aos arquivos e projetos; não apenas cliente remoto.
4. **Android = mensageiro/controle** com seleção de motor, destino e execução remota.
5. **VPS já existente = central de coordenação** e execução remota quando apropriada; não desperdiçar seus recursos.
6. **Motor** e **local de execução** são escolhas separadas, automáticas ou manuais.
7. **Codex CLI / Claude Code CLI** devem ser integráveis, inclusive em VPS se configurados.
8. Se o **PC estiver online, preferi-lo para desenvolvimento complexo**; caso contrário, cloud dos provedores e/ou VPS conforme compatibilidade.
9. **Credenciais, arquivos locais e permissões** limitam qualquer fallback: não transferir dados ou segredos silenciosamente.

## VISÃO E ESCOPO

| ID | Requisito / capacidade | Origem | Estado |
|---|---|---|---|
| **AV-001** | Avora é um ecossistema agêntico autônomo pessoal e privado, exclusivo do proprietário; não será distribuído ao público. | Decisão explícita | Planejado |
| **AV-002** | Todos os 263 requisitos do inventário original continuam obrigatórios, independentemente de fase ou dificuldade. | Decisão explícita | Planejado |
| **AV-003** | A meta é superar Grok Bot, OpenClaw e Hermes nos fluxos reais do proprietário; validar superioridade por métricas reproduzíveis, não marketing. | Objetivo explícito + critério técnico | Planejado |
| **AV-004** | Não construir billing, assinaturas, publicação SaaS ou suporte multi-tenant como objetivo de produto; preservar requisitos de colaboração que o proprietário declarou obrigatórios. | Consequência de escopo | Planejado |
| **AV-005** | A VPS já existe e será aproveitada como infraestrutura central de coordenação e trabalho remoto. | Decisão explícita | Planejado |

## TECNOLOGIAS E APLICATIVOS

| ID | Requisito / capacidade | Origem | Estado |
|---|---|---|---|
| **AV-006** | Backend principal do Avora em Go, podendo usar Python em componentes especializados de IA. | Stack recomendada e aceita como direção | Planejado |
| **AV-007** | Aplicativo desktop nativo para Windows em C++20 com Qt 6/QML, sujeito a avaliação técnica sem reduzir recursos. | Preferência explícita | Planejado |
| **AV-008** | Aplicativo Android nativo em Kotlin e Jetpack Compose, empacotado como APK instalável diretamente pelo proprietário, sem Play Store. | Decisão explícita | Planejado |
| **AV-009** | Criar runtime/executor local independente em Go, executável no Windows, além de runtime na VPS quando necessário. | Decisão de arquitetura | Planejado |
| **AV-010** | O desktop Windows deve ser um agente de IA completo: conversar, planejar, executar ferramentas, ler/alterar arquivos, usar terminal, navegador e trabalhar em projetos locais — não apenas um terminal da VPS. | Correção explícita do proprietário | Planejado |
| **AV-011** | O Android deve servir como mensageiro e centro de controle, com conversas/agentes, envio de tarefas, progresso, resultados e notificações. | Decisão explícita | Planejado |
| **AV-012** | A VPS deve operar como control plane (coordenação, memória, sessões, roteamento, presença e tarefas persistentes), não como executor obrigatório de toda carga pesada. | Decisão explícita + detalhamento de arquitetura | Planejado |

## MOTORES E DEV TOOLING

| ID | Requisito / capacidade | Origem | Estado |
|---|---|---|---|
| **AV-013** | Avora deve possuir um agente integrado/nativo capaz de resolver tarefas sem depender obrigatoriamente de Codex ou Claude Code. | Decisão explícita | Planejado |
| **AV-014** | Integrar Codex CLI ao runtime, acessível por comando manual ou seleção automática conforme a tarefa. | Decisão explícita | Planejado |
| **AV-015** | Integrar Claude Code CLI ao runtime, acessível por comando manual ou seleção automática conforme a tarefa. | Decisão explícita | Planejado |
| **AV-016** | Para clientes externos usar, quando apropriado e suportado, as interfaces oficiais de integração de Codex (ex.: app-server/SDK) e Claude Code (ex.: CLI/Agent SDK), mantendo adaptadores separados. | Direção técnica, sujeita a API oficial | Planejado |
| **AV-017** | Permitir escolher o motor por conversa, agente ou tarefa: Avora nativo, Codex, Claude Code e outros futuros; oferecer modo automático. | Decisão explícita | Planejado |
| **AV-018** | Tarefas complexas de desenvolvimento, grandes refatorações e depuração devem preferir Codex/Claude Code no Windows quando o PC estiver online. | Decisão explícita | Planejado |
| **AV-019** | Tarefas de desenvolvimento simples podem usar agente integrado ou ambientes remotos de menor custo/carga, conforme avaliação do roteador. | Decisão explícita | Planejado |
| **AV-020** | Integrações de programação devem atuar nos diretórios e repositórios apropriados, com suporte planejado a projetos Git, diffs, logs, worktrees e gerenciamento de sessões. | Detalhamento de experiência | Planejado |

## AMBIENTES E ROTEAMENTO

| ID | Requisito / capacidade | Origem | Estado |
|---|---|---|---|
| **AV-021** | Tratar motor de IA e ambiente de execução como escolhas independentes: um motor pode ter adaptadores para Windows, cloud de provedor e VPS quando suportado. | Decisão arquitetural explícita | Planejado |
| **AV-022** | Permitir seleção manual do destino da tarefa em qualquer conversa: Meu PC, VPS, nuvem oficial de provedor quando disponível ou Automático. | Decisão explícita | Planejado |
| **AV-023** | Detectar automaticamente se o PC Windows está online usando presença/heartbeat autenticado, refletindo o status no desktop e no Android. | Decisão explícita | Planejado |
| **AV-024** | Com o PC online, priorizá-lo como ambiente de desenvolvimento e execução quando adequado, sem ignorar a escolha manual do usuário. | Decisão explícita | Planejado |
| **AV-025** | Com o PC desligado, permitir tarefas por Codex/Claude em nuvem própria dos provedores, quando o produto/API oferecer a modalidade e a conta estiver autorizada. | Esclarecimento explícito do proprietário | Planejado |
| **AV-026** | Com o PC desligado, permitir também execução de Codex CLI/Claude Code CLI na VPS, mediante instalação, credenciais válidas, recursos e permissões. | Esclarecimento explícito do proprietário | Planejado |
| **AV-027** | Preferir recursos hospedados pelos próprios provedores para tarefas compatíveis quando isso evitar consumo desnecessário de CPU/RAM/armazenamento da VPS. | Decisão explícita | Planejado |
| **AV-028** | Para automações leves, permitir execução pelo agente nativo na VPS ou no Windows, conforme objetivo, custo e disponibilidade. | Decisão explícita | Planejado |
| **AV-029** | O roteamento automático deve avaliar complexidade, localização dos arquivos, ferramentas exigidas, presença, permissões, custo, latência, recursos e disponibilidade dos provedores. | Política técnica derivada | Planejado |
| **AV-030** | Se o destino manual estiver indisponível, informar bloqueio ou pedir nova escolha; não redirecionar silenciosamente uma execução manual. | Regra de segurança proposta | Planejado |
| **AV-031** | Não assumir que arquivos exclusivamente locais estão disponíveis na nuvem; fallback de desenvolvimento exige projeto sincronizado/autorizado (ex.: Git ou artefatos). | Regra técnica explicitamente discutida | Planejado |
| **AV-032** | Distinguir execução da CLI na VPS da execução hospedada na nuvem oficial do Codex/Claude: são modalidades independentes, com capacidades e custos diferentes. | Esclarecimento explícito | Planejado |
| **AV-033** | O sistema deve permitir execução e monitoramento remoto a partir do Android quando o PC estiver ligado, sem exigir interação física com o PC. | Decisão explícita | Planejado |
| **AV-034** | O desktop deverá manter operações locais disponíveis sem conexão à VPS e sincronizar estado compatível quando a conectividade retornar. | Requisito proposto em arquitetura anterior | Planejado |
| **AV-035** | Para jobs de longa duração, manter histórico, checkpoints quando suportados, progressos, cancelamento e retomada segura; não prometer migração de processo em andamento sem suporte. | Critério técnico derivado | Planejado |
| **AV-036** | Permitir múltiplas tarefas ou agentes em paralelo com isolamento e limites de recursos adequados. | Direção explicitada na arquitetura | Planejado |

## AUTENTICAÇÃO E SEGURANÇA

| ID | Requisito / capacidade | Origem | Estado |
|---|---|---|---|
| **AV-037** | Credenciais de cada provedor devem ser gerenciadas por mecanismo seguro, separado por ambiente e dispositivo, respeitando autenticação oficial e escopos de autorização. | Decisão explícita | Planejado |
| **AV-038** | Nunca copiar livremente sessões, cookies ou tokens do Windows para a VPS; usar credenciais e integrações oficialmente suportadas, com revogação. | Restrição de segurança | Planejado |
| **AV-039** | Comunicação PC–VPS–Android deve ser autenticada e cifrada (HTTPS/WSS, VPN privada opcional); não expor portas perigosas do PC. | Regra técnica derivada | Planejado |
| **AV-040** | Ações sensíveis exigem permissões explícitas, aprovação humana quando aplicável, isolamento, registro auditável e limites de custo/tempo. | Requisito de proteção já aceito | Planejado |
| **AV-041** | Permitir revogar autorização de dispositivos e interromper tarefas; execução de comandos arbitrários não deve ser habilitada até existir controle de acesso robusto. | Regra de segurança | Planejado |
| **AV-042** | O proprietário deverá poder ver qual motor, dispositivo, conta e ambiente executou cada tarefa, com rastreamento de resultados e erros. | Regra de observabilidade | Planejado |

## Critérios de aceite cruzados

- **PC ligado:** heartbeat autenticado indica online; tarefas de desenvolvimento complexas podem ser iniciadas e acompanhadas do Windows ou Android, com Codex/Claude em workspace local.
- **PC desligado:** o cliente apresenta indisponibilidade do Windows; tarefas compatíveis podem ser enviadas para cloud oficial do provedor ou runtime na VPS autorizado.
- **Arquivos privados:** tarefas que requerem arquivos inacessíveis no alvo não são executadas; a UI explica o bloqueio e oferece sincronização autorizada quando disponível.
- **Escolhas manuais:** seleção explícita de motor ou alvo não é alterada silenciosamente.
- **Segurança:** tokens separados e revogáveis, criptografia, logs e políticas de permissão; ações de maior risco pedem aprovação.
- **Eficiência:** roteador documenta por que escolheu o destino, evitando usar a VPS como executor pesado quando cloud oficial atender à tarefa.
- **Sem distribuição:** APK Android instalável diretamente; arquitetura otimizada para um proprietário sem billing ou multi-tenancy comercial.

## Matriz de rastreabilidade

| Origem | IDs | Total | Obrigatório |
|---|---|---:|---|
| [Inventário original](requirements.md) | A01–Q15 (16 categorias) | 263 | Sim |
| Esta especificação complementar | AV-001–AV-042 | 42 | Sim, sujeito à interpretação técnica descrita |
| **Total de requisitos rastreados** | | **305** | **Sim** |

## Controle de alterações
- **2026-11-10:** consolidação das decisões do proprietário e do planejamento técnico. Nenhuma funcionalidade desta lista foi declarada concluída nesta edição.
- Novas decisões devem receber o próximo ID AV, sem reutilizar IDs existentes. Mudanças de critério ou prioridade exigem registro explícito e não alteram silenciosamente os 263 itens originais.
