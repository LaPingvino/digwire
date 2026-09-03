@echo off
setlocal enabledelayedexpansion

echo ========================================================
echo   Digwire BitTorrent Client - Windows Quick Installer
echo ========================================================
echo.

set "INSTALL_DIR=%LOCALAPPDATA%\Digwire"
set "BIN_SOURCE=%~dp0digwire.exe"
set "ICO_SOURCE=%~dp0digwire.ico"

if not exist "!BIN_SOURCE!" (
    echo [ERROR] digwire.exe was not found in the installer directory.
    pause
    exit /b 1
)

echo [*] Creating application directory: !INSTALL_DIR!
if not exist "!INSTALL_DIR!" mkdir "!INSTALL_DIR!"

echo [*] Copying Digwire binaries and icon...
copy /Y "!BIN_SOURCE!" "!INSTALL_DIR!\digwire.exe" >nul
if exist "!ICO_SOURCE!" (
    copy /Y "!ICO_SOURCE!" "!INSTALL_DIR!\digwire.ico" >nul
)

echo [*] Creating Desktop and Start Menu shortcuts...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$ws = New-Object -ComObject WScript.Shell; $s1 = $ws.CreateShortcut([System.IO.Path]::Combine([Environment]::GetFolderPath('Desktop'), 'Digwire.lnk')); $s1.TargetPath = [System.IO.Path]::Combine($env:LOCALAPPDATA, 'Digwire', 'digwire.exe'); if (Test-Path ([System.IO.Path]::Combine($env:LOCALAPPDATA, 'Digwire', 'digwire.ico'))) { $s1.IconLocation = [System.IO.Path]::Combine($env:LOCALAPPDATA, 'Digwire', 'digwire.ico') }; $s1.Save(); $programs = [System.IO.Path]::Combine([Environment]::GetFolderPath('Programs'), 'Digwire'); if (!(Test-Path $programs)) { New-Item -ItemType Directory -Path $programs | Out-Null }; $s2 = $ws.CreateShortcut([System.IO.Path]::Combine($programs, 'Digwire.lnk')); $s2.TargetPath = [System.IO.Path]::Combine($env:LOCALAPPDATA, 'Digwire', 'digwire.exe'); if (Test-Path ([System.IO.Path]::Combine($env:LOCALAPPDATA, 'Digwire', 'digwire.ico'))) { $s2.IconLocation = [System.IO.Path]::Combine($env:LOCALAPPDATA, 'Digwire', 'digwire.ico') }; $s2.Save();"

echo [*] Registering file associations (magnet links and .torrent files)...
reg add "HKCU\Software\Classes\magnet" /ve /t REG_SZ /d "URL:Magnet Protocol" /f >nul
reg add "HKCU\Software\Classes\magnet" /v "URL Protocol" /t REG_SZ /d "" /f >nul
reg add "HKCU\Software\Classes\magnet\DefaultIcon" /ve /t REG_SZ /d "\"!INSTALL_DIR!\digwire.ico\"" /f >nul
reg add "HKCU\Software\Classes\magnet\shell\open\command" /ve /t REG_SZ /d "\"!INSTALL_DIR!\digwire.exe\" \"%%1\"" /f >nul

reg add "HKCU\Software\Classes\.torrent" /ve /t REG_SZ /d "Digwire.Torrent" /f >nul
reg add "HKCU\Software\Classes\Digwire.Torrent" /ve /t REG_SZ /d "BitTorrent Seed File" /f >nul
reg add "HKCU\Software\Classes\Digwire.Torrent\DefaultIcon" /ve /t REG_SZ /d "\"!INSTALL_DIR!\digwire.ico\"" /f >nul
reg add "HKCU\Software\Classes\Digwire.Torrent\shell\open\command" /ve /t REG_SZ /d "\"!INSTALL_DIR!\digwire.exe\" \"%%1\"" /f >nul

echo.
echo ========================================================
echo   Digwire has been successfully installed!
echo ========================================================
echo.
set /p LAUNCH="Do you want to launch Digwire now? (Y/N): "
if /I "!LAUNCH!"=="Y" (
    start "" "!INSTALL_DIR!\digwire.exe"
)
exit /b 0
