# Plano: o Avora vira um ecossistema de agentes, multiplayer e singleplayer

Escrito em 2026-11-17. Ele **muda o foco** do [plano anterior](plano-agentes-chat.md), que deixava de fora "várias pessoas, cargos e convites". Agora isso é o centro do app. As fases que sobraram do plano anterior (2 a 9) continuam valendo e entram aqui onde se encaixam.

## O que foi lido

**agentes-chat** (commit `817a1ee`). Lidas as seções "Decision" dos ADRs que tratam de:
- pessoas e espaços: 0006, 0085, 0159, 0183, 0154;
- convites: 0068, 0090, 0184;
- computadores: 0021, 0025, 0030, 0077, 0115, 0096;
- navegador e tela do agente: 0040, 0058, 0065, 0110;
- arquivos e wiki: 0019, 0023, 0111;
- modelos próprios: 0146;
- conversas, presença e início: 0080, 0084, 0108.

Também li as telas `ChannelPage.tsx` e `components/browser/BrowserCard.tsx`.

**OpenBot** (`nightly-labs/openbot`, commit `6a99e5a`):
- `README`;
- `docs/architecture/*`: browser, side-panel, channels-and-messaging, servers, team-api;
- `docs/multi-tenant-hosting.md` e `docs/messaging.md`.

## Como os dois resolvem o que você pediu

