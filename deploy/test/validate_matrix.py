"""Read-only independent matrix oracle; export redacted evidence, fail honestly."""
import collections
import json
import os
from pathlib import Path
import sys

from validate import identity, validate_inbound, validate_outbound

PATHS = {"/v1/messages", "/v1/responses", "/v1/chat/completions"}


def main(run):
    strict = os.environ.get('STRICT_MATRIX') == '1'
    directory = Path('/artifacts') / run
    cases = json.loads((directory / 'runs.json').read_text())
    lane_filter = os.environ.get('VALIDATE_LANE')
    if lane_filter:
        cases = [c for c in cases if c['lane'] == lane_filter]
        assert cases, 'requested lane has no cases'
    rows = [json.loads(line) for line in Path('/artifacts/capture.jsonl').read_text().splitlines()]
    index = collections.defaultdict(lambda: collections.defaultdict(list))
    for row in rows:
        index[row['headers'].get('x-test-request-id')][row['stage']].append(row)
    token = Path('/artifacts/token').read_text().strip()
    report = {'run': run, 'cases': [], 'failures': [], 'exposures': [], 'registrations': {}}
    selected = set()
    continuity = collections.defaultdict(lambda: collections.defaultdict(set))
    routes = collections.defaultdict(set)
    rejected_first = set()
    for case in cases:
        stage = 'direct' if case['lane'] == 'direct' else 'native'
        natives = [r for r in rows if r['stage'] == stage and r['body'] and not r['headers'].get('x-test-suite') and
                   r['path'].split('?')[0] in PATHS and case['start_ns'] <= r['time_ns'] <= case['end_ns']]
        result = {k: case[k] for k in ('case', 'lane', 'phase', 'reply_match')}
        result.update(requests=[], errors=[])
        if strict and not natives and case['phase'] in ('resume','third') and (case['lane'],case['case']) in rejected_first and case['exit_code'] != 0:
            result['blocked_by_first_rejection'] = True
            report['cases'].append(result)
            continue
        contract = case['lane'] == 'cache-contract'
        expected_reject = strict and not contract and stage == 'native' and natives and all(
            not identity(dict(r,headers={k:v for k,v in r['headers'].items() if '_' not in k})) for r in natives)
        result['expected_rejection'] = bool(expected_reject)
        if not natives or not case['reply_match'] and not expected_reject:
            result['errors'].append('CLI reply failed or no inference request')
        for native in natives:
            trace = native['headers']['x-test-request-id']
            selected.add(trace)
            chain = index[trace]
            body = json.loads(native['body'])
            headers = {k: v for k, v in native['headers'].items() if 'session' in k or 'conversation' in k or k == 'thread-id'}
            signals = {k: body[k] for k in ('prompt_cache_key', 'conversation', 'session_id', 'metadata', 'store') if k in body}
            request = dict(trace=trace, path=native['path'], model=body.get('model'), headers=headers,
                           body_signals=signals, status=[r['status'] for r in chain[stage + '_response']])
            result['requests'].append(request)
            try:
                if expected_reject:
                    assert request['status'] == [400], 'missing identity not rejected with 400'
                    assert not any(chain[s] for s in ('canonical','egress','supplier')), 'rejected request reached downstream'
                    assert chain[stage + '_response'][0].get('response_headers',{}).get('x-affinity-error') == 'affinity_identity_required', 'missing actionable identity error'
                    request['rejection'] = 'PASS'
                    if case['phase']=='first': rejected_first.add((case['lane'],case['case']))
                    continue
                assert request['status'] == [200], 'HTTP status ' + str(request['status'])
                if stage == 'native':
                    assert all(len(chain[s]) == 1 for s in ('canonical', 'egress', 'supplier')), 'incomplete/retried chain'
                    canonical, egress, supplier = (chain[s][0] for s in ('canonical', 'egress', 'supplier'))
                    internal = validate_inbound(native, canonical, token, cache_contract=contract)
                    route, outbound = validate_outbound(canonical, egress, supplier, isolate_body=strict)
                    request.update(inbound='PASS', outbound='PASS', canonical=internal, route=route)
                    continuity[case['case']][case['phase']].add(internal)
                    routes[(case['case'], internal, body['model'])].add(route)
                    supplier_body = json.loads(supplier['body'])
                    if identity(dict(supplier, headers={})) and (not strict or identity(dict(supplier, headers={})) == identity(native)):
                        report['exposures'].append(dict(trace=trace, case=case['case'], field='body conversation/metadata session identity'))
                    if body.get('prompt_cache_key') and supplier_body.get('prompt_cache_key') == body['prompt_cache_key']:
                        report['exposures'].append(dict(trace=trace, case=case['case'], field='prompt_cache_key'))
                    if headers.get('x-claude-code-session-id') == supplier['headers'].get('x-claude-code-session-id') and headers.get('x-claude-code-session-id'):
                        report['exposures'].append(dict(trace=trace, case=case['case'], field='x-claude-code-session-id'))
            except (AssertionError, KeyError, ValueError, IndexError) as error:
                result['errors'].append(str(error))
        report['cases'].append(result)
        if result['errors']:
            report['failures'].append(dict(case=case['case'], phase=case['phase'], errors=result['errors']))
    for name, phases in continuity.items():
        if not (phases['first'] == phases['resume'] == phases['third'] and len(phases['first']) == 1):
            report['failures'].append(dict(case=name, error='session continuity failure'))
        if len(phases['new']) != 1 or phases['first'] & phases['new']:
            report['failures'].append(dict(case=name, error='new session isolation failure'))
    for (name, internal, model), sites in routes.items():
        if len(sites) > 1:
            report['failures'].append(dict(case=name, model=model, canonical=internal, error='same-session same-model channel drift', routes=sorted(sites)))
    for name in dict.fromkeys(c['case'] for c in cases):
        turns = [c for c in report['cases'] if c['case'] == name]
        requests = [r for c in turns for r in c['requests']]
        report['registrations'][name] = dict(replies=sum(c['reply_match'] for c in turns), turns=len(turns),
            requests=len(requests), protocols=sorted({r['path'].split('?')[0] for r in requests}),
            statuses=sorted({s for r in requests for s in r['status']}),
            rejected=sum(r.get('rejection') == 'PASS' for r in requests),
            checked=sum(r.get('inbound') == 'PASS' for r in requests))
    suffix = '-' + lane_filter if lane_filter else ''
    (directory / ('validation' + suffix + '.json')).write_text(json.dumps(report, indent=2))
    evidence = ''.join(json.dumps(r) + '\n' for r in rows if r['headers'].get('x-test-request-id') in selected)
    assert token not in evidence, 'credential leaked in capture'
    (directory / ('capture' + suffix + '.jsonl')).write_text(evidence)
    print(json.dumps({k: report[k] for k in ('run', 'registrations', 'failures')}, indent=2))
    print('identity exposure observations:', len(report['exposures']))
    return int(bool(report['failures'] or report['exposures']))


if __name__ == '__main__':
    sys.exit(main(sys.argv[1]))
