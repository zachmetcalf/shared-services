:: Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

@echo off
title zip

set cwd=%~dp0
set projectdir=%cwd%..\..

pushd "%projectdir%"

for %%i in ("%projectdir%") do set projectname=%%~ni

set zipdir=%projectdir%\..
set zipname=%projectname%.zip
set zippath=%zipdir%\%zipname%

if exist "%zippath%" (
	del "%zippath%"
)

powershell -NoProfile -Command "Compress-Archive -Force -Path (Get-ChildItem -LiteralPath '.' -File | Select-Object -ExpandProperty FullName) -DestinationPath '%zippath%'"
powershell -NoProfile -Command "$paths = @('.codex','.github','.vscode','config','docs','infra','scripts','services','source') | Where-Object { Test-Path $_ }; if ($paths.Length -gt 0) { Compress-Archive -Update -Path $paths -DestinationPath '%zippath%' }"

popd

echo zip completed
exit /b 0
