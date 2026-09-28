[CmdletBinding()]
param(
  [string]$Root = "$env:LOCALAPPDATA\laowangbot",
  [string]$Version = 'latest',
  [string]$Binary,
  [string]$Migrate,
  [switch]$Wizard,
  [ValidateSet('auto','mibot-lite','mibox','telebox')][string]$From = 'auto',
  [string]$Restore,
  [switch]$NoService,
  [switch]$Rollback
)
$ErrorActionPreference = 'Stop'
if ($Wizard) {
  if ($Migrate -or $Restore -or $Rollback) { throw '-Wizard cannot be combined with -Migrate, -Restore or -Rollback' }
  Write-Host "迁移到 laowangbot：无需手动复制配置、会话和数据。`n1) mibot-lite`n2) MiBox`n3) TeleBox`n0) 取消"
  switch (Read-Host '请选择旧人形 [1-3]') {
    '1' { $From = 'mibot-lite' }; '2' { $From = 'mibox' }; '3' { $From = 'telebox' }
    default { throw '已取消，未更改旧部署' }
  }
  $Migrate = Read-Host '旧部署目录（包含 config.json）'
  if (!$Migrate) { throw '旧目录不能为空，迁移已取消' }
}
if ($Migrate) {
  $Migrate = (Resolve-Path -LiteralPath $Migrate -ErrorAction Stop).Path
  if (!(Test-Path -LiteralPath (Join-Path $Migrate 'config.json') -PathType Leaf)) { throw '旧部署缺少 config.json' }
}
$Root = [IO.Path]::GetFullPath($Root)
if ($Root.Contains('"')) { throw 'Root cannot contain quotes' }
if ($Migrate -and $Restore) { throw 'Choose migration or restore' }
if (($Migrate -or $Restore) -and (Test-Path $Root) -and (Get-ChildItem -Force $Root | Select-Object -First 1)) { throw 'Migration/restore destination must be empty' }
if ($Wizard) {
  Write-Host "旧目录：$Migrate`n新目录：$Root`n旧目录保留，未知插件归档并列入报告。"
  if ((Read-Host '确认旧人形已停止（计划任务、Docker、PM2 等），输入 y 继续') -notin @('y','Y')) { throw '已取消，请先停止旧实例' }
}
$exe = Join-Path $Root 'laowangbot.exe'
$previous = Join-Path $Root 'laowangbot.previous.exe'
$work = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory $work | Out-Null
$new = Join-Path $work 'new.exe'
$changed = $false; $stopped = $false; $registered = $false; $hadBinary = Test-Path $exe
$task = Get-ScheduledTask -TaskName laowangbot -ErrorAction SilentlyContinue
$oldTaskXml = $null
function Invoke-Bot([string]$Path, [string[]]$BotArgs) {
  & $Path @BotArgs
  if ($LASTEXITCODE -ne 0) { throw "laowangbot failed with exit code $LASTEXITCODE" }
}
try {
  if ($task) {
    if ($task.Actions.Execute -ne $exe) { throw 'Existing task points at another root' }
    $oldTaskXml = Export-ScheduledTask -TaskName laowangbot
  }
  if ($Rollback) {
    if ($Migrate -or $Restore -or $Binary) { throw 'Rollback cannot be combined with migration, restore or binary' }
    Copy-Item $previous $new
  } elseif ($Binary) { Copy-Item $Binary $new } else {
    $arch = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    if ($arch -eq 'x64') { $arch = 'amd64' }
    if ($arch -notin @('amd64','arm64')) { throw 'Unsupported architecture' }
    $asset = "laowangbot-windows-$arch.exe"
    $base = if ($Version -eq 'latest') { 'https://github.com/OrionG-hub/laowangbot/releases/latest/download' } else { "https://github.com/OrionG-hub/laowangbot/releases/download/$Version" }
    Invoke-WebRequest "$base/$asset" -OutFile $new
    $sums = (Invoke-WebRequest "$base/checksums.txt").Content
    $lines = @($sums -split "`n" | Where-Object { $_ -match ('^[a-fA-F0-9]{64}\s+\*?' + [regex]::Escape($asset) + '\s*$') })
    if ($lines.Count -ne 1) { throw 'Missing or ambiguous checksum' }
    if ((Get-FileHash $new -Algorithm SHA256).Hash -ne ($lines[0] -split '\s+')[0]) { throw 'Checksum mismatch' }
  }
  New-Item -ItemType Directory -Force $Root | Out-Null
  if ($Migrate) { Invoke-Bot $new @('--migrate',$Migrate,'--from',$From,'--root',$Root) }
  if ($Restore) { Invoke-Bot $new @('--restore',$Restore,'--root',$Root) }
  if (!(Test-Path (Join-Path $Root 'config.json'))) { Invoke-Bot $new @('--login','--root',$Root) }
  Invoke-Bot $new @('--check','--root',$Root)
  if ($task -and $task.State -eq 'Running') {
    Stop-ScheduledTask -TaskName laowangbot; $stopped = $true
    for ($i = 0; $i -lt 30; $i++) {
      if ((Get-ScheduledTask -TaskName laowangbot).State -ne 'Running') { break }
      Start-Sleep -Seconds 1
    }
    if ((Get-ScheduledTask -TaskName laowangbot).State -eq 'Running') { throw 'Task did not stop; binary unchanged' }
  }
  # Task state alone does not prove a child has exited. Never overwrite a live executable.
  if (Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe }) { throw 'Executable is still running; stop it before updating' }
  if ($hadBinary) { Copy-Item $exe (Join-Path $work 'old.exe') }
  $changed = $true
  Copy-Item $new $exe -Force
  if (!$NoService) {
    $action = New-ScheduledTaskAction -Execute $exe -Argument ('--supervise --root "' + $Root + '"') -WorkingDirectory $Root
    $user = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
    $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName laowangbot -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
    $registered = $true
    Start-ScheduledTask -TaskName laowangbot
  } elseif ($stopped) { Start-ScheduledTask -TaskName laowangbot }
  if ($hadBinary) { Copy-Item (Join-Path $work 'old.exe') $previous -Force }
  if ($Migrate) { Write-Host "旧部署保留：$Migrate；迁移报告：$(Join-Path $Root 'migration-report.json')" }
  Write-Host "Installed $exe; configuration checked, Telegram connectivity not verified."
} catch {
  $failure = $_
  if ($changed) {
    if ($registered) {
      Stop-ScheduledTask -TaskName laowangbot -ErrorAction SilentlyContinue
      for ($i = 0; $i -lt 30; $i++) {
        if (!(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe })) { break }
        Start-Sleep -Seconds 1
      }
      if (Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe }) {
        # Keep a recoverable copy outside temporary storage if Windows still holds the executable.
        if ($hadBinary) { Copy-Item (Join-Path $work 'old.exe') $previous -Force }
        throw 'New executable is still running; automatic rollback cannot replace it. Previous binary is preserved.'
      }
    }
    if ($hadBinary) { Copy-Item (Join-Path $work 'old.exe') $exe -Force } else { Remove-Item $exe -Force }
  }
  if ($registered) {
    if ($oldTaskXml) { Register-ScheduledTask -TaskName laowangbot -Xml $oldTaskXml -Force | Out-Null } else { Unregister-ScheduledTask -TaskName laowangbot -Confirm:$false }
  }
  if ($stopped) { Start-ScheduledTask -TaskName laowangbot }
  throw $failure
} finally { Remove-Item $work -Recurse -Force }
