// Package node provides small helpers on go.yaml.in/yaml/v3 nodes shared by every ktpl package.
package node

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// DeepCopy returns a deep copy of n. Alias targets are copied as well.
// Visited nodes are tracked to prevent infinite recursion on cyclic structures.
func DeepCopy(n *yaml.Node) *yaml.Node {
	return deepCopyWithVisited(n, make(map[*yaml.Node]*yaml.Node))
}

func deepCopyWithVisited(n *yaml.Node, visited map[*yaml.Node]*yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if c, ok := visited[n]; ok {
		return c
	}
	c := *n
	visited[n] = &c
	if n.Alias != nil {
		c.Alias = deepCopyWithVisited(n.Alias, visited)
	}
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = deepCopyWithVisited(child, visited)
		}
	}
	return &c
}

// MapIndex returns the index in m.Content of the key node named key, or -1.
func MapIndex(m *yaml.Node, key string) int {
	if m == nil || m.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// MapValue returns the value node of key in mapping m, or nil.
func MapValue(m *yaml.Node, key string) *yaml.Node {
	if i := MapIndex(m, key); i >= 0 {
		return m.Content[i+1]
	}
	return nil
}

// MapKey returns the key node of key in mapping m, or nil.
func MapKey(m *yaml.Node, key string) *yaml.Node {
	if i := MapIndex(m, key); i >= 0 {
		return m.Content[i]
	}
	return nil
}

// SetMapValue sets key to value in mapping m, appending the pair if the key does not exist.
func SetMapValue(m *yaml.Node, key string, value *yaml.Node) {
	if i := MapIndex(m, key); i >= 0 {
		m.Content[i+1] = value
		return
	}
	m.Content = append(m.Content, String(key), value)
}

// String returns a plain string scalar node.
func String(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// Mapping returns an empty block mapping node.
func Mapping() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// IsString reports whether n is a string scalar.
func IsString(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str"
}

// IsNull reports whether n is a null scalar.
func IsNull(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// KindName returns a human readable type name used in error messages.
func KindName(n *yaml.Node) string {
	if n == nil {
		return "nothing"
	}
	switch n.Kind {
	case yaml.MappingNode:
		return "map"
	case yaml.SequenceNode:
		return "list"
	case yaml.AliasNode:
		return KindName(n.Alias)
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!str":
			return "string"
		case "!!int":
			return "int"
		case "!!float":
			return "float"
		case "!!bool":
			return "bool"
		case "!!null":
			return "null"
		default:
			return n.ShortTag()
		}
	default:
		return "unknown"
	}
}

// Walk calls fn for n and every descendant, depth first, in document order.
// Tracks visited nodes to prevent cycles from causing infinite recursion.
func Walk(n *yaml.Node, fn func(*yaml.Node)) {
	walkWithVisited(n, fn, make(map[*yaml.Node]bool))
}

func walkWithVisited(n *yaml.Node, fn func(*yaml.Node), visited map[*yaml.Node]bool) {
	if n == nil || visited[n] {
		return
	}
	visited[n] = true
	fn(n)
	for _, c := range n.Content {
		walkWithVisited(c, fn, visited)
	}
}

// ClearComments removes every comment from n and its descendants.
func ClearComments(n *yaml.Node) {
	Walk(n, func(c *yaml.Node) {
		c.HeadComment, c.LineComment, c.FootComment = "", "", ""
	})
}

// SetLine sets the line (and column) of n and every descendant.
func SetLine(n *yaml.Node, line, column int) {
	Walk(n, func(c *yaml.Node) {
		c.Line, c.Column = line, column
	})
}

const (
	maxAliasDepth      = 100
	maxAliasExpansions = 10000
)

// ExpandAliases replaces every alias node below n by a deep copy of its target and drops anchors,
// so that the tree can be mutated safely. It returns an error if cycles or limits are exceeded.
func ExpandAliases(n *yaml.Node) error {
	expansions := 0
	return expandAliases(n, 0, &expansions, make(map[*yaml.Node]bool))
}

func expandAliases(n *yaml.Node, depth int, expansions *int, active map[*yaml.Node]bool) error {
	if n == nil {
		return nil
	}
	if depth > maxAliasDepth {
		return fmt.Errorf("line %d: alias expansion depth exceeded (%d)", n.Line, maxAliasDepth)
	}
	for i, c := range n.Content {
		if c.Kind == yaml.AliasNode {
			*expansions++
			if *expansions > maxAliasExpansions {
				return fmt.Errorf("line %d: alias expansion limit exceeded (%d)", c.Line, maxAliasExpansions)
			}
			target, err := resolveAlias(c, active)
			if err != nil {
				return err
			}
			active[c] = true
			cp := DeepCopy(target)
			n.Content[i] = cp
			if err := expandAliases(cp, depth+1, expansions, active); err != nil {
				return err
			}
			delete(active, c)
			continue
		}
		if err := expandAliases(n.Content[i], depth+1, expansions, active); err != nil {
			return err
		}
	}
	n.Anchor = ""
	return nil
}

func resolveAlias(n *yaml.Node, active map[*yaml.Node]bool) (*yaml.Node, error) {
	seen := make(map[*yaml.Node]bool)
	cur := n
	for cur.Kind == yaml.AliasNode && cur.Alias != nil {
		if seen[cur] || active[cur] {
			return nil, fmt.Errorf("line %d: circular alias reference detected", cur.Line)
		}
		seen[cur] = true
		cur = cur.Alias
	}
	if cur.Kind == yaml.AliasNode {
		return nil, fmt.Errorf("line %d: unresolved alias", cur.Line)
	}
	return cur, nil
}
