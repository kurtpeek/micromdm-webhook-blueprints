import base64
from flask import Flask, request

app = Flask(__name__)

@app.route('/webhook', methods=['POST'])
def webhook():
    raw_payload = request.json['acknowledge_event']['raw_payload']
    payload = base64.b64decode(raw_payload)
    print(payload)
    return ''

if __name__ == '__main__':
    app.run(host='0.0.0.0')
