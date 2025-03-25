# GoScanRentalTide

GoScanRentalTide is a standalone background application that reads data from a connected scanner via the serial port and exposes parsed license data as JSON through an HTTP endpoint. This executable runs on macOS and Windows without requiring any additional installations.

## Features
- **Standalone Executable**: No extra dependencies are needed.  
- **Serial Port Communication**: Automatically detects the connected scanner (macOS: usbserial devices, Windows: COM ports).  
- **JSON API**: Exposes an HTTP endpoint (`http://localhost:3500/scanner/scan`) that returns structured license data.  
- **Robust Error Handling**: Returns all errors as JSON responses.  

## Download
You can download the latest executable from our GitHub releases page:  
[GoScanRentalTide Releases](https://github.com/RentalTide/GoScanRentalTide/releases)

## System Requirements
- **Operating Systems**: macOS, Windows (Linux may work with additional adjustments).  
- **Hardware**: A scanner that communicates via a serial port (a USB-to-serial adapter may be required). Tested with E-Seek M260.

## Installation & Setup
1. **Download the Executable**:  
    Go to the [GitHub Releases](https://github.com/RentalTide/GoScanRentalTide/releases) page and download the appropriate executable for your system.  

2. **Place the Executable**:  
    Save the executable to a directory of your choice.  

3. **Set Execution Permissions (macOS/Linux)**:  
    Open your terminal, navigate to the directory containing the executable, and run:  
    ```bash
    chmod +x GoScanRentalTide
    ```

## Running the Application
1. **Start the Application**:  
    Open a terminal (or Command Prompt on Windows), navigate to the directory containing the executable, and run:  
    - On macOS/Linux:  
      ```bash
      ./GoScanRentalTide
      ```
    - On Windows:  
      ```cmd
      GoScanRentalTide.exe
      ```
    The application will start a background HTTP server on port `3500` and log a message similar to:  
    ```
    Starting server on http://localhost:3500
    ```

## Troubleshooting

- **Serial Port Detection**:  
  Make sure your scanner is properly connected. On macOS, devices with "usbserial" in the name are automatically detected. On Windows, COM ports are detected.  

- **Permissions**:  
  Verify that your system allows access to the serial port, especially on macOS where additional permissions may be required.  

## Stopping the Application
To stop the application, simply close the terminal window where it is running or press `Ctrl+C` (or the equivalent command) in that terminal.

## Support
For further assistance, please visit our [GitHub repository](https://github.com/RentalTide/GoScanRentalTide) or contact our support team at `support@rentaltide.com`.  