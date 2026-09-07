"""End-to-end check for the isolated console Compose; no real credentials."""
import base64
import json
import os
import uuid
import urllib.error
import urllib.request
from pathlib import Path

token = Path('/artifacts/token').read_text().strip()
session = 'console-' + str(uuid.uuid4())

def inference(identity, stream=False):
    headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'}
    if identity:
        headers['X-Session-Id'] = identity
    req = urllib.request.Request('http://affinity-gateway:8236/v1/chat/completions',
        data=json.dumps({'model': 'affinity-test', 'stream': stream,
                         'messages': [{'role': 'user', 'content': 'console integration'}]}).encode(), headers=headers)
    try:
        response = urllib.request.urlopen(req, timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        body = response.read()
        assert response.status == (200 if identity else 400), (response.status, body)
        if not identity:
            return None
        if not stream:
            return json.loads(json.loads(body)['choices'][0]['message']['content'])
        assert b'data: [DONE]' in body
        text = ''
        for line in body.splitlines():
            if line.startswith(b'data: ') and line != b'data: [DONE]':
                for choice in json.loads(line[6:]).get('choices', []):
                    text += choice.get('delta', {}).get('content', '')
        return json.loads(text)

results = [inference(session), inference(session), inference(session, True)]
assert len({r['route'] for r in results}) == 1
assert len({r['session'] for r in results}) == 1
assert all(r['internal'] is None and r['generic'] is None for r in results)
inference(None)

base = 'http://console'
with urllib.request.urlopen(base + '/', timeout=10) as response:
    assert b'<div id="root">' in response.read()
try:
    urllib.request.urlopen(base + '/api/observations', timeout=10)
except urllib.error.HTTPError as error:
    assert error.code == 401
else:
    raise AssertionError('unauthenticated API')
password = os.environ.get('AFFINITY_CONSOLE_PASSWORD', 'affinity-local-console-development')
headers = {'Authorization': 'Basic ' + base64.b64encode(('admin:' + password).encode()).decode()}
with urllib.request.urlopen(urllib.request.Request(base + '/api/observations', headers=headers), timeout=10) as response:
    snapshot = json.load(response)
supplier_id = 'verify-' + uuid.uuid4().hex[:12]
supplier_headers = {**headers, 'Content-Type': 'application/json'}
create = urllib.request.Request(base + '/api/suppliers',
    data=json.dumps({'id': supplier_id, 'origin': 'https://api.example.com'}).encode(),
    headers=supplier_headers, method='POST')
try:
    with urllib.request.urlopen(create, timeout=10) as response:
        assert response.status == 201
        configured = json.load(response)['items']
    row = next(item for item in configured if item['id'] == supplier_id)
    assert row['origin'] == 'https://api.example.com'
    assert row['internal_base_url'] == 'http://affinity-gateway:8237/r/' + supplier_id
    try:
        urllib.request.urlopen(create, timeout=10)
    except urllib.error.HTTPError as error:
        assert error.code == 409
    else:
        raise AssertionError('supplier id/origin mutation was accepted')
finally:
    delete = urllib.request.Request(base + '/api/suppliers/' + supplier_id, headers=headers, method='DELETE')
    with urllib.request.urlopen(delete, timeout=10) as response:
        assert response.status == 204
rows = snapshot['items']
assert {'inbound', 'outbound'} <= {r['mode'] for r in rows}
assert any(r.get('error') == 'affinity_identity_required' for r in rows)
inbound = {r.get('session') for r in rows if r['mode'] == 'inbound'}
outbound = {r.get('session') for r in rows if r['mode'] == 'outbound'}
assert (inbound & outbound) - {None, ''}
assert token not in json.dumps(snapshot)
assert session not in json.dumps(snapshot)
with urllib.request.urlopen(urllib.request.Request(base + '/api/events', headers=headers), timeout=10) as response:
    assert response.headers['Content-Type'].startswith('text/event-stream')
    assert response.readline().strip() == b'event: snapshot'
print(json.dumps({'passed': True, 'observations': len(rows), 'supplier_route': results[0]['route'],
                  'stable_affinity': True, 'model_sse': True, 'console_sse': True,
                  'auth': True, 'redaction': True, 'supplier_config': True}))
