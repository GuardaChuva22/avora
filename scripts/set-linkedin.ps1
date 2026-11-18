# Grava o Client ID e o Client Secret do app LinkedIn do dono na VPS.
# Cada valor é digitado sem aparecer na tela e vai só pela conexão SSH
# (scripts/set-secret.sh). O servidor reinicia sozinho depois de cada um.
$ErrorActionPreference = "Stop"
$bash = "C:\Program Files\Git\bin\bash.exe"
Set-Location (Split-Path $PSScriptRoot -Parent)
Write-Host "LinkedIn: cole o Client ID e depois o Client Secret (Auth, em linkedin.com/developers/apps)." -ForegroundColor Cyan
& $bash scripts/set-secret.sh AVORA_LINKEDIN_CLIENT_ID
if ($LASTEXITCODE -ne 0) { throw "o Client ID não foi gravado" }
& $bash scripts/set-secret.sh AVORA_LINKEDIN_CLIENT_SECRET
if ($LASTEXITCODE -ne 0) { throw "o Client Secret não foi gravado" }
Write-Host "Pronto. No app: Plugins -> LinkedIn -> Conectar." -ForegroundColor Green
Write-Host "Lembre: no app do LinkedIn, em Auth, a Redirect URL tem que ser http://127.0.0.1:47821/callback" -ForegroundColor Yellow
