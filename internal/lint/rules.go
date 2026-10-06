package lint

import (
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/path"
)

var (
	pMetadata    = path.Path{path.Key("metadata")}
	pName        = path.Path{path.Key("metadata"), path.Key("name")}
	pNamespace   = path.Path{path.Key("metadata"), path.Key("namespace")}
	pLabels      = path.Path{path.Key("metadata"), path.Key("labels")}
	pAnnotations = path.Path{path.Key("metadata"), path.Key("annotations")}
)

// structureRule: apiVersion and kind are non-empty strings, metadata is a mapping with a non-empty name.
type structureRule struct{}

func (structureRule) Name() string { return "structure" }

func (structureRule) Check(ctx *State, o *object.Object) []Error {
	var errs []Error
	body := o.Body()
	for _, key := range []string{"apiVersion", "kind"} {
		v := node.MapValue(body, key)
		if !node.IsString(v) || v.Value == "" {
			errs = append(errs, newError(o, path.Path{path.Key(key)}, v, "%s must be a non-empty string", key))
		}
	}
	meta := node.MapValue(body, "metadata")
	if meta == nil || meta.Kind != yaml.MappingNode {
		if !ctx.pending(o, pMetadata) {
			errs = append(errs, newError(o, pMetadata, meta, "metadata must be a mapping, got %s", node.KindName(meta)))
		}
		return errs
	}
	if name := node.MapValue(meta, "name"); !ctx.pending(o, pName) && (!node.IsString(name) || name.Value == "") {
		errs = append(errs, newError(o, pName, name, "metadata.name must be a non-empty string, got %s", node.KindName(name)))
	}
	if ns := node.MapValue(meta, "namespace"); ns != nil && !node.IsNull(ns) && !ctx.pending(o, pNamespace) && !node.IsString(ns) {
		errs = append(errs, newError(o, pNamespace, ns, "metadata.namespace must be a string, got %s", node.KindName(ns)))
	}
	return errs
}

// nameRule: resolved metadata.name and metadata.namespace are valid Kubernetes names.
type nameRule struct{}

func (nameRule) Name() string { return "name" }

func (nameRule) Check(ctx *State, o *object.Object) []Error {
	var errs []Error
	meta := node.MapValue(o.Body(), "metadata")
	if name := node.MapValue(meta, "name"); node.IsString(name) && name.Value != "" && !ctx.pending(o, pName) {
		validate := ValidDNS1123Subdomain
		if strings.EqualFold(o.ID.Group, "rbac.authorization.k8s.io") {
			validate = ValidPathSegmentName
		}
		if msg := validate(name.Value); msg != "" {
			errs = append(errs, newError(o, pName, name, "metadata.name %q %s", name.Value, msg))
		}
	}
	if ns := node.MapValue(meta, "namespace"); node.IsString(ns) && ns.Value != "" && !ctx.pending(o, pNamespace) {
		if msg := ValidDNS1123Label(ns.Value); msg != "" {
			errs = append(errs, newError(o, pNamespace, ns, "metadata.namespace %q %s", ns.Value, msg))
		}
	}
	return errs
}

// labelsRule: label keys are qualified names, values are valid label strings.
type labelsRule struct{}

func (labelsRule) Name() string { return "labels" }

func (labelsRule) Check(ctx *State, o *object.Object) []Error {
	return checkStringMap(ctx, o, pLabels, "label", true)
}

// annotationsRule: annotation keys are qualified names, values are strings.
type annotationsRule struct{}

func (annotationsRule) Name() string { return "annotations" }

func (annotationsRule) Check(ctx *State, o *object.Object) []Error {
	return checkStringMap(ctx, o, pAnnotations, "annotation", false)
}

func checkStringMap(ctx *State, o *object.Object, p path.Path, what string, labelValues bool) []Error {
	m, err := path.Get(o.Body(), p)
	if err != nil || node.IsNull(m) || ctx.pending(o, p) && m.Kind != yaml.MappingNode {
		return nil
	}
	if m.Kind != yaml.MappingNode {
		return []Error{newError(o, p, m, "%ss must be a mapping, got %s", what, node.KindName(m))}
	}
	var errs []Error
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Value == "<<" {
			continue
		}
		kp := p.Child(path.Key(k.Value))
		if msg := validQualifiedName(k.Value); msg != "" {
			errs = append(errs, newError(o, kp, k, "%s key %q: %s", what, k.Value, msg))
		}
		if ctx.pending(o, kp) {
			continue
		}
		if !node.IsString(v) {
			errs = append(errs, newError(o, kp, v, "%s value must be a string, got %s", what, node.KindName(v)))
			continue
		}
		if labelValues {
			if msg := validLabelValue(v.Value); msg != "" {
				errs = append(errs, newError(o, kp, v, "label value %q %s", v.Value, msg))
			}
		}
	}
	return errs
}

// roundTripRule: the object survives a YAML encode/decode round trip.
type roundTripRule struct{}

func (roundTripRule) Name() string { return "yaml" }

func (roundTripRule) Check(_ *State, o *object.Object) []Error {
	out, err := yaml.Marshal(o.Doc)
	if err == nil {
		var back yaml.Node
		err = yaml.Unmarshal(out, &back)
	}
	if err != nil {
		return []Error{newError(o, nil, nil, "invalid YAML after rendering: %v", err)}
	}
	return nil
}

// duplicateRule: no two objects share the same resolved identity.
type duplicateRule struct{}

func (duplicateRule) Name() string { return "duplicate" }

func (duplicateRule) CheckAll(ctx *State, objs []*object.Object) []Error {
	var errs []Error
	seen := map[string]*object.Object{}
	for _, o := range objs {
		if ctx.pending(o, pName) || ctx.pending(o, pNamespace) {
			continue
		}
		key := o.ID.Key()
		if first, ok := seen[key]; ok {
			errs = append(errs, newError(o, nil, nil, "duplicate object identity, also defined at %s", first.Location(nil)))
			continue
		}
		seen[key] = o
	}
	return errs
}
