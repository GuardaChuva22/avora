# Fluxo das telas: a barra lateral é o app

Escrito em 2026-11-17, a partir do código do agentes-chat (`apps/web/src/routes/org.$orgId.tsx`, `components/Sidebar.tsx`, `SidebarSetup.tsx`, `packages/client/src/lib/sidebar-items.ts`, `pages/DmPage.tsx`, `components/agents/AgentSidebar.tsx`, `ChannelHeader.tsx`, `SidePanel.tsx`).

## O problema do Avora hoje
O chat é **uma página entre outras**. A barra da esquerda tem Início, Chat, Plugins, Skills, Docs, Objetivos e Configurações. Ao entrar em Chat, aparece uma **segunda lista** no meio (Time, canais, agentes). À direita fica uma **terceira coluna**, com as conversas daquele agente. São três navegações uma dentro da outra, e as ferramentas (docs, rotinas, aprovações) ficam longe da conversa.

## Como o agentes-chat faz
Uma barra lateral só, e o resto da janela é o trabalho.

**Barra lateral, de cima para baixo:**
1. **Cartão do espaço:** nome e logo. O menu dele troca de espaço, convida pessoas e abre as configurações do espaço. Ao lado fica **"Ir para… Ctrl+K"**.
2. **Ferramentas**, uma linha cada, com contador:
   - **Esperando você** e **Aprovações** só aparecem quando há algo.
   - Num espaço de time, Threads, Issues, Drive e Equipe ficam sempre à vista. O resto fica em **"Mais"** (Wiki, Rotinas, Calendário, Painéis, Gerenciador de tarefas).
   - Cada pessoa escolhe o que fica à vista.
3. **CANAIS**, com categorias dobráveis e "+".
4. **AGENTES:** a conversa com cada agente é a própria linha dele.
   - Agrupados por setor, com os fixados no topo.
   - A linha mostra quem está trabalhando ou esperando.
   - No fim, "Novo agente".
5. **MENSAGENS DIRETAS:** as pessoas.
6. **Rodapé:** você (menu da conta, com configurações e sair) e o sino de notificações.

A barra pode encolher para uma faixa só de ícones.

**Modo pessoal** (só você no espaço): sem cabeçalhos de seção, a lista são os seus agentes, e as ferramentas só aparecem quando algo espera por você. O resto fica em "Mais".

**Área de trabalho:**
- No topo, avisos de uma linha: computador desligado, agentes pausados.
- Embaixo, a página aberta. O endereço "puro" do espaço é o **Início**.

**Conversa:**
- **Cabeçalho** de canal, no estilo do Discord: `#nome`, o tópico, e botões de threads, fixados, configurações do canal, membros, busca e ⋯.
- **Cabeçalho** da conversa com um agente: rosto, nome e linha de status. Clicar abre a página do agente.
- **Coluna da direita:**
  - num canal, os **Membros**;
  - na conversa com um agente, **o agente** numa rolagem só: quem é, **o computador dele** (print recente da tela e o que está usando), rotinas, como pensa e tarefas recentes.
- **Painel flutuante** (`SidePanel`): thread, resultado de busca, página do agente, conversa de uma tarefa. É um cartão que entra pela direita sobre um fundo escurecido, e Esc ou clique fora fecha.

**Criar agente:** um fluxo focado, sem barra lateral, com os passos à esquerda.

## A nova disposição do Avora
1. **Uma barra lateral só** (`Sidebar.qml`), no lugar da barra atual e da lista do meio do chat. Ordem:
   1. cartão do espaço com o menu de espaços, mais "Ir para… Ctrl+K";
   2. ferramentas: **Início**, **Esperando você** (só com algo esperando), **Docs**, **Objetivos**, **Equipe**, e **Mais** (Rotinas, Gerenciador de tarefas, Memória do time, Plugins, Skills);
   3. **CANAIS**;
   4. **AGENTES**, por setor;
   5. **MENSAGENS DIRETAS**;
   6. rodapé com você (menu: Configurações, presença, Sair) e o sino.

   A barra de espaços (WorkspaceRail) some: os espaços ficam no menu do cartão.
2. **A conversa ocupa o resto.** O cabeçalho do agente tem rosto, nome e status (clicar abre a página do agente), o botão do modelo, **"Conversas"** (o histórico, que hoje é uma coluna fixa, vira um menu ou painel) e o botão da coluna da direita.
3. **Coluna da direita:**
   - num canal, **Membros** (pessoas e agentes, com presença e o que fazem);
   - com um agente, **o agente**: quem é, o computador e o navegador dele ao vivo, rotinas, como pensa e tarefas recentes.

   Ela substitui as abas do `SidePanel` da primeira onda.
4. **Painel flutuante:** docs, página do agente, execução e thread abrem num cartão que entra pela direita sobre um fundo escurecido, não em gavetas fixas.
5. **Ctrl+K:** ir para qualquer agente, canal, pessoa, página ou ação ("Novo agente", "Convidar pessoas", "Conectar uma IA").
6. **Avisos** de uma linha no topo da área de trabalho.
7. **Modo pessoal:** sem cabeçalhos de seção e ferramentas só quando há algo esperando.
