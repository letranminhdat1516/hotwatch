# Install the hotwatch agent on this Windows or Windows Server host.
#
#   powershell -ExecutionPolicy Bypass -File install.ps1 -Mode tailscale|ssh [-Binary path\to\hotwatch-agent.exe]
#
# Run from an elevated (Administrator) PowerShell. Every step is printed and
# needs a "y" answer. Nothing but the agent itself is installed: no packages,
# no downloads.
param(
	[Parameter(Mandatory = $true)][ValidateSet('tailscale', 'ssh')][string]$Mode,
	[string]$Binary = (Join-Path $PSScriptRoot 'hotwatch-agent.exe'),
	[int]$Port = 7171
)
$ErrorActionPreference = 'Stop'

function Confirm-Step([string]$What) {
	Write-Host ""
	Write-Host "==> $What"
	$answer = Read-Host '    Proceed? [y/N]'
	if ($answer -notin @('y', 'Y')) {
		Write-Host 'Aborted; nothing further was changed.'
		exit 1
	}
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
	Write-Error 'install.ps1: run from an elevated (Administrator) PowerShell'
}
if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) {
	Write-Error "install.ps1: agent binary not found: $Binary"
}
if ($Mode -eq 'tailscale' -and -not (Get-Command tailscale -ErrorAction SilentlyContinue)) {
	Write-Error 'install.ps1: tailscale mode needs an installed, running Tailscale (not installed by this script)'
}

$installDir = Join-Path $env:ProgramFiles 'Hotwatch'
$exe = Join-Path $installDir 'hotwatch-agent.exe'
$dataDir = Join-Path $env:ProgramData 'Hotwatch'
$config = Join-Path $dataDir 'agent.json'
$service = 'hotwatch-agent'

Confirm-Step "Install $Binary to $exe"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Copy-Item -LiteralPath $Binary -Destination $exe -Force

if (Test-Path -LiteralPath $config) {
	Write-Host "$config already exists; keeping it (rotate with: hotwatch-agent init --mode $Mode --rotate)."
} else {
	Confirm-Step "Write $config for mode $Mode (folder and file readable only by SYSTEM and Administrators) and print the access token (shown once, stored only as a hash)"
	& $exe init --mode $Mode
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

if ($Mode -eq 'tailscale') {
	$listen = (Get-Content -Raw -LiteralPath $config | ConvertFrom-Json).listen
	$ip, $listenPort = $listen -split ':'
	$rule = 'Hotwatch agent (Tailscale)'
	if (Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue) {
		Write-Host "Firewall rule '$rule' already exists."
	} else {
		Confirm-Step "Allow inbound TCP $listenPort on $ip from the tailnet only (100.64.0.0/10) in Windows Firewall"
		New-NetFirewallRule -DisplayName $rule -Direction Inbound -Action Allow -Protocol TCP `
			-LocalAddress $ip -LocalPort $listenPort -RemoteAddress 100.64.0.0/10 | Out-Null
	}
}

if (Get-Service -Name $service -ErrorAction SilentlyContinue) {
	Confirm-Step "Restart the existing service $service"
	Restart-Service -Name $service
} else {
	Confirm-Step "Create the service $service (LocalSystem, starts automatically, restarts after a failure) and start it"
	New-Service -Name $service -DisplayName 'Hotwatch agent' -StartupType Automatic `
		-Description 'Serves this host''s metrics, processes and containers to the Hotwatch app.' `
		-BinaryPathName ('"{0}" run --config "{1}"' -f $exe, $config) | Out-Null
	& sc.exe failure $service reset= 86400 actions= restart/5000/restart/5000/restart/60000 | Out-Null
	Start-Service -Name $service
}

Write-Host ""
Write-Host "Done. Check with: Get-Service $service; logs in $dataDir\agent.log"
