# Requires Windows PowerShell 5.1+ or PowerShell 7, and Go on PATH.
[CmdletBinding()]
param([string]$GoExecutable = 'go')

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectDir = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $projectDir 'dist'
$packageName = 'Tickets-Local-Windows-x64'
$stageDir = Join-Path $distDir ('.windows-stage-' + [guid]::NewGuid().ToString('N'))
$packageDir = Join-Path $stageDir $packageName
$savedEnvironment = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'Run this script on Windows; use package-release.sh on macOS or Linux.'
}
$go = (Get-Command $GoExecutable -CommandType Application -ErrorAction Stop).Source
Push-Location $projectDir
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & $go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed; no new package was produced.' }
    & $go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed; no new package was produced.' }

    New-Item -ItemType Directory -Path $packageDir -Force | Out-Null
    $executable = Join-Path $packageDir 'Tickets Local.exe'
    & $go build -buildvcs=false -trimpath '-ldflags=-s -w -H=windowsgui' -o $executable .
    if ($LASTEXITCODE -ne 0) { throw 'Windows build failed; no new package was produced.' }
    $versionOutput = (& $executable --version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $versionOutput -notmatch '^Tickets Local (\d+\.\d+\.\d+)$') {
        throw 'Could not verify the built executable version.'
    }
    $version = $Matches[1]
    if ((Get-AuthenticodeSignature -LiteralPath $executable).Status -ne 'NotSigned') {
        throw 'Unexpected signature status; this script produces explicitly unsigned packages only.'
    }

    Copy-Item -LiteralPath 'packaging/PORTABLE-README.txt' -Destination (Join-Path $packageDir 'README.txt')
    Copy-Item -LiteralPath 'LICENSE' -Destination (Join-Path $packageDir 'LICENSE.txt')
    Copy-Item -LiteralPath 'NOTICE.md' -Destination (Join-Path $packageDir 'NOTICE.md')
    Copy-Item -LiteralPath 'THIRD-PARTY-NOTICES.md' -Destination (Join-Path $packageDir 'THIRD-PARTY-NOTICES.md')
    # Always archive a fresh directory containing only these five release files.
    # Never archive the workspace, a previous package directory, or runtime data.
    $zipName = "Tickets-Local-$version-Windows-x64-unsigned.zip"
    $stagedZip = Join-Path $stageDir $zipName
    Compress-Archive -LiteralPath $packageDir -DestinationPath $stagedZip
    $checksum = (Get-FileHash -LiteralPath $stagedZip -Algorithm SHA256).Hash.ToLowerInvariant()
    $checksumName = "$zipName.sha256"
    [IO.File]::WriteAllText((Join-Path $stageDir $checksumName), "$checksum  $zipName`n", [Text.Encoding]::ASCII)

    $publishedDir = Join-Path $distDir $packageName
    New-Item -ItemType Directory -Path $publishedDir -Force | Out-Null
    foreach ($name in @('Tickets Local.exe', 'README.txt', 'LICENSE.txt', 'NOTICE.md', 'THIRD-PARTY-NOTICES.md')) {
        Copy-Item -LiteralPath (Join-Path $packageDir $name) -Destination (Join-Path $publishedDir $name) -Force
    }
    Move-Item -LiteralPath $stagedZip -Destination (Join-Path $distDir $zipName) -Force
    Move-Item -LiteralPath (Join-Path $stageDir $checksumName) -Destination (Join-Path $distDir $checksumName) -Force
    Write-Output "Unsigned Windows x64 GUI package: $(Join-Path $distDir $zipName)"
    Write-Output "SHA-256: $checksum"
    Write-Output 'Signing prerequisite: a trusted Authenticode certificate or Microsoft Artifact Signing profile, plus Windows SDK SignTool.'
}
finally {
    Pop-Location
    foreach ($name in $savedEnvironment.Keys) {
        if ($null -eq $savedEnvironment[$name]) {
            Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
        } else {
            [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
        }
    }
    # Only remove the unique staging directory allocated by this invocation.
    $resolvedStage = [IO.Path]::GetFullPath($stageDir)
    $expectedParent = [IO.Path]::GetFullPath($distDir)
    if ([IO.Path]::GetDirectoryName($resolvedStage) -ne $expectedParent -or
        [IO.Path]::GetFileName($resolvedStage) -notmatch '^\.windows-stage-[a-f0-9]{32}$') {
        throw 'Refusing to remove a staging directory outside dist.'
    }
    if (Test-Path -LiteralPath $resolvedStage) {
        Remove-Item -LiteralPath $resolvedStage -Recurse -Force
    }
}
