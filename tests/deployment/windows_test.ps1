$ErrorActionPreference = 'Stop'
$repo = Split-Path (Split-Path $PSScriptRoot)
$work = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory $work | Out-Null
try {
  $code = @'
package main
import("os";"path/filepath")
func main(){
 args:=os.Args[1:]; mode:=args[0]; root:=""; source:=""
 for i:=1;i<len(args);i++ { if args[i]=="--root" {i++;root=args[i]} else if args[i]=="--from" {i++} else {source=args[i]} }
 if mode=="--migrate" {entries,e:=os.ReadDir(root);if e!=nil||len(entries)!=0{os.Exit(1)}; b,e:=os.ReadFile(filepath.Join(source,"config.json"));if e!=nil{os.Exit(1)};if os.WriteFile(filepath.Join(root,"config.json"),b,0600)!=nil{os.Exit(1)}} else if mode=="--check" {if _,e:=os.Stat(filepath.Join(root,"config.json"));e!=nil{os.Exit(1)};if _,e:=os.Stat(filepath.Join(root,"fail-check"));e==nil{os.Exit(1)}} else {os.Exit(1)}
}
'@
  $sourceFile = Join-Path $work 'fixture.go'
  [IO.File]::WriteAllText($sourceFile,$code)
  $fixture = Join-Path $work 'fixture.exe'
  & go build -o $fixture $sourceFile
  if ($LASTEXITCODE -ne 0) { throw 'Fixture build failed' }
  $source = Join-Path $work 'source'; $root = Join-Path $work 'root'
  New-Item -ItemType Directory $source | Out-Null
  Set-Content (Join-Path $source 'config.json') '{"session":"fixture"}'
  $installer = Join-Path $repo 'scripts/install.ps1'
  & $installer -NoService -Binary $fixture -Migrate $source -Root $root
  if (!(Test-Path (Join-Path $root 'laowangbot.exe'))) { throw 'Missing installed executable' }
  $rejected = $false
  try { & $installer -NoService -Binary $fixture -Migrate $source -Root $root } catch { $rejected = $true }
  if (!$rejected) { throw 'Occupied migration accepted' }
  & $installer -NoService -Binary $fixture -Root $root
  if (!(Test-Path (Join-Path $root 'laowangbot.previous.exe'))) { throw 'Missing previous binary' }
  & $installer -NoService -Rollback -Root $root
  New-Item (Join-Path $root 'fail-check') | Out-Null
  $before = (Get-FileHash (Join-Path $root 'laowangbot.exe')).Hash
  $rejected = $false
  try { & $installer -NoService -Binary $fixture -Root $root } catch { $rejected = $true }
  if (!$rejected) { throw 'Failed check accepted' }
  if ((Get-FileHash (Join-Path $root 'laowangbot.exe')).Hash -ne $before) { throw 'Failed check changed binary' }
  Write-Host 'Windows offline migration/update/rollback tests passed'
} finally { Remove-Item $work -Recurse -Force }
