# Roadmap

Cada fase termina com algo que dá pra **usar**, não só testar. Módulos referem-se à [spec](spec.md).

## Virada (2026-11-11): de "Claude Code piorado" a funcionário de verdade

### Diagnóstico

- **Os agentes não têm mãos.** As 11 ferramentas do motor nativo são todas de bastidor (rotinas, responsabilidades, consultar colega). Pra fazer qualquer coisa real (ler um site, um arquivo, um e-mail) o agente delega ao Claude Code. O Avora vira um intermediário mais lento na frente das mesmas ferramentas.
- **Não conhecem o mundo do dono.** Sem Gmail, Drive, ClickUp, Omniarch e sem memória, cada conversa começa do zero.
- **Só agem quando chamados.** Não há heartbeat, gatilhos por evento nem trabalho que dure mais que uma conversa.
- **O cérebro é frágil.** O motor nativo roda no trial da NVIDIA (~40 req/min, sem garantia, prompts podem ser usados). Um modelo fraco, com poucas ferramentas, parece medíocre por mais polida que seja a tela.
- **A confiança não é mostrada.** Não há uma tela de saúde, erros acionáveis ou medição de qualidade, então tudo parece "pode quebrar a qualquer momento".

### O que só o Avora faz (o norte)

O OpenBot e o Hermes são fortes, mas o OpenBot é *local-first*: rotinas e webhooks só rodam com o PC dele ligado e não há agendador por horário. Claude Code e Codex vivem dentro de uma sessão. O Avora roda **24h na VPS**, conhece a **vida do dono** (Omniarch + conectores) e trabalha em **missões que duram dias**, esperando respostas e eventos sem ninguém olhando. Toda fase abaixo serve a isso.

### Fluidez (2026-11-16): o app tem que parecer vivo
Comparado ao agentes.chat, o Avora tem recursos mas parece um chat travado. Três partes, nesta ordem:
- [x] **Tempo real (D47):** canal ao vivo `/v1/live` no lugar dos timers; mensagens e passos aparecem na hora, sem a lista pular; linha de andamento acima do campo.
- [x] **Agentes conversando (D48):** numa sala, quem responde (um só, a não ser que seja para todos); perguntar a um colega e esperar, ou mandar recado sem esperar, com o cartão "Mensagem de @fulano"; o agente chamado sem nome pode ficar quieto.
- [ ] **Acabamento:** composer flutuante com `/`, Ctrl+K, movimento só nas entradas (120 a 220 ms), avatar que mostra pensando/escrevendo, azul só nos agentes e âmbar só no que espera o dono.

### Fase A — Mãos e memória
Ferramentas nativas, sem delegar:
- [x] `buscar_na_web` e `ler_pagina`, com limite de tamanho e cache;
- [x] arquivos no workspace do agente: ler, escrever, listar, com cota de disco;
- [x] `executar` num sandbox (bubblewrap na VPS: sem privilégios, sem rede por padrão, tempo e RAM limitados);
- [x] `lembrar` / `esquecer` / `recordar`, com memória visível e editável no perfil (M11).
- [x] suíte de evals (`TestEval`, 31 cenários);
- [ ] cérebro local (adiado: fica o Nemotron por enquanto).

O Claude Code e o Codex ficam pro que é pesado de verdade.

**Cérebro local:**
- O `avora-runtime` do PC expõe um modelo do Ollama ao servidor pelo tailnet.
- Dado privado (e-mails, arquivos, Omniarch) é processado no PC quando ele está ligado.
- Com o PC desligado, a tarefa privada espera, a não ser que o dono libere a API.

**Evals:** a suíte de 30 tarefas reais (ferramenta certa, argumentos certos, tarefa concluída) roda contra cada modelo candidato e decide qual fica com qual papel. Ninguém decide no achismo.
**Pronto quando:** "compara o preço do notebook X em 3 lojas" é resolvido em menos de 1 min sem chamar o Claude Code, e no dia seguinte o agente lembra da resposta.

