[CmdletBinding()]
param(
  [string]$Root = "$env:LOCALAPPDATA\laowangbot",
  [ValidatePattern('^[A-Za-z0-9_][A-Za-z0-9_.-]*$')][string]$ServiceName = 'laowangbot',
  [string]$Version = 'latest',
  [string]$Binary,
  [string]$Migrate,
  [switch]$Wizard,
  [switch]$NonInteractive,
  [ValidateSet('auto','mibot-lite','mibox','telebox')][string]$From = 'auto',
  [string]$Restore,
  [switch]$NoService,
  [switch]$Rollback
)
$ErrorActionPreference = 'Stop'
$UseColor = [Environment]::GetEnvironmentVariable('NO_COLOR') -eq $null -and ![Console]::IsOutputRedirected
function Write-Status([string]$Text, [ConsoleColor]$Color = 'Cyan') {
  if ($UseColor) { Write-Host $Text -ForegroundColor $Color } else { Write-Host $Text }
}
function Read-Choice([string]$Prompt) { Write-Status $Prompt Cyan; return (Read-Host) }
trap { Write-Status $_.Exception.Message Red; break }
$Relogin = $false
$Interactive = !$NonInteractive -and ![Console]::IsInputRedirected
if ($Wizard -and $NonInteractive) { throw '-Wizard cannot be combined with -NonInteractive' }
if ($Interactive -and !$Wizard -and !$Migrate -and !$Restore -and !$Rollback) {
  Write-Status "请选择安装方式：`n1) 全新安装`n2) 从旧部署迁移`n0) 取消"
  switch (Read-Choice '请选择 [1-2]：') {
    '1' { }
    '2' { $Wizard = $true }
    default { throw '已取消，未更改部署' }
  }
}
if ($Wizard) {
  if ($Migrate -or $Restore -or $Rollback) { throw '-Wizard cannot be combined with -Migrate, -Restore or -Rollback' }
  Write-Status "迁移到 laowangbot：无需手动复制配置、会话和数据。" Cyan
  Write-Status "1) mibot-lite`n2) MiBox`n3) TeleBox" Green
  Write-Status "0) 取消" Yellow
  switch (Read-Choice '请选择旧人形 [1-3]：') {
    '1' { $From = 'mibot-lite' }; '2' { $From = 'mibox' }; '3' { $From = 'telebox' }
    default { throw '已取消，未更改旧部署' }
  }
  $Migrate = Read-Choice '旧部署目录（包含 config.json）：'
  if (!$Migrate) { throw '旧目录不能为空，迁移已取消' }
}
if ($Migrate) {
  $Migrate = (Resolve-Path -LiteralPath $Migrate -ErrorAction Stop).Path
  if (!(Test-Path -LiteralPath (Join-Path $Migrate 'config.json') -PathType Leaf)) { throw '旧部署缺少 config.json' }
}
$Root = [IO.Path]::GetFullPath($Root)
if ($Interactive -and !$Migrate -and !$Restore -and !$Rollback -and
    ((Test-Path -LiteralPath (Join-Path $Root 'config.json')) -or (Test-Path -LiteralPath (Join-Path $Root 'gotd-session.json')))) {
  Write-Status "检测到目标目录已有登录信息。`n1) 新目录多开（保留当前账号）`n2) 覆盖登录（保留插件数据）`n0) 取消"
  switch (Read-Choice '请选择 [1-2]：') {
    '1' {
      $parent = Split-Path -Parent $Root
      $number = 2
      $suggested = Join-Path $parent "account-$number"
      while (Test-Path -LiteralPath $suggested) { $number++; $suggested = Join-Path $parent "account-$number" }
      $answer = Read-Choice "新账号目录 [$suggested]："
      if (!$answer) { $answer = $suggested }
      $Root = [IO.Path]::GetFullPath($answer)
      if ((Test-Path -LiteralPath $Root) -and (!(Test-Path -LiteralPath $Root -PathType Container) -or (Get-ChildItem -LiteralPath $Root -Force | Select-Object -First 1))) { throw '新账号目录必须不存在或为空' }
      $suggestedService = 'laowangbot-' + ((Split-Path -Leaf $Root) -replace '[^A-Za-z0-9_.-]', '-')
      $answer = Read-Choice "新账号服务名 [$suggestedService]："
      $ServiceName = if ($answer) { $answer } else { $suggestedService }
    }
    '2' { $Relogin = $true }
    default { throw '已取消，未更改部署' }
  }
}
if ($ServiceName -notmatch '^[A-Za-z0-9_][A-Za-z0-9_.-]*$') { throw 'Invalid service name' }
if ($Relogin -and (Test-Path -LiteralPath (Join-Path $Root '.compiled/pending'))) {
  throw '存在未完成的源码更新，请先使用原部署程序恢复该事务，再覆盖登录。'
}
# Check before downloading, creating temporary files, or touching scheduled tasks.
$compiled = Test-Path -LiteralPath (Join-Path $Root '.compiled/current.json')
$pluginDir = Join-Path $Root 'plugins'
$hasPlugins = (Test-Path -LiteralPath $pluginDir -PathType Container) -and (Get-ChildItem -LiteralPath $pluginDir -Force | Select-Object -First 1)
if ($compiled -and $Rollback) { throw '源码部署不能单独回滚二进制。Windows 不支持源码自动替换或回滚；请停止服务后，手动恢复二进制及匹配的源码快照。' }
if ((!$Relogin -or !(Test-Path -LiteralPath (Join-Path $Root 'laowangbot.exe') -PathType Leaf)) -and ($compiled -or $hasPlugins)) { throw '部署包含插件或源码构建记录，拒绝覆盖。Windows 不支持源码自动构建替换（停止服务后的 CLI 也不支持）；请在外部使用 Go 源码构建，停止服务后手动替换二进制及匹配的源码快照。' }
if ($Root.Contains('"')) { throw 'Root cannot contain quotes' }
if ($Migrate -and $Restore) { throw 'Choose migration or restore' }
if (($Migrate -or $Restore) -and (Test-Path $Root) -and (Get-ChildItem -Force $Root | Select-Object -First 1)) { throw 'Migration/restore destination must be empty' }
if ($Wizard) {
  Write-Status "旧目录：$Migrate`n新目录：$Root" Cyan
  Write-Status "旧目录保留，未知插件归档并列入报告。" Yellow
  Write-Status '请确认旧人形已停止（计划任务、Docker、PM2 等）。' Yellow
  Write-Status '1) 已停止，继续迁移' Green
  Write-Status '0) 取消（默认）' Yellow
  if ((Read-Choice '请选择 [0-1，回车默认 0]：') -notin @('1','y','Y')) { throw '已取消，请先停止旧实例' }
}
$exe = Join-Path $Root 'laowangbot.exe'
$previous = Join-Path $Root 'laowangbot.previous.exe'
$TaskName = $ServiceName
if ($ServiceName.EndsWith('.service', [StringComparison]::OrdinalIgnoreCase)) { throw 'Service name must omit the .service suffix' }
$work = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory $work | Out-Null
$new = Join-Path $work 'new.exe'
$changed = $false; $stopped = $false; $registered = $false; $hadBinary = Test-Path $exe
$accountChanged = $false; $brandChanged = $false; $startAttempted = $false
$accountFiles = @('config.json', 'gotd-session.json', 'updates.json')
$brandPath = Join-Path $Root 'data/brand.json'
$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
$oldTaskXml = $null
function Invoke-Bot([string]$Path, [string[]]$BotArgs) {
  & $Path @BotArgs
  if ($LASTEXITCODE -ne 0) { throw "laowangbot failed with exit code $LASTEXITCODE" }
}
function Test-TaskUsesRoot([object]$Candidate, [string]$Executable, [string]$TargetRoot) {
  $rootPattern = '(?i)(^|\s)--root\s+"?' + [regex]::Escape($TargetRoot) + '"?(\s|$)'
  foreach ($action in @($Candidate.Actions)) {
    if ([string]::Equals([string]$action.Execute, $Executable, [StringComparison]::OrdinalIgnoreCase) -and [string]$action.Arguments -match $rootPattern) {
      return $true
    }
  }
  return $false
}
foreach ($candidate in @(Get-ScheduledTask -ErrorAction SilentlyContinue)) {
  if ($candidate.TaskName -eq $TaskName) { continue }
  if (Test-TaskUsesRoot $candidate $exe $Root) {
    throw "Existing task $($candidate.TaskName) already points at $Root; use that task name or stop/remove it first"
  }
}
try {
  if ($task) {
    if ($task.Actions.Execute -ne $exe) { throw 'Existing task points at another root' }
    $oldTaskXml = Export-ScheduledTask -TaskName $TaskName
  }
  if ($Relogin -and $hadBinary) {
    Copy-Item -LiteralPath $exe -Destination $new
  } elseif ($Rollback) {
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
  if ($Relogin -and $task -and $task.State -eq 'Running') {
    Stop-ScheduledTask -TaskName $TaskName; $stopped = $true
    for ($i = 0; $i -lt 30; $i++) {
      if ((Get-ScheduledTask -TaskName $TaskName).State -ne 'Running') { break }
      Start-Sleep -Seconds 1
    }
    if ((Get-ScheduledTask -TaskName $TaskName).State -eq 'Running') { throw 'Task did not stop; account unchanged' }
  }
  if ($Relogin -or !(Test-Path -LiteralPath (Join-Path $Root 'config.json'))) {
    if ($NonInteractive) { throw 'Login required: run the installer interactively, or migrate an existing account' }
    if (Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe }) { throw 'Executable is still running; stop it before replacing the account' }
    New-Item -ItemType Directory (Join-Path $work 'account') | Out-Null
    foreach ($file in $accountFiles) {
      $path = Join-Path $Root $file
      if (Test-Path -LiteralPath $path) { Copy-Item -LiteralPath $path -Destination (Join-Path $work "account/$file") }
    }
    $accountChanged = $true
    if ($Relogin) { Invoke-Bot $new @('--login','--force','--root',$Root) }
    else { Invoke-Bot $new @('--login','--root',$Root) }
    Remove-Item -LiteralPath (Join-Path $Root 'updates.json') -Force -ErrorAction SilentlyContinue
  }
  Invoke-Bot $new @('--check','--root',$Root)
  if ($Interactive -and !$Rollback) {
    $brand = Read-Choice '机器人显示名称（最多 32 个字符，回车保留当前名称；新安装默认 laowangbot）：'
    if ($brand) {
      $brand = $brand.Trim()
      if (!$brand) { throw '机器人名称不能为空白' }
      if ($brand -match '[\x00-\x1f\x7f]') { throw '机器人名称不能包含控制字符' }
      # Count Unicode code points, including supplementary characters, like the rename plugin.
      $brand = (([regex]::Matches($brand, '[\uD800-\uDBFF][\uDC00-\uDFFF]|[\s\S]') | Select-Object -First 32 | ForEach-Object { $_.Value }) -join '').Trim()
      New-Item -ItemType Directory -Force (Join-Path $Root 'data') | Out-Null
      if (Test-Path -LiteralPath $brandPath) { Copy-Item -LiteralPath $brandPath -Destination (Join-Path $work 'brand.json') }
      $brandChanged = $true
      $document = @{ name = $brand } | ConvertTo-Json -Compress
      $temporaryBrand = Join-Path $Root 'data/.brand-install.json'
      [IO.File]::WriteAllText($temporaryBrand, $document + "`n", (New-Object Text.UTF8Encoding($false)))
      Move-Item -LiteralPath $temporaryBrand -Destination $brandPath -Force
    }
  }
  if ($Relogin -and $hadBinary) {
    if ($task -and (!$NoService -or $stopped)) { $startAttempted = $true; Start-ScheduledTask -TaskName $TaskName }
    Write-Status "账号登录已更新：$Root；插件数据已保留。" Green
    Write-Status '请在新账号的 Telegram 收藏夹执行 .ping 确认连接。' Yellow
    return
  }
  if ($task -and $task.State -eq 'Running') {
    Stop-ScheduledTask -TaskName $TaskName; $stopped = $true
    for ($i = 0; $i -lt 30; $i++) {
      if ((Get-ScheduledTask -TaskName $TaskName).State -ne 'Running') { break }
      Start-Sleep -Seconds 1
    }
    if ((Get-ScheduledTask -TaskName $TaskName).State -eq 'Running') { throw 'Task did not stop; binary unchanged' }
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
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
    $registered = $true
    Start-ScheduledTask -TaskName $TaskName
  } elseif ($stopped) { Start-ScheduledTask -TaskName $TaskName }
  if ($hadBinary) { Copy-Item (Join-Path $work 'old.exe') $previous -Force }
  if ($Migrate) { Write-Status "旧部署保留：$Migrate；迁移报告：$(Join-Path $Root 'migration-report.json')" Green }
  Write-Status "安装完成：$exe" Green
  Write-Status "本地配置检查通过；请在 Telegram 执行 .ping 确认连接。" Yellow
} catch {
  $failure = $_
  if ($startAttempted) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    for ($i = 0; $i -lt 30; $i++) {
      if (!(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe })) { break }
      Start-Sleep -Seconds 1
    }
    if (Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $exe }) {
      Copy-Item -LiteralPath $work -Destination (Join-Path $Root '.install-recovery') -Recurse -Force
      throw '无法确认服务已停止；恢复文件保存在 .install-recovery，请手动检查账号。'
    }
  }
  if ($changed) {
    if ($registered) {
      Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
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
    if ($oldTaskXml) { Register-ScheduledTask -TaskName $TaskName -Xml $oldTaskXml -Force | Out-Null } else { Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false }
  }
  if ($accountChanged) {
    foreach ($file in $accountFiles) {
      $snapshot = Join-Path $work "account/$file"
      $path = Join-Path $Root $file
      if (Test-Path -LiteralPath $snapshot) { Copy-Item -LiteralPath $snapshot -Destination $path -Force }
      else { Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue }
    }
  }
  if ($brandChanged) {
    $snapshot = Join-Path $work 'brand.json'
    if (Test-Path -LiteralPath $snapshot) { Copy-Item -LiteralPath $snapshot -Destination $brandPath -Force }
    else { Remove-Item -LiteralPath $brandPath -Force -ErrorAction SilentlyContinue }
  }
  if ($stopped) { Start-ScheduledTask -TaskName $TaskName }
  throw $failure
} finally { Remove-Item $work -Recurse -Force }
