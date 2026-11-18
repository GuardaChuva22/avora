# Avora — Operação

Como rodar o Avora no seu servidor, fazer deploy, parear nós e consultar o que aconteceu.

> Executa `ping`, `diagnostics` e tarefas de agente (Claude Code e Codex pela assinatura). Não roda shell livre.

## Onde roda

| O quê | Onde |
|---|---|
| `avora-server` | Seu servidor Linux (systemd), `127.0.0.1:3190`, usuário `avora` |
| Endereço | Com Tailscale: `https://<máquina>.<tailnet>.ts.net:8443` (via `tailscale serve`). Sem Tailscale: um proxy TLS seu (Caddy, nginx) para `127.0.0.1:3190` |
| Banco | Postgres do servidor, database `avora`, socket local com autenticação peer (sem senha) |
| Token de admin | `/etc/avora/avora.env` (root:avora 640). Gerado na VPS, nunca sai dela |
| Nó da VPS | `avora-runtime.service`, estado em `/var/lib/avora/runtime/node.json` (0600) |
| Binários | `/opt/avora/bin`; helper `avora-pair` em `/usr/local/bin` |
| Nó do PC | `%LOCALAPPDATA%\Avora\bin\avora-runtime.exe` (sem janela), estado em `%APPDATA%\Avora\node.json`, log em `%APPDATA%\Avora\runtime.log`, inicia com o Windows pelo atalho `Avora Runtime` na pasta Inicializar |

## Deploy

Do PC, no Git Bash, na raiz do repo:

```bash
./scripts/deploy-vps.sh
```

Compila para Linux (`arm64`; `AVORA_VPS_ARCH=amd64` para Intel/AMD), envia os binários e `deploy/vps/`, e roda `deploy/vps/install.sh` como root. O instalador é idempotente: cria usuário, pastas, token de admin, role e database no Postgres e a entrada do `tailscale serve` só quando faltam; reinicia os serviços e confere a saúde. Na primeira vez, também pareia o nó da VPS sozinho.

O alvo vem de `.env.vps` na raiz (fora do git; copie de `.env.vps.example`): `AVORA_VPS_HOST` (`usuario@host`), e opcionalmente `AVORA_VPS_KEY` (arquivo da chave SSH) e `AVORA_TIMEZONE` (fuso do dono, gravado na primeira instalação). O instalador funciona em Fedora/RHEL, Debian/Ubuntu, Arch e openSUSE com systemd.

Logs na VPS:

```bash
sudo journalctl -u avora-server -u avora-runtime -f
```

## App desktop (Windows)

C++20 + Qt 6/QML, em `desktop/`. Fala só com a API do `avora-server` pelo tailnet.

Compilar e instalar (precisa do VS Build Tools com C++ e do Qt 6.11 para MSVC em `C:\Qt\6.11.3\msvc2022_64`; `AVORA_QT` troca o caminho):

```bash
powershell -ExecutionPolicy Bypass -File desktop/build.ps1
```

```bash
powershell -ExecutionPolicy Bypass -File desktop/install.ps1
```

Instala em `%LOCALAPPDATA%\Avora\desktop` e cria o atalho "Avora" no Menu Iniciar. No primeiro uso, rode `bash scripts/admin-token.sh` e clique em **Colar** no login. O token fica cifrado com o DPAPI do Windows em `%APPDATA%\Avora\Desktop	oken.dat`, legível só pelo seu usuário neste PC; "Sair" apaga.

O texto usa renderização por curvas (antialiasing em cinza, sem as franjas coloridas do ClearType). Para comparar, `AVORA_TEXT_RENDER=native` ou `qt` antes de abrir o app.

Instalar o Qt sem conta: o `aqtinstall` 3.3 não lê o layout novo do repositório da Qt. O jeito usado aqui foi baixar direto de `download.qt.io` os pacotes `qtbase`, `qtdeclarative`, `qtsvg`, `qtshadertools` e `d3dcompiler_47` do `qt6_6113_msvc2022_64`, conferindo o SHA-1 de cada um.

Para testes, `--profile <nome>` isola configurações e token; só com ele valem `--server` e `--token-file`. `--grab arquivo.png --page chat|tarefas|atividade|nos` salva a tela e fecha.

## App web (provisório)

Abra `https://<seu-servidor>:8443` em qualquer aparelho do tailnet. É um app web instalável (PWA): no Chrome do PC ou do Android, use "Instalar app" / "Adicionar à tela inicial".

Telas: Chat (motor nativo, com streaming), Tarefas (dispara Claude Code ou Codex no PC ou na VPS e acompanha os passos), Atividade (o log de eventos) e Nós (estado e revogação).

Login com o token de admin. Para copiá-lo sem que apareça na tela:

```bash
bash scripts/admin-token.sh
```

O script lê o token na VPS pelo SSH e joga direto na área de transferência; cole no login com Ctrl+V. O app guarda o token só no navegador daquele aparelho; "Sair" apaga.

O código fica em `web/` (React + Vite). `npm run build` gera `internal/webui/static`, que o `avora-server` embute no binário; `deploy-vps.sh` já faz esse build. Para desenvolver com recarga instantânea: `AVORA_DEV_API=http://127.0.0.1:3190 npx vite` dentro de `web/`.

## Parear um nó novo

1. Na VPS, gere um código (vale 10 min e uma única vez):
   ```bash
   sudo avora-pair "meu-pc" windows
   ```
2. No nó, rode o runtime uma vez com o código. No Windows (PowerShell):
   ```powershell
   go build -o avora-runtime.exe ./cmd/avora-runtime
   $env:AVORA_SERVER_URL = "https://<seu-servidor>:8443"
   $env:AVORA_PAIRING_CODE = "XXXX-XXXX-XXXX-XXXX"
   .\avora-runtime.exe
   ```
   O token recebido fica em `%APPDATA%\Avora\node.json`, junto com o endereço do servidor. Nas próximas execuções nem o código nem as variáveis são necessários.

   Pra rodar sem janela, compile com `-ldflags "-H=windowsgui"` e passe `-log <arquivo>`; foi assim que o PC atual foi instalado. Erros repetidos (ex.: Tailscale desligado) aparecem uma vez no log, não a cada 5 s.

O servidor guarda só o hash SHA-256 do token. Se o arquivo de estado se perder, gere um código novo e revogue o nó antigo.

## Revogar um nó

```bash
. /etc/avora/avora.env   # como root na VPS
curl -X POST -H "Authorization: Bearer $AVORA_ADMIN_TOKEN" http://127.0.0.1:3190/v1/nodes/<id>/revoke
```

O acesso cai na hora, os jobs não terminados daquele nó são cancelados e o runtime revogado encerra.

## API

Admin (Bearer `AVORA_ADMIN_TOKEN`):
- `GET /v1/capabilities`
- `GET /v1/nodes`: nós, presença e revogação
- `POST /v1/nodes/pairing-codes` `{"name","kind":"windows|vps"}`: código de pareamento
- `POST /v1/nodes/{id}/revoke`
- `POST /v1/jobs` `{"target":"auto|windows|vps","action":"ping|diagnostics|agent.run","agent":{...}}`
- `GET /v1/jobs/{id}`, `POST /v1/jobs/{id}/cancel`
- `GET /v1/events?after=&job=&limit=`: log de eventos append-only, paginado
- `GET /v1/agents`: o time, com quem está trabalhando, número de conversas e a última prévia
- `POST /v1/agents`, `GET|PUT /v1/agents/{id}` `{"name","role","persona","engine":"native|claude|codex","model","mode":"read|edit","color","position"}`: cada gravação vira uma versão nova
- `GET /v1/agents/{id}/versions`, `POST /v1/agents/{id}/versions/{n}/restore`: a restauração grava a versão antiga como a mais nova
- `PUT|GET|DELETE /v1/agents/{id}/avatar`: foto do funcionário (corpo cru WebP, PNG ou JPEG, até 512 KB; 413 se maior). O app recorta em quadrado, reduz a 256 px e converte para WebP antes de enviar. `GET /v1/agents` traz `avatar_at` para o cache
- `POST /v1/conversations` `{"title","agent_id"}`, `GET /v1/conversations?agent=&limit=&archived=1`: sem `archived` lista as ativas; com `archived=1`, só as arquivadas
- `PATCH /v1/conversations/{id}` `{"archived":true|false}`, `DELETE /v1/conversations/{id}`: arquiva ou apaga de vez (409 enquanto responde; o `#time` não pode)
- `GET /v1/conversations/{id}/messages`
- `POST /v1/conversations/{id}/stop`: para a resposta em andamento (no `#time`, todas)
- `GET /v1/routines?agent=`, `POST /v1/routines`, `PUT|DELETE /v1/routines/{id}` `{"agent_id","title","prompt","schedule":{"at":"08:00","days":[1,2,3,4,5]} ou {"every":60},"deliver":"time|conversation","responsibility_id","enabled"}`
- `POST /v1/routines/{id}/run`: roda agora (409 se já está rodando)
- `GET /v1/channel`: estado do `#time` (quem está trabalhando, respostas seguidas, trava)
- `POST /v1/conversations/{id}/messages` `{"content","reply_to"}`: `reply_to` é o id de uma mensagem da mesma conversa (400 se não for); o modelo recebe o trecho citado e, no `#time`, responder a um funcionário sem @ chama ele. No `#time` (id `time`), devolve 202 `{user, called}` e chama os funcionários das @menções. Nas outras conversas, quem responde é o funcionário da conversa. Motor nativo: com `Accept: text/event-stream` vem em SSE (`user`, `reasoning`, `delta`, `done`, `error`); sem isso, JSON com as duas mensagens. Claude/Codex: vira um job `agent.run` e devolve 202 `{user, job_id}` (SSE: `user`, `job`, `step`, `done`); 409 enquanto a conversa já tem um job rodando

