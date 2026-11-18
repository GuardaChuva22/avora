# Avora — Catálogo mestre dos 263 requisitos originais

> **Fonte canônica no GitHub**: esta lista registra **todos os itens do inventário discutido e aprovado no chat**. A planilha anterior não precisa mais ser acessada para conhecer os requisitos. Para decisões posteriores, leia [requisitos complementares](requirements-complementares.md).

## Decisão de escopo

- **Prioridade:** todos os 263 itens são **obrigatórios** para a visão final, sem exceção.
- **Status de partida:** planejado. Um item só muda para implementado após código, teste, demonstração e evidência.
- **Uso:** pessoal/privado, sem distribuição comercial. Itens de colaboração continuam obrigatórios conforme solicitado, mesmo que inicialmente sejam exercitados com identidades ou agentes de teste.
- **Comparação:** a presença no catálogo não afirma que um concorrente possui exatamente a mesma implementação. Desempenho e paridade precisarão ser auditados.
- **IDs:** estáveis; não renumerar. Adições após a lista original ficam em documento próprio.
- **Critério comum de aceite:** funcionalidade operante em cenário real, verificação de falhas, segurança aplicável, logs e teste reproduzível. Especificações e integração detalhadas serão acrescentadas por item.

## Índice

| Área | Código | Quantidade | Obrigatoriedade |
|---|---|---:|---|
| Agentes e sistema multiagente | A01–A15 | 15 | Obrigatória |
| Memória e conhecimento persistente | B01–B15 | 15 | Obrigatória |
| Autonomia, proatividade e rotinas | C01–C16 | 16 | Obrigatória |
| Computador, navegador e execução real | D01–D18 | 18 | Obrigatória |
| Integrações, APIs e ferramentas | E01–E17 | 17 | Obrigatória |
| Interfaces, comunicação e voz | F01–F22 | 22 | Obrigatória |
| Skills, aprendizado e evolução | G01–G16 | 16 | Obrigatória |
| Modelos de IA e inteligência | H01–H16 | 16 | Obrigatória |
| Pesquisa, programação e produção de arquivos | I01–I18 | 18 | Obrigatória |
| Segurança, permissões e controle | J01–J18 | 18 | Obrigatória |
| Infraestrutura, operação e observabilidade | K01–K18 | 18 | Obrigatória |
| Trabalho em equipe e funcionalidades extras | L01–L11 | 11 | Obrigatória |
| Inteligência autônoma avançada | M01–M14 | 14 | Obrigatória |
| Memória pessoal superior | N01–N14 | 14 | Obrigatória |
| Automação integral da vida | P01–P20 | 20 | Obrigatória |
| Engenharia de confiabilidade e evolução | Q01–Q15 | 15 | Obrigatória |
| **TOTAL** | | **263** | **100% obrigatórios** |

## A — Agentes e sistema multiagente

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **A01** | Criar agentes personalizados com funções específicas | Planejado |
| **A02** | Agente principal para coordenar outros | Planejado |
| **A03** | Agentes persistentes entre conversas e tarefas | Planejado |
| **A04** | Dar nome, personalidade e instruções próprias a cada agente | Planejado |
| **A05** | Criar subagentes para executar subtarefas | Planejado |
| **A06** | Delegar tarefas automaticamente | Planejado |
| **A07** | Executar vários agentes em paralelo | Planejado |
| **A08** | Comunicação e colaboração entre agentes | Planejado |
| **A09** | Sessões e contextos isolados por agente | Planejado |
| **A10** | Especialistas com ferramentas e permissões próprias | Planejado |
| **A11** | Bots colaborando em conversas de grupo | Planejado |
| **A12** | Agentes compartilhados com outras pessoas | Planejado |
| **A13** | Criar ou configurar novos agentes por instrução | Planejado |
| **A14** | Interromper, redirecionar ou acompanhar tarefas em andamento | Planejado |
| **A15** | Templates reutilizáveis de agentes | Planejado |