### Fase B — Conectores e página de Plugins
- **Base:** pacote `internal/connect` com conector tipado (auth OAuth ou chave, ferramentas com nível de risco leitura/escrita/envio/gasto, `Health()`), mais o cofre M7 (AES-GCM, chave em `/etc/avora`, fora do banco).
- **Tokens:**
  - [x] renovados sozinhos;
  - [x] falha de renovação vira "Reconectar" na página Plugins e um aviso no `#time`;
  - [x] nunca aparecem no contexto do modelo.
- [x] **Política (D18):** ler e escrever no espaço do dono é livre; enviar, comentar, convidar ou apagar é 🔒. "Sempre permitir" é dado por ferramenta.
- **Prioridade, nativos em Go** (testados, com retry/backoff, paginação e limite de taxa):
  1. [x] **Google (um OAuth só): Gmail, Drive e Agenda.** O app do Google Cloud fica em *produção não verificada*, porque em modo *Teste* o refresh token expira em 7 dias.
  2. [x] **ClickUp:** token pessoal e API REST v2 (D19), no lugar do MCP oficial.
  3. [x] **Instagram (D34):** API oficial para a conta profissional do dono (perfil, posts, métricas, comentários, publicar, DMs), token renovado sozinho. Navegador para o resto (ver "Instagram e X").
  4. [x] **ElevenLabs:** chave de API. Vozes e TTS em MP3 na pasta do agente. Falta: clonagem e tocar o áudio no chat.
  5. [x] **Discord:** bot próprio que lê e posta no servidor do dono. Falta: servir de canal pra falar com o Avora pelo celular até o Android existir.
  6. [x] **Pluggy (Meu Pluggy):** bancos do dono, só leitura.
  7. [x] **GitHub** (MCP oficial com token), **Spotify** (app próprio, PKCE) e **LinkedIn** (perfil e posts; porta fixa 47821).
  8. [x] **iFood** pelos MCPs oficiais (D41): busca, sacola, pedido com aprovação e acompanhamento; sandbox até a homologação.
  9. [x] **NFS-e do MEI** pelo Sistema Nacional (D42): emitir, consultar, PDF e cancelar; certificado A1 só no cofre; produção restrita até `AVORA_NFSE_ENV=producao`.
- [x] **Cauda longa:** cliente MCP genérico com login de um clique (D21) e catálogo de 45 apps.
  - [x] Servidor remoto (HTTP streamable + OAuth 2.1, conforme a spec do MCP), adicionado por URL.
  - [ ] Servidor stdio rodando num nó.
  - As mesmas regras de risco e auditoria valem pros dois.
- **Página Plugins:**
  - [x] catálogo com o estado de cada conector (conectado, precisa reconectar, erro), escopos, quais agentes usam e última atividade, com um botão "Testar";
  - [x] chat-first: o agente pede "preciso do Gmail pra isso" com um cartão de conectar (D37).
- **Restrição do tailnet (D10):** sem porta pública, os webhooks do Google, da Meta e do ClickUp não chegam. Gmail é lido por `history` a cada 2 min; ClickUp e Instagram, por polling. Um Tailscale Funnel restrito a um caminho fica como opção futura.

**Pronto quando:** "responde os e-mails de orçamento de hoje e cria as tarefas no ClickUp" funciona de ponta a ponta, com aprovação só no envio.

### Instagram e X (plano, 2026-11-12)
As APIs oficiais não deixam "navegar": o Instagram só abre a conta profissional do próprio dono, e o X cobra por leitura. Então são duas camadas, e o agente escolhe a mais barata que resolve.

- **Camada 1: ler o que o dono manda (sem login, primeiro a fazer).**
  - Link do X: `x_ver` lê o post pelo endpoint público de embed (texto, autor, mídia, citado). Sem chave, sem custo.
  - Link do Instagram ou do X com vídeo: `yt-dlp` no nó baixa o vídeo e a legenda; `ffmpeg` tira quadros e o áudio; o Whisper transcreve. O agente "vê" o post pelos quadros e pela transcrição.
