@echo off
echo Creating shortcut with admin privileges...

set SCRIPT="%TEMP%\create_shortcut.vbs"

echo Set oWS = WScript.CreateObject("WScript.Shell") > %SCRIPT%
echo sLinkFile = "%~dp0scanner_admin.lnk" >> %SCRIPT%
echo Set oLink = oWS.CreateShortcut(sLinkFile) >> %SCRIPT%
echo oLink.TargetPath = "%~dp0scanner.exe" >> %SCRIPT%
echo oLink.Description = "License Scanner (Run as Administrator)" >> %SCRIPT%
echo oLink.WorkingDirectory = "%~dp0" >> %SCRIPT%
echo oLink.Save >> %SCRIPT%

cscript /nologo %SCRIPT%
del %SCRIPT%

echo.
echo Created shortcut "scanner_admin.lnk"
echo Right-click the shortcut, select Properties, click Advanced, and check "Run as administrator"
echo.
echo Press any key to exit...
pause > nul
