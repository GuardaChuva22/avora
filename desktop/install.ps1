# Installs the built desktop app for this Windows user, with "Avora" in the
# Start menu and on the Desktop. Build first with desktop\build.ps1.
# Running it again updates the installed copy; an app that was open is
# closed and opened again on the new version.
#
#   powershell -ExecutionPolicy Bypass -File desktop\install.ps1 [-Open]
param([switch]$Open)
$ErrorActionPreference = 'Stop'

$src = Join-Path $PSScriptRoot 'build\out'
if (-not (Test-Path "$src\avora-desktop.exe")) { throw 'Build first: desktop\build.ps1' }
$dest = Join-Path $env:LOCALAPPDATA 'Avora\desktop'
$exe = "$dest\avora-desktop.exe"

$running = Get-Process avora-desktop -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -eq $exe }
if ($running) {
    $running | Stop-Process -Force
    $running | Wait-Process -Timeout 10 -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500 # let Windows release the DLL handles
}
if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
New-Item -ItemType Directory -Force $dest | Out-Null
Copy-Item -Recurse "$src\*" $dest

$shell = New-Object -ComObject WScript.Shell
foreach ($dir in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('Desktop'))) {
    $lnk = $shell.CreateShortcut((Join-Path $dir 'Avora.lnk'))
    $lnk.TargetPath = $exe
    $lnk.WorkingDirectory = $dest
    $lnk.IconLocation = "$exe,0"
    $lnk.Description = 'Avora'
    $lnk.Save()
}
Write-Host "Installed to $dest (Start menu and Desktop: Avora)."

if ($running -or $Open) {
    Start-Process -FilePath $exe -WorkingDirectory $dest
    Write-Host 'Avora opened on the new version.'
}
