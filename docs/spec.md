# Avora — Especificação v2

> Substitui o catálogo de 305 requisitos (arquivado em [`archive/`](archive/)). Em vez de uma lista de tudo que existe no mercado, esta spec descreve **12 módulos concretos**, com comportamento, limites e critérios de pronto. As decisões que levaram a ela estão em [decisions.md](decisions.md).

## Visão em uma frase

O Avora é um **funcionário digital 24h**, pessoal e privado: conversa com você pelo app próprio (desktop e Android), age sozinho com iniciativa, usa o **Omniarch** como fonte da verdade sobre a sua vida e orquestra motores existentes (Claude Code, Codex) em vez de reinventá-los.

## Princípios

1. **Orquestrar, não reinventar.** Claude Code e Codex são os trabalhadores pesados. O Avora é o cockpit: rotas, memória operacional, proatividade, permissões e auditoria.
2. **Proatividade com freio.** Ele age sozinho, mas dentro de níveis de autonomia, orçamento e regras explícitas.
3. **Tudo passa pelo portão.** Nenhuma ação acontece fora do portão de políticas, e toda ação deixa rastro.
4. **Omniarch guarda a vida, Avora age.** Tarefas, metas, agenda, finanças e notas vivem no Omniarch. O Avora lê e escreve lá pela API, nunca em arquivos soltos.
5. **Leve na VPS.** O núcleo é Go; o trabalho pesado vai pro PC quando ele estiver ligado.

## Topologia

```
            Tailscale (nenhuma porta pública)
 ┌──────────────┐     ┌──────────────────────────────┐     ┌──────────────┐
 │ App Android  │◄───►│ VPS 6 GB                     │◄───►│ PC Windows   │
 │ (nó celular) │     │  avora-server (Go)           │     │ avora-runtime│
 └──────────────┘     │  Postgres (db avora)         │     │ Claude Code  │
 ┌──────────────┐     │  avora-runtime (nó VPS)      │     │ Codex        │
 │ App desktop  │◄───►│  containers dos agentes      │     │ projetos     │
 └──────────────┘     │  ntfy (push)                 │     └──────────────┘
                      │  Omniarch (API REST)         │
                      └──────────────────────────────┘
```

- **avora-server:** control plane. Conversas, jobs, fila, roteador, portão de políticas, log de eventos, rotinas, heartbeat, API pros apps.
- **avora-runtime:** o mesmo binário roda em cada nó (VPS e PC), anuncia capacidades e executa jobs.
- **Banco:** database `avora` separado, no Postgres da VPS. Nunca escreve no banco do Omniarch diretamente.

---

## M1 — Funcionários (agentes com cargo)

Cada agente é um registro versionado com:

| Campo | Exemplo |
|---|---|
| `nome`, `cargo`, `persona` (SOUL.md) | "Vega", Chefe de Gabinete |
| `motor`, `modelo`, `esforço` | claude / sonnet / médio |
| `workspace` | pasta e container próprios (M2) |
| `ferramentas` | lista explícita de ferramentas e MCPs liberados |
| `política` | regras de aprovação próprias (M6) |
| `memória` | memória operacional própria (M11) |
| `responsabilidades` | metas permanentes com métrica, cadência e progresso |

- **Time inicial:** Chefe de Gabinete (coordena e fala com você), Financeiro, Dev, Pesquisador, Growth. Todos criados a partir de templates editáveis.
- **Responsabilidade** = objetivo + métrica + cadência. Ex.: Financeiro → "gastos do mês abaixo do orçamento do Omniarch, checar diariamente às 8h". O agente gera as próprias rotinas (M5) pra cumprir a meta e registra progresso.
- **Criar agente por instrução:** "cria um agente que cuida dos meus estudos" gera um rascunho que você aprova.

**Pronto quando:** dá pra criar, editar, versionar e voltar versão de um agente; cada job registra qual agente e qual versão o executou.

## M2 — Computador próprio por agente

- Cada agente tem um **container Docker persistente** no nó VPS, com limite de RAM e CPU, volume próprio, terminal e um **Chromium com perfil persistente** (logins sobrevivem entre sessões).
- **Visão ao vivo:** o app mostra a tela do container (screenshot periódico ou VNC) e a lista de atividades (comandos rodados, páginas lidas, arquivos salvos com caminho e tamanho).
- **Assumir controle:** ao bater em login, 2FA ou captcha, o agente pausa e te notifica. Você abre a tela no app e resolve. Enquanto você controla, ações do agente são recusadas; início e fim do controle vão pro log.
- **Isolamento:** containers escutam só em loopback, com token por container; variáveis de ambiente mínimas; sem acesso à rede do host.

**Pronto quando:** um agente abre um site, faz login com sua ajuda via takeover, e no dia seguinte continua logado.

## M3 — Nós (dispositivos pareados)

