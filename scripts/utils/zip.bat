:: Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

@echo off
title zip
setlocal

set cwd=%~dp0
set projectdir=%cwd%..\..

for %%i in ("%projectdir%") do set projectname=%%~ni

set zipdir=%projectdir%\..
set zipname=%projectname%.zip
set zippath=%zipdir%\%zipname%

powershell -NoProfile -Command ^
	"$ErrorActionPreference = 'Stop';" ^
	"$paths = @(Get-ChildItem -LiteralPath $env:projectdir -File | Select-Object -ExpandProperty FullName);" ^
	"$paths += @('.github','.vscode','infra','scripts','services') | ForEach-Object { Join-Path $env:projectdir $_ } | Where-Object { Test-Path -LiteralPath $_ };" ^
	"Compress-Archive -Force -LiteralPath $paths -DestinationPath $env:zippath"
if errorlevel 1 exit /b 1

echo zip completed
exit /b 0
