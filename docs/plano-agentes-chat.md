# Plano: trazer o melhor do agentes-chat para o Avora

Lido em 2026-11-17 do repositório `agentes-chat/agentes-chat` (commit `817a1ee`). Fontes:
- o README;
- os 194 ADRs (`docs/adr`);
- o esquema do banco (`packages/db/src/schema/agents.ts`);
- os textos em português (`packages/i18n/src/pt-BR.ts`: `agentPage`, `brain`, `settings`);
- as telas (`apps/web/src/pages`).

Este documento substitui a lista curta de [comparacao-agentes-chat.md](comparacao-agentes-chat.md), que já foi feita: canais (D54), cérebro por agente (D55) e aprovações por agente (D56).

**O que fica de fora:** o agentes-chat é feito para equipes, com várias pessoas, cargos, convites, cobrança, nuvem e licença comercial. O Avora tem um dono só, então isso não entra. Todo o resto entra, adaptado.

## Onde o Avora está fraco, em uma frase cada

1. **O agente é um modal**, e lá ele é uma página. No agentes-chat, cada agente tem uma página com 8 abas, números, trabalho em andamento, histórico e uma barra que mostra "o que mudou" antes de salvar. No Avora, é um formulário que abre por cima do chat.
2. **Escolher o modelo é escondido.** No agentes-chat, o modelo se troca no próprio chat, com busca, etiquetas (Econômico, Rápido, Topo de linha), "Automático" (escolhe modelo e esforço por tarefa) e desfazer. A tela também explica por que um cérebro está indisponível.
3. **As configurações são uma página só.** Lá são cerca de 20 seções com abas, incluindo: IA e uso com medidores, Cofre, Dispositivos, Importar, Aparência e Sobre.
4. **A memória é uma lista.** Lá ela tem tipos (decisão, fato, preferência, lição, regra), é marcada como verificada ou não, pode ficar "sempre no contexto" e gera propostas de melhoria nas instruções.
5. **Criar um agente é preencher um formulário.** Lá são 2 passos, com modelos prontos, e depois uma lista do que falta configurar, que aparece no chat do próprio agente.
6. **Não dá para ver o trabalho.** Faltam o status de cada agente, o gerenciador de tarefas ("parar"), uma tela "Esperando você", o uso por agente no mês e o histórico de execuções.

## Fases

Cada fase vira uma entrada nova em [decisions.md](decisions.md) quando for feita. A ordem vai do que você mais sente para o resto.

### Fase 1: página do agente (ADRs 0155, 0164; `pages/agent`)

A página substitui o `AgentEditor` modal e abre pelo nome ou pela foto do agente em qualquer lugar.

| Aba | O que tem |
| --- | --- |
| Visão geral | Ficha: cérebro, computador, canais, rotinas e status. Números do mês: execuções, tokens e custo. O que está fazendo agora. Atalhos. |
| Instruções | Persona, funções e jeito de falar, com histórico de versões (já existe: restaurar). Propostas de mudança feitas pelo próprio agente (ADR 0092). |
| Ferramentas e acesso | Plugins e MCPs que ele pode usar, skills, pastas e projetos, sites que o navegador pode abrir (`browser_allowed_domains`) e regras de aprovação. |
| Memória | A memória nova da Fase 4. |
| Arquivos | Os Docs que ele criou ou mudou, do mais novo para o mais antigo. |
| Uso | Tokens e custo por dia e por modelo, e quanto de cada conta do plano ele usou. |
| Atividade | Execuções (o mesmo que o RunPane já mostra), com filtro e status. |
| Configurações | Identidade e aparência (cor, olhos, acessório, apelido para @), cérebro, orçamento do mês, canais, computador e arquivar ou apagar (digitando o nome). |

Em todas as abas, a barra "O que mudou" lista as alterações com Salvar e Descartar, e avisa se você tenta sair sem salvar.

### Fase 2: cérebro completo (ADRs 0032, 0041, 0059, 0086, 0094, 0143, 0146, 0149)

**Seletor de modelo no topo da conversa com o agente** ("Mudar o modelo do Atlas"):
- busca, com uma etiqueta por modelo: Econômico, Rápido ou Topo de linha;
- "Vale a partir da próxima resposta", com Desfazer.