| Nó | Capacidades |
|---|---|
| VPS | containers, rotinas 24h, Claude Code e Codex CLI (se configurados) |
| PC Windows | projetos locais, Claude Code e Codex locais, arquivos, captura de tela |
| Celular | localização, câmera, notificações, voz, compartilhar |

- **Pareamento:** o app gera um código de uso único; o nó troca por um token próprio. O servidor guarda **só o hash** do token. Revogar um nó corta o acesso na hora.
- **Presença:** heartbeat a cada 5 s; offline depois de 30 s sem sinal (já existe no POC).
- **Capacidades anunciadas:** cada nó informa o que tem instalado e autenticado (ex.: `claude-code@x.y`, `codex`, `docker`, projetos disponíveis).

## M4 — Heartbeat (proatividade)

- A cada **30 min** (a cada 2 h entre 0h e 7h), um modelo barato recebe:
  - o checklist editável `HEARTBEAT.md`;
  - um **retrato do estado**: o que mudou no Omniarch desde o último ciclo, jobs abertos ou travados, agenda das próximas 24 h, descobertas pendentes.
- Decide uma de quatro saídas: **nada**, **avisar**, **agir** (dentro do nível de autonomia) ou **abrir um job**.
- **Orçamento diário** próprio. Se estourar, o heartbeat cai pra modo mínimo e te avisa.
- Toda decisão vai pro log de eventos com o motivo, inclusive "nada".

**Pronto quando:** sem você mandar nada, ele te avisa de uma tarefa vencendo no Omniarch e oferece ajuda.

## M5 — Rotinas e gatilhos

**Rotinas** (prompts agendados executados por um agente):
- agendamento em linguagem natural ("toda segunda às 9h") ou cron;
- intervalo mínimo de 15 min;
- desativação automática depois de 10 falhas seguidas, com aviso;
- **sem você por perto, comando perigoso é negado por padrão** (não há quem aprove);
- resultado entregue numa conversa definida.

**Gatilhos por evento**, no mesmo barramento:
- webhooks do Omniarch (tarefa venceu, conta vence amanhã, meta parada);
- Gmail, GitHub, RSS;
- nó ficou online ou offline, job terminou;
- arquivo novo numa pasta vigiada;
- queda de preço em item vigiado;
- URL genérica de webhook com segredo.

**Rotinas iniciais:** briefing da manhã, turno da noite (M5.1), revisão semanal de metas.

### M5.1 — Turno da noite
De madrugada, o Chefe de Gabinete pega no Omniarch tarefas marcadas como **"Avora pode fazer"** (e, com o tempo, tarefas que ele aprendeu que pode pegar), distribui pro time e entrega de manhã: *feito / esperando aprovação / travado e por quê*.

## M6 — Portão de políticas, aprovações e auditoria

- **Caminho único:** toda chamada de ferramenta segue `identificar alvo → avaliar regras → gravar evento de auditoria → executar → gravar resultado`.
- **Regras declarativas** (YAML) sobre: agente, ferramenta, comando, host, caminho de arquivo, custo estimado, origem do job (você, rotina, heartbeat, outro agente).
  - negar vence permitir; **sem regra, nada é permitido**; regra com erro recusa;
  - a recusa informa **qual regra** bloqueou.
- **Níveis de autonomia** por categoria de ação:

| Nível | Comportamento | Exemplo |
|---|---|---|
| 0 | só sugere | reorganizar a semana |
| 1 | faz e conta | criar tarefa no Omniarch, rodar testes |
| 2 | faz em silêncio, fica no log | atualizar índice de memória |
| 🔒 | sempre pede aprovação | enviar mensagem, gastar dinheiro, apagar, `git push`, deploy |

- **Aprovações:** *Permitir uma vez / Sempre permitir / Negar*, no app e direto na notificação; expiram depois de um prazo configurável; "Sempre permitir" vira regra visível e revogável.
- **Log de eventos append-only** no Postgres: única fonte da verdade. Telas de Atividade e Auditoria são consultas sobre ele. Cada evento tem job, agente (e versão), nó, motor, modelo, conta, tokens, custo, regra aplicada e resultado. Segredos sempre redigidos.

**Pronto quando:** uma ação recusada aparece na Auditoria com a regra responsável, e uma aprovação dada no celular libera o job em menos de 2 s.

## M7 — Cofre de credenciais

- Credenciais **só de escrita**: a API nunca devolve o valor.
- Agentes usam referências (`{{segredo:gmail}}`); o valor é injetado no momento da execução pelo runtime, **fora do contexto do modelo**.
- Transcrições registram "segredo pedido", nunca o valor.
- O primeiro preenchimento automático em cada site exige aprovação.
- Cifrado em repouso com chave fora do banco.
- O cofre de senhas do Omniarch continua inacessível ao Avora.

## M8 — Multi-motor, failover e cota