- **Camada 2: navegador do agente (para navegar, buscar e achar posts).**
  - Um Chromium com perfil próprio por conta, no **PC** (IP residencial; IP de datacenter da VPS cai em verificação), controlado pelo runner via CDP.
  - O dono entra uma vez pelo app (takeover, com 2FA); o Avora nunca vê a senha, só usa o perfil logado.
  - Ferramentas: `navegador_abrir`, `navegador_ler` (árvore de acessibilidade, não pixels), `navegador_clicar`, `navegador_digitar`, `navegador_rolar`, `navegador_print`. Mais atalhos por site: `instagram_buscar` (perfil, hashtag, lugar), `instagram_perfil`, `instagram_post` (legenda, comentários), `x_buscar`, `x_perfil`, `x_timeline`.
  - Ritmo humano: até ~200 páginas/dia por conta, pausas aleatórias, nada em rajada. Curtir, seguir, comentar, postar e DM são 🔒 como enviar.
  - Risco honesto: automação fere os termos das duas redes e pode travar a conta. Por isso leitura primeiro, ações sempre aprovadas e a opção de usar uma conta secundária só para pesquisa.
- **Camada 3: APIs oficiais, para a conta do dono.**
  - Instagram API com login do Instagram (conta Profissional): publicar foto, carrossel e Reels, ler e responder comentários, insights, DMs (este pede App Review). Hashtag Search exige App Review e conta 30 hashtags únicas por 7 dias; Business Discovery lê perfis profissionais de terceiros pelo @.
  - X: pay-per-use desde 2026 (cerca de US$ 0,005 por post lido, com teto mensal). Fica opcional, com limite de gasto no Avora, só para postar e para buscas que o navegador não cobre.

### Skills, workflows e ferramentas (feito em 2026-11-12, D26)
Feito: skills no servidor (página Skills, `skill_ler`, `skill_criar` com aprovação, 5 prontas, importar SKILL.md), rotinas que seguem uma skill, e ferramentas do nó com ações fixas (`ferramenta`, `GET /v1/tools`). Falta: instalar uma ferramenta pelo chat (🔒 `winget`/`dnf` no nó), scripts e modelos dentro da pasta da skill, e devolver o arquivo gerado como anexo no chat.

Plano original:
- **Skills** = uma pasta com `SKILL.md` (nome, descrição e instruções, o mesmo formato das Agent Skills do Claude), mais scripts e modelos opcionais. Ficam no servidor, versionadas.
  - O prompt de cada agente leva só o nome e a descrição das skills dele; `skill_ler` carrega o resto quando precisa (revelação progressiva, contexto barato).
  - Chat-first: "aprende a fazer o relatório do mês assim" → o agente escreve a skill com `skill_criar`, mostra e o dono aprova. Depois de uma tarefa difícil ele propõe guardar como skill.
  - Importar: colar a pasta ou o link de uma skill pública.
- **Workflows** = skill com passos numerados + gatilho (rotina, evento ou pedido). Rodam pela fila de jobs com checkpoint por passo, sem DSL nova: as rotinas que já existem ganham `skill`.
- **Ferramentas** = binários no nó, com manifesto: `yt-dlp`, `ffmpeg`/`ffprobe`, `whisper.cpp`, `pandoc`, ImageMagick, `tesseract`, `qpdf`.
  - O nó anuncia o que tem instalado (versão); o agente vê só o que existe em algum nó online.
  - Execução por job `tool.run`, sem shell: argumentos validados pelo manifesto, pasta de trabalho do agente, tempo e tamanho limitados, saída de volta como anexo no chat.
  - Primeiras skills que vêm prontas: "baixar e resumir um vídeo", "cortar trecho e legendar", "transcrever áudio", "converter documento", "OCR de print".
  - Instalar ferramenta nova é 🔒 (`winget`/`apt` no nó).

### Fase C — Autonomia real
Não é agendamento nem "monitora e dispara". Agendar o Claude e o Codex já fazem, e monitorar é só um `if`. Autonomia real é um **laço contínuo dirigido por objetivo**: o agente decide sozinho o que fazer, quando, com qual ferramenta e quando terminou.
- **Iniciativas próprias:**
  - O agente lê as metas do dono (Omniarch e responsabilidades) e mantém um **backlog de iniciativas** que ele mesmo cria, prioriza e descarta.
  - Ele trabalha nelas continuamente dentro de um orçamento diário.
  - Cada ciclo é observar → decidir → agir → avaliar o resultado → mudar de estratégia.
  - Exemplo: "renda extra de R$ 5 mil/mês" vira pesquisar nichos com base nas habilidades reais, testar uma oferta, medir a resposta e dobrar a aposta ou abandonar, com o relatório semanal do que funcionou.