| Assunto | agentes-chat | OpenBot | O que o Avora vai fazer |
|---|---|---|---|
| **Quem usa** | Contas de pessoas (e-mail e senha) num servidor. Espaços (`organizations`) com membros e cargos que se somam (0085). Agentes não são usuários. | O app de desktop **é** um servidor. Outras pessoas "entram no servidor" dele e veem uma barra de servidores à esquerda. | Contas de pessoas no servidor do Avora. **Espaços** são os "servidores" do Discord: alguém cria, convida e os outros entram. Uma barra de espaços fica à esquerda, como no Discord, e pode misturar espaços de servidores diferentes. |
| **Convite** | Um link de 7 dias, guardado só como hash. Quem usa o link pede para entrar e um admin aprova (0068, 0090). O convite diz o cargo que a pessoa ganha (0084). | Convite com aceite e *epoch* (plano 005). | Link com cargo. Quem entra pelo link fica esperando um admin aprovar. O dono pode desligar a aprovação. |
| **Sozinho** | Modo pessoal (0159): o mesmo espaço, mas sem o `#geral` e sem a lista de pessoas enquanto só tem você. | Local-first: funciona sem conta. | Modo pessoal: o primeiro espaço é seu, sem nada de equipe à vista, até chegar a segunda pessoa. Quem só quer um agente com os seus conectores usa assim. |
| **Conversas** | Canais com categorias (0080), DMs, threads, e uma "cadeia" de quem responde quando ninguém foi citado (0074). | Canal com um **líder** e membros. O roteamento não usa modelo quando a resposta é óbvia (citado, reply, tarefa aberta) e usa o modelo do líder quando não é. | Canais com categorias, DMs com agentes e com pessoas, e threads. **Painel de membros à direita**: pessoas e agentes, com presença e o que cada um está fazendo. Quem responde segue a ordem do OpenBot: citado, depois reply, depois a tarefa aberta, e só então o líder decide. |
| **Presença** | Pessoas escolhem como aparecem (online, ausente, ocupado, invisível). A presença dos agentes vem do trabalho, e o agente escreve a própria linha com `set_status` (0084). | Atividade do computador. | Igual ao agentes-chat. A linha do agente aparece no painel de membros ("Revisando o PR #12"). |
| **Painel lateral** | `SidePanel` por cima da conversa: thread, busca, agente. | Painel à direita **por conversa**, com abas: arquivos, threads, configurações e navegador (`SIDE_PANEL_PANES`). | Um painel lateral por conversa, com abas **Membros · Docs · Navegador · Tarefas**. Ele substitui a gaveta global de Docs e o navegador global, e acaba com os dois botões do topo. |
| **Navegador** | Cada agente tem o próprio navegador, no computador dele (`agent-browser` + Chrome). No chat aparece um card ao vivo enquanto ele navega. Dá para assistir, assumir e devolver, num diálogo só por agente (0040, 0065). Perfis de login com nome, liberados por agente (0110). | Abas do navegador **pertencem a um agente**. Um membro assume e devolve uma aba, e enquanto isso as ações do agente esperam. Tem o modo "ensinar", que grava os passos de uma tarefa. | **Um navegador por agente**, com perfil e sessão próprios. Card ao vivo no chat e aba "Navegador" no painel lateral, com "assumir" e "devolver". Perfis de login com nome, liberados por agente. O navegador global sai. |
| **Computadores** | Todo computador é "conectado", com o runner por WebSocket (0077). Cada agente tem um ambiente isolado: git, chaves e pasta próprios (0030). Uma pasta compartilhada por computador (0115). Cada agente só alcança as próprias portas (0096). | Pasta de trabalho por agente. Hospedagem de vários inquilinos num Mac, com um usuário do sistema para cada. | O nó do Avora vira o **computador do espaço**. Cada agente ganha uma **casa**: computador e pasta, que você escolhe (na VPS, no PC ou num servidor). Ganha também identidade git própria e uma pasta compartilhada por computador. |
| **Modelos locais** | Um endpoint próprio é uma credencial do espaço: Ollama ou qualquer API compatível, com os modelos testados (0146). | Detecta Ollama e LM Studio na máquina. | Um provedor local **pertence a um computador**. A chamada passa pelo runner daquele computador, então o agente de outra pessoa usa o Ollama do seu PC sem o servidor precisar alcançá-lo. Quem pode usar é decidido pelo dono da conta (contas de IA são de pessoas, ADR 0059). |
| **Docs** | Wiki: páginas com pastas, `[[links]]`, versões, backlinks e trava. Drive: arquivos com lixeira, e quem vê um arquivo segue a conversa de onde ele veio (0023, 0019, 0111). | Biblioteca: arquivos por servidor, agente e conversa. | Docs vira **Páginas + Arquivos** num lugar só. No chat, os docs abrem na aba Docs do painel da conversa, nunca numa gaveta à parte. |
| **Código** | Uma worktree por missão (0044). Git por agente, com branch `agentes/<agente>/*`, e o merge só com aprovação (0024, 0030). Clones com cache (0116). | Codex App Server e Claude Code com uma pasta por agente. | Uma worktree por tarefa de código, na casa do agente. Branch do agente, PR pelo conector do GitHub com aprovação, testes rodados antes de dizer "pronto", e um terminal ao vivo. |
| **Conectores** | Do espaço. Várias contas por conector (0099). Pessoais ou do time (o Cofre, 0144). Liberados por agente. | Marketplace de apps e skills. | Conectores do espaço, com várias contas cada. A conta é pessoal ou do time, e cada agente recebe só as que foram liberadas para ele. |

## Fases

Cada fase vira uma entrada nova em [decisions.md](decisions.md) quando for feita. A ordem: primeiro o que você sente no uso, depois a fundação multiplayer, porque ela mexe em tudo.

### Fase A: Docs sem duplicar e o topo limpo (agora)
- Saem os botões **Docs** e **Navegador** do topo da janela.
- Um doc aberto pelo chat aparece ao lado da conversa. Na página Docs ele abre dentro da própria página, e entrar nela fecha o painel lateral. Nunca aparecem dois na tela.
- A página Docs ganha:
  - uma coluna de pastas e filtros (Fixados, Recentes, Meus, Dos agentes, Arquivos);
  - uma lista com autor e data;
  - o doc aberto à direita, na própria página.

### Fase B: painel lateral da conversa
- Abas: **Membros** (você, os agentes do canal e, depois, as outras pessoas), **Docs** (os da conversa e os citados), **Navegador** (o do agente da conversa) e **Tarefas**.
- Ele se lembra da aba e da largura em cada conversa.