Plugins (admin):
- `GET /v1/connectors`: catálogo com cada conector, as ferramentas (com risco e se pedem aprovação) e as contas conectadas em `connections` (`id`, `account`, `status` ok|error|reconnect, `last_ok`, `agents`; a primeira é a principal). `connection` repete a primeira, ou `null`. O token nunca volta
- `PUT /v1/connectors/{kind}` `{"token","connection"}`: testa o token no app (`Check`), lacra no cofre e guarda. Sem `connection`, adiciona uma conta, ou atualiza a que tiver o mesmo nome de conta; com `connection`, reconecta aquela conta (400 se o token for de outra pessoa)
- `POST /v1/connectors/{kind}/test?id=`, `DELETE /v1/connectors/{kind}?id=`: uma conta (sem `id`, a principal)
- `PUT /v1/connectors/{kind}/agents?id=` `{"agents":[ids]}`: quem pode usar aquela conta (`[]` = todos)
- `POST /v1/connectors/{kind}/oauth/start` `{"redirect_uri","connection"}` → `{url, state}` e `POST /v1/connectors/{kind}/oauth/finish` `{"state","code"}`: login pelo navegador (Google e apps MCP). O `redirect_uri` precisa ser `http://127.0.0.1:<porta>/…`; o `state` vale 10 min e uma vez só
- `POST /v1/connectors/mcp` `{"url","name"}`: adiciona um servidor MCP pelo endereço https (descobre se o login é de um clique, por chave ou aberto); `DELETE` num app adicionado assim também o tira do catálogo

Runtime:
- `POST /v1/runtime/pair` `{"code"}`: sem token; devolve o token do nó uma única vez
- `POST /v1/runtime/heartbeat` `{"running":[ids]}` → `{"stop":[ids]}`: presença, renova o lease das tarefas em andamento e diz quais parar
- `POST /v1/runtime/claim` `{"agents":bool}`, `POST /v1/runtime/jobs/{id}/complete`, `POST /v1/runtime/jobs/{id}/steps`: Bearer do nó

Sem autenticação: `GET /healthz`.

## Chat e motor nativo

Gravar a chave da NVIDIA (pede o valor sem mostrar, manda só pelo SSH):

```bash
./scripts/set-secret.sh NVIDIA_API_KEY
```

Testar na VPS: `sudo /usr/local/bin/avora-chat "oi"` (continuar com `-c <id>`).

Variáveis opcionais em `/etc/avora/avora.env`: `AVORA_NATIVE_BASE_URL` (padrão `https://integrate.api.nvidia.com/v1`), `AVORA_NATIVE_MODEL` (padrão `nvidia/nemotron-3-ultra-550b-a55b`), `AVORA_NATIVE_PROVIDER`, `AVORA_NATIVE_RPM` (padrão 30). Sem chave, as rotas de chat respondem 503.

O conteúdo das mensagens fica só na tabela `messages`; os eventos registram tamanho, tokens, modelo e duração, nunca o texto. Falhas do modelo viram evento `llm.failed`.

## Projetos

Na lista do Chat, cada projeto é uma seção com os funcionários dele; os sem projeto ficam em **Universais**. O ícone de pasta no topo cria um projeto; a engrenagem no cabeçalho da seção edita, e o **+** contrata alguém já dentro dele. Para mudar alguém de projeto, arraste a linha até a seção ou clique com o botão direito e escolha. As conversas de cada funcionário ficam no seletor no topo da conversa (abrir, arquivar, apagar).

- **Pastas:** a principal fica no PC (escolhida na janela de pastas do Windows) ou na VPS (um caminho dentro do workspace, padrão `projects/<nome>`). Até 8 pastas por projeto, com nome.
- **AGENTS.md:** as instruções do projeto ficam no `AGENTS.md` da pasta principal. Na VPS o servidor grava e lê o arquivo (o que alguém editar nele vale). No PC quem grava e lê é o app: ao abrir o projeto, o arquivo da pasta vale mais que a cópia do servidor.
- **Quem vê quem:** um funcionário de projeto só consulta, chama no `#time` e vê no prompt os colegas do projeto e os universais. Um universal vê todos e recebe a lista de projetos com quem está em cada um.
- **Delegação:** `claude_code` e `codex` de um funcionário de projeto rodam na pasta principal (ou em outra, com `pasta`), no nó onde ela fica, com as instruções do projeto no pedido. Um universal escolhe o projeto com `projeto`. Se o nó da pasta estiver offline, a ferramenta diz isso.
- **Segurança no PC:** o runtime só roda numa pasta absoluta se ela estiver em `%APPDATA%\Avora\folders.json`, que o app grava quando o dono escolhe a pasta naquele PC. O servidor não escreve nesse arquivo.
- **API:** `GET/POST /v1/projects`, `GET/PUT/DELETE /v1/projects/{id}` (excluir não apaga pastas; os funcionários viram universais), `PUT /v1/agents/{id}/project` `{"project_id": ""}`, e `project_id` em `POST /v1/agents`.

## Missões

Uma **missão** é um trabalho longo que o agente toca em segundo plano enquanto a conversa segue.

- **Como começa:** o agente chama `missao_iniciar` com título, objetivo, passos e executor (Claude Code ou Codex). Se precisar editar arquivos, pede aprovação uma vez 🔒.
- **Rodadas:**
  - cada rodada roda no nó escolhido no início (a pasta do projeto, a conta ou o nó com a CLI) e continua a sessão da anterior;
  - quando termina, aparece na conversa uma mensagem `🎯 Missão «…» · a rodada N terminou`, dobrada (clique para abrir), e o agente responde em seguida;
  - com `missao_atualizar` ele marca os passos e escolhe o próximo movimento: `continuar` (nova rodada), `concluida`, `aguardando_dono` (pergunta) ou `cancelada`.
- **No app:** os cartões acima do campo de mensagem mostram o título, o estado, quantos passos estão feitos e o checklist (clique no título). **Parar** (clique duas vezes) cancela a rodada, e o agente não é acordado.
- **Limites:** 8 rodadas por missão, 6 missões ativas e 120 min por rodada. Se o nó da missão estiver offline, o agente não consegue continuar e pergunta ao dono.
- **Reinício:** o estado fica no banco. Se a rodada terminou com o servidor fora do ar, o agente é acordado quando ele volta.
- **API:** `GET /v1/missions?conversation=&agent=&active=1` e `POST /v1/missions/{id}/cancel`.

## Execuções do Claude Code e do Codex

