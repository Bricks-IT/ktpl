// Package object models a Kubernetes object loaded by ktpl and its identity.
package object

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/path"
)

// Annotations read or written by ktpl.
const (
	AnnotationLocal       = "ktpl.io/local"
	AnnotationLocalConfig = "config.kubernetes.io/local-config"
	AnnotationIgnore      = "ktpl.io/ignore"
	AnnotationIgnoreKey   = "ktpl.io/ignore-key"
	AnnotationRendered    = "ktpl.io/rendered"
)

// Identity identifies an object: namespace (may be empty), API group, kind and name.
type Identity struct {
	Namespace string
	Group     string
	Kind      string
	Name      string
}

// String formats the identity as "ns/Kind/name" or "Kind/name".
func (id Identity) String() string {
	if id.Namespace == "" {
		return id.Kind + "/" + id.Name
	}
	return id.Namespace + "/" + id.Kind + "/" + id.Name
}

// Key returns a normalized key (case-insensitive kind) used to detect duplicates and match overlays.
func (id Identity) Key() string {
	return id.Namespace + "/" + strings.ToLower(id.Kind) + "." + id.Group + "/" + id.Name
}

// Object is one YAML document of the input.
type Object struct {
	Doc         *yaml.Node // document node; Doc.Content[0] is the body mapping
	File        string     // display path of the source file (slash separated)
	Root        string     // input folder the file was found in
	Folder      int        // index of Root in the command line
	APIVersion  string
	ID          Identity
	Floating    bool // metadata.name or metadata.namespace is templated: never referenceable
	Local       bool // not emitted unless --keep-local
	Ignore      bool // never templated
	IgnorePaths []path.Path

	// origins records nodes copied from another file by an overlay merge.
	origins map[*yaml.Node]string
}

// Error is a load-time error located in a file.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg) }

// Body returns the root mapping of the object.
func (o *Object) Body() *yaml.Node { return o.Doc.Content[0] }

// Line returns the line where the object starts.
func (o *Object) Line() int { return o.Body().Line }

// Rel returns the path of the source file relative to its input folder.
func (o *Object) Rel() string {
	rel, err := filepath.Rel(filepath.FromSlash(o.Root), filepath.FromSlash(o.File))
	if err != nil || o.Root == o.File {
		return filepath.Base(o.File)
	}
	return filepath.ToSlash(rel)
}

// FileOf returns the source file of n: the overlay file it was copied from, or the object's file.
func (o *Object) FileOf(n *yaml.Node) string {
	if f, ok := o.origins[n]; ok {
		return f
	}
	return o.File
}

// Location formats "file:line" for n.
func (o *Object) Location(n *yaml.Node) string {
	if n == nil {
		return fmt.Sprintf("%s:%d", o.File, o.Line())
	}
	return fmt.Sprintf("%s:%d", o.FileOf(n), n.Line)
}

// RecordOrigin marks every node of the subtree n as coming from file.
func (o *Object) RecordOrigin(n *yaml.Node, file string) {
	if o.origins == nil {
		o.origins = map[*yaml.Node]string{}
	}
	node.Walk(n, func(c *yaml.Node) { o.origins[c] = file })
}

// New analyses a decoded document and returns the corresponding object.
// leftDelim is used to detect templated identity fields.
func New(doc *yaml.Node, file, root string, folder int, leftDelim string) (*Object, error) {
	o := &Object{Doc: doc, File: file, Root: root, Folder: folder}
	if err := o.analyze(leftDelim); err != nil {
		return nil, err
	}
	return o, nil
}

// Reanalyze recomputes identity and flags after the body was modified (overlay merge).
func (o *Object) Reanalyze(leftDelim string) error { return o.analyze(leftDelim) }

func (o *Object) errorf(n *yaml.Node, format string, args ...any) error {
	line := o.Line()
	if n != nil {
		line = n.Line
	}
	file := o.File
	if n != nil {
		file = o.FileOf(n)
	}
	return &Error{File: file, Line: line, Msg: fmt.Sprintf(format, args...)}
}

