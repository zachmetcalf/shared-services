:: Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

@echo off
title standards
setlocal

set cwd=%~dp0
set projectdir=%cwd%..\..
set tempdir=%projectdir%\.standards

if exist "%tempdir%" (
	rmdir /s /q "%tempdir%"
	if errorlevel 1 exit /b 1
)

git clone --branch main https://github.com/zachmetcalf/zachmetcalf "%tempdir%"
set result=%errorlevel%
if not "%result%"=="0" goto Cleanup

call "%tempdir%\scripts\standards\standards.bat" "%projectdir%"
set result=%errorlevel%

:Cleanup
if exist "%tempdir%" (
	rmdir /s /q "%tempdir%"
	if errorlevel 1 if "%result%"=="0" set result=1
)
if not "%result%"=="0" exit /b %result%

echo standards completed
exit /b 0