- ~~Motor, modelo e esforço por agente~~ (substituído por D17: todo agente usa o motor nativo e chama Claude Code e Codex como ferramentas). Modelo e esforço continuam escolhíveis por tarefa delegada.
- **Troca no meio da conversa** sem perder histórico nem workspace.
- **Failover automático:** limite de uso ou queda no motor principal → tarefa continua no reserva, com aviso no chat.
- **Painel de cota:** consumo de cada assinatura por janela de uso. O roteador usa isso pra guardar cota pras tarefas importantes.
- **Roteador** decide nó + motor com base em: escolha manual, presença, capacidades, onde estão os arquivos, cota, custo e carga da VPS. Escolha manual nunca é trocada em silêncio. Cada decisão grava o motivo.
- **Limite da VPS:** no máximo **3 agentes pesados simultâneos**; o resto entra na fila.
- Antes de usar assinaturas de forma automatizada, conferir os termos atuais de cada provedor.

## M9 — Canais com vários agentes

- Conversas 1:1 ou em grupo (você + vários agentes).
- Cada tarefa tem **um dono**; agentes passam trabalho e revisam uns aos outros.
- Controles: parar, retomar, reatribuir, arquivar, restaurar.
- **Trava contra loop:** no máximo N trocas entre agentes sem intervenção humana (padrão 8); ao estourar, pausa e te chama.

## M10 — Skills

- Formato **`SKILL.md`** compatível com Claude Code. As skills que você já tem funcionam direto.
- Carregadas só quando relevantes; chamadas com `/` no chat; liberadas por agente.
- **Auto-criação:** após uma tarefa complexa bem-sucedida, o agente propõe salvar como skill; só grava quando você aprova.
- Versionadas; testadas num ambiente de teste antes de ativar.
- **Ensinar por demonstração:** você grava a tela fazendo um fluxo; o agente gera a skill a partir da gravação.

## M11 — Memória

- **Sobre você** → Omniarch (gostos, metas, pessoas, fatos pessoais).
- **Operacional** → Avora: o que cada agente aprendeu fazendo, decisões de projetos, histórico de jobs.
- Tudo **visível, editável e apagável** no app, com origem de cada memória (conversa, job, fonte externa) e data.
- **Lembretes periódicos** fazem o agente registrar aprendizados ao fim de tarefas longas.
- Modo opcional: cada escrita de memória exige aprovação.
- Busca textual + semântica (pgvector) sobre conversas, eventos e memórias.

## M12 — Interface

**Apps:** desktop Windows e Android, mesmo backend, mesma conta.

Telas:
- **Chat** com streaming, cartões de chamada de ferramenta e cartões de aprovação.
- **Atividade:** linha do tempo do que foi feito sozinho, com nó, motor e custo.
- **Jobs:** o que está rodando agora, onde, com botão de parar.
- **Funcionários:** agentes, responsabilidades, progresso, memória.
- **Rotinas:** agendadas, disparadas, falhas.
- **Descobertas:** o que os radares acharam, com 👍/👎 que treina a relevância.
- **Auditoria** e **Cota**.

Recursos:
- **Cartões interativos:** tabelas, gráficos, formulários e objetos do Omniarch clicáveis.
- **Canvas:** o agente monta um mini-painel sob demanda.
- **Android:** "Enviar pro Avora" no menu de compartilhar, mensagem de voz transcrita, widget, push com botões de ação.

---

## Integração com o Omniarch

1. **Adaptador MCP `omniarch-mcp`** expondo ferramentas como `criar_tarefa`, `listar_agenda`, `registrar_gasto`, `atualizar_meta`. Todos os motores usam o Omniarch pelo mesmo caminho.
2. **Token do Avora com escopo por módulo** (mudança no Omniarch). Ex.: escreve em tarefas, agenda, notas e compras; lê finanças e metas; **nunca** acessa senhas nem dispositivos.
3. **Origem visível:** registros criados pelo Avora levam `created_by = avora` e o id do job; o Omniarch mostra um selo e permite desfazer.
4. **Webhooks do Omniarch → Avora** alimentam M4 e M5.
5. O módulo de chat do Omniarch será removido quando o app do Avora estiver usável.

## Radares (aplicação de M1 + M4 + M5)

Agentes de fundo com tema: notícias dos seus assuntos, otimização do seu trabalho, promoções da sua lista de compras, oportunidades de renda baseadas nas suas habilidades reais. Resultados vão pra **Descobertas** com nota de relevância: nota alta vira push, o resto entra no resumo diário. O 👍/👎 ajusta a nota. Quando faz sentido, o radar já entrega a ação pronta (rascunho de proposta, script, item na lista) pra você aprovar.

## Fora de escopo

Billing, multi-tenant, distribuição pública, marketplace. Colaboração com outras pessoas só se você pedir explicitamente no futuro.
