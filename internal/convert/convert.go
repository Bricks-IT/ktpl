// Package convert converts between yaml.Node trees and the Go values seen by templates.
package convert

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

// FromNode converts n to a Go value: map[string]any, []any, string, int, float64, bool or nil.
// Timestamps are kept as strings, as written.
func FromNode(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return FromNode(n.Content[0])
	case yaml.AliasNode:
		return FromNode(n.Alias)
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			v, err := FromNode(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[n.Content[i].Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := FromNode(c)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
		return s, nil
	case yaml.ScalarNode:
		if n.ShortTag() == "!!str" {
			return n.Value, nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		if _, ok := v.(time.Time); ok {
			return n.Value, nil
		}
		return v, nil
	default:
		return nil, fmt.Errorf("line %d: unsupported node kind %d", n.Line, n.Kind)
	}
}

// ToNode converts a Go value to a node. Map keys are sorted.
func ToNode(v any) (node *yaml.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("encoding value to YAML node: %v", r)
		}
	}()
	if s, ok := v.(string); ok {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}, nil
	}
	var n yaml.Node
	if encErr := n.Encode(v); encErr != nil {
		return nil, encErr
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return n.Content[0], nil
	}
	return &n, nil
}
