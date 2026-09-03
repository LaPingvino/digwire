@echo off
setlocal enabledelayedexpansion

echo ========================================================
echo   Digwire BitTorrent Client - Windows Uninstaller
echo ========================================================
echo.

set "INSTALL_DIR=%LOCALAPPDATA%\Digwire"

taskkill /F /IM digwire.exe >nul 2>&1

echo [*] Removing shortcuts...
del /F /Q "%USERPROFILE%\Desktop\Digwire.lnk" >nul 2>&1
rd /S /Q "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Digwire" >nul 2>&1

echo [*] Removing registry entries...
reg delete "HKCU\Software\Classes\magnet" /f >nul 2>&1
reg delete "HKCU\Software\Classes\Digwire.Torrent" /f >nul 2>&1
reg delete "HKCU\Software\Classes\.torrent" /f >nul 2>&1

echo [*] Removing application files...
rd /S /Q "!INSTALL_DIR!" >nul 2>&1

echo.
echo ========================================================
echo   Digwire has been successfully uninstalled.
echo ========================================================
pause
