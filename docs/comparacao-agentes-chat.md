# Avora × agentes-chat: o que eles têm e o que entra aqui

Lido em 2026-11-17 do repositório `agentes-chat/agentes-chat` (commit `ed94988`): o esquema do banco
(`packages/db/src/schema/agents.ts`, `chat.ts`), os 188 ADRs e os textos em português da tela do agente
(`packages/i18n/src/pt-BR.ts`, `agentPage`). O agentes-chat é para equipes (várias pessoas, permissões, orçamento em
dólar por agente); o Avora é de um dono só. O que é de equipe fica de fora; o resto está abaixo.

## 1. Canais

| Eles | Avora hoje | Entra |
| --- | --- | --- |
| Vários canais, cada um com nome e tópico (`channels.name`, `topic`) | Um só, `#time`, fixo no código (`TeamChannelID`) | Canais criados pelo dono: nome, propósito, membros |
| Membros por canal (`channel_members`), e por membro "responde sozinho" (`auto_reply`: o roteador só o escolhe se ligado; desligado, só com @) | Todos os agentes estão no #time | Membros por canal com "responde sozinho" |
| Categorias na barra lateral (ADR 0080), ordem arrastável | — | O canal fica dentro de um Projeto (o Avora já tem projetos, D25), ou solto |
| Arquivar e apagar canal | — | Arquivar |
| Roteador em cadeia (ADR 0074): modelo pequeno → plano do autor → regras → ninguém | Regras (D52) quando o principal é o Codex | O mesmo roteador, por canal, só com os membros |
| Silenciar canal, não lidas por canal | — | Depois |

## 2. O agente ("Quem pensa" e o resto)

| Eles | Avora hoje | Entra |
| --- | --- | --- |
| **Cérebro por agente** (`brain`: API, Claude Code, Codex, Cursor, OpenCode, Grok, Antigravity), **modelo** (`brain_model`), **conta** do plano (`brain_account_id`) e **esforço** (`brain_effort`: baixo…máximo) | Um modelo principal para todos (Configurações → Modelos) | Cada agente escolhe a conta (Codex, Claude ou uma API), o modelo e o esforço; vazio = o principal |
| Se o computador cair, responder pela API (`brain_fallback`) | — | Cair no modelo principal quando a conta do agente não responde |
| Velocidade rápida do Codex (`brain_speed`, ADR 0149) | — | Depois (só ChatGPT com Fast mode) |
| Janela de contexto por agente (200K/400K/1M, ADR 0086) | Fixa (64K nativo, ~14K no Codex) | Depois |
| "Usar em outros agentes" (ADR 0094): copia o cérebro para vários | — | Depois |
| **Nível de aprovação por agente** (`approvals_level`: sempre pergunta / só o arriscado / nunca, ADR 0173) | Regras fixas (D18) | Feito (D56) |
| Freios de autonomia (ADR 0131): o que pode começar sozinho, quantas coisas de uma vez | Limites fixos das missões | Depois |
| Sites que o navegador pode abrir (`browser_allowed_domains`) | O navegador abre qualquer site | Depois |
| Jeito (cor, olhos, acessório), apelido de menção, setor, "responde a" | Cor e avatar; jeito de falar (D50) | Já tem o essencial |
| Figurinhas e GIFs dos agentes (ADR 0091) | Reações com emoji (D50) | Depois |
| Status do agente ("trabalhando no #canal", ADR 0084) | Digitando no canal | Depois |
| Agente propõe mudar as próprias instruções (ADR 0092) | — | Depois |

## 3. Ordem

1. Canais (seção 1). Feito: D54.
2. Cérebro por agente: conta, modelo e esforço, e o fallback (seção 2). Feito: D55.
3. Nível de aprovação por agente. Feito: D56.
4. Janela de contexto, "usar em outros agentes", sites do navegador, status.
