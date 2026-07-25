; TinyPlay's per-user Windows installer.
; Build from the repository root with:
;   ISCC.exe /DMyAppVersion=1.2.3 windows\TinyPlay.iss
;
; Keep AppId stable forever: Inno Setup uses it to recognise an existing
; installation, retain its selected directory, and perform an in-place upgrade.

#define MyAppName "TinyPlay"
#define MyAppPublisher "TinyPlay"
#define MyAppURL "https://github.com/YangHanqing/tinyplay"
#ifndef MyAppVersion
  #define MyAppVersion "0.0.0-dev"
#endif

[Setup]
AppId={{5E57C0C8-6E39-4E50-9740-CC4F58CD9D8A}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/issues
AppUpdatesURL={#MyAppURL}/releases/latest
DefaultDirName={localappdata}\Programs\{#MyAppName}
DisableProgramGroupPage=yes
DisableWelcomePage=no
OutputDir=..\dist
OutputBaseFilename=TinyPlay-Setup-x64
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\TinyPlay.exe
ArchitecturesAllowed=x64os
ArchitecturesInstallIn64BitMode=x64os
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
CloseApplications=yes
RestartApplications=no
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern

[Languages]
; English only for now: GitHub's windows-latest Inno Setup install does not
; ship Languages\ChineseSimplified.isl, so referencing it fails the release
; job. Re-add a Chinese messages file (vendored under windows/) when we want
; a dual-language installer again.
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "..\dist\TinyPlay.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\mpv\*"; DestDir: "{app}\mpv"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\dist\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\TinyPlay.exe"; WorkingDir: "{app}"; IconFilename: "{app}\TinyPlay.exe"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\TinyPlay.exe"; WorkingDir: "{app}"; IconFilename: "{app}\TinyPlay.exe"

[Run]
Filename: "{app}\TinyPlay.exe"; Description: "{cm:LaunchProgram,TinyPlay}"; WorkingDir: "{app}"; Flags: nowait postinstall skipifsilent

[Code]
procedure MigrateConfigFromRunningLegacyApp;
var
  ResultCode: Integer;
  Command: String;
begin
  { ZIP releases stored configuration beside TinyPlay.exe. Normally people
    start an update from the running tray app, so obtain that process's path
    before stopping it and copy its config to the installer-era location. A
    config already in %AppData% always wins. This is deliberately best-effort:
    a failed migration must never prevent an install. }
  Command := '-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command ' +
    '"$target = Join-Path ([Environment]::GetFolderPath(''ApplicationData'')) ''TinyPlay''; ' +
    'if (Test-Path (Join-Path $target ''config.json'')) { exit 0 }; ' +
    'Get-Process -Name ''TinyPlay'' -ErrorAction SilentlyContinue | ForEach-Object { ' +
    '$legacyConfig = Join-Path (Split-Path $_.Path -Parent) ''data\\config.json''; ' +
    'if (Test-Path $legacyConfig) { New-Item -ItemType Directory -Force -Path $target | Out-Null; ' +
    'Copy-Item -Force $legacyConfig (Join-Path $target ''config.json''); exit 0 } }; exit 0"';
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Command, '', SW_HIDE,
      ewWaitUntilTerminated, ResultCode) then
    Exit;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Result := '';

  MigrateConfigFromRunningLegacyApp;

  { TinyPlay is a tray app with no conventional main window.  Ask Windows to
    close any running instance before files are replaced, so an ordinary update
    never asks the user to find and exit it manually.  ERROR_NOT_FOUND (128)
    simply means this is a first install or TinyPlay is not running. }
  if not Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM "TinyPlay.exe"', '', SW_HIDE,
      ewWaitUntilTerminated, ResultCode) then begin
    Result := 'Unable to close TinyPlay. Please close it and run this installer again.';
    Exit;
  end;
  if (ResultCode <> 0) and (ResultCode <> 128) then
    Result := 'TinyPlay is still running and could not be closed automatically. Please close it and run this installer again.';
end;
