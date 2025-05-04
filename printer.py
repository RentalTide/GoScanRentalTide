
import tempfile
import webbrowser
import os
import platform

def generate_html(receipt):
    # Simplified version: recreate your HTML template logic here
    return f"<html><body><h1>Receipt for {receipt['transactionId']}</h1></body></html>"

def print_receipt(receipt):
    html = generate_html(receipt)
    with tempfile.NamedTemporaryFile(delete=False, suffix=".html") as f:
        f.write(html.encode("utf-8"))
        tmp_path = f.name

    if platform.system() == "Windows":
        os.startfile(tmp_path, "print")  # Triggers system print dialog
    else:
        webbrowser.open(tmp_path)  # Opens in browser as fallback
