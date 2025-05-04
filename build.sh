#!/bin/bash

echo "Building Windows executable from Mac..."

# First, remove any existing admin files to avoid conflicts
rm -f admin_windows.go windows_admin.go

# Create a Windows-specific admin handler file
cat > admin_windows.go << 'EOL'
//go:build windows
// +build windows

package main

import (
    "fmt"
    "os"
)

func init() {
    // Check if running as admin
    if !isAdmin() {
        fmt.Println("This application requires administrator privileges to access scanner hardware.")
        fmt.Println("Please right-click the executable and select 'Run as administrator'.")
        
        // Wait for user input before exiting
        fmt.Println("\nPress Enter to exit...")
        fmt.Scanln()
        os.Exit(1)
    }
}

func isAdmin() bool {
    _, err := os.Open("\\\\.\\PHYSICALDRIVE0")
    return err == nil
}
EOL

# Create a shortcut generator script
cat > generate_shortcut.bat << 'EOL'
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
EOL

# Create a readme file for Windows users
cat > README_WINDOWS.txt << 'EOL'
SCANNER APPLICATION - WINDOWS SETUP INSTRUCTIONS
=================================================

This application requires administrator privileges to access scanner hardware on Windows.

OPTION 1: Run with Right-Click Method
-------------------------------------
1. Right-click on "scanner.exe"
2. Select "Run as administrator"
3. Allow the UAC prompt

OPTION 2: Create Admin Shortcut (Recommended)
---------------------------------------------
1. Double-click on "generate_shortcut.bat"
2. A shortcut named "scanner_admin.lnk" will be created
3. Right-click this shortcut and select "Properties"
4. Click "Advanced" and check "Run as administrator"
5. Click OK twice to save
6. Now you can double-click the shortcut to run with admin privileges

SCANNER CONFIGURATION
---------------------
By default, the application will try to detect the correct COM port.
If needed, you can specify the port with the -port flag:

scanner.exe -port=COM3

Available ports on your system will be displayed when the application starts.

TROUBLESHOOTING
--------------
If the scanner is not detected:
1. Verify the scanner is powered on and connected to the computer
2. Check Device Manager to identify the correct COM port
3. Try running with explicit port: scanner.exe -port=COM3 (replace COM3 with your port)
4. Verify the scanner drivers are installed

For serial port configuration, the application uses:
- Baud rate: 1200
- Data bits: 7
- Parity: None
- Stop bits: 1
EOL

# Create a batch file for launching with admin rights
cat > run_scanner_admin.bat << 'EOL'
@echo off
echo Launching scanner with administrator privileges...
powershell -Command "Start-Process -FilePath '%~dp0scanner.exe' -ArgumentList '%*' -Verb RunAs"
EOL

# Build the Windows executable
echo "Cross-compiling for Windows..."
GOOS=windows GOARCH=amd64 go build -o scanner.exe

# Check if build was successful
if [ -f "scanner.exe" ]; then
    echo "✅ Build complete! scanner.exe was created"
    
    # Calculate file size
    SIZE=$(du -h scanner.exe | cut -f1)
    echo "File size: $SIZE"
    
    # Create a ZIP file with all necessary files
    echo "Creating deployment package..."
    zip -j scanner_windows.zip scanner.exe generate_shortcut.bat README_WINDOWS.txt run_scanner_admin.bat
    echo "✅ Created scanner_windows.zip with all necessary files"
    echo "Transfer this ZIP file to your Windows customers and have them extract all files"
else
    echo "❌ Build failed! scanner.exe was not created"
    # Show any errors in the build
    echo "Trying standard build to see errors..."
    go build -v
fi