- Enquanto o Claude Code ou o Codex trabalha para um agente, a execução aparece **aninhada sob o agente** na barra lateral, com um ponto pulsando. Clique para abrir o painel: o pedido (dobrado), o que a ferramenta disse, cada ferramenta que usou e a resposta, ao vivo. **Voltar** fecha o painel.
- Quando a execução termina, ela sai da barra lateral e fica nos passos da mensagem do agente: **ver o que fez ›** abre o mesmo painel com o histórico.
- **API:** `GET /v1/runs?active=1&agent=` e `GET /v1/runs/{id}` (pedido, passos, resposta, erro). O que a CLI diz chega como passo `texto` em `/v1/runtime/jobs/{id}/steps`; o runtime do PC precisa estar atualizado para mandar esses passos.

## Navegador dos agentes

- O navegador fica **dentro do app**: um painel à direita da conversa (item **Navegador** na barra lateral), redimensionável pela borda esquerda. É um Chromium (QtWebEngine) com perfil próprio por perfil do app (`navegador-<perfil>`, cookies persistentes). Entre nos seus sites uma vez no painel; os agentes usam essas sessões.
- O app aberto e logado é o nó `app`: busca os comandos por long poll em `POST /v1/browser/app/next` e responde em `POST /v1/browser/app/result` (token de admin). Quando um agente começa a navegar, o painel abre sozinho; cliques, rolagem e teclas são eventos de entrada reais na view, e o texto entra como se fosse digitado.
- **Assumir** faz os agentes esperarem; **Devolver aos agentes** libera. Uma faixa mostra quem está navegando.
- O Chrome do PC pelo runtime virou opcional: só liga com `AVORA_BROWSER=1` no runtime (`AVORA_BROWSER_HEADLESS=1` sem janela). Com o app aberto, o app tem preferência.
- A ferramenta `navegador` (abrir, ler, clicar, digitar, rolar, tecla, voltar, abas, print) só aparece para os agentes quando há um navegador pronto. Um agente por vez; o outro espera até 3 min. Senha o agente não digita.
- Enviar, publicar, comprar, pagar, apagar, seguir ou comentar pede aprovação (`navegador.enviar`; dá para liberar sempre por funcionário).
- **API:** `GET /v1/browser`, `GET /v1/browser/script` (o leitor de páginas), `GET /v1/browser/{node}/screen`, `POST /v1/browser/{node}/control` `{owner}`, `POST /v1/browser/{node}/command` (abrir, print, voltar, rolar, fechar), `GET /v1/files/{id}`.
- QtWebEngine 6.11: declarar `settings.*` direto no `WebEngineView` derruba o app na abertura; esses ajustes são feitos em `Component.onCompleted`.

## WhatsApp

- **Plugins → WhatsApp → Conectar** mostra um QR: no celular, WhatsApp → Aparelhos conectados → Conectar um aparelho. O servidor vira um aparelho conectado (como o WhatsApp Web), pela biblioteca whatsmeow, a mesma do whatsapp-mcp. Nada roda no PC.
- As chaves do aparelho ficam no Postgres (`whatsmeow_*`); conversas e mensagens em `wa_chats` e `wa_messages` (texto; a mídia fica guardada como mensagem e o arquivo só é baixado quando alguém precisa). O histórico chega aos poucos depois de conectar.
- Ferramentas dos agentes (só com o WhatsApp conectado): `whatsapp_conversas`, `whatsapp_ler`, `whatsapp_midia`, `whatsapp_enviar`. Enviar pede aprovação (`whatsapp.enviar`) ou passa pelo fiscal dentro do mandato de um objetivo.
- **Fale com a Vega pelo WhatsApp:** escreva em "Mensagem para você"; ela responde ali (conversa «📱 WhatsApp» no app). Aprovações continuam no app.
- **Mídia (D33):** foto, áudio, vídeo ou documento que você manda no seu chat vira anexo da conversa (o mesmo caminho dos anexos do app: a foto chega ao Codex, o PDF tem o texto lido). Áudio é transcrito pelo ElevenLabs Speech to Text (conecte o ElevenLabs com a permissão Speech to Text); sem ele, o agente avisa. `whatsapp_midia` abre a mídia de qualquer conversa na pasta do agente. Até 15 MB por arquivo.
- Resposta de alguém que está no funil de um objetivo (contato com o mesmo número) acorda o objetivo na hora.
- Privacidade: com o NVIDIA grátis como modelo principal, as ferramentas e o canal recusam (ele pode treinar com as conversas).
- Risco: não é a API oficial; mandar muitas mensagens para quem não te conhece pode bloquear o número.
- **API:** `GET /v1/whatsapp`, `POST /v1/whatsapp/connect`, `POST /v1/whatsapp/logout`, `GET /v1/whatsapp/qr.png`.

## Instagram

- **Plugins → Instagram:** conta Profissional (Comercial ou Criador). Em developers.facebook.com/apps crie um app com o caso de uso "Gerenciar mensagens e conteúdo no Instagram", use a configuração da API com **login unificado** (a que a Meta recomenda e continua atualizando), conecte a sua conta e cole o token (EAA...). O Avora roda no graph.facebook.com e acha a conta do Instagram pelas Páginas do token; se não achar, cole `TOKEN#ID_DA_CONTA`. O token do login do Instagram (IG...) também funciona, no graph.instagram.com; esse vale 60 dias e o servidor renova quando faltam menos de 20. Token vencido ou revogado vira "Reconectar".
- Ferramentas (as mesmas do instagram-mcp, nativas em Go): `instagram_perfil`, `instagram_posts`, `instagram_metricas`, `instagram_comentarios`, `instagram_conversas`, `instagram_mensagens`; com aprovação: `instagram_publicar` (foto, carrossel, Reels), `instagram_responder_comentario`, `instagram_dm`.
- Publicar precisa da mídia num https público: o Instagram baixa de lá, e os arquivos do Avora ficam no tailnet.
- DMs de outras pessoas pedem Advanced Access (revisão do app pela Meta); responder só até 24 h depois da última mensagem da pessoa. Perfis de terceiros e busca seguem pelo navegador dos agentes.

## Tinder

- **Plugins → Tinder:** o Tinder não tem API oficial; o Avora usa a mesma API do tinder.com (`api.gotinder.com`) com o token da sua sessão, como o tinder-mcp-server. Para pegar: tinder.com no Chrome → F12 → Application → Local Storage → `https://tinder.com` → valor de `TinderWeb/APIToken`. Vale até você sair da conta no site.
- Ferramentas: `tinder_perfil`, `tinder_sugestoes`, `tinder_matches` (novos ou conversas, com ⏳ quando a pessoa espera resposta), `tinder_mensagens`, `tinder_passar`; com aprovação: `tinder_curtir` (e super like) e `tinder_enviar`.
- Risco: automação vai contra os termos do Tinder e as chamadas saem do IP da VPS; use com calma. 403 vira "Reconectar".

## Conectar pelo chat

- Quando um pedido precisa de um app que não está conectado, o agente chama `pedir_conexao` e a conversa mostra um cartão com o logo e **Conectar**: login de um clique abre o navegador; token ou formulário abre o campo ali mesmo, com a ajuda de onde pegar; WhatsApp mostra o QR no cartão.
- Ao terminar, o app manda "Conectei o X. Pode continuar." e o agente segue o pedido.

## Marcar com @ no chat

- No campo de mensagem, `@` abre a lista do que dá para marcar: os apps conectados para aquele agente (Gmail, Drive e Agenda separados), o WhatsApp, as skills que ele vê e as ferramentas Navegador, Pesquisa e Web. Setas escolhem, Enter ou Tab completam.
- O turno recebe "o dono marcou": app → use só as ferramentas daquele conector (não o navegador); skill → os passos dela já vão junto; ferramenta → use aquela.
- **API:** `GET /v1/mentions?agent=`.

## Anexos no chat

- No app: o botão de clipe ao lado de enviar, **Ctrl+V** com uma imagem copiada (print) ou **arrastar** arquivos para a conversa. Até 15 MB por arquivo; fotos acima de 4 MB viram JPEG de até 2560 px.
- A mensagem leva os arquivos em Markdown: `![nome](avora:file/<id>)` para imagem, `[📎 nome](avora:file/<id>#nome)` para o resto. Clicar num anexo salva em `Downloads\Avora` e abre.
- O agente recebe cada arquivo na pasta dele (`anexos/`) e o texto dos legíveis (texto, CSV, código, PDF com texto, Word) junto com a mensagem, até 24 mil caracteres. Imagens chegam ao modelo quando o principal é o Codex (`codex exec --image`, até 4 por mensagem, 5 MB cada); outros modelos são avisados de que não enxergam.
- **API:** `POST /v1/files?name=&conversation=` com os bytes no corpo e o tipo em `Content-Type` (um "image/*" que não é imagem de verdade é guardado como binário); `GET /v1/files/{id}`.

