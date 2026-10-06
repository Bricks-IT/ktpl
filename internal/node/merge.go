package node

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ExpandMergeKeys recursively processes n, expanding any YAML merge key ("<<") found in mapping nodes.
// If "<<" maps to a MappingNode, its key-value pairs are merged into the parent mapping,
// skipping any keys already explicitly defined in the parent mapping.
// If "<<" maps to a SequenceNode of MappingNodes, each mapping is merged in order.
// If "<<" maps to a ScalarNode, it is left unexpanded (as it may be a pending template).
// When expansion occurs, the "<<" key and its value are removed from the parent mapping.
func ExpandMergeKeys(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := ExpandMergeKeys(c); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if err := ExpandMergeKeys(c); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if err := ExpandMergeKeys(n.Content[i+1]); err != nil {
				return err
			}
		}

		idx := MapIndex(n, "<<")
		if idx < 0 {
			return nil
		}

		val := n.Content[idx+1]
		if IsString(val) && strings.Contains(val.Value, "{{") {
			return nil
		}

		var mapsToMerge []*yaml.Node
		switch val.Kind {
		case yaml.MappingNode:
			mapsToMerge = append(mapsToMerge, val)
		case yaml.SequenceNode:
			for _, item := range val.Content {
				if item.Kind != yaml.MappingNode {
					return fmt.Errorf("YAML merge key sequence must contain mappings, got %s", KindName(item))
				}
				mapsToMerge = append(mapsToMerge, item)
			}
		default:
			return fmt.Errorf("YAML merge key '<<' must be a mapping or sequence of mappings, got %s", KindName(val))
		}

		existing := make(map[string]bool, len(n.Content)/2)
		for j := 0; j+1 < len(n.Content); j += 2 {
			if j != idx {
				existing[n.Content[j].Value] = true
			}
		}

		newContent := make([]*yaml.Node, 0, len(n.Content))
		for j := 0; j+1 < len(n.Content); j += 2 {
			if j != idx {
				newContent = append(newContent, n.Content[j], n.Content[j+1])
			}
		}

		for _, m := range mapsToMerge {
			for j := 0; j+1 < len(m.Content); j += 2 {
				k := m.Content[j]
				v := m.Content[j+1]
				if !existing[k.Value] {
					existing[k.Value] = true
					newContent = append(newContent, DeepCopy(k), DeepCopy(v))
				}
			}
		}

		n.Content = newContent
	}
	return nil
}
