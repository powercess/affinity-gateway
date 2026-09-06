"""Mutation tests ensure the capture oracle actually detects broken rewrites."""
import copy
import hashlib
import json
import unittest

from validate import derive, validate_inbound, validate_outbound


class Oracle(unittest.TestCase):
    def setUp(self):
        self.native = {"headers": {"authorization": "[REDACTED]", "x-session-id": "session-a"},
                       "body": "{}", "body_sha256": hashlib.sha256(b"{}").hexdigest()}
        self.canonical = copy.deepcopy(self.native)
        self.canonical["headers"] = {"x-session-affinity": "sa:v1:" + derive("inbound:v1", "Bearer test-token", "conversation", "session-a")}
        self.egress = copy.deepcopy(self.canonical)
        self.supplier = copy.deepcopy(self.native)
        self.supplier["headers"] = {"x-test-route": "opencode-a", "x-opencode-session":
            derive("outbound:v1", "opencode-a", self.canonical["headers"]["x-session-affinity"])}

    def test_valid_chain(self):
        validate_inbound(self.native, self.canonical, "test-token")
        validate_outbound(self.canonical, self.egress, self.supplier)

    def test_wrong_namespace(self):
        with self.assertRaises(AssertionError):
            validate_inbound(self.native, self.canonical, "other-token")

    def test_altered_inbound_body(self):
        self.canonical["body_sha256"] = "wrong"
        with self.assertRaises(AssertionError):
            validate_inbound(self.native, self.canonical, "test-token")

    def test_missing_passthrough(self):
        self.egress["headers"] = {}
        with self.assertRaises(AssertionError):
            validate_outbound(self.canonical, self.egress, self.supplier)

    def test_wrong_site(self):
        self.supplier["headers"]["x-test-route"] = "opencode-b"
        with self.assertRaises(AssertionError):
            validate_outbound(self.canonical, self.egress, self.supplier)

    def test_internal_leak(self):
        self.supplier["headers"].update(self.canonical["headers"])
        with self.assertRaises(AssertionError):
            validate_outbound(self.canonical, self.egress, self.supplier)

    def test_altered_outbound_body(self):
        self.supplier["body_sha256"] = "wrong"
        with self.assertRaises(AssertionError):
            validate_outbound(self.canonical, self.egress, self.supplier)

    def test_declared_body_rewrites_only(self):
        internal=self.canonical['headers']['x-session-affinity']
        before={'input':'same','temperature':0,'conversation':'conv-resource','prompt_cache_key':'cache',
                'metadata':{'user_id':json.dumps({'session_id':'private','device_id':'device'})}}
        after=copy.deepcopy(before)
        after['prompt_cache_key']=derive('cache:v1','opencode-a',internal,'cache')
        after['metadata']['user_id']=json.dumps({'session_id':self.supplier['headers']['x-opencode-session'],'device_id':'device'})
        self.egress['body']=json.dumps(before)
        self.supplier['body']=json.dumps(after)
        validate_outbound(self.canonical,self.egress,self.supplier,isolate_body=True)
        for field,value in [('prompt_cache_key','cache'),('conversation','changed-resource'),('input','changed text'),('temperature',1)]:
            bad=copy.deepcopy(after); bad[field]=value
            self.supplier['body']=json.dumps(bad)
            with self.assertRaises(AssertionError):
                validate_outbound(self.canonical,self.egress,self.supplier,isolate_body=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
