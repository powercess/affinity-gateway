"""Validate one externally orchestrated isolated fault, retain evidence."""
import json
from pathlib import Path
import sys
import time
from strict_e2e import request

run=Path('/artifacts/latest-strict').read_text().strip()
out=Path('/artifacts')/run
state=json.loads((out/'fault-state.json').read_text())
name=sys.argv[1]
start=time.time_ns()
result=request(state['sid'],text='[mock-fail:503]' if name=='upstream-503' else 'probe')
end=time.time_ns()
expected=int(sys.argv[2])
assert result['status']==expected,(name,result)
if expected==200:
    assert result['supplier']==state['before']['supplier'],(name,'binding drift',result)
elif len(sys.argv)>3:
    assert result['error']==sys.argv[3],(name,result)
rows=[json.loads(x) for x in Path('/artifacts/capture.jsonl').read_text().splitlines()]
natives=[r for r in rows if r['stage']=='native' and start<=r['time_ns']<=end and r['headers'].get('x-session-id')==state['sid']]
traces={r['headers']['x-test-request-id'] for r in natives}
suppliers=[r for r in rows if r['stage']=='supplier' and r['headers'].get('x-test-request-id') in traces]
if expected!=200 and name!='upstream-503': assert not suppliers,'rejection reached supplier'
if name=='upstream-503':
    assert len(suppliers)==1,('upstream failure retried',len(suppliers))
    assert suppliers[0]['headers']['x-test-route']==state['before']['supplier']['route'],'failure migrated'
record={'case':name,'start_ns':start,'end_ns':end,'result':result,'supplier_requests':len(suppliers),'status':'PASS'}
with (out/'faults.jsonl').open('a') as file:file.write(json.dumps(record)+'\n')
print(json.dumps({'case':name,'status':'PASS','http_status':result['status'],'supplier_requests':len(suppliers)}))