### Fase C: um navegador por agente
- No app de desktop: um perfil do QtWebEngine por agente (`storageName` = id do agente), com cookies e logins separados.
- No servidor: os comandos `browser.*` levam o id do agente, e a central de comandos vira um mapa por agente.
- No chat: um card ao vivo na mensagem enquanto o agente navega, e "assumir" ou "devolver" (enquanto você controla, os comandos dele esperam).
- Perfis de login com nome, liberados por agente (0110).
- Depois: o navegador headless no computador do agente, para quando o app de desktop está fechado.

### Fase D: pessoas, espaços, cargos e convites (a fundação)
**Banco:**
- `people`: e-mail, nome, foto e hash da senha (argon2id);
- `sessions`;
- `workspaces`: nome, ícone, modo (pessoal ou time), contexto;
- `workspace_members`: dono ou membro, e disponibilidade;
- `roles` e `member_roles`, com as chaves das áreas do 0085, simplificadas;
- `invites`: hash do link, cargo, validade e se pede aprovação.

**Escopo:**
- Recebem `workspace_id`: agentes, canais, projetos, docs, conectores, contas de IA, rotinas e nós.
- A migração põe tudo que já existe no espaço "Pessoal" da primeira conta.

**Acesso:**
- O token de admin continua só para o operador e para parear nós.
- O app entra com e-mail e senha e guarda a sessão de cada servidor.
- Cada rota checa `can(membro, chave)`.

**Ao vivo:** os eventos são filtrados por espaço e por canal (0014: sair do espaço corta o WebSocket).

### Fase E: o Avora como Discord
- Uma barra de espaços à esquerda, com o ícone de cada espaço e "+" para criar ou entrar.
- Os espaços podem ser de servidores diferentes (0154), e o app guarda várias contas.
- Canais com categorias, DMs entre pessoas, e menções a pessoas e a `@todos`.
- Presença e status (0084), e a linha que o agente escreve com `set_status`.
- Quem responde: citado, depois reply, depois a tarefa aberta, e só então o líder do canal decide (OpenBot).

### Fase F: computadores e a casa de cada agente
- Cada nó pertence a um espaço, e quem pode parear é decidido pelo cargo.
- O agente tem `home_node_id` e `home_dir`. Projetos ligam um repositório a uma pasta em um computador.
- Isolamento leve (0030, nível 1): git, chaves e `/tmp` por agente. Pasta compartilhada por computador (0115).
- Provedores locais presos a um computador, com as chamadas passando pelo runner. A conta de IA é de uma pessoa, e ela escolhe com quem divide.

### Fase G: Docs = Páginas + Arquivos
- `[[links]]`, backlinks, versões com restaurar, pastas, trava (só admins editam).
- Quem vê um arquivo segue a conversa de onde ele veio (0111).
- Os agentes leem por `search_docs` e `read_doc`, e só escrevem quando alguém pede.

### Fase H: código de verdade
- Uma worktree por tarefa na casa do agente, e a branch `avora/<agente>/<tarefa>`.
- Commits com o nome do agente e um `Co-authored-by` de quem pediu.
- Testes rodados antes de dizer "pronto". PR pelo conector do GitHub, e o merge só com aprovação.
- Um terminal ao vivo da tarefa na aba Tarefas do painel.

### Fase I: o que sobrou do plano anterior
Fases 2 a 9 do [plano anterior](plano-agentes-chat.md), agora por espaço:
- cérebro completo;
- configurações;
- memória;
- criar agente;
- trabalho visível;
- conversa rica;
- time que se organiza;
- painéis.

## Princípios que valem em toda fase
- **Um lugar para cada coisa.** Nada abre em dois lugares ao mesmo tempo.
- **Tudo funciona sozinho ou em grupo.** O modo pessoal só esconde coisas, nunca muda as regras.
- **Agente não é usuário.** Ele age só pelas ferramentas do Avora, com as permissões que recebeu, nunca com o cargo de quem pediu.
- **Nenhum valor fixo no código.** Endereço, porta, modelo e pasta vêm da configuração ou da tela.
