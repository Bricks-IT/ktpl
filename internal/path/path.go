// Package path parses, formats and evaluates ktpl field paths such as
// spec.template.spec.containers[0].image or metadata.labels['app.kubernetes.io/name'].
package path

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
)

// Segment is one step of a Path: a mapping key or a sequence index.
type Segment struct {
	Key     string
	Index   int
	IsIndex bool
}

// Path is a sequence of segments starting at the object root. The empty path designates the root.
type Path []Segment

// Key returns a mapping key segment.
func Key(k string) Segment { return Segment{Key: k} }

// Index returns a sequence index segment.
func Index(i int) Segment { return Segment{Index: i, IsIndex: true} }

// Child returns a new path made of p followed by s. p is never modified.
func (p Path) Child(s Segment) Path {
	out := make(Path, len(p), len(p)+1)
	copy(out, p)
	return append(out, s)
}

// HasPrefix reports whether prefix is an ancestor of p or equal to p.
func (p Path) HasPrefix(prefix Path) bool {
	if len(prefix) > len(p) {
		return false
	}
	for i := range prefix {
		if p[i] != prefix[i] {
			return false
		}
	}
	return true
}

// Equal reports whether p and o designate the same node.
func (p Path) Equal(o Path) bool { return len(p) == len(o) && p.HasPrefix(o) }

// Overlaps reports whether one path is an ancestor of (or equal to) the other.
func (p Path) Overlaps(o Path) bool { return p.HasPrefix(o) || o.HasPrefix(p) }

// String returns the canonical form of p: dotted plain keys, [n] indexes and ['k'] for other keys.
func (p Path) String() string {
	var b strings.Builder
	for i, s := range p {
		switch {
		case s.IsIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.Index))
			b.WriteByte(']')
		case isPlainKey(s.Key):
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.Key)
		default:
			b.WriteString("['")
			b.WriteString(strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s.Key))
			b.WriteString("']")
		}
	}
	return b.String()
}

func isPlainKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		switch r {
		case '.', '[', ']', '\'', '"', '\\':
			return false
		}
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Parse parses a path. Accepted syntax: "a.b", "a[0]", "a['k.x']", "a[\"k/x\"]", "" (root).
func Parse(s string) (Path, error) {
	p := Path{}
	i := 0
	for i < len(s) {
		switch s[i] {
		case '[':
			seg, next, err := parseBracket(s, i)
			if err != nil {
				return nil, err
			}
			p = append(p, seg)
			i = next
		case '.':
			if i == 0 {
				return nil, parseErr(s, i, "path cannot start with '.'")
			}
			key, next, err := scanKey(s, i+1)
			if err != nil {
				return nil, err
			}
			p = append(p, Key(key))
			i = next
		default:
			if i != 0 {
				return nil, parseErr(s, i, "expected '.' or '['")
			}
			key, next, err := scanKey(s, i)
			if err != nil {
				return nil, err
			}
			p = append(p, Key(key))
			i = next
		}
	}
	return p, nil
}

// MustParse is Parse for constant paths in tests and internal code; it panics on error.
func MustParse(s string) Path {
	p, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return p
}

func scanKey(s string, start int) (string, int, error) {
	i := start
	for i < len(s) && s[i] != '.' && s[i] != '[' {
		if s[i] == ']' {
			return "", 0, parseErr(s, i, "unexpected ']'")
		}
		i++
	}
	if i == start {
		return "", 0, parseErr(s, start, "empty key")
	}
	return s[start:i], i, nil
}

func parseBracket(s string, start int) (Segment, int, error) {
	i := start + 1
	if i >= len(s) {
		return Segment{}, 0, parseErr(s, start, "unterminated '['")
	}
	if q := s[i]; q == '\'' || q == '"' {
		var b strings.Builder
		i++
		for {
			if i >= len(s) {
				return Segment{}, 0, parseErr(s, start, "unterminated quoted key")
			}
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			if c == q {
				break
			}
			b.WriteByte(c)
			i++
		}
		i++ // closing quote
		if i >= len(s) || s[i] != ']' {
			return Segment{}, 0, parseErr(s, i, "expected ']' after quoted key")
		}
		return Key(b.String()), i + 1, nil
	}
	end := strings.IndexByte(s[i:], ']')
	if end < 0 {
		return Segment{}, 0, parseErr(s, start, "unterminated '['")
	}
	digits := s[i : i+end]
	if digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
		return Segment{}, 0, parseErr(s, i, "index must be a non-negative integer or a quoted key")
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return Segment{}, 0, parseErr(s, i, "index out of range")
	}
	return Index(n), i + end + 1, nil
}

func parseErr(s string, pos int, msg string) error {
	return fmt.Errorf("invalid path %q: %s at offset %d", s, msg, pos)
}

// NotFoundError is returned by Get when a segment does not exist.
type NotFoundError struct {
	Path Path
	At   int // index of the failing segment
	Msg  string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("path %q not found: %s", e.Path.String(), e.Msg)
}

// Get returns the node designated by p below root.
func Get(root *yaml.Node, p Path) (*yaml.Node, error) {
	cur := root
	for i, s := range p {
		for cur.Kind == yaml.AliasNode && cur.Alias != nil {
			cur = cur.Alias
		}
		switch {
		case s.IsIndex:
			if cur.Kind != yaml.SequenceNode {
				return nil, &NotFoundError{Path: p, At: i, Msg: fmt.Sprintf("cannot index %s with [%d]", node.KindName(cur), s.Index)}
			}
			if s.Index >= len(cur.Content) {
				return nil, &NotFoundError{Path: p, At: i, Msg: fmt.Sprintf("index %d out of range (length %d)", s.Index, len(cur.Content))}
			}
			cur = cur.Content[s.Index]
		default:
			if cur.Kind != yaml.MappingNode {
				return nil, &NotFoundError{Path: p, At: i, Msg: fmt.Sprintf("cannot read key %q of %s", s.Key, node.KindName(cur))}
			}
			v := node.MapValue(cur, s.Key)
			if v == nil {
				return nil, &NotFoundError{Path: p, At: i, Msg: fmt.Sprintf("key %q does not exist", s.Key)}
			}
			cur = v
		}
	}
	return cur, nil
}

// Walk calls fn for every value node below root (root excluded) in document order.
// Mapping keys are not visited. If fn returns false, the children of that node are skipped.
func Walk(root *yaml.Node, fn func(p Path, n *yaml.Node) bool) {
	walk(root, Path{}, fn)
}

func walk(n *yaml.Node, p Path, fn func(Path, *yaml.Node) bool) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			cp := p.Child(Key(n.Content[i].Value))
			if fn(cp, n.Content[i+1]) {
				walk(n.Content[i+1], cp, fn)
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			cp := p.Child(Index(i))
			if fn(cp, c) {
				walk(c, cp, fn)
			}
		}
	}
}