## B — Memória e conhecimento persistente

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **B01** | Memória persistente entre sessões | Planejado |
| **B02** | Perfil do usuário, preferências e contexto pessoal | Planejado |
| **B03** | Memória individual de cada agente | Planejado |
| **B04** | Informações compartilhadas entre agentes | Planejado |
| **B05** | Histórico de conversas | Planejado |
| **B06** | Registro do trabalho realizado | Planejado |
| **B07** | Memorização automática de fatos relevantes | Planejado |
| **B08** | Corrigir, atualizar e excluir memórias | Planejado |
| **B09** | Busca e recuperação de memórias antigas | Planejado |
| **B10** | Busca semântica e/ou textual por conhecimento | Planejado |
| **B11** | Resumos e compactação de contexto | Planejado |
| **B12** | Arquivos de contexto por projeto | Planejado |
| **B13** | Provedores de memória externos e plugáveis | Planejado |
| **B14** | Aprendizado sobre preferências e comportamento do usuário | Planejado |
| **B15** | Rastreabilidade da origem de informações memorizadas | Planejado |

## C — Autonomia, proatividade e rotinas

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **C01** | Trabalhar sem você ficar conversando | Planejado |
| **C02** | Continuar tarefas com o aplicativo fechado | Planejado |
| **C03** | Agendar tarefas recorrentes | Planejado |
| **C04** | Agendar usando linguagem natural | Planejado |
| **C05** | Expressões cron e horários específicos | Planejado |
| **C06** | Iniciar automações por eventos | Planejado |
| **C07** | Receber webhooks como gatilho | Planejado |
| **C08** | Reagir a novos e-mails | Planejado |
| **C09** | Reagir a mensagens em aplicativos | Planejado |
| **C10** | Executar rotinas periodicamente | Planejado |
| **C11** | Monitorar informações e gerar alertas | Planejado |
| **C12** | Preparar relatórios automáticos | Planejado |
| **C13** | Realizar tarefas compostas por várias etapas | Planejado |
| **C14** | Pedir aprovação e continuar depois | Planejado |
| **C15** | Inspecionar e gerenciar rotinas existentes | Planejado |
| **C16** | Receber notificações sobre resultados e bloqueios | Planejado |

## D — Computador, navegador e execução real

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **D01** | Navegar automaticamente na internet | Planejado |
| **D02** | Clicar, preencher formulários e interagir com páginas | Planejado |
| **D03** | Ler e extrair conteúdo de sites | Planejado |
| **D04** | Executar comandos no terminal | Planejado |
| **D05** | Criar, ler e modificar arquivos | Planejado |
| **D06** | Executar scripts e programas | Planejado |
| **D07** | Usar um computador remoto ou na nuvem | Planejado |
| **D08** | Usar ambientes locais | Planejado |
| **D09** | Persistir arquivos e sessões entre tarefas | Planejado |
| **D10** | Executar tarefas em Docker ou ambientes isolados | Planejado |
| **D11** | Execução remota por SSH | Planejado |
| **D12** | Sessões e perfis de navegador configuráveis | Planejado |
| **D13** | Visualizar o que o agente faz na tela | Planejado |
| **D14** | Assumir controle manual durante um bloqueio | Planejado |
| **D15** | Capturar screenshots para análise | Planejado |
| **D16** | Gerenciar sessões autenticadas | Planejado |
| **D17** | Várias superfícies de trabalho simultâneas | Planejado |
| **D18** | Acessar recursos do celular, como câmera e comandos do aparelho | Planejado |

## E — Integrações, APIs e ferramentas

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **E01** | Suporte a MCP (Model Context Protocol) | Planejado |
| **E02** | Conexão com APIs externas | Planejado |
| **E03** | Plugins e extensões personalizadas | Planejado |
| **E04** | Ferramentas desenvolvidas pelo usuário | Planejado |
| **E05** | Catálogo ou marketplace de extensões | Planejado |
| **E06** | Integração com Gmail e serviços de e-mail | Planejado |
| **E07** | Integração com Google Calendar | Planejado |
| **E08** | Integração com Google Drive | Planejado |
| **E09** | Integração com sistemas de tarefas e projetos | Planejado |
| **E10** | Integração com GitHub | Planejado |
| **E11** | Integração com Slack | Planejado |
| **E12** | Integração com bancos de dados | Planejado |
| **E13** | Integração com armazenamento de arquivos | Planejado |
| **E14** | Conexão com serviços sem conector nativo por navegador | Planejado |
| **E15** | Criar ou adaptar integrações para novas ferramentas | Planejado |
| **E16** | Webhooks de entrada e saída | Planejado |
| **E17** | Ferramentas habilitadas por agente e por permissão | Planejado |