**Opções do cérebro:**
- **Automático:** um modelo rápido escolhe o modelo e o esforço de cada tarefa entre os que o agente pode usar.
- **Plano esgotado:** se a conta do agente não responde ou acabou a cota, ele cai no modelo principal (`brain_fallback`, que hoje é implícito). Isso aparece na tela.
- **Rotação de contas:** quando o plano de uma conta chega no limite, o agente passa para a próxima conta do mesmo tipo. A opção "Só esta conta" desliga isso. É o pool do Antigravity estendido para Claude e Codex.
- **Outro provedor com teste:** além de listar os modelos, o Avora testa se cada um aceita ferramentas. Aceita o formato da Anthropic, não só o da OpenAI, e permite pôr o preço de cada modelo para o uso aparecer em dinheiro.
- **Por que está indisponível:** uma frase para cada caso. O computador está offline, o programa não está instalado, a versão está velha, ou o modelo chegou no limite e renova em tal hora.
- **Janela de contexto por agente:** 200K, 400K ou 1M, conforme o modelo. Hoje é fixa.
- **Velocidade:** padrão ou rápida, onde o provedor tiver.
- **Copiar o cérebro:** "Usar em outros agentes" copia o setup para vários agentes, e há um setup padrão para agentes novos (ADR 0094).

**Antigravity:**
- a lista de modelos passa a vir de `agy models` na conta, e não do código;
- dá para escolher a conta por agente, não só o pool inteiro.

**Novos cérebros por CLI, se estiverem instalados no PC:**
- OpenCode, com modelos grátis (ADR 0132);
- Grok Build (ADR 0142);
- Cursor (ADR 0114).

**Validação da chave ao colar.** Por exemplo: "As chaves da OpenRouter começam com sk-or-v1-".

### Fase 3: configurações reorganizadas (ADRs 0063, 0099, 0108, 0144, 0189, 0193; `settings-sections.ts`)

Uma lista à esquerda, e seções com abas:

- **Geral:** nome do time, fuso (hoje vem só do servidor), idioma, cor de destaque e fundo das conversas.
- **IA e uso:** os provedores e as contas de plano com medidores de cota, o uso por agente e por mês, e o modelo do roteador e do resumo.
- **Plugins:** como hoje, mas com várias contas por conector (ADR 0099).
- **Skills:** como hoje.
- **Cofre** (ADR 0144): logins, chaves e contatos que os agentes podem usar, com acesso concedido por agente.
- **Dispositivos:** computadores (os nós de hoje), celular e navegadores.
- **Notificações:** o que chega no PC e no celular.
- **Importar e exportar** (ADR 0055): agentes, skills, rotinas e memória de OpenClaw, Hermes, OpenBot, Grok Bot e ChatGPT. O time inteiro sai como um arquivo.
- **Sobre:** versão, servidor e licença.

Além disso, busca nas configurações com Ctrl+K.

### Fase 4: memória que se prova (ADRs 0017, 0028, 0120, 0167)

- **Tipos:** decisão, fato, preferência, lição e regra.
- **Verificação:** cada item é "Verificado" (confirmado pelo dono ou por um resultado real) ou "Não verificado" (só um agente afirmou, e entra no contexto marcado como dica).
- **Controles:** fixar ("Sempre no contexto"), confirmar, arquivar e buscar.
- **Aprende sozinho:** o agente aprende com correções e com aprovações recusadas e propõe lições. Uma lição que se repete vira "Proposta de melhoria" nas instruções, e o dono aceita ou recusa.
- **Memória do time:** compartilhada entre todos os agentes, além da memória de cada um.
- **Regras e skills que se aprendem:** quando você dá uma regra ("nunca mande e-mail sem me mostrar"), ela vira uma regra permanente. Um método que o agente repete vira uma skill. As duas coisas podem ser desfeitas.
- **Como os agentes falam com você:** simples (sem nomes de ferramenta nem caminhos de arquivo) ou técnico (com detalhes que abrem e fecham).

### Fase 5: criar agente fácil (ADRs 0081, 0155, 0164)

- Um card "Novo agente" com dois passos: escolher um modelo pronto (PM, Dev, Pesquisa, Ops, Assistente pessoal ou em branco) e depois o nome e o cérebro.
- O chat do agente novo abre com uma lista "Pra eu começar": cérebro, ferramentas, canais e a primeira rotina.
- Os agentes podem propor um agente novo, e o dono cria com um clique.
- **Primeira abertura guiada** (ADR 0162), que se pode retomar:
  1. conectar o servidor;
  2. "Sobre você" (nome, foto e do que você cuida), que vira o perfil que todo agente lê;
  3. trazer agentes de outro app, começar com um time pronto ou começar do zero;
  4. conectar uma IA;
  5. o time diz oi no canal.
