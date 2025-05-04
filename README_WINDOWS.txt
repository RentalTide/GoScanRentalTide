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