## Heartbeat

- **O que faz:** a cada 30 min, das 7h às 22h (Brasília), o servidor confere sem o modelo:
  - e-mails sem resposta do dono há 2 dias ou mais (`gmail_sem_resposta`: a última mensagem da conversa é de outra pessoa, sem promoções, listas de e-mail (com link de descadastro) nem remetentes automáticos);
  - compromissos nas próximas 2 horas;
  - tarefas do ClickUp vencendo hoje ou atrasadas;
  - DMs do Instagram esperando resposta (curtida ou figurinha sozinha não conta);
  - aprovações paradas há mais de 30 min;
  - rotinas que falharam.
- **Quando acorda a Vega:** só se apareceu algo que ainda não foi mostrado. Ela lê o checklist do dono e decide: nada, avisar ou agir no que é seguro (por exemplo, deixar a resposta em Rascunhos, sem enviar). O aviso vai para o #time com 💓, e a conversa «💓 Heartbeat» guarda o trabalho. Se ela responde "Nada novo.", nada vai para o #time.
- **Orçamento:** padrão de 12 acordadas por dia. Quando acaba, avisa uma vez no #time e o que apareceu espera o dia seguinte. Toda decisão, inclusive "nada", vai para o log de eventos (`heartbeat`).
- **Privacidade:** com o NVIDIA grátis como modelo principal, o heartbeat fica parado, porque lê e-mails.
- **No app:** Configurações → **Heartbeat**: ligar ou desligar, a cada quanto (15 min a 2 h), quantas vezes por dia, o checklist e **Conferir agora**.
- **API:** `GET /v1/heartbeat`, `PUT /v1/heartbeat` `{enabled, agent_id, every, budget, checklist}`, `POST /v1/heartbeat/run`.

## Turno da noite

- **Fila:** o que fica para a noite entra de três jeitos:
  - no chat ("deixa isso pra noite"), quando o agente chama `deixar_para_noite`;
  - em Configurações → **Turno da noite** → Adicionar;
  - por tarefa do ClickUp com a etiqueta `avora` (troque ou apague na mesma tela). Elas entram na fila quando o turno começa.
- **O turno:** começa às 01:00 e para de pegar tarefas às 06:00 (Brasília); faz até 5 tarefas por noite, uma por vez. Cada tarefa tem a conversa «🌙 …» da Vega com o trabalho inteiro. O que não coube fica para a noite seguinte.
- **Sem aprovação à noite:** ação travada (enviar, publicar, comentar, apagar, gastar) é recusada na hora, sem esperar ninguém. O agente deixa pronto e marca ESPERANDO APROVAÇÃO.
- **Relatório:** ao terminar, vai para o #time com ✅ Feito, ⏳ Esperando você e ⛔ Travado (e o porquê). O status vem da última linha do agente (FEITO / ESPERANDO APROVAÇÃO / TRAVADO).
- **Limpeza:** tarefas terminadas somem da fila depois de 14 dias. Uma tarefa do ClickUp que ainda tem a etiqueta volta à fila depois disso, então tire a etiqueta quando terminar.
- **API:** `GET/PUT /v1/night` `{enabled, agent_id, start, end, max_tasks, clickup_tag}`, `POST /v1/night/tasks` `{text}`, `DELETE /v1/night/tasks/{id}`, `POST /v1/night/run` (roda agora). `clickup_tarefas` ganhou o filtro `etiqueta`.

## Objetivos com mandato

- No chat: "consegue 5 reuniões com clínicas até 30/11; pode mandar a primeira mensagem do roteiro no Instagram, nunca fale de preço". O agente cria o objetivo (`objetivo_criar`) e passa a trabalhar nele numa conversa própria («🎯 título»).
- Página **Objetivos** no app: o funil por etapa, o mandato, quando o agente acorda e por quê, o que aprendeu, as páginas vigiadas, os eventos esperados; botões Conversa, Acordar agora, Pausar/Retomar.
- O servidor acorda os objetivos ativos só das 8h às 21h (horário de Brasília); sem horário marcado pelo agente, acorda em 3 h. Revisão diária a cada 20 h. Vigias: no mínimo 30 min, no máximo 5 por objetivo, uma checagem por vez, e só quando o app (navegador) está aberto e o dono não assumiu.
- **Esperar um evento (aguardar_evento):** o agente usa `objetivo_atualizar` com `esperar` (`email` com uma busca do Gmail, `tarefa` no ClickUp, `instagram_dm`, `tinder_mensagem`, `tinder_match`), um `motivo` e um prazo opcional `ate`. O servidor confere o app a cada 5 min (mínimo 2), sem o modelo e com a conta do Plugin. A primeira conferida só aprende o que já existe. O que chegar de novo acorda o agente com o item, e a espera termina, a não ser que seja `sempre`. Se o prazo vencer sem nada, o agente acorda para cobrar ou desistir. Mensagens de contatos do funil no WhatsApp já acordam o objetivo sozinhas. Vigias e esperas: no máximo 8 por objetivo. A página Objetivos mostra as esperas na linha **Espera**.
- Fiscal do mandato: só dentro da conversa do objetivo e só com mandato escrito. Sem mandato, tudo que é travado pede aprovação como sempre.
- **API:** `GET /v1/goals?agent=`, `POST /v1/goals/{id}` `{status, mandate, never}`, `POST /v1/goals/{id}/wake`.

## Pesquisa a fundo

- `pesquisar` manda o assunto ao Codex com busca na web ao vivo (`codex --search exec`), modelo `gpt-6.1-sol` (troque com `AVORA_RESEARCH_MODEL`) e raciocínio alto. Usa a assinatura do ChatGPT do nó e aparece como execução aninhada sob o agente.
- `buscar_na_web` continua para consultas rápidas: DuckDuckGo, e Yahoo e Bing quando ele bloqueia a VPS.

## Contexto e compactação

- O medidor no topo da conversa mostra quantos mil tokens a conversa ocupa (estimativa). Clique para ver as partes (instruções e memória, ferramentas, resumo, mensagens).
- Ao passar de **64 mil**, as mensagens mais antigas viram um resumo feito pelo modelo principal e saem do contexto; as 12 mais recentes ficam inteiras. Você continua vendo todas no app. **Compactar agora** faz isso na hora.
- Com o Codex como modelo principal não há compactação (ele cuida da própria sessão).
- **API:** `GET /v1/conversations/{id}/context` e `POST /v1/conversations/{id}/compact`.

## Gatilhos de rotina

- No editor da rotina, **Quando algo acontecer**: e-mail novo (uma busca do Gmail), tarefa nova no ClickUp (lista e texto opcionais), ou o PC/VPS ligar ou desligar. O agente também cria pela ferramenta `criar_rotina` com `gatilho`.
- E-mail e ClickUp são conferidos a cada 2 a 1440 min (padrão 5) com a conta do Plugin. A primeira conferida só aprende o que já existe; depois, cada item novo dispara a rotina, que recebe o que chegou junto com a instrução.
- Nó: o servidor confere a cada minuto se o nó mudou de estado.

## Skills e ferramentas

**Skills** ficam na página Skills do app. São três jeitos de criar: **Nova skill**, **Colar SKILL.md** (o formato das Agent Skills, com `name` e `description` no cabeçalho) e as **prontas** (resumir-video, cortar-e-legendar, transcrever-audio, converter-documento, ler-print).

- **Quem vê:** cada skill é do time todo ou de um projeto. Um funcionário de projeto vê as do projeto dele e as do time; um universal vê todas.
- **No chat:** o prompt leva só o nome e a descrição. Quando o pedido casa, o agente chama `skill_ler` e segue os passos. Para guardar um jeito novo, ele usa `skill_criar`, que pede aprovação 🔒 (o card mostra os passos). Um funcionário de projeto guarda no projeto dele.
- **Workflows:** uma rotina pode seguir uma skill. No editor de rotina, escolha em **Segue a skill**; no chat, use o parâmetro `skill` de `criar_rotina`. Os passos vão junto com a instrução em cada execução.
- **API:** `GET/POST /v1/skills` (`markdown` importa um SKILL.md), `GET/PUT/DELETE /v1/skills/{id}`, `GET /v1/skills/starters`, e `skill` em `/v1/routines`.

