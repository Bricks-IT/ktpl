// Package overlay merges objects coming from several input folders (JSON Merge Patch, RFC 7386).
package overlay

import (
	"fmt"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
)

// Merge folds objects sharing the same identity. Duplicates inside one folder are an error.
// An object from a later folder is merged onto the earlier one, which keeps its position;
// objects only present in later folders are appended. Floating objects are never merged.
// A merged object records the folders it was built from in Sources, in application order.
func Merge(objs []*object.Object, leftDelim string) ([]*object.Object, error) {
	out := make([]*object.Object, 0, len(objs))
	byKey := map[string]*object.Object{}
	lastSeen := map[string]*object.Object{}
	for _, o := range objs {
		if o.Floating {
			out = append(out, o)
			continue
		}
		key := o.ID.Key()
		base, ok := byKey[key]
		if !ok {
			byKey[key] = o
			lastSeen[key] = o
			out = append(out, o)
			continue
		}
		if prev := lastSeen[key]; prev.Folder == o.Folder {
			return nil, fmt.Errorf("duplicate object %s: %s:%d and %s:%d",
				o.ID, prev.File, prev.Line(), o.File, o.Line())
		}
		lastSeen[key] = o
		if len(base.Sources) == 0 {
			base.Sources = []string{folderName(base.Root)}
		}
		base.Sources = append(base.Sources, folderName(o.Root))
		Patch(base.Body(), o.Body(), func(n *yaml.Node) { base.RecordOrigin(n, o.File) })
		if err := base.Reanalyze(leftDelim); err != nil {
			return nil, err
		}
		if base.ID.Key() != key {
			return nil, fmt.Errorf("%s:%d: overlay changes the identity of %s", o.File, o.Line(), o.ID)
		}
	}
	return out, nil
}

// folderName normalizes an input folder as typed on the command line ("templates/prod/" -> "templates/prod").
func folderName(root string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(root)))
}

// Patch applies patch onto dst (both mappings) with JSON Merge Patch semantics:
// maps are merged recursively, other values replace, null deletes the key.
// onCopy is called for every subtree copied from patch.
func Patch(dst, patch *yaml.Node, onCopy func(*yaml.Node)) {
	for i := 0; i+1 < len(patch.Content); i += 2 {
		k, v := patch.Content[i], patch.Content[i+1]
		idx := node.MapIndex(dst, k.Value)
		if node.IsNull(v) {
			if idx >= 0 {
				dst.Content = slices.Delete(dst.Content, idx, idx+2)
			}
			continue
		}
		if idx < 0 {
			nk, nv := node.DeepCopy(k), node.DeepCopy(v)
			if onCopy != nil {
				onCopy(nk)
				onCopy(nv)
			}
			dst.Content = append(dst.Content, nk, nv)
			continue
		}
		cur := dst.Content[idx+1]
		if cur.Kind == yaml.MappingNode && v.Kind == yaml.MappingNode {
			Patch(cur, v, onCopy)
			continue
		}
		repl := node.DeepCopy(v)
		if repl.HeadComment == "" && repl.LineComment == "" && repl.FootComment == "" {
			repl.HeadComment, repl.LineComment, repl.FootComment = cur.HeadComment, cur.LineComment, cur.FootComment
		}
		if onCopy != nil {
			onCopy(repl)
		}
		dst.Content[idx+1] = repl
	}
}
