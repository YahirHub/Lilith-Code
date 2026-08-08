@echo off
setlocal
set "INSTALLER_URL=https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/scripts/install.ps1"
set "TMP_PS1=%TEMP%\lilith-install-%RANDOM%-%RANDOM%.ps1"

powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -Command "Invoke-WebRequest -UseBasicParsing -Uri '%INSTALLER_URL%' -OutFile '%TMP_PS1%'"
if errorlevel 1 goto :error
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%TMP_PS1%" %*
if errorlevel 1 goto :error

del /q "%TMP_PS1%" >nul 2>&1
endlocal & set "PATH=%LOCALAPPDATA%\Programs\Lilith\bin;%PATH%"
li version
exit /b 0

:error
set "CODE=%ERRORLEVEL%"
del /q "%TMP_PS1%" >nul 2>&1
endlocal & exit /b %CODE%
