@echo off
setlocal
cd /d "%~dp0.."

rem VERSION goes into the linker flags, so every line of it must be plain MAJOR.MINOR.PATCH.
findstr /v /r /x /c:"[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*" VERSION >nul 2>&1
if not errorlevel 1 (
  echo error: VERSION must look like 1.2.3
  exit /b 1
)
set "VER="
set /p VER=<VERSION
if not defined VER (
  echo error: VERSION is empty
  exit /b 1
)

where go >nul 2>&1
if errorlevel 1 (
  echo error: go is not installed. Install it from https://go.dev/dl/
  exit /b 1
)
where node >nul 2>&1
if errorlevel 1 (
  echo error: node is not installed. Install it from https://nodejs.org/
  exit /b 1
)
where pnpm >nul 2>&1
if errorlevel 1 (
  echo error: pnpm is not installed. Run corepack enable or see https://pnpm.io/installation
  exit /b 1
)

powershell -NoProfile -Command "$want=[version](Select-String -Path server/go.mod -Pattern '^go\s+(\S+)').Matches[0].Groups[1].Value; $have=[version]((go env GOVERSION) -replace '^go','' -replace '[^0-9.].*',''); if ($have -lt $want) { Write-Host \"error: Go $want or newer is required, found $have\"; exit 1 }"
if errorlevel 1 exit /b 1
powershell -NoProfile -Command "$want=[int]((Get-Content .nvmrc -Raw).Trim().TrimStart('v').Split('.')[0]); $have=[int]((node --version).TrimStart('v').Split('.')[0]); if ($have -lt $want) { Write-Host \"error: Node $want or newer is required, found $have\"; exit 1 }"
if errorlevel 1 exit /b 1

set "HASH="
for /f "usebackq delims=" %%h in (`powershell -NoProfile -Command "$n=(Get-Location).Path.Length+1; $f=@('web','server','api') | ForEach-Object { Get-ChildItem $_ -Recurse -File } | Where-Object { $p=$_.FullName.Substring($n).Replace('\','/'); $p -notmatch '(^|/)(node_modules|\.next)/' -and $p -notmatch '^web/(out/|next-env\.d\.ts$|tsconfig\.tsbuildinfo$)' -and $p -notmatch '^server/web/' }; $f=@($f)+@('VERSION','package.json','pnpm-workspace.yaml','pnpm-lock.yaml','go.work','.nvmrc' | ForEach-Object { Get-Item $_ }); $s=($f | Sort-Object FullName | ForEach-Object { $_.FullName.Substring($n)+':'+(Get-FileHash $_.FullName -Algorithm SHA256).Hash }) -join ';'; [BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($s))).Replace('-','')"`) do set "HASH=%%h"
if not defined HASH (
  echo error: could not hash the sources
  exit /b 1
)

set "OLD="
if exist .build-hash set /p OLD=<.build-hash
if "%HASH%"=="%OLD%" if exist bin\prdash.exe (
  echo ==^> sources unchanged, skipping build
  goto run
)

echo ==^> pnpm install
call pnpm install --frozen-lockfile
if errorlevel 1 exit /b 1
echo ==^> web build
call pnpm -C web build
if errorlevel 1 exit /b 1
echo ==^> embed web build
for /d %%d in (server\web\*) do rmdir /s /q "%%d"
for %%f in (server\web\*) do if /i not "%%~nxf"==".gitkeep" del /q "%%f"
xcopy /e /i /y /q web\out server\web >nul
if errorlevel 1 exit /b 1
echo ==^> go build
go build -trimpath -ldflags "-X main.version=%VER%" -o bin\prdash.exe .\server\cmd\prdash
if errorlevel 1 exit /b 1
>.build-hash echo %HASH%

:run
bin\prdash.exe %*
exit /b %errorlevel%
