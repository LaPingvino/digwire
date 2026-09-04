; Digwire NSIS Installer Script
!define PRODUCT_NAME "Digwire"
!define PRODUCT_VERSION "0.2.4"
!define PRODUCT_PUBLISHER "Digwire Team"
!define PRODUCT_WEB_SITE "https://github.com/LaPingvino/digwire"
!define PRODUCT_DIR_REGKEY "Software\Microsoft\Windows\CurrentVersion\App Paths\digwire.exe"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"
!define PRODUCT_UNINST_ROOT_KEY "HKCU"

SetCompressor /SOLID lzma

!include "MUI2.nsh"

!define MUI_ABORTWARNING
!define MUI_ICON "..\internal\web\embedded\digwire.ico"
!define MUI_UNICON "..\internal\web\embedded\digwire.ico"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\digwire.exe"
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Name "${PRODUCT_NAME} ${PRODUCT_VERSION}"
OutFile "..\dist\windows\Digwire-v${PRODUCT_VERSION}-Setup.exe"
InstallDir "$LOCALAPPDATA\Digwire"
InstallDirRegKey HKCU "${PRODUCT_DIR_REGKEY}" ""
ShowInstDetails show
ShowUnInstDetails show

Section "MainSection" SEC01
  SetOutPath "$INSTDIR"
  SetOverwrite ifnewer
  File "..\dist\windows\digwire.exe"
  File "..\internal\web\embedded\digwire.ico"
  
  CreateDirectory "$SMPROGRAMS\Digwire"
  CreateShortcut "$SMPROGRAMS\Digwire\Digwire.lnk" "$INSTDIR\digwire.exe" "" "$INSTDIR\digwire.ico"
  CreateShortcut "$DESKTOP\Digwire.lnk" "$INSTDIR\digwire.exe" "" "$INSTDIR\digwire.ico"
  CreateShortcut "$SMPROGRAMS\Digwire\Uninstall.lnk" "$INSTDIR\uninst.exe"

  ; File & Protocol Associations
  WriteRegStr HKCU "Software\Classes\magnet" "" "URL:Magnet Protocol"
  WriteRegStr HKCU "Software\Classes\magnet" "URL Protocol" ""
  WriteRegStr HKCU "Software\Classes\magnet\DefaultIcon" "" "$INSTDIR\digwire.ico"
  WriteRegStr HKCU "Software\Classes\magnet\shell\open\command" "" '"$INSTDIR\digwire.exe" "%1"'

  WriteRegStr HKCU "Software\Classes\.torrent" "" "Digwire.Torrent"
  WriteRegStr HKCU "Software\Classes\Digwire.Torrent" "" "BitTorrent Seed File"
  WriteRegStr HKCU "Software\Classes\Digwire.Torrent\DefaultIcon" "" "$INSTDIR\digwire.ico"
  WriteRegStr HKCU "Software\Classes\Digwire.Torrent\shell\open\command" "" '"$INSTDIR\digwire.exe" "%1"'
SectionEnd

Section -Post
  WriteUninstaller "$INSTDIR\uninst.exe"
  WriteRegStr HKCU "${PRODUCT_DIR_REGKEY}" "" "$INSTDIR\digwire.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayName" "$(^Name)"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString" "$INSTDIR\uninst.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayIcon" "$INSTDIR\digwire.ico"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
SectionEnd

Function un.onUninstSuccess
  HideWindow
  MessageBox MB_ICONINFORMATION|MB_OK "$(^Name) was successfully removed from your computer."
FunctionEnd

Section Uninstall
  Delete "$INSTDIR\digwire.exe"
  Delete "$INSTDIR\digwire.ico"
  Delete "$INSTDIR\uninst.exe"

  Delete "$SMPROGRAMS\Digwire\Digwire.lnk"
  Delete "$SMPROGRAMS\Digwire\Uninstall.lnk"
  Delete "$DESKTOP\Digwire.lnk"
  RMDir "$SMPROGRAMS\Digwire"
  RMDir "$INSTDIR"

  DeleteRegKey ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}"
  DeleteRegKey HKCU "${PRODUCT_DIR_REGKEY}"
  DeleteRegKey HKCU "Software\Classes\magnet"
  DeleteRegKey HKCU "Software\Classes\Digwire.Torrent"
  DeleteRegKey HKCU "Software\Classes\.torrent"
  
  SetAutoClose true
SectionEnd