## F — Interfaces, comunicação e voz

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **F01** | Interface de chat | Planejado |
| **F02** | Conversas por voz | Planejado |
| **F03** | Transcrição de áudio para texto | Planejado |
| **F04** | Respostas faladas (TTS) | Planejado |
| **F05** | Comando de ativação por voz (wake word) | Planejado |
| **F06** | Envio e recebimento de arquivos | Planejado |
| **F07** | Envio e análise de imagens | Planejado |
| **F08** | Análise de áudio e vídeo | Planejado |
| **F09** | Aplicação para computador | Planejado |
| **F10** | Interface para celular | Planejado |
| **F11** | Interface de terminal/CLI | Planejado |
| **F12** | Dashboard de gerenciamento | Planejado |
| **F13** | Mensagens em Telegram | Planejado |
| **F14** | Mensagens em WhatsApp | Planejado |
| **F15** | Mensagens em Discord | Planejado |
| **F16** | Mensagens em Signal | Planejado |
| **F17** | Integração com Microsoft Teams | Planejado |
| **F18** | iMessage e outros canais de mensagens | Planejado |
| **F19** | Grupos, menções e canais colaborativos | Planejado |
| **F20** | Bot com seu próprio endereço de e-mail | Planejado |
| **F21** | Interagir com o Bot pelo X | Planejado |
| **F22** | Mensagens de progresso durante execução | Planejado |

## G — Skills, aprendizado e evolução

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **G01** | Criar habilidades (skills) personalizadas | Planejado |
| **G02** | Reutilizar habilidades em tarefas futuras | Planejado |
| **G03** | Criar skills a partir de tarefas concluídas | Planejado |
| **G04** | Carregar skills apenas quando necessárias | Planejado |
| **G05** | Aprender procedimentos com a experiência | Planejado |
| **G06** | Melhorar skills durante o uso | Planejado |
| **G07** | Criar novas ferramentas e integrações | Planejado |
| **G08** | Publicar e compartilhar skills | Planejado |
| **G09** | Importar skills da comunidade | Planejado |
| **G10** | Skills específicas para cada agente | Planejado |
| **G11** | Arquivos de personalidade SOUL.md | Planejado |
| **G12** | Hooks para modificar comportamentos | Planejado |
| **G13** | Ensinar procedimentos por demonstração visual | Planejado |
| **G14** | Bibliotecas de conhecimento procedural | Planejado |
| **G15** | Executar código gerado para combinar ferramentas | Planejado |
| **G16** | Personalizar ferramentas sem alterar o núcleo do sistema | Planejado |

## H — Modelos de IA e inteligência

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **H01** | Usar diferentes provedores de IA | Planejado |
| **H02** | Integrar modelos OpenAI, Anthropic e Google | Planejado |
| **H03** | Usar modelos locais | Planejado |
| **H04** | Usar modelos hospedados na nuvem | Planejado |
| **H05** | Selecionar modelos por agente | Planejado |
| **H06** | Roteamento de modelos por configuração e prioridade | Planejado |
| **H07** | Modelos alternativos quando o principal falha | Planejado |
| **H08** | Configurar prioridade de modelos | Planejado |
| **H09** | Otimizar seleção por custo, velocidade ou qualidade | Planejado |
| **H10** | Gerenciar múltiplas chaves de API | Planejado |
| **H11** | Rotacionar credenciais quando há limite de uso | Planejado |
| **H12** | Cache de prompts | Planejado |
| **H13** | Modelos especializados em visão | Planejado |
| **H14** | Modelos de transcrição e síntese de voz | Planejado |
| **H15** | Modelos de geração de imagem | Planejado |
| **H16** | Modelos de geração de vídeo | Planejado |

## I — Pesquisa, programação e produção de arquivos

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **I01** | Pesquisa na web | Planejado |
| **I02** | Extração e análise de páginas | Planejado |
| **I03** | Pesquisa aprofundada em múltiplas fontes | Planejado |
| **I04** | Leitura e análise de PDFs | Planejado |
| **I05** | Criação e edição de documentos | Planejado |
| **I06** | Criação e edição de planilhas | Planejado |
| **I07** | Criação de apresentações | Planejado |
| **I08** | Análise e transformação de dados | Planejado |
| **I09** | Escrita e execução de código | Planejado |
| **I10** | Edição de projetos de software | Planejado |
| **I11** | Integração com ambientes de desenvolvimento | Planejado |
| **I12** | Delegação de programação para agentes de código | Planejado |
| **I13** | Uso de repositórios e controle de versão | Planejado |
| **I14** | Geração de imagens | Planejado |
| **I15** | Criação de relatórios e resumos | Planejado |
| **I16** | Análise de logs e depuração de erros | Planejado |
| **I17** | Processamento em lote de múltiplas tarefas | Planejado |
| **I18** | Exportação de trajetórias para avaliações e treinamento | Planejado |

