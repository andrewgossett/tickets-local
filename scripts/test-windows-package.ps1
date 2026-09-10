# Integration checks for the real unsigned release, plus a failed-tool run.
[CmdletBinding()]
param([string]$GoExecutable = 'go')

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectDir = Split-Path -Parent $PSScriptRoot
$before = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $before[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
function Assert-BuildEnvironmentRestored {
    foreach ($name in $before.Keys) {
        if ([Environment]::GetEnvironmentVariable($name, 'Process') -cne $before[$name]) {
            throw "Build environment not restored: $name"
        }
    }
}

& (Join-Path $PSScriptRoot 'package-windows.ps1') -GoExecutable $GoExecutable
Assert-BuildEnvironmentRestored
$executable = Join-Path $projectDir 'dist/Tickets-Local-Windows-x64/Tickets Local.exe'
$versionText = (& $executable --version | Out-String).Trim()
if ($versionText -notmatch '^Tickets Local (\d+\.\d+\.\d+)$') { throw 'Invalid executable version' }
$zipName = "Tickets-Local-$($Matches[1])-Windows-x64-unsigned.zip"
$zipPath = Join-Path $projectDir "dist/$zipName"
$checksumPath = "$zipPath.sha256"
$hash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
if ([IO.File]::ReadAllText($checksumPath).Trim() -cne "$hash  $zipName") { throw 'ZIP checksum mismatch' }
if ((Get-AuthenticodeSignature -LiteralPath $executable).Status -ne 'NotSigned') { throw 'Expected unsigned executable' }

$bytes = [IO.File]::ReadAllBytes($executable)
$peOffset = [BitConverter]::ToInt32($bytes, 60)
if ([BitConverter]::ToUInt16($bytes, $peOffset + 4) -ne 0x8664 -or
    [BitConverter]::ToUInt16($bytes, $peOffset + 24 + 68) -ne 2) {
    throw 'Expected an x64 Windows GUI executable'
}
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [IO.Compression.ZipFile]::OpenRead($zipPath)
try {
    $expected = @('LICENSE.txt', 'NOTICE.md', 'README.txt', 'THIRD-PARTY-NOTICES.md', 'Tickets Local.exe') |
        ForEach-Object { "Tickets-Local-Windows-x64/$_" }
    $actual = @($zip.Entries | ForEach-Object { $_.FullName.Replace('\', '/') })
    if (Compare-Object $expected $actual) { throw 'Unexpected file set in Windows ZIP' }
} finally {
    $zip.Dispose()
}

# Windows whoami.exe rejects Go's test arguments with a nonzero exit before
# reporting identity. Unlike where.exe, this cannot succeed by finding test.exe
# on the runner's PATH. No shell fixture or extra compiler is required.
$failed = $false
try {
    & (Join-Path $PSScriptRoot 'package-windows.ps1') -GoExecutable (Join-Path $env:SystemRoot 'System32/whoami.exe')
} catch {
    if ($_.Exception.Message -notlike '*Go tests failed*') { throw }
    $failed = $true
}
if (!$failed) { throw 'A failing test command was accepted' }
Assert-BuildEnvironmentRestored
if ((Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash) {
    throw 'A failed build changed the release ZIP'
}
if ([IO.File]::ReadAllText($checksumPath).Trim() -cne "$hash  $zipName") { throw 'A failed build changed the checksum' }
Write-Output 'PASS: x64 GUI executable, unsigned signature, notices, exact ZIP allowlist, SHA-256, failure guard, and environment restoration.'
exit 0
