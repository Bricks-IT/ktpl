package object

import (
	"fmt"
	"strings"
)

// Ref is a parsed `ref` identity: "ns/kind/name" (namespaced) or "kind/name" (no namespace).
// Kind may be qualified with its API group: "service.serving.knative.dev".
type Ref struct {
	Raw       string
	Namespace string
	Kind      string // lower case
	Group     string // lower case, empty = any group
	Name      string
}

// ParseRef parses a ref identity string.
func ParseRef(s string) (Ref, error) {
	parts := strings.Split(s, "/")
	r := Ref{Raw: s}
	switch len(parts) {
	case 2:
		r.Kind, r.Name = parts[0], parts[1]
	case 3:
		r.Namespace, r.Kind, r.Name = parts[0], parts[1], parts[2]
		if r.Namespace == "" {
			return Ref{}, fmt.Errorf("invalid identity %q: empty namespace (use \"<kind>/<name>\" for objects without namespace)", s)
		}
	default:
		return Ref{}, fmt.Errorf("invalid identity %q: expected \"<namespace>/<kind>/<name>\" or \"<kind>/<name>\"", s)
	}
	if r.Kind == "" || r.Name == "" {
		return Ref{}, fmt.Errorf("invalid identity %q: empty kind or name", s)
	}
	if kind, group, ok := strings.Cut(r.Kind, "."); ok {
		if kind == "" || group == "" {
			return Ref{}, fmt.Errorf("invalid identity %q: malformed kind.group %q", s, r.Kind)
		}
		r.Kind, r.Group = kind, group
	}
	r.Kind, r.Group = strings.ToLower(r.Kind), strings.ToLower(r.Group)
	return r, nil
}

// Matches reports whether o is designated by r (case-insensitive kind, group checked only if given).
func (r Ref) Matches(o *Object) bool {
	if o.ID.Namespace != r.Namespace || o.ID.Name != r.Name || strings.ToLower(o.ID.Kind) != r.Kind {
		return false
	}
	return r.Group == "" || strings.ToLower(o.ID.Group) == r.Group
}
