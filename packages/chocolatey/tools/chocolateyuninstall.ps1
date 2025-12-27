$ErrorActionPreference = 'Stop'

$packageName = 'terminal-tunnel'
$softwareName = 'tt*'

$uninstalled = $false
[array]$key = Get-UninstallRegistryKey -SoftwareName $softwareName

if ($key.Count -eq 1) {
    $key | ForEach-Object {
        $file = "$($_.UninstallString)"
        Uninstall-ChocolateyPackage -PackageName $packageName -FileType 'exe' -SilentArgs '/S' -File "$file"
    }
} elseif ($key.Count -eq 0) {
    Write-Warning "$packageName has already been uninstalled by other means."
} elseif ($key.Count -gt 1) {
    Write-Warning "$($key.Count) matches found!"
    $key | ForEach-Object { Write-Warning "- $($_.DisplayName)" }
}