- **Aprende com o resultado:** grava o que deu certo como skill e o que falhou como memória, e propõe ferramentas novas pra si mesmo (Fase 8).
- **Eventos só acordam o laço:** "chegou resposta", "tarefa concluída". Quem decide o que fazer com eles é o agente, não uma regra.
- **Limites honestos:**
  - a autonomia vai até onde o modelo acerta em tarefas longas, porque erro composto é o que derruba agente;
  - ações irreversíveis (enviar, gastar, apagar, publicar) continuam 🔒 até o dono liberar por categoria.
- **Missões em segundo plano (feito em 2026-11-12, D27):** `missao_iniciar` e `missao_atualizar`. O objetivo e o checklist ficam no banco. Cada rodada do Claude Code/Codex roda solta no nó e continua a mesma sessão (`--resume`). Quando ela termina, o agente é acordado na conversa (também depois de um reinício), marca os passos e continua, conclui ou pergunta ao dono. No app, os cartões acima do campo de mensagem mostram o checklist e têm o botão Parar. Feito em 2026-11-16 (D38): `aguardar_evento` nos objetivos, com `objetivo_atualizar esperar` (e-mail, tarefa, DM do Instagram, Tinder) e prazo. Falta: o mesmo para as missões do Claude Code/Codex.
- **Navegador e pesquisa a fundo (feito em 2026-11-12, D29):** navegador dentro do app, num painel ao lado da conversa, com as contas do dono e o dono podendo assumir, aprovação para enviar/comprar/publicar, prints no chat; `pesquisar` pelo Codex com busca ao vivo. Feito em 2026-11-16: mídia do WhatsApp (D33), com áudio transcrito pelo ElevenLabs. Falta: missão tocada pelo próprio agente com o navegador.
- **Gatilhos por evento e conversa longa (feito em 2026-11-12, D28):** rotinas disparam por e-mail novo, tarefa nova no ClickUp ou nó ligando/desligando; a conversa mostra o contexto em uso e se compacta sozinha em 64 mil tokens; as execuções do Claude Code/Codex abrem como conversas à parte, aninhadas sob o agente. Falta: webhooks (em vez de conferir de tempos em tempos) e contar tokens de verdade pelo provedor.
- **Missão (visão completa)** = objetivo + prazo + critério de pronto, com um agente dono.
  - Ele planeja, executa passo a passo pela fila de jobs e espera eventos ("resposta do fornecedor", "tarefa concluída") com `aguardar_evento`.
  - Salva checkpoint no banco a cada passo e sobrevive a reinício.
  - Mostra o progresso no chat; pede aprovação só no 🔒.
  - Exemplo: "consegue 3 orçamentos de notebook até sexta". Ele pesquisa, envia e-mails às lojas (com aprovação), espera as respostas por dias e entrega a tabela.
- **Entradas que acordam o laço:** e-mail novo que casa com uma regra, tarefa do ClickUp atribuída, comentário no Instagram, nó online/offline, URL de webhook com segredo.
- **Heartbeat (M4)**: feito em 2026-11-16 (D39), com orçamento diário. **Turno da noite (M5.1)**: feito em 2026-11-16 (D40).

**Pronto quando:** sem o dono mandar nada, ele avisa de um e-mail importante sem resposta há 2 dias e oferece o rascunho.

### Fase D — Computador próprio (M2)
Container por agente na VPS com Chromium persistente, visão ao vivo e takeover pra login/2FA.

