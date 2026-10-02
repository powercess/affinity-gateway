package affinity

import "testing"

func TestResolverRebuildsFromHistory(t *testing.T) {
	resolver := NewResolver()
	turn1 := []byte(`{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	turn2 := []byte(`{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"again"}]}`)

	id1, ok := resolver.FromHistory("cred", turn1)
	if !ok || id1.ID == "" {
		t.Fatalf("turn 1 must resolve")
	}
	if id1.Source != "history:new" {
		t.Fatalf("unexpected source %q", id1.Source)
	}
	id2, ok := resolver.FromHistory("cred", turn2)
	if !ok || id2.ID != id1.ID {
		t.Fatalf("turn 2 must reuse session: %q vs %q", id2.ID, id1.ID)
	}
	if id2.Source != "history:prefix-2" {
		t.Fatalf("unexpected turn 2 source %q", id2.Source)
	}

	// A different conversation must not share the session.
	other := []byte(`{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"unrelated"}]}`)
	idOther, _ := resolver.FromHistory("cred", other)
	if idOther.ID == id1.ID {
		t.Fatalf("different conversations must differ")
	}

	// A different credential must not share the rebuilt session.
	idCred, _ := resolver.FromHistory("other-cred", turn1)
	if idCred.ID == id1.ID {
		t.Fatalf("different credentials must differ")
	}
}

func TestResolverWithoutMessages(t *testing.T) {
	resolver := NewResolver()
	if _, ok := resolver.FromHistory("cred", []byte(`{"model":"m"}`)); ok {
		t.Fatalf("expected no identity without messages")
	}
}

func TestMetadataFields(t *testing.T) {
	cases := map[string]string{
		`{"metadata":{"session_id":"a"}}`:     "a",
		`{"metadata":{"conversationId":"b"}}`: "b",
		`{"metadata":{"thread_id":"c"}}`:      "c",
		`{"prompt_cache_key":"d"}`:            "d",
		`{"previous_response_id":"e"}`:        "e",
		`{"conversation":"f"}`:                "f",
		`{"metadata":{"session":"g"}}`:        "g",
	}
	for body, want := range cases {
		got, ok := FromMetadata([]byte(body))
		if !ok || got.ID != want {
			t.Fatalf("body %s: want %q got %q ok=%v", body, want, got.ID, ok)
		}
	}
}
