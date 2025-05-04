#!/bin/bash

# First, let's check if the manifest file exists
if [ ! -f "scanner.exe.manifest" ]; then
    echo "Creating scanner.exe.manifest..."
    cat > scanner.exe.manifest << 'EOL'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.0.0.0" processorArchitecture="*" name="scanner" type="win32"/>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="requireAdministrator" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
</assembly>
EOL
    echo "Manifest file created."
fi

# Find where rsrc was installed
GOPATH=$(go env GOPATH)
RSRC_PATH="$GOPATH/bin/rsrc"

echo "Checking for rsrc at $RSRC_PATH"

# Check if rsrc exists
if [ ! -f "$RSRC_PATH" ]; then
    echo "rsrc not found. Installing..."
    go install github.com/akavel/rsrc@latest
    # Double-check installation
    if [ ! -f "$RSRC_PATH" ]; then
        echo "Failed to install rsrc. Checking alternate location..."
        # Try alternate bin locations
        if [ -f "$HOME/go/bin/rsrc" ]; then
            RSRC_PATH="$HOME/go/bin/rsrc"
            echo "Found rsrc at $RSRC_PATH"
        else
            echo "Could not find rsrc. Please install it manually with: go install github.com/akavel/rsrc@latest"
            exit 1
        fi
    fi
else
    echo "rsrc found at $RSRC_PATH"
fi

# Generate a .syso file from the manifest
echo "Generating .syso file..."
"$RSRC_PATH" -manifest scanner.exe.manifest -o scanner.syso

if [ $? -ne 0 ]; then
    echo "Failed to generate .syso file."
    exit 1
fi

echo "Building application..."
# Build for Windows (cross-compilation)
GOOS=windows GOARCH=amd64 go build -o scanner.exe

if [ $? -eq 0 ]; then
    echo "✅ Build completed successfully!"
    echo "The scanner.exe will automatically run with admin privileges on Windows."
    echo ""
    echo "To use on Windows, copy scanner.exe to the Windows machine and run it."
    echo "If needed, specify the COM port: scanner.exe -port=COM3"
else
    echo "❌ Build failed. Please check for errors."
fi