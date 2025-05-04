
# main.py
from flask import Flask, request, jsonify
from scanner import scan_license
from printer import print_receipt

app = Flask(__name__)

@app.route("/scanner/scan", methods=["GET"])
def handle_scan():
    try:
        data = scan_license()
        return jsonify({"status": "success", "licenseData": data})
    except Exception as e:
        return jsonify({"status": "error", "message": str(e)}), 500

@app.route("/print/receipt", methods=["POST"])
def handle_print():
    try:
        receipt = request.json
        print_receipt(receipt)
        return jsonify({"status": "success", "message": "Receipt printed successfully"})
    except Exception as e:
        return jsonify({"status": "error", "message": str(e)}), 500

if __name__ == "__main__":
    app.run(port=3500)
