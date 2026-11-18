# Builds the Avora desktop app (Release) and bundles the Qt runtime next to it.
#
#   powershell -ExecutionPolicy Bypass -File desktop\build.ps1
#
# Needs Visual Studio Build Tools (MSVC, CMake, Ninja) and Qt 6 for MSVC.
# AVORA_QT overrides the Qt prefix (default C:\Qt\6.11.3\msvc2022_64).
# Output: desktop\build\out\avora-desktop.exe
$ErrorActionPreference = 'Stop'

$here = $PSScriptRoot
$qt = if ($env:AVORA_QT) { $env:AVORA_QT } else { 'C:\Qt\6.11.3\msvc2022_64' }
if (-not (Test-Path "$qt\bin\qtpaths.exe")) { throw "Qt not found at $qt (set AVORA_QT)." }

$vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
$vs = & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if (-not $vs) { throw 'Visual Studio Build Tools with the C++ workload not found.' }

# Load the x64 MSVC environment into this PowerShell session.
Import-Module "$vs\Common7\Tools\Microsoft.VisualStudio.DevShell.dll"
Enter-VsDevShell -VsInstallPath $vs -SkipAutomaticLocation -DevCmdArguments '-arch=x64 -host_arch=x64' | Out-Null

$build = Join-Path $here 'build'
$out = Join-Path $build 'out'
cmake -S $here -B $build -G Ninja -DCMAKE_BUILD_TYPE=Release "-DCMAKE_PREFIX_PATH=$qt"
if ($LASTEXITCODE) { exit $LASTEXITCODE }
cmake --build $build
if ($LASTEXITCODE) { exit $LASTEXITCODE }

New-Item -ItemType Directory -Force $out | Out-Null
Copy-Item "$build\avora-desktop.exe" $out -Force
& "$qt\bin\windeployqt.exe" --release --qmldir (Join-Path $here 'qml') --no-translations --no-system-d3d-compiler --no-opengl-sw "$out\avora-desktop.exe" | Out-Null
if ($LASTEXITCODE) { exit $LASTEXITCODE }
# --no-translations also drops the browser panel's Chromium locales; keep the two it uses.
$locales = "$out\translations\qtwebengine_locales"
New-Item -ItemType Directory -Force $locales | Out-Null
foreach ($l in @('pt-BR', 'en-US')) { Copy-Item "$qt\translations\qtwebengine_locales\$l.pak" $locales -Force }
Write-Host "Built: $out\avora-desktop.exe"