### Robustez, em todas as fases
- **Turnos duráveis:** reinício do servidor retoma a resposta em vez de terminar com erro.
- **Envio idempotente** (`client_message_id`).
- **Tela "Sistema":** servidor, banco, nós, latência do modelo, conectores e cota, com verde/vermelho honesto.
- **Erros em português com a ação:** "Gmail desconectou · Reconectar".
- **Evals noturnos:** 30 tarefas reais contra o modelo de verdade, com taxa de acerto registrada. A qualidade é medida, não sentida.
- **UI enxuta:** só o que serve à conversa e às ações.


## Histórico das fases

## Fase 0 — Fundação ✅
- [x] POC autenticado PC ↔ VPS: heartbeat, presença, jobs allowlisted (`ping`, `diagnostics`)
- [x] Roteamento puro com testes

## Fase 1 — Núcleo durável ✅
- [x] Postgres (database `avora`) com migrações
- [x] Log de eventos append-only (base de M6)
- [x] Jobs persistentes: fila, timeout, cancelamento, recuperação após queda do worker
- [x] Nós no banco com tokens em hash, pareamento por código e revogação (M3)
- [x] Deploy na VPS: systemd + `tailscale serve`

## Fase 2 — Primeiro agente de verdade
- [x] Motor nativo (NVIDIA build, DeepSeek V4.1 Flash) com conversas guardadas no Postgres
- [x] Adaptador Claude Code CLI (stream-json, passos auditados, cancelamento) — PC
- [x] Claude Code na VPS (usuário `avora`, sandbox do systemd, 2 GB)
- [x] Adaptador Codex CLI (PC e VPS instalados e logados; falta teste real: cota do ChatGPT esgotada até 11/11 18:19)
- [x] Portão de políticas v1 + aprovações (M6): regras no código, Permitir / Sempre permitir / Negar, expiração
- [x] API de chat com streaming (SSE) pros apps

## Fase 3 — App
- [x] App desktop Windows em C++20 + Qt 6/QML (`desktop/`): Chat, Atividade, Nós (a página Tarefas saiu: os agentes delegam pelo chat)
- [ ] App Android em Kotlin + Jetpack Compose
- [ ] Remover o app web provisório (`web/`) quando o Android existir
- [x] Aprovações no app: cartões no chat e no #time, "Sempre permitido" revogável no perfil (push no celular fica com o Android)
- [ ] Push com botões de ação

## Fase 4 — Omniarch
- [ ] `omniarch-mcp`
- [ ] Escopos no token e `created_by` no Omniarch
- [ ] Webhooks Omniarch → Avora
- [ ] Remover o chat do Omniarch

## Fase 5 — Proatividade
- [x] Heartbeat (M4, D39): e-mails sem resposta, agenda, tarefas, DMs, aprovações e rotinas, sem o modelo; o agente só acorda com o que é novo e avisa no #time
- [x] Rotinas agendadas (M5): briefing da manhã, revisão semanal, desativação depois de 10 falhas
- [x] Turno da noite (M5.1, D40): fila pelo chat, pelo app e por etiqueta no ClickUp, relatório no #time. Falta pegar do Omniarch ("Avora pode fazer") quando ele estiver integrado.
- [ ] Gatilhos por evento do Omniarch (M5)

## Fase 6 — Time
- [x] Funcionários versionados com conversas próprias (M1, parte 1)
- [x] **Agentes com ferramentas (D17):** todos pensam com o motor nativo e delegam ao Claude Code/Codex, criam rotinas e responsabilidades e consultam colegas pelo chat, com cartões de ação no app
- [x] Responsabilidades e rotinas por funcionário (M1, parte 2)
- [x] Canal #time com @menções entre funcionários e trava contra loop de 8 respostas (M9)
- [ ] Skills (M10)
- [ ] Radares e Descobertas

## Fase 7 — Execução avançada
- [ ] Container por agente, navegador persistente, visão ao vivo e takeover (M2)
- [ ] Cofre de credenciais (M7)
- [ ] Failover e painel de cota (M8)

## Fase 8 — Evolução
- [ ] Memória inspecionável com busca semântica (M11)
- [ ] Canvas e cartões interativos
- [ ] Ensinar por demonstração
- [ ] Agentes que criam ferramentas e propõem melhorias no próprio Avora e no Omniarch
