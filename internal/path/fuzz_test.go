package path

import "testing"

// FuzzParse checks that the canonical form of any parsable path parses back to the same path.
// Native Go fuzzing (Ginkgo has no fuzzing support): `make fuzz`.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"a.b[0]", "metadata.labels['app.kubernetes.io/name']", `x["y"]`, "[3]", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := Parse(s)
		if err != nil {
			return
		}
		again, err := Parse(p.String())
		if err != nil {
			t.Fatalf("Parse(%q) ok but canonical %q fails: %v", s, p.String(), err)
		}
		if !again.Equal(p) {
			t.Fatalf("round trip mismatch for %q: %#v vs %#v", s, p, again)
		}
	})
}
