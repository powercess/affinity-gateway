"""Strict binding regression: real Caddy + patched new-api + SQL + mock.

Concurrency tests send distinct prompts, not repeated identical body fingerprints.
Fault injection is orchestrated separately and must never target production.
"""
import concurrent.futures
import json
import time
import uuid
import urllib.request
import urllib.error
from pathlib import Path

TOKEN=Path('/artifacts/token').read_text().strip()
BASE='http://capture:8001'


def request(sid, text='probe', headers=None, body=None):
    payload=body or {'model':'affinity-test','messages':[{'role':'user','content':text}]}
    h={'Content-Type':'application/json','Authorization':'Bearer '+TOKEN,'X-Test-Suite':'strict-e2e'}
    if sid is not None: h['X-Session-Id']=sid
    h.update(headers or {})
    req=urllib.request.Request(BASE+'/v1/chat/completions',data=json.dumps(payload).encode(),headers=h)
    try:
        response=urllib.request.urlopen(req,timeout=30)
    except urllib.error.HTTPError as error:
        response=error
    raw=response.read().decode()
    try: parsed=json.loads(raw)
    except ValueError: parsed={'raw':raw}
    result={'status':response.status,'error':response.headers.get('X-Affinity-Error'),'body':parsed}
    if response.status==200:
        result['supplier']=json.loads(result['body']['choices'][0]['message']['content'])
    return result


def main():
    RUN='strict-'+str(uuid.uuid4())
    OUT=Path('/artifacts')/RUN
    OUT.mkdir()
    results=[]
    start=time.time_ns()
    for turn in range(20):
        sid=RUN+'-'+str(turn)
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            replies=list(pool.map(lambda i:request(sid,'distinct prompt '+str(i)),range(8)))
        assert all(r['status']==200 for r in replies),replies
        assert len({r['supplier']['route'] for r in replies})==1, 'cold-session channel drift'
        assert len({r['supplier']['session'] for r in replies})==1, 'outbound identity drift'
        results.append({'case':'cold-concurrency','session':sid,'requests':8,'route':replies[0]['supplier']['route']})
    for name,sid,headers,body,code in [
        ('missing',None,{},None,'affinity_identity_required'),
        ('cache-not-session',None,{}, {'model':'affinity-test','prompt_cache_key':'cache','messages':[]},'affinity_identity_required'),
        ('header-conflict','one',{'Session-Id':'two'},None,'affinity_identity_conflict'),
        ('body-conflict','one',{}, {'model':'affinity-test','metadata':{'user_id':'{"session_id":"two"}'},'messages':[]},'affinity_identity_conflict'),
        ('plain-user',None,{}, {'model':'affinity-test','metadata':{'user_id':'ordinary-user'},'messages':[]},'affinity_identity_required'),
    ]:
        result=request(sid,headers=headers,body=body)
        assert result['status']==400 and result['error']==code,result
        results.append({'case':name,'result':result})
    fault_sid=RUN+'-durable'
    before=request(fault_sid)
    assert before['status']==200,before
    (OUT/'fault-state.json').write_text(json.dumps({'sid':fault_sid,'before':before}))
    (OUT/'validation.json').write_text(json.dumps({'run':RUN,'start_ns':start,'end_ns':time.time_ns(),'results':results},indent=2))
    Path('/artifacts/latest-strict').write_text(RUN)
    print(json.dumps({'run':RUN,'concurrency_requests':160,'cold_sessions':20,'admission_cases':5,'status':'PASS'}),flush=True)


if __name__=='__main__': main()