## J — Segurança, permissões e controle

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **J01** | Aprovação humana para ações sensíveis | Planejado |
| **J02** | Políticas configuráveis de permissão | Planejado |
| **J03** | Aprovar ou negar comandos de terminal | Planejado |
| **J04** | Restringir ferramentas por agente | Planejado |
| **J05** | Isolamento de execução (sandbox) | Planejado |
| **J06** | Armazenamento protegido de credenciais | Planejado |
| **J07** | Integração com gerenciadores de senhas | Planejado |
| **J08** | Preenchimento de senhas sem expor valores ao modelo | Planejado |
| **J09** | Entrada segura de informações sensíveis | Planejado |
| **J10** | Revisão automatizada de ações | Planejado |
| **J11** | Regras de autorização em linguagem natural | Planejado |
| **J12** | Controle de usuários e dispositivos autorizados | Planejado |
| **J13** | Histórico e auditoria de ações | Planejado |
| **J14** | Recuperação de arquivos e alterações | Planejado |
| **J15** | Proteções contra prompt injection | Planejado |
| **J16** | Separação de agentes por acesso e execução | Planejado |
| **J17** | Controles de segurança de rede | Planejado |
| **J18** | Política externa de inspeção de prompts | Planejado |

## K — Infraestrutura, operação e observabilidade

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **K01** | Execução contínua em servidor | Planejado |
| **K02** | Instalação autohospedada | Planejado |
| **K03** | Execução local em computador pessoal | Planejado |
| **K04** | Execução em Docker | Planejado |
| **K05** | Execução em infraestrutura cloud | Planejado |
| **K06** | Ambientes de execução remotos | Planejado |
| **K07** | Persistência de arquivos e estado | Planejado |
| **K08** | Gerenciamento de sessões | Planejado |
| **K09** | Gerenciamento de processos e tarefas | Planejado |
| **K10** | Logs de execução | Planejado |
| **K11** | Diagnóstico de problemas | Planejado |
| **K12** | Monitoramento e telemetria | Planejado |
| **K13** | Rastreamento de chamadas e ferramentas | Planejado |
| **K14** | Backup, checkpoints e recuperação | Planejado |
| **K15** | Integração com serviços próprios via API | Planejado |
| **K16** | Servidor compatível com API OpenAI | Planejado |
| **K17** | Atualização e manutenção do sistema | Planejado |
| **K18** | Infraestrutura distribuída entre máquinas | Planejado |

## L — Trabalho em equipe e funcionalidades extras

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **L01** | Bots acessíveis a múltiplos usuários | Planejado |
| **L02** | Bots compartilhados com permissões de colaboração | Planejado |
| **L03** | Compartilhamento de templates de agentes | Planejado |
| **L04** | Personalização visual de agentes | Planejado |
| **L05** | Organização de agentes por função ou projeto | Planejado |
| **L06** | Mensagens de agentes em canais de equipe | Planejado |
| **L07** | Integração com sistemas de colaboração | Planejado |
| **L08** | Administração centralizada de agentes | Planejado |
| **L09** | Controle de acesso por pessoa ou grupo | Planejado |
| **L10** | Compartilhamento de skills com uma equipe | Planejado |
| **L11** | Interface de gerenciamento e configuração | Planejado |

## M — Inteligência autônoma avançada

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **M01** | Decomposição automática de objetivos complexos em projetos e tarefas | Planejado |
| **M02** | Planejamento de longo prazo com dependências | Planejado |
| **M03** | Orquestrador que escolhe agentes, ferramentas e modelos dinamicamente | Planejado |
| **M04** | Redistribuição automática quando um agente falha | Planejado |
| **M05** | Planejamento paralelo com avaliação de alternativas | Planejado |
| **M06** | Crítico independente que revisa decisões importantes | Planejado |
| **M07** | Verificação de resultados contra critérios objetivos | Planejado |
| **M08** | Detecção de tarefas incompletas ou esquecidas | Planejado |
| **M09** | Retomada automática após reinício ou queda de energia | Planejado |
| **M10** | Aprendizado de estratégias com base em resultados mensurados | Planejado |
| **M11** | Simulação de ações antes de executá-las | Planejado |
| **M12** | Gerenciamento de prioridades concorrentes | Planejado |
| **M13** | Coordenação de tarefas que duram dias ou semanas | Planejado |
| **M14** | Agentes capazes de colaborar sem gerar loops infinitos | Planejado |

