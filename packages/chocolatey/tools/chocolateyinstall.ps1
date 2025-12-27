$ErrorActionPreference = 'Stop'

$packageName = 'terminal-tunnel'
$version = '1.4.1'

# Determine architecture
if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
        $arch = 'arm64'
    } else {
        $arch = 'amd64'
    }
} else {
    throw "32-bit Windows is not supported"
}

$url = "https://github.com/artpar/terminal-tunnel/releases/download/v$version/tt-windows-$arch.zip"

$packageArgs = @{
    packageName   = $packageName
    unzipLocation = "$(Split-Path -Parent $MyInvocation.MyCommand.Definition)"
    url64bit      = $url
    checksum64    = 'CCD42FFB33CE4111D38E8CE70ADA1736AED2DE9BC10438629E4250A2006CBF98'
    checksumType64= 'sha256'
}

Install-ChocolateyZipPackage @packageArgs
