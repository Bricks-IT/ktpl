// Package render writes rendered objects: ktpl.io/rendered annotation and YAML encoding.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/engine"
	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
)

// Options configures the output.
type Options struct {
	Annotations bool // write ktpl.io/rendered
	KeepLocal   bool // emit local objects
}

// Annotate writes the ktpl.io/rendered annotation on every object that has rendered fields.
func Annotate(res *engine.Result) error {
	for _, o := range res.Objects {
		fields := res.RenderedPaths(o)
		if len(fields) == 0 {
			continue
		}
		var b strings.Builder
		b.WriteByte('{')
		for i, f := range fields {
			if i > 0 {
				b.WriteByte(',')
			}
			key, err := jsonString(f.Path.String())
			if err != nil {
				return err
			}
			b.WriteString(key)
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(f.Iteration))
		}
		b.WriteByte('}')
		setAnnotation(o, object.AnnotationRendered, b.String())
	}
	return nil
}

func jsonString(s string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func setAnnotation(o *object.Object, key, value string) {
	meta := node.MapValue(o.Body(), "metadata")
	ann := node.MapValue(meta, "annotations")
	if ann == nil || ann.Kind != yaml.MappingNode {
		ann = node.Mapping()
		node.SetMapValue(meta, "annotations", ann)
	}
	v := node.String(value)
	v.Style = yaml.SingleQuotedStyle
	node.SetMapValue(ann, key, v)
}

// Selected returns the objects to emit, in order.
func Selected(objs []*object.Object, opts Options) []*object.Object {
	out := make([]*object.Object, 0, len(objs))
	for _, o := range objs {
		if o.Local && !opts.KeepLocal {
			continue
		}
		out = append(out, o)
	}
	return out
}

// Encode writes objs as a multi-document YAML stream (indent 2).
func Encode(w io.Writer, objs []*object.Object) error {
	if len(objs) == 0 {
		return nil
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	for _, o := range objs {
		if err := enc.Encode(o.Doc); err != nil {
			return fmt.Errorf("%s: encoding %s: %w", o.File, o.ID, err)
		}
	}
	return enc.Close()
}

// EncodeObject returns the YAML of a single object (used by --step diffs).
func EncodeObject(o *object.Object) (string, error) {
	var b bytes.Buffer
	if err := Encode(&b, []*object.Object{o}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// WriteDir writes each object to dir/<path of its source file relative to its input folder>.
// Objects from the same source file are written to the same file, in order.
func WriteDir(dir string, objs []*object.Object) error {
	var order []string
	groups := map[string][]*object.Object{}
	for _, o := range objs {
		rel := o.Rel()
		if _, ok := groups[rel]; !ok {
			order = append(order, rel)
		}
		groups[rel] = append(groups[rel], o)
	}
	for _, rel := range order {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		var b bytes.Buffer
		if err := Encode(&b, groups[rel]); err != nil {
			return err
		}
		if err := os.WriteFile(target, b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