**Ferramentas** são programas instalados num nó: yt-dlp, FFmpeg/ffprobe, whisper.cpp, Pandoc, ImageMagick, Tesseract e qpdf. Cada nó procura os que tem a cada 10 minutos e anuncia no heartbeat. A seção **Ferramentas nos nós** da página Skills mostra o que cada nó online tem e o comando para instalar o que falta. No PC, reabra o Avora depois de instalar, para o runtime enxergar o PATH novo.

- **Como o agente usa:** a ferramenta `ferramenta` só aparece quando algum nó online tem alguma coisa, e lista só o que existe. Cada ação tem argumentos fixos (ex.: `ffmpeg.cortar(entrada, inicio, fim, saida)`); o agente passa valores, nunca flags.
- **Onde roda:** na pasta principal do projeto do agente, se o nó dela tiver a ferramenta. Senão, na pasta `agents/<id>` dele: downloads (yt-dlp) primeiro no PC, porque YouTube e outros sites bloqueiam o endereço da VPS ("confirm you're not a bot"); o resto primeiro na VPS. `no` e `projeto`/`pasta` escolhem.
- **Segurança:**
  - os arquivos são caminhos relativos dentro da pasta (sem `..`, sem `:`, sem `%`, sem começar com `-`), conferidos de novo no nó, também contra links que saem da pasta;
  - uma saída nunca sobrescreve um arquivo existente;
  - não há shell: no PC o programa roda direto; na VPS roda dentro do bubblewrap (com a internet de `pasta` só para yt-dlp);
  - o nó remonta o comando a partir do próprio catálogo, então o servidor não manda argumentos soltos.
- **VPS:** yt-dlp (`yt-dlp_linux_aarch64` oficial) e FFmpeg/ffprobe (build estático BtbN `linuxarm64-gpl`) estão em `/usr/local/bin`, conferidos pelo SHA-256 publicado. Para atualizar o yt-dlp: `sudo /usr/local/bin/yt-dlp -U`. O sandbox só enxerga `/usr`, então os programas precisam estar em `/usr/bin` ou `/usr/local/bin`. O whisper.cpp precisa de `AVORA_WHISPER_MODEL` com o caminho absoluto de um modelo ggml (na VPS, dentro de `/usr/local/share`).
- **API:** `GET /v1/tools` (o catálogo e o que cada nó tem).

## Funcionários

Na primeira subida o servidor cria o time: Vega (Chefe de Gabinete), Atlas (Dev), Iris (Pesquisadora), Cora (Financeiro) e Nico (Growth). Depois disso é só editar; o seed nunca sobrescreve.

Todo funcionário pensa com o motor nativo e age por ferramentas (D17). Numa resposta, o modelo chama ferramentas em rodadas (até 10) e depois responde:

| Ferramenta | O que faz |
|---|---|
| `buscar_na_web`, `ler_pagina` | Pesquisa (DuckDuckGo; Brave Search API se `AVORA_BRAVE_API_KEY` estiver no env) e lê páginas como texto, em partes de ~6500 caracteres, com os links. Só endereços públicos: localhost, rede privada, CGNAT, `.ts.net`, `.local` e `.internal` são recusados, também depois de redirecionar. Cache de 10 min, 3 MB por página. |
| `listar_arquivos`, `ler_arquivo`, `escrever_arquivo`, `apagar_arquivo` | A pasta do agente na VPS (`AVORA_WORKSPACE/agents/<id>`, a mesma do runtime), lida direto pelo servidor por `os.Root`: nenhum caminho nem symlink sai dela. Cota de 200 MB, 1 MB por escrita. |
| `executar` | Um comando de shell num sandbox `bwrap` (bubblewrap) no nó da VPS: job `agent.run` com `engine=shell`, pasta do agente montada em `/work`, `/usr` só leitura, sem rede (`rede=true` pede aprovação e dá internet por um namespace próprio do `pasta`, sem acesso ao localhost da VPS, à rede da Oracle, ao serviço de metadados nem à tailnet; o comando roda sem capabilities), 1 GB de memória virtual, 400 MB por arquivo, até 10 min, 64 KB de saída. Nunca vai para o PC. |
| `lembrar`, `recordar`, `esquecer` | Memória do agente (tabela `agent_memories`): até 300 por agente, 1000 caracteres cada. As 40 mais recentes entram no prompt de toda conversa; as outras ele busca. O dono vê, edita e apaga no perfil. |
| `claude_code`, `codex` | Delega um trabalho pesado: vira um job `agent.run` na pasta `agents/<id>` do nó (PC primeiro, depois VPS), `read` ou `edit` (`pode_editar`), até 30 min. A resposta espera o resultado e o agente conta o que saiu. |
| `criar_rotina`, `alterar_rotina`, `apagar_rotina`, `rodar_rotina`, `ver_rotinas` | Rotinas do time, criadas e mudadas pela conversa ("toda segunda às 9, revise meus gastos"). |
| `definir_responsabilidade`, `remover_responsabilidade` | Metas permanentes de qualquer funcionário. |
| `consultar_colega` | Pergunta a um colega e usa a resposta (só em conversa direta; no `#time` se chama com @). |
| `estado_do_avora` | Nós, tarefas das últimas 24 h, rotinas e responsabilidades. |

A resposta roda no servidor, separada da requisição: fechar o app não a interrompe. A conversa fica ocupada (`job_id` = id da resposta) até ela terminar; os passos ficam no log (`/v1/events?job=<id>`) e na mensagem (`steps`), e o app mostra as ações como cartões. `POST /v1/conversations/{id}/stop` para a resposta e o job delegado. Respostas interrompidas por reinício do servidor terminam com erro na subida.

Memória pela API: `GET /v1/agents/{id}/memories?q=&limit=`, `POST` com `{"content":"…"}`, `PUT` e `DELETE /v1/agents/{id}/memories/{mid}`. Eventos `agent.memory_saved` e `agent.memory_deleted` (sem o texto).

Evals: `AVORA_EVAL=1 go test ./internal/control -run TestEval -v` manda ~30 pedidos reais ao modelo e confere a primeira ferramenta e os argumentos (nenhuma ferramenta roda). Na VPS, com a chave só lá: `GOOS=linux GOARCH=arm64 go test -c -o control.test ./internal/control`, copiar e rodar com o `/etc/avora/avora.env` carregado. `AVORA_EVAL_ONLY=a,b` filtra, `AVORA_EVAL_MIN=0.8` falha abaixo da taxa.

Modelo dos CLIs delegados: `AVORA_CLAUDE_MODEL` e `AVORA_CODEX_MODEL` em `/etc/avora/avora.env` (vazio = padrão da conta).

### Portão de políticas e aprovações (M6)

Toda chamada de ferramenta passa pelo portão antes de rodar. As regras ficam no código (`internal/control/policy.go`), sem nada pra configurar:

| Ação | Regra |
|---|---|
| Ler estado e rotinas, consultar colega, delegar só leitura, criar ou alterar rotinas e responsabilidades | livre: faz e mostra o cartão |
| `claude_code.edit`, `codex.edit` (delegar com `pode_editar`) | 🔒 pede aprovação |
| `apagar_rotina`, `remover_responsabilidade`, `apagar_arquivo`, `executar` com `rede` | 🔒 pede aprovação |
| Pesquisar, ler páginas, ler e escrever na própria pasta, `executar` sem rede, memória | livre |

Uma ação 🔒 cria uma aprovação com o que vai acontecer em palavras ("Claude Code vai criar e editar arquivos na pasta agents/atlas: …") e a resposta do agente espera. No app aparece um cartão acima da caixa de mensagem com **Permitir**, **Sempre permitir** e **Negar**, e o funcionário mostra "precisa de você" na lista do time. **Sempre permitir** vira uma permissão daquele funcionário para aquela ação, listada no perfil dele em "Sempre permitido" e revogável. Negada ou sem resposta, a ação não acontece e o modelo lê o motivo pra te contar.

- Tempo de espera: `AVORA_APPROVAL_MINUTES` (padrão 15, de 1 a 60). Expirada, vira `expired`.
- Parar a resposta ou reiniciar o servidor expira as aprovações pendentes dela.
- API: `GET /v1/approvals?status=pending|approved|denied|expired&limit=`, `POST /v1/approvals/{id}` com `{"decision":"once"|"always"|"deny"}` (409 se já decidida), `GET /v1/grants?agent=`, `DELETE /v1/grants/{id}`.
- Eventos: `approval.requested`, `approval.decided`, `grant.created`, `grant.deleted`.