## N — Memória pessoal superior

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **N01** | Memória autobiográfica cronológica | Planejado |
| **N02** | Grafo de conhecimento pessoal | Planejado |
| **N03** | Memória episódica de tarefas e acontecimentos | Planejado |
| **N04** | Memória semântica de fatos e conhecimentos | Planejado |
| **N05** | Memória procedural de métodos e habilidades | Planejado |
| **N06** | Memória contextual por projeto, pessoa e objetivo | Planejado |
| **N07** | Identificação de contradições entre memórias | Planejado |
| **N08** | Atualização e expiração de informações antigas | Planejado |
| **N09** | Rastreamento de origem e grau de confiança | Planejado |
| **N10** | Aprendizado explícito com seus feedbacks | Planejado |
| **N11** | Busca unificada entre arquivos, mensagens e conhecimento | Planejado |
| **N12** | Política de privacidade e retenção por categoria | Planejado |
| **N13** | Exportação e migração de toda a memória | Planejado |
| **N14** | Capacidade de explicar por que uma informação foi lembrada | Planejado |

## P — Automação integral da vida

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **P01** | Administrar agenda e compromissos | Planejado |
| **P02** | Organizar tarefas, prioridades e prazos | Planejado |
| **P03** | Gerenciar e-mails, mensagens e respostas | Planejado |
| **P04** | Pesquisar informações e oportunidades relevantes | Planejado |
| **P05** | Administrar projetos pessoais | Planejado |
| **P06** | Administrar projetos profissionais | Planejado |
| **P07** | Monitorar gastos, contas e orçamento | Planejado |
| **P08** | Organizar documentos e arquivos | Planejado |
| **P09** | Organizar estudos e aprendizado | Planejado |
| **P10** | Monitorar hábitos e metas | Planejado |
| **P11** | Ajudar a planejar alimentação, exercícios e bem-estar | Planejado |
| **P12** | Gerenciar viagens e reservas | Planejado |
| **P13** | Controlar dispositivos de casa inteligente | Planejado |
| **P14** | Automatizar compras e comparação de produtos | Planejado |
| **P15** | Produzir relatórios sobre sua vida e seus projetos | Planejado |
| **P16** | Antecipar conflitos e problemas de agenda | Planejado |
| **P17** | Sugerir ações com base em seus objetivos | Planejado |
| **P18** | Executar rotinas pessoais sem comandos repetidos | Planejado |
| **P19** | Administrar múltiplas contas e serviços | Planejado |
| **P20** | Criar novas automações a partir de necessidades recorrentes | Planejado |

## Q — Engenharia de confiabilidade e evolução

| ID | Funcionalidade obrigatória | Estado inicial |
|---|---|---|
| **Q01** | Testes automatizados de cada agente | Planejado |
| **Q02** | Benchmark próprio de desempenho | Planejado |
| **Q03** | Comparar modelos em tarefas reais | Planejado |
| **Q04** | Medir custo de cada tarefa | Planejado |
| **Q05** | Medir taxa de conclusão correta | Planejado |
| **Q06** | Detectar regressões após atualizações | Planejado |
| **Q07** | Testar skills novas antes de ativá-las | Planejado |
| **Q08** | Versionar agentes, prompts e ferramentas | Planejado |
| **Q09** | Recuperar versões anteriores automaticamente | Planejado |
| **Q10** | Limites de custo, tempo e ações por tarefa | Planejado |
| **Q11** | Dashboard com saúde de todos os agentes | Planejado |
| **Q12** | Alertas de erros e comportamentos anormais | Planejado |
| **Q13** | Auditoria de permissões e acesso a dados | Planejado |
| **Q14** | Simulação em ambiente seguro antes de alterações importantes | Planejado |
| **Q15** | Melhoria contínua baseada em testes verificáveis | Planejado |

## Regras de rastreamento

1. Cada ID deverá ter implementação, testes, responsável técnico, dependências, métricas e referência a issue/PR.
2. Nenhuma ausência ou adiamento remove um requisito do escopo.
3. Segurança e permissões precedem execução de ações externas e acesso a credenciais.
4. Requisitos adicionados após o inventário **não substituem nem diminuem** estes 263 itens: consulte [requirements-complementares.md](requirements-complementares.md).
5. É permitido dividir a entrega em etapas; não é permitido declarar paridade antes de testes de ponta a ponta.
