# scanner.py

import serial
import time
import re
import platform

def find_scanner_port():
    import serial.tools.list_ports
    ports = serial.tools.list_ports.comports()
    for port in ports:
        if platform.system() == "Windows" and port.device.startswith("COM"):
            return port.device
        elif platform.system() == "Darwin" and "usbserial" in port.device.lower():
            return port.device
        elif platform.system() == "Linux" and ("ttyUSB" in port.device or "usb" in port.device.lower()):
            return port.device
    raise Exception("No compatible serial port found")

def send_scanner_command(command="<TXPING>"):
    port_name = find_scanner_port()
    ser = serial.Serial(port_name, baudrate=9600, timeout=2)
    
    cmd = b"\x01" + command.encode("utf-8") + b"\x04"
    ser.write(cmd)

    time.sleep(1)  # Give the scanner time to respond
    response = ser.read(1024).decode(errors="ignore")
    ser.close()

    if not response or response == chr(0x15):
        raise Exception("No license scanned or scanner not triggered")

    return response

def parse_license_data(raw):
    lines = [line.strip() for line in raw.splitlines() if line.strip()]
    data = {}
    license_class = "NA"

    for line in lines:
        if line.startswith("DCS"):
            data["lastName"] = line[3:].strip()
        elif line.startswith("DAC"):
            data["firstName"] = line[3:].strip()
        elif line.startswith("DAD"):
            data["middleName"] = line[3:].strip()
        elif line.startswith("DBA"):
            d = line[3:].strip()
            data["expiryDate"] = f"{d[0:4]}/{d[4:6]}/{d[6:8]}" if len(d) >= 8 else ""
        elif line.startswith("DBD"):
            d = line[3:].strip()
            data["issueDate"] = f"{d[0:4]}/{d[4:6]}/{d[6:8]}" if len(d) >= 8 else ""
        elif line.startswith("DBB"):
            d = line[3:].strip()
            data["dob"] = f"{d[0:4]}/{d[4:6]}/{d[6:8]}" if len(d) >= 8 else ""
        elif line.startswith("DBC"):
            s = line[3:].strip()
            data["sex"] = "M" if s == "1" else "F" if s == "2" else s
        elif line.startswith("DAU"):
            data["height"] = line[3:].strip().replace(" ", "")
        elif line.startswith("DAG"):
            data["address"] = line[3:].strip()
        elif line.startswith("DAI"):
            data["city"] = line[3:].strip()
        elif line.startswith("DAJ"):
            data["state"] = line[3:].strip()
        elif line.startswith("DAK"):
            data["postal"] = line[3:].strip()
        elif line.startswith("DAQ"):
            ln = line[3:].strip()
            if len(ln) == 15:
                ln = f"{ln[:5]}-{ln[5:10]}-{ln[10:]}"
            data["licenseNumber"] = ln
        elif "DCAG" in line:
            match = re.search(r"DCAG(\w+)", line)
            if match:
                license_class = match.group(1)

    data["licenseClass"] = license_class
    return data

def scan_license():
    raw_data = send_scanner_command()
    return parse_license_data(raw_data)