### Canal #time

Uma conversa só (id `time`) com você e o time inteiro. Sem @, a Vega responde e distribui; `@Nome` chama quem você quiser e `@todos` chama todo mundo. Um funcionário que escreve `@Colega` na resposta chama esse colega, que lê as últimas 24 mensagens do canal. Cada funcionário responde uma chamada por vez; a chamada que chega enquanto ele trabalha roda em seguida.

Trava contra loop: depois de 8 respostas seguidas do time sem você escrever, ninguém mais é chamado e o canal mostra o aviso até a sua próxima mensagem. No canal os funcionários usam as mesmas ferramentas (menos `consultar_colega`).

### Responsabilidades e rotinas

Cada funcionário tem até 10 responsabilidades (objetivo, métrica, cadência), versionadas junto com ele e incluídas no prompt em toda conversa. `PUT /v1/agents/{id}` sem o campo `responsibilities` mantém as atuais.

Rotina é um prompt que um funcionário roda sozinho: num horário (`at`, com `days` de 0 = domingo a 6; vazio = todo dia), uma vez só (`at` + `date` AAAA-MM-DD: um lembrete, que desliga sozinho depois de rodar) ou a cada N minutos (`every`, mínimo 15). O horário é o de `AVORA_TIMEZONE` (padrão `America/Sao_Paulo`). O resultado vai para o `#time` ou para uma conversa própria da rotina com o funcionário. Cada execução recebe um retrato do estado: nós, tarefas das últimas 24 h, rotinas e responsabilidades do time. Uma execução perdida com o servidor fora roda uma vez só; uma que ainda está rodando pula o horário seguinte. Depois de 10 falhas seguidas a rotina desliga e avisa no `#time`; religar zera a contagem.

Rotinas iniciais da Vega no `#time`: briefing da manhã (dias úteis, 08:00) e revisão semanal (segunda, 09:00).

## Claude Code e Codex pela assinatura (ferramentas dos agentes)

O Avora não usa API key da Anthropic nem lê o login: o runtime abre o `claude` oficial com uma pasta de login só dele (`CLAUDE_CONFIG_DIR`) e remove `ANTHROPIC_API_KEY` do ambiente, então nunca cobra API por engano.

Login no PC (uma vez; escolha no navegador a conta que o Avora vai usar, de preferência numa janela anônima):

```powershell
$env:CLAUDE_CONFIG_DIR = "$env:APPDATA\Avora\claude"; & "$env:USERPROFILE\.local\bin\claude.exe" auth login
```

Pedir uma tarefa:

```bash
curl -X POST -H "Authorization: Bearer $AVORA_ADMIN_TOKEN" http://127.0.0.1:3190/v1/jobs \
  -d '{"target":"windows","action":"agent.run","agent":{"engine":"claude","prompt":"...","mode":"read","workdir":"pasta","max_minutes":20}}'
```

- `mode`: `read` (ler arquivos e a web) ou `edit` (também editar arquivos). Nenhum modo tem shell ainda; pela conversa, `edit` passa pelo portão de políticas (M6).
- `workdir`: caminho relativo dentro do workspace do nó (`%LOCALAPPDATA%\Avora\work` no PC, `AVORA_WORKSPACE` para trocar). Não sai dele.
- Cada ferramenta usada vira evento `agent.step` (ferramenta + arquivo/URL/busca); o fim registra turnos, tokens, duração e o custo equivalente.
- Cancelar (`POST /v1/jobs/{id}/cancel`) mata o processo em até 5 s. Uma tarefa de agente nunca é refeita sozinha: se o nó some, ela vira `timed_out`.
- Limite simultâneo: 2 no PC, 3 na VPS (`AVORA_MAX_AGENTS`).
- Variáveis do runtime: `AVORA_CLAUDE_BIN` (padrão: `claude` no PATH ou `~/.local/bin`), `AVORA_CLAUDE_CONFIG_DIR`, `AVORA_WORKSPACE`, `AVORA_MAX_AGENTS`.
- Quota das assinaturas (`GET /v1/quotas`, mostrada na sidebar do app): o runtime lê só pelos CLIs oficiais, nunca pelos logins. Codex: `account/rateLimits/read` do `codex app-server` a cada 5 min (grátis). Claude: o `rate_limit_event` de cada execução; sem execução há 60 min, um turno mínimo com Haiku atualiza (`AVORA_CLAUDE_QUOTA_PROBE_MIN`, 0 desliga). Fica só em memória e volta no próximo heartbeat.
- `engine`: `claude` ou `codex`; `model` opcional (`haiku`, `sonnet`, `opus`; `gpt-6-luna`, `gpt-6.1-sol`...). Sem `model`, vale o padrão da conta. Para testes, use `haiku` e `gpt-6-luna`.
- Codex executa tudo como comandos no sandbox dele: `read` = `read-only`, `edit` = `workspace-write` (escreve só no workdir, sem rede). Login próprio em `CODEX_HOME` (`%APPDATA%\Avora\codex` no PC); `OPENAI_API_KEY` é removida do ambiente.
- No PC, o Codex é o build do winget (`OpenAI.Codex`); o shim do npm não é executável direto.

Na VPS, `install.sh` instala os dois CLIs oficiais no usuário `avora` (`/var/lib/avora/.local/bin`, Codex via npm) e o runtime tem 3 vagas e 2 GB. Login, uma vez, do PC:

```bash
./scripts/vps-login.sh claude   # link + colar o código
./scripts/vps-login.sh codex    # código de dispositivo (ligar "device code login" na segurança do ChatGPT)
./scripts/vps-login.sh status
```

Testar da VPS: `sudo /usr/local/bin/avora-agent -n vps -e claude -m haiku "tarefa"` (`-n windows` manda pro PC; `-M edit`, `-w pasta`, `-t minutos`).

## Modelos (Configurações)

Configurações → Modelos mostra o modelo principal, os provedores e as
assinaturas do Claude e do Codex. Atividade e Nós também moram em
Configurações.

- **Provedores**: NVIDIA, qualquer API compatível com OpenAI (OpenRouter,
  Groq, Together, DeepSeek...) e **Local (llama.cpp)**. Ao conectar, o Avora
  lista os modelos (`GET /models`) e marca o provedor como ok ou com erro. A
  chave é selada no cofre (`provider:<id>`) e nunca volta para o app.
- **Local**: rode no computador
  `llama-server -m modelo.gguf --host 0.0.0.0 --port 8080 --jinja` e use o
  endereço Tailscale dele (`http://100.x.y.z:8080/v1`). http só é aceito para
  o próprio servidor e para a Tailscale (100.64.0.0/10 ou `*.ts.net`); chave
  opcional. O `--jinja` liga as ferramentas, sem ele o modelo só conversa.
- **Modelo principal**: vale na hora para todos os funcionários, sem
  reiniciar, e volta depois de um reinício (tabela `settings`). Remover o
  provedor do modelo principal volta ao "NVIDIA do servidor" (o do
  `/etc/avora/avora.env`), ou a nenhum.
- **Assinaturas**: além da conta com que cada nó foi configurado (a
  "principal"), dá para adicionar outras contas do Claude e do Codex. "Outra
  conta do Claude/Codex" escolhe o nó (a VPS por padrão) e abre o login do
  próprio CLI lá, na pasta `accounts/<id>` ao lado da principal
  (`/var/lib/avora/accounts/claude.1a2b3c` na VPS). O app mostra o link e o
  código: no Codex você digita o código na página; no Claude você cola no app
  o código que a página mostra. Os tokens ficam na pasta, escritos pelo CLI;
  o Avora só repassa link e código, e eles não entram na auditoria.
- Os agentes usam a principal; "conta" no `claude_code`/`codex` escolhe
  outra pelo nome ou e-mail, e quando a principal está no limite a delegação
  vai sozinha para outra conta online. Remover a conta tira do Avora, a pasta
  fica no nó.
- API: `GET /v1/models`, `POST /v1/models/providers`,
  `PUT|DELETE /v1/models/providers/{id}`, `POST .../{id}/refresh`,
  `PUT /v1/models/main`.

