"""Export only trace-correlated synthetic fault/concurrency evidence."""
import json
from pathlib import Path

root=Path('/artifacts')
run=(root/'latest-strict').read_text().strip()
out=root/run
summary=json.loads((out/'validation.json').read_text())
faults=[json.loads(x) for x in (out/'faults.jsonl').read_text().splitlines()]
windows=[(summary['start_ns'],summary['end_ns'])]+[(r['start_ns'],r['end_ns']) for r in faults]
rows=[json.loads(x) for x in (root/'capture.jsonl').read_text().splitlines()]
traces={r['headers'].get('x-test-request-id') for r in rows if r['stage']=='native' and any(a<=r['time_ns']<=b for a,b in windows)}
traces.discard(None)
evidence=''.join(json.dumps(r)+'\n' for r in rows if r['headers'].get('x-test-request-id') in traces)
assert (root/'token').read_text().strip() not in evidence
(out/'capture.jsonl').write_text(evidence)
print(json.dumps({'run':run,'trace_count':len(traces),'fault_checks':len(faults)}))
