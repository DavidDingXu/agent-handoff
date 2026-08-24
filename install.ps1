param(
    [string]$Repo = $(if ($env:REPO) { $env:REPO } else { "DavidDingXu/agent-handoff" }),
    [string]$Version = $(if ($env:VERSION) { $env:VERSION } else { "latest" }),
    [string]$InstallDir = $(if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\agent-handoff" })
)

$ErrorActionPreference = "Stop"

if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -ne [System.Runtime.InteropServices.Architecture]::X64) {
    throw "Unsupported Windows architecture. The current release supports windows-amd64."
}

if ($Version -eq "latest") {
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $release.tag_name
}
if (-not $Version) {
    throw "Could not resolve the latest release of $Repo"
}

$assetVersion = $Version.TrimStart("v")
$asset = "agent-handoff-$assetVersion-windows-amd64.zip"
$releaseBase = "https://github.com/$Repo/releases/download/$Version"
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("agent-handoff-" + [guid]::NewGuid())

try {
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    $archive = Join-Path $tempDir $asset
    $checksums = Join-Path $tempDir "checksums.txt"

    Write-Host "==> downloading agent-handoff $Version (windows/amd64)"
    Invoke-WebRequest "$releaseBase/$asset" -OutFile $archive
    Invoke-WebRequest "$releaseBase/checksums.txt" -OutFile $checksums

    $line = Get-Content $checksums | Where-Object { $_ -match "\s+$([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $line) {
        throw "Checksum entry missing for $asset"
    }
    $want = ($line -split "\s+")[0].ToLowerInvariant()
    $got = (Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant()
    if ($got -ne $want) {
        throw "Checksum mismatch (want $want, got $got)"
    }
    Write-Host "    checksum ok"

    $expanded = Join-Path $tempDir "expanded"
    Expand-Archive -Path $archive -DestinationPath $expanded
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item (Join-Path $expanded "agent-handoff.exe") (Join-Path $InstallDir "agent-handoff.exe") -Force

    if (($env:PATH -split ";") -notcontains $InstallDir) {
        Write-Host "note: $InstallDir is not on PATH"
    }
    & (Join-Path $InstallDir "agent-handoff.exe") version
}
finally {
    if (Test-Path $tempDir) {
        Remove-Item -Recurse -Force $tempDir
    }
}