## Plugins (Fase B)

O dono conecta cada app uma vez, na página **Plugins** do app desktop; os funcionários passam a ter as ferramentas daquele app.

### Apps com um clique (MCP)

46 apps com servidor MCP oficial (`internal/connect/catalog.go`): GitHub (token pessoal, `api.githubcopilot.com/mcp/`), ClickUp, Notion, Asana, monday.com, Linear, Jira e Confluence, Todoist, Airtable, Miro, Canva, Gamma, Webflow, Wix, WordPress.com, Cal.com, Granola, Fireflies, tl;dv, Brevo, Semrush, Ahrefs, Attio, Intercom, Jotform, Mercado Pago, Stripe, PayPal, Square, Dropbox, Mem, Raindrop.io, Exa, Hugging Face, Zapier, Make, Apify, Hostinger, Vercel, Netlify, Supabase, GitLab, Sentry, PostHog, ClickHouse Cloud e Prisma. Qualquer outro servidor MCP entra pelo endereço, no fim da página.

- **Como conecta** (`internal/mcp`): o servidor pergunta ao app como logar (`/.well-known/oauth-protected-resource` e `oauth-authorization-server`), cadastra o Avora como cliente (registro dinâmico, `client_name` Avora, retorno `http://127.0.0.1:<porta>/callback`), devolve o link de consentimento com PKCE S256 e `resource`, o app desktop abre o navegador e pega o código na porta local, e o servidor troca o código pelo token. O token e o `refresh_token` ficam no cofre; a renovação é automática e, se o app recusar, a conexão vira `reconnect`.
- **Transporte:** MCP Streamable HTTP (JSON ou SSE, com `Mcp-Session-Id`, reabrindo a sessão quando o servidor esquece). O servidor legado só com SSE não é suportado.
- **Rede:** as chamadas MCP saem por um cliente que recusa loopback, faixas privadas, link-local e o CGNAT do Tailscale, para que os metadados de um app não apontem o Avora para serviços da própria VPS. Só https.
- **Ferramentas:** lidas com `tools/list` ao conectar e em Testar, guardadas em `connections.meta` (migração 013) e restauradas no boot. O nome vira `<app>_<ferramenta>` (até 64 caracteres). Apps com mais de 20 ferramentas aparecem para o modelo como `<app>_ferramentas` (lista e argumentos) e `<app>_usar` (executa), com o risco da ferramenta de dentro.
- **Risco de cada ferramenta:** `readOnlyHint` vale leitura e `destructiveHint` vale apagar. Sem dica, decide o primeiro verbo do nome: ler/buscar/listar é livre; criar/atualizar é livre; enviar, comentar, compartilhar, publicar ou convidar é 🔒; apagar, remover ou arquivar é 🔒; pagar, reembolsar, cobrar ou transferir é 🔒 (`gastar`). O cartão de aprovação mostra o app, a ferramenta e os argumentos.
- **Logos:** `desktop/qml/logos.js` traz os logos de 34 apps do [Simple Icons](https://simpleicons.org) 16.36.0 (CC0), na cor da marca (clareada quando a oficial sumiria no fundo escuro), desenhados pelo `Icon.qml`. Quem não está lá (monday.com, Canva, Gamma, Granola, Fireflies, tl;dv, Ahrefs, Attio, Jotform, Mem, Raindrop.io, Exa, Apify) e o Cal.com, cujo logo é só o nome, mostram a inicial na cor da marca. Os conectados aparecem como os do catálogo; a engrenagem abre os detalhes (conta, Testar, Desconectar, quem usa e ferramentas).
- **Fora do catálogo:** Figma (recusa clientes desconhecidos) e Calendly (não aceita retorno local). Exa e Hugging Face funcionam sem login.

### Ponte: Claude Code e Codex usam os Plugins

Quando um funcionário delega uma tarefa ao Claude Code ou ao Codex e tem Plugins liberados para ele, a execução recebe o Avora como servidor MCP (`avora`, em `POST /v1/bridge/mcp`, `internal/control/bridge.go`). As ferramentas são as mesmas do funcionário (mesmas contas, `conta`, "Quem usa"), e cada chamada volta ao servidor e passa pela mesma aprovação: enviar, comentar, apagar ou pagar espera o dono no app, com a chamada aberta até 30 min.

- **Chave da execução:** `avb_…`, criada quando a tarefa sai e apagada quando ela termina; o servidor guarda só o hash. Não abre nenhuma outra rota (o token de admin também não abre a ponte).
- **No nó:** o Claude Code recebe a chave num arquivo `mcp.json` temporário (0600) com `--mcp-config --strict-mcp-config` e `mcp__avora` liberado; o Codex recebe `-c mcp_servers.avora.*` e a chave em `AVORA_BRIDGE_KEY`, com `default_tools_approval_mode="approve"` (quem aprova é o Avora). A chave nunca vai na linha de comando.
- **Credenciais:** continuam no cofre do servidor; o CLI só vê o resultado de cada chamada.
- **Testes de ponta a ponta** com os binários reais: `AVORA_E2E_CLAUDE=<claude> CLAUDE_CONFIG_DIR=<login do Avora>` e `AVORA_E2E_CODEX=<codex> CODEX_HOME=<login do Avora>` em `go test ./internal/control -run BridgeReal`.

### Com chave, token ou login próprio

| Plugin | Como conecta | Ferramentas |
|---|---|---|
| Google (Gmail, Drive, Agenda) | login no navegador (OAuth com PKCE) | `gmail_buscar`, `gmail_ler`, `gmail_rascunho`, `gmail_enviar` 🔒, `drive_buscar`, `drive_ler`, `drive_criar`, `agenda_eventos`, `agenda_criar` (🔒 com convidados) |
| ClickUp (token), plano B do MCP | token pessoal `pk_` | `clickup_espacos`, `clickup_tarefas`, `clickup_tarefa`, `clickup_criar_tarefa`, `clickup_atualizar_tarefa`, `clickup_comentar` 🔒 |
| ElevenLabs | chave de API | `elevenlabs_vozes`, `elevenlabs_falar` (MP3 em `audio/` na pasta do agente), `elevenlabs_uso` |
| Discord | token de um bot próprio | `discord_canais`, `discord_ler`, `discord_enviar` 🔒 |
| Spotify | login no navegador (app próprio, PKCE) | `spotify_tocando`, `spotify_buscar`, `spotify_controlar` (tocar/pausar/pular/volume, pede Premium), `spotify_fila`, `spotify_playlists`, `spotify_playlist_adicionar`, `spotify_historico` |
| LinkedIn | login no navegador (app próprio, porta fixa 47821) | `linkedin_perfil`, `linkedin_publicar` 🔒, `linkedin_apagar_post` 🔒 |
| iFood | login no navegador (agente próprio no portal do iFood, PKCE, porta fixa 47822) | MCPs oficiais (usuário, descoberta, sacola, pedidos): `ifood_buscar`, `ifood_explorar`, `ifood_loja`, `ifood_item`, `ifood_recomendacoes`, `ifood_enderecos`, `ifood_criar_sacola`, `ifood_adicionar_item`, `ifood_cupons`, `ifood_escolher_entrega`, `ifood_escolher_pagamento`, `ifood_fazer_pedido` 🔒 (gasta), `ifood_acompanhar`, `ifood_pedido` e mais |
| NFS-e (MEI) | certificado e-CNPJ A1 (.pfx) + senha, município IBGE e código do serviço, pelo formulário do app | `nfse_emitir` 🔒, `nfse_notas`, `nfse_consultar`, `nfse_pdf` (salva na pasta do agente), `nfse_cancelar` 🔒 |

- **Risco:** ler, buscar e criar no espaço do dono (rascunho, tarefa, documento, compromisso sem convidados, áudio) é livre; enviar, comentar, postar, convidar ou apagar passa pelo portão de aprovações, com o texto completo no cartão. "Sempre permitir" vale por ferramenta e por funcionário.
- **Cofre:** credenciais lacradas com AES-256-GCM (`internal/vault`), chave em `/etc/avora/vault.key` (gerada pelo `install.sh`, root:avora 640, fora do banco). O banco guarda só o lacre (tabela `connections`). O token abre só dentro da chamada: nunca vai pro modelo, pros eventos (`connector.called` grava ferramenta, agente, ok e duração, sem conteúdo) nem pras respostas da API. Perder a chave = reconectar tudo.
- **Várias contas:** cada app aceita mais de uma conta (Gmail pessoal e do trabalho, dois ClickUps). Em Plugins, a engrenagem do card → **Adicionar outra conta**; o Google mostra o seletor de contas. Cada conta tem o próprio card, status, Testar, Desconectar e "Quem usa". Quando um funcionário tem mais de uma conta de um app, todas as ferramentas desse app ganham o argumento `conta` (lista das contas); sem ele vale a principal, a primeira conectada. Logar de novo na mesma conta só atualiza a conexão; nos apps MCP o servidor não diz quem é a pessoa, então cada login por "Adicionar outra conta" vira uma conta nova.
- **Renovação:** o token do Google se renova sozinho. Se o app recusar (401, `invalid_grant`), a conexão vira `reconnect`, as ferramentas somem dos agentes e o `#time` recebe um aviso.
- **Quem usa:** por padrão todos os funcionários; a página deixa restringir.

### Ligar o Google (uma vez)

1. No [Google Cloud](https://console.cloud.google.com/), crie um projeto e ative as APIs **Gmail**, **Google Drive** e **Google Calendar**.
2. Em *Google Auth Platform*: público **Externo**, adicione o seu e-mail como usuário de teste e **publique o app** (status *Em produção*). Em *Teste*, o login expira a cada 7 dias. Não precisa verificar: como só você usa, o Google mostra o aviso "app não verificado" e você clica em Avançado → Acessar.
3. Em *Clientes*, crie um cliente OAuth do tipo **App para computador**.
4. Grave o id e o segredo do cliente na VPS, sem aparecer na tela:
   ```bash
   ./scripts/set-secret.sh AVORA_GOOGLE_CLIENT_ID
   ./scripts/set-secret.sh AVORA_GOOGLE_CLIENT_SECRET
   ```
5. No app: Plugins → **Conectar com Google**. O navegador abre, você aceita os escopos (`gmail.modify`, `drive.readonly`, `drive.file`, `calendar.events`) e o app pega o código numa porta local `127.0.0.1` e entrega ao servidor.

### Pluggy (bancos, só leitura)

1. Conecte os bancos em meu.pluggy.ai.
2. No dashboard.pluggy.ai: Applications → sua aplicação → Demo → Connect
   Account → **MeuPluggy**. Em Connected Items, copie o itemId de cada banco
   (a Pluggy não deixa listar os itens do Meu Pluggy).
3. Em Plugins → Pluggy, preencha Client ID, Client Secret e os item IDs
   (separados por vírgula). O Avora troca o par por uma chave de 2 h a cada
   uso; tudo fica selado no cofre.

Ferramentas: `pluggy_contas`, `pluggy_transacoes` (de/ate/busca/conta),
`pluggy_faturas`, `pluggy_investimentos`. Nada move dinheiro. A autorização do
Meu Pluggy vale 1 ano; depois os dados voltam vazios e é preciso refazer o
passo 2 (o itemId pode mudar).

### Spotify (uma vez)

1. Em [developer.spotify.com/dashboard](https://developer.spotify.com/dashboard), **Create app**: marque **Web API** e cadastre o redirect `http://127.0.0.1/callback` (sem porta: o Spotify aceita qualquer porta no loopback; `localhost` não vale).
2. Grave o Client ID (não tem segredo, é PKCE): `./scripts/set-secret.sh AVORA_SPOTIFY_CLIENT_ID`.
3. Plugins → Spotify → Conectar. Em modo de desenvolvimento o app só aceita as contas cadastradas em *User Management* (a sua já entra como dona).

### LinkedIn (uma vez)

O LinkedIn só abre para apps o perfil e a publicação: não dá para ler o feed, buscar nem ver mensagens pela API.

1. Em [linkedin.com/developers/apps](https://www.linkedin.com/developers/apps), **Create app** (pede uma Página da empresa; pode ser uma página sua).
2. Em *Products*, adicione **Sign In with LinkedIn using OpenID Connect** e **Share on LinkedIn**.
3. Em *Auth*, cadastre o redirect exato `http://127.0.0.1:47821/callback`.
4. Grave as chaves: `./scripts/set-secret.sh AVORA_LINKEDIN_CLIENT_ID` e `./scripts/set-secret.sh AVORA_LINKEDIN_CLIENT_SECRET`.
5. Plugins → LinkedIn → Conectar. O token vale 60 dias e não renova sozinho (apps comuns não ganham refresh token); depois disso a conexão vira `reconnect` e é só conectar de novo. Os posts vão pela Posts API (`/rest/posts`, `LinkedIn-Version` de dois meses atrás); `#palavras` viram hashtags.

### iFood (uma vez)

Pelos MCPs oficiais do iFood (cliente, não loja). Os esquemas reais das ferramentas vêm do `tools/list` dos servidores e ficam em cache por 6 h.

1. Crie a conta em [mcp-developer.ifood.com.br](https://mcp-developer.ifood.com.br) e um **agente** com o callback exato `http://127.0.0.1:47822/callback` e os escopos de usuário, descoberta, sacola (ler e escrever) e pedidos.
2. Grave as chaves: `./scripts/set-secret.sh AVORA_IFOOD_CLIENT_ID` e, se o agente tiver segredo, `./scripts/set-secret.sh AVORA_IFOOD_CLIENT_SECRET`.
3. Perfil **Pessoal** = só sandbox (padrão). Pedido de verdade pede o perfil **Profissional** (validação de 24–48 h) e a homologação do iFood (1–2 semanas); depois `./scripts/set-secret.sh AVORA_IFOOD_ENV` com `producao`.
4. Plugins → iFood → Conectar. `ifood_fazer_pedido` sempre espera a sua aprovação (e no turno da noite nunca roda).

### NFS-e do MEI (uma vez)

Emissão direta no Sistema Nacional da NFS-e (obrigatório pro MEI desde 1º/9/2023), sem intermediário: o servidor monta a DPS, assina com o e-CNPJ A1 (XMLDSig RSA-SHA256) e envia por TLS mútuo. Leiaute e regras seguem o [emissor-nfse-nacional](https://github.com/gabrielbuzziv/emissor-nfse-nacional) (MIT).

1. Plugins → NFS-e (MEI): escolha o arquivo `.pfx`, digite a senha, o código IBGE de 7 dígitos do município do CNPJ e o código de tributação nacional do serviço (6 dígitos). Inscrição municipal só se a prefeitura exigir. O certificado e a senha vão direto pro cofre.
2. Ao conectar, o servidor abre o certificado, lê o CNPJ e faz uma consulta ao governo para ver se ele aceita o certificado.
3. Padrão: **produção restrita** (as notas não valem). Para notas reais: `./scripts/set-secret.sh AVORA_NFSE_ENV` com `producao`. A numeração da DPS (série 1) é separada por ambiente e fica no cofre; se um número já tiver nota (E0014), o servidor confere se é a mesma nota (valor e tomador) e, se não for, passa para o próximo.
4. `nfse_emitir` e `nfse_cancelar` sempre esperam a sua aprovação. MEI: `opSimpNac` 2, ISS não retido, `indTotTrib` 0.

## Comportamento

- Presença: heartbeat a cada 5 s; offline depois de 30 s sem sinal.
- Roteamento automático: PC Windows online primeiro, depois VPS.
- Jobs: lease de 2 min; se o nó some, o job volta pra fila (até 3 tentativas) e depois vira `timed_out`.
- Toda mudança grava um evento na mesma transação; o banco recusa editar ou apagar eventos.
- Sem `AVORA_DATABASE_URL`, o servidor roda com estado em memória (só pra desenvolvimento).

## Limitações atuais

- O sandbox do `executar` só existe na VPS; delegações ao PC usam a pasta do PC, que é outra. O portão de políticas v1 tem regras fixas no código; regras próprias por funcionário ficam para depois.
- Sem idempotência ou app; a API de admin usa um token único.
- Presença é heartbeat, não prova de capacidade.
- Plugins: Instagram ainda não existe; servidores MCP só com o transporte SSE antigo não funcionam; Gmail e ClickUp não têm gatilho por evento (sem porta pública), os agentes consultam quando pedem ou numa rotina.

Veja [spec](spec.md), [decisões](decisions.md), [arquitetura](architecture.md) e [roadmap](roadmap.md).