- **Cofre importado** do 1Password e do Bitwarden, de CSV ou de `.env` (ADR 0144). O agente usa a senha sem que ela apareça para o modelo.

### Fase 6: trabalho visível (ADRs 0084, 0108, 0128, 0172, 0063, 0127)

- **Início, a página que abre o app:** só mostra as seções que têm itens, até 5 linhas cada:
  - saudação e resumo ("2 coisas esperam você, 3 agentes trabalhando");
  - "Peça a um agente": um campo de mensagem com o último agente com quem você falou primeiro;
  - Esperando você;
  - Trabalhando agora;
  - "Desde a sua última visita": o que foi entregue nos últimos 7 dias;
  - as próximas 3 rotinas.
- **"Ao abrir o app":** Início, a última conversa ou um canal escolhido.
- **Consultar o próprio uso:** o agente responde "como está meu limite?" com uma ferramenta (`get_my_usage`).
- **Status por agente:** "trabalhando em #canal", "esperando você", "livre". Aparece com um emoji ao lado do nome, na barra lateral e no chat.
- **Gerenciador de tarefas:** tudo o que cada agente está rodando agora, com Parar (o agente é avisado).
- **Esperando você:** uma página com aprovações, perguntas e decisões pendentes.
- **Uso:** por agente, por modelo e por conta, em tokens e em reais ou dólares.

### Fase 7: conversa mais rica (ADRs 0091, 0100, 0113, 0124, 0138, 0145, 0189)

- **Blocos ao vivo e enquetes:** o agente mostra algo para ser usado (um placar, um formulário, uma votação), não só texto.
- **Links de mensagem:** um link permanente que vira uma pílula clicável na mensagem.
- **Threads:** a partir de uma mensagem, que se seguem e se fecham.
- **Mensagens de voz:** gravar no app e transcrever com Whisper, que já existe no servidor.
- **Figurinhas e GIFs** dos agentes.
- **Fundos de conversa.**

### Fase 8: time que se organiza (ADRs 0034, 0048, 0083, 0093, 0095, 0131)

- **Setores de agentes** na barra lateral, cada um com nome, cor, ícone e líder, ordem arrastável e até 5 agentes fixados no topo.
- **Agentes que propõem mudanças nos colegas:** nas instruções, na função ou nos apps. O dono decide num card, e aplicar tem Desfazer.
- **Hierarquia:** "Reporta a" e um coordenador do time.
- **Recados entre agentes:** `tell_agent` manda um recado sem esperar resposta, e as hand-offs voltam para quem pediu.
- **Freios de autonomia por agente:** o que pode começar sozinho e quantas coisas ao mesmo tempo.

### Fase 9: wiki e painéis (ADRs 0023, 0135)

- **Docs vira wiki:** `[[links]]` a partir de mensagens e tarefas, páginas que linkam para esta (backlinks), grafo e importação do Obsidian.
- **Painéis com widgets** que os agentes mantêm atualizados.

## Como vou executar: os trabalhadores do Antigravity

O orquestrador (`internal/runner/agy_swarm_test.go`, fora do git, só no seu PC) roda vários `agy` ao mesmo tempo, um por conta do pool, usando o mesmo código do Avora (`RunAntigravity`). Ele põe o login de cada conta, um de cada vez, e deixa as contas trabalhando em paralelo, cada uma na sua pasta. Funcionou com 2 contas em paralelo no Gemini 4 Carbon (High), lendo e escrevendo arquivos.

Divisão do trabalho:
- **Eu:** desenho, escrevo o pedido de cada parte com os arquivos e as regras do `Theme.qml`, reviso o que volta, compilo, rodo os testes, conserto e faço o commit.
- **Os trabalhadores:** escrevem as partes independentes, cada um numa cópia do repositório (`git worktree`). Nunca dois mexem no mesmo arquivo.

Limites encontrados:
- No modo de edição do agy, os trabalhadores não rodam comandos (o agy recusa sozinho). Por isso o build, os testes e o git ficam comigo.
- Cada conta tem cota: o Avora já lê o uso de cada uma a cada 5 minutos, e eu distribuo o trabalho pela mais livre.
