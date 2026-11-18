# Avora

Um time de funcionários digitais 24h, pessoal e privado. Você conversa com eles pelo app desktop (Windows). Eles agem sozinhos, com iniciativa: usam ferramentas, se conectam aos seus apps (Gmail, Drive, ClickUp, Discord, MCPs...), rodam rotinas e missões de vários dias e delegam ao Claude Code e ao Codex quando precisam.

Tudo roda no **seu** servidor, com o **seu** provedor de modelo. Nada é preso a uma conta ou máquina específica.

## Como funciona

- **`avora-server`** (Go): o servidor. Guarda conversas, agentes, rotinas e credenciais (cifradas) num Postgres e expõe a API para o app.
- **`avora-runtime`** (Go): um nó que executa o trabalho pesado (Claude Code, Codex, navegador, comandos em sandbox). Roda no servidor e, se quiser, também no seu PC.
- **App desktop** (C++/Qt 6, Windows): onde você conversa com o time e configura tudo.

## Rodando o seu

### 1. Servidor (Linux com systemd)

Precisa de Postgres e, de preferência, Tailscale (ou outro proxy TLS).

```bash
cp .env.vps.example .env.vps   # coloque AVORA_VPS_HOST=usuario@seu-servidor
./scripts/deploy-vps.sh        # compila, envia e instala
```

O instalador cria o usuário `avora`, o banco, o token de admin e a chave do cofre. Com Tailscale, ele também publica o servidor em `https://<máquina>.<tailnet>.ts.net:8443`. Sem Tailscale, coloque um proxy TLS (Caddy, nginx) na frente de `127.0.0.1:3190`.

Todas as opções do servidor estão em [`.env.example`](.env.example).

### 2. App desktop (Windows)

**Jeito fácil:** baixe o `Avora-windows.zip` na página de [Releases](../../releases), descompacte e abra `avora-desktop.exe`.

**Do código:** precisa de Qt 6 (MSVC) e do VS Build Tools.

```powershell
powershell -ExecutionPolicy Bypass -File desktop\build.ps1
powershell -ExecutionPolicy Bypass -File desktop\install.ps1
```

Na primeira abertura, informe o endereço do seu servidor e clique em **Criar conta**. A primeira conta vira a dona do espaço "Pessoal". O token de admin continua valendo como entrada de operador (`bash scripts/admin-token.sh` copia o token sem mostrar na tela).

### 3. Conectar uma IA

Em **Configurações → Contas de IA**, clique em **Conectar uma IA**. Dá para conectar várias:

- **Assinaturas:** Claude, ChatGPT (Codex) e Google AI (Antigravity), que rodam pelos programas oficiais num computador seu.
- **APIs:** OpenRouter, Anthropic, OpenAI, Google Gemini, xAI, DeepSeek, Mistral, Groq, Cerebras, Together, Fireworks e NVIDIA.
- **Locais:** Ollama, LM Studio e llama.cpp, inclusive no PC de outra pessoa do espaço, pelo runner dela.
- **Qualquer outra** API no formato OpenAI.

Cada agente escolhe com qual pensa, ou fica no **Automático**, que usa a melhor conectada.

## Usando com amigos

1. Uma pessoa sobe o servidor (passo 1) num lugar que todos alcançam: uma VPS com um proxy TLS (Caddy, nginx) ou um Tailscale com a máquina compartilhada com os amigos.
2. Ela cria a conta dela no app e, em **Configurações → Pessoas**, clica em **Convidar pessoas**. O link vale 7 dias e pode pedir aprovação.
3. Os amigos abrem o app, informam o mesmo endereço do servidor e usam **Tenho um convite**.

Cada pessoa pode criar os próprios espaços, como os servidores do Discord. Os agentes, as conversas, os docs e as contas de IA de um espaço só aparecem para quem está nele.

## Desenvolvendo

- Go 1.26+: `go test ./...` (os testes de Postgres rodam no CI com `AVORA_TEST_DATABASE_URL`).
- App: `desktop/qml`. As cores, tamanhos de fonte, raios e espaçamentos vêm de [`Theme.qml`](desktop/qml/Theme.qml); não use valores soltos. Os blocos comuns são `Card`, `Sheet` (modais), `Overline` (rótulo de seção), `Pill`, `PrimaryButton`, `GhostButton` e `Field`.
- Como contribuir: [CONTRIBUTING.md](CONTRIBUTING.md). Licença: [MIT](LICENSE).
- Documentos: [plano do ecossistema](docs/plano-ecossistema.md), [fluxo das telas](docs/fluxo-telas.md), [especificação](docs/spec.md), [decisões](docs/decisions.md), [arquitetura](docs/architecture.md), [roadmap](docs/roadmap.md) e [operação](docs/operations.md).