func (o *Object) analyze(leftDelim string) error {
	body := o.Body()
	if body.Kind != yaml.MappingNode {
		return o.errorf(body, "document must be a mapping, got %s", node.KindName(body))
	}
	apiVersion, err := o.requiredString(body, "apiVersion", leftDelim)
	if err != nil {
		return err
	}
	kind, err := o.requiredString(body, "kind", leftDelim)
	if err != nil {
		return err
	}
	meta := node.MapValue(body, "metadata")
	if meta == nil {
		return o.errorf(body, "%s: metadata is required", kind)
	}
	if meta.Kind != yaml.MappingNode {
		return o.errorf(meta, "metadata must be a mapping, got %s", node.KindName(meta))
	}
	name := node.MapValue(meta, "name")
	if !node.IsString(name) || name.Value == "" {
		at := name
		if at == nil {
			at = meta
		}
		return o.errorf(at, "metadata.name must be a non-empty string")
	}
	var namespace string
	floating := strings.Contains(name.Value, leftDelim)
	if ns := node.MapValue(meta, "namespace"); ns != nil && !node.IsNull(ns) {
		if !node.IsString(ns) {
			return o.errorf(ns, "metadata.namespace must be a string, got %s", node.KindName(ns))
		}
		namespace = ns.Value
		floating = floating || strings.Contains(ns.Value, leftDelim)
	}

	o.APIVersion = apiVersion
	o.ID = Identity{Namespace: namespace, Group: GroupOf(apiVersion), Kind: kind, Name: name.Value}
	o.Floating = floating
	o.Local, o.Ignore, o.IgnorePaths = false, false, nil

	ann := node.MapValue(meta, "annotations")
	if ann == nil || ann.Kind != yaml.MappingNode {
		return nil
	}
	isTrue := func(key string) bool {
		v := node.MapValue(ann, key)
		return node.IsString(v) && v.Value == "true"
	}
	o.Local = isTrue(AnnotationLocal) || isTrue(AnnotationLocalConfig)
	o.Ignore = isTrue(AnnotationIgnore)
	if v := node.MapValue(ann, AnnotationIgnoreKey); v != nil {
		if !node.IsString(v) {
			return o.errorf(v, "%s must be a JSON list of paths, got %s", AnnotationIgnoreKey, node.KindName(v))
		}
		var raw []string
		if err := json.Unmarshal([]byte(v.Value), &raw); err != nil {
			return o.errorf(v, "%s must be a JSON list of paths: %v", AnnotationIgnoreKey, err)
		}
		for _, r := range raw {
			p, err := path.Parse(r)
			if err != nil {
				return o.errorf(v, "%s: %v", AnnotationIgnoreKey, err)
			}
			o.IgnorePaths = append(o.IgnorePaths, p)
		}
	}
	return nil
}

func (o *Object) requiredString(body *yaml.Node, key, leftDelim string) (string, error) {
	v := node.MapValue(body, key)
	if !node.IsString(v) || v.Value == "" {
		at := v
		if at == nil {
			at = body
		}
		return "", o.errorf(at, "%s must be a non-empty string", key)
	}
	if strings.Contains(v.Value, leftDelim) {
		return "", o.errorf(v, "%s cannot be templated", key)
	}
	return v.Value, nil
}

// RefreshIdentity re-reads metadata.name and metadata.namespace (used for floating objects once rendered).
func (o *Object) RefreshIdentity() {
	meta := node.MapValue(o.Body(), "metadata")
	if v := node.MapValue(meta, "name"); node.IsString(v) {
		o.ID.Name = v.Value
	}
	if v := node.MapValue(meta, "namespace"); node.IsString(v) {
		o.ID.Namespace = v.Value
	}
}

// IsIgnored reports whether p is inside a ktpl.io/ignore-key subtree.
func (o *Object) IsIgnored(p path.Path) bool {
	for _, ip := range o.IgnorePaths {
		if p.HasPrefix(ip) {
			return true
		}
	}
	return false
}

// GroupOf returns the API group of an apiVersion ("apps/v1" -> "apps", "v1" -> "").
func GroupOf(apiVersion string) string {
	if i := strings.LastIndexByte(apiVersion, '/'); i >= 0 {
		return apiVersion[:i]
	}
	return ""
}
