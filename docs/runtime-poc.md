# Avora — Primeira comunicação autenticada Windows ↔ VPS

> **Prova de conceito**, não pronta para produção. Somente as ações allowlisted `ping` e `diagnostics` são executáveis. Não aceita shell, scripts, Codex, Claude ou qualquer texto arbitrário. Tarefas e presença ficam **somente na memória** do processo servidor.

## Executáveis
- `go run ./cmd/avora-server` — control plane Go com API HTTP autenticada, **somente loopback**.
- `go run ./cmd/avora-runtime` — worker Windows/VPS Go que faz heartbeat de 5 em 5 segundos e busca tarefas.

## Teste local no Windows (PowerShell)
Pré-requisito: Go 1.23+.

1. Gere **dois tokens fortes diferentes**, cada um com ao menos 32 caracteres aleatórios. Por exemplo, em PowerShell 7:

```powershell
[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
```

Execute o comando duas vezes. Guarde os valores fora do Git.

2. Na janela **PowerShell 1**, configure servidor e dispositivo (substitua os tokens):

```powershell
$env:AVORA_ADMIN_TOKEN = "<TOKEN_ADMIN_FORTE>"
$env:AVORA_DEVICES_JSON = '{"meu-pc":{"token":"<TOKEN_PC_DIFERENTE>","target":"windows"}}'
$env:AVORA_LISTEN_ADDR = "127.0.0.1:8080"
go run ./cmd/avora-server
```

3. Na janela **PowerShell 2**, configure o runtime (mesmo token de dispositivo):

```powershell
$env:AVORA_SERVER_URL = "http://127.0.0.1:8080"
$env:AVORA_DEVICE_ID = "meu-pc"
$env:AVORA_DEVICE_TOKEN = "<TOKEN_PC_DIFERENTE>"
go run ./cmd/avora-runtime
```

4. Na janela **PowerShell 3**, use o token de administrador para consultar dispositivos e enviar um job seguro:

```powershell
$env:AVORA_ADMIN_TOKEN = "<TOKEN_ADMIN_FORTE>"
$headers = @{ Authorization = "Bearer $env:AVORA_ADMIN_TOKEN" }
Invoke-RestMethod -Uri "http://127.0.0.1:8080/v1/devices" -Headers $headers
$job = Invoke-RestMethod -Method POST -Uri "http://127.0.0.1:8080/v1/jobs" -Headers $headers -ContentType "application/json" -Body '{"target":"auto","action":"ping"}'
$job
Start-Sleep -Seconds 6
Invoke-RestMethod -Uri "http://127.0.0.1:8080/v1/jobs/$($job.id)" -Headers $headers
```

O resultado esperado é `status=succeeded`, `output=pong from meu-pc`. Se o dispositivo ficar sem heartbeat por ~30 s, é considerado offline. A seleção automática escolhe Windows online; se não houver Windows online, pode selecionar VPS online se houver dispositivo VPS autorizado.

## Quando o servidor estiver na VPS
- O servidor escuta somente em `127.0.0.1`. **Não abra a porta 8080 diretamente na Internet.**
- É possível conectar o PC por **túnel SSH**, por exemplo: `ssh -N -L 18080:127.0.0.1:8080 usuario@vps`; nesse caso, no runtime Windows use `AVORA_SERVER_URL=http://127.0.0.1:18080`. O tráfego remoto passa cifrado pelo SSH.
- Alternativamente, configure um **proxy TLS** com autenticação e controle de rede, apontando para o loopback da VPS; o runtime exige `https://` quando o host remoto não é loopback.
- Acesso direto do APK Android exigirá APIs TLS estáveis, identidade e pareamento de dispositivo; **não está implementado**.

## Contrato desta etapa
- `GET /healthz`: saúde (não exige token, apenas loopback).
- `GET /v1/capabilities`: capacidades do POC (admin).
- `GET /v1/devices`: lista e estado de presença (admin).
- `POST /v1/jobs`: aceita `{"target":"auto|windows|vps","action":"ping|diagnostics"}` (admin).
- `GET /v1/jobs/{id}`: resultado (admin).
- `POST /v1/runtime/heartbeat`: identifica dispositivo e atualiza presença (token + `X-Avora-Device-ID`).
- `POST /v1/runtime/claim`: retira o próximo job atribuído ao dispositivo.
- `POST /v1/runtime/jobs/{id}/complete`: conclui job atribuído ao mesmo dispositivo.

## Limitações e próximos passos
- Estado 100% em memória, sem persistência, retomada, cancelamento, streaming, fila durável ou idempotência.
- Registro de dispositivos em variável de ambiente, sem pareamento/revogação dinâmica.
- Um job pode ficar em `running` se o worker cair; recuperação e timeout não existem ainda.
- A presença é deduzida de heartbeat, não de prova de capacidade ou saúde completa.
- Sem integração real de IA, Git, execução de programas, Codex/Claude, UI Qt ou Android.
- Não usar com dados ou credenciais pessoais sensíveis até concluir hardening, autorização robusta, logs de auditoria e persistência segura.

Consulte [os 263 requisitos](requirements.md), [os 42 complementares](requirements-complementares.md), [arquitetura](architecture.md) e [roadmap](roadmap.md).
