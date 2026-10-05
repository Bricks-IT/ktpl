package object

import (
	"strings"
	"testing"
)

// FuzzParseRef checks that accepted identities are always well formed. Native Go fuzzing: `make fuzz`.
func FuzzParseRef(f *testing.F) {
	for _, s := range []string{"a/b/c", "b/c", "a/b.c.d/e", "///"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := ParseRef(s)
		if err != nil {
			return
		}
		if r.Kind == "" || r.Name == "" || strings.Contains(r.Name, "/") || strings.Contains(r.Kind, ".") {
			t.Fatalf("invalid ref accepted: %q -> %+v", s, r)
		}
	})
}
