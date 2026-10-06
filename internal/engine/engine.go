// Package engine implements ktpl's iterative rendering: pending registry, snapshot semantics,
// deferral, lint after each iteration, cycle and max-iteration detection.
package engine

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/convert"
	"github.com/bricks-it/ktpl/internal/lint"
	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/path"
	"github.com/bricks-it/ktpl/internal/tmpl"
)

// errDeferred is returned by `ref` when it reaches a pending value; the field is retried next iteration.
var errDeferred = errors.New("deferred: target is still pending")

// DefaultMaxIterations is the default value of --max-iterations.
const DefaultMaxIterations = 5

// Options configures a run.
type Options struct {
	MaxIterations int // >= 1
	StopAfter     int // 0 = disabled; otherwise emit the partial state after this iteration
	Template      tmpl.Options
	Observer      Observer // optional, used by --step
}

// Observer is notified after each iteration. Returning stop=true ends the run with a partial state.
type Observer interface {
	Iteration(r *IterationReport) (stop bool, err error)
}

// IterationReport describes one finished iteration.
type IterationReport struct {
	Iteration int
	Max       int
	Rendered  []*Field
	Pending   []*Field
	Objects   []*object.Object
}

// Field is a templated string value.
type Field struct {
	Obj       *object.Object
	Path      path.Path
	Node      *yaml.Node // live node, replaced in place when rendered
	Iteration int        // 0 while pending
	Blocker   *Field     // pending field that deferred the last attempt

	tmpl     *tmpl.Template
	location string
}

// Location returns "file:line" of the template.
func (f *Field) Location() string { return f.location }

// Result is the outcome of a run.
type Result struct {
	Objects    []*object.Object
	Fields     []*Field // all fields, in registration (document) order
	Iterations int
	Complete   bool // false when stopped early (--stop-after or observer)
}

// RenderedPaths returns, in document order, the rendered fields of o.
func (r *Result) RenderedPaths(o *object.Object) []*Field {
	var out []*Field
	for _, f := range r.Fields {
		if f.Obj == o && f.Iteration > 0 {
			out = append(out, f)
		}
	}
	return out
}

// FieldError is an error located on a field.
type FieldError struct {
	Field     *Field
	Iteration int
	Err       error
}

func (e *FieldError) Error() string {
	var refErr *RefError
	msg := e.Err.Error()
	if errors.As(e.Err, &refErr) {
		msg = refErr.Error()
	}
	prefix := ""
	if e.Iteration > 0 {
		prefix = fmt.Sprintf("iteration %d: ", e.Iteration)
	}
	return fmt.Sprintf("%s%s %s (%s): %s", prefix, e.Field.Obj.ID, e.Field.Path, e.Field.location, msg)
}

func (e *FieldError) Unwrap() error { return e.Err }

// RefError is a failed `ref` call.
type RefError struct {
	ID   string
	Path *string
	Msg  string
}

func (e *RefError) Error() string {
	if e.Path != nil {
		return fmt.Sprintf("ref %q %q: %s", e.ID, *e.Path, e.Msg)
	}
	return fmt.Sprintf("ref %q: %s", e.ID, e.Msg)
}

// LintError aborts an iteration.
type LintError struct {
	Iteration int
	Errors    []lint.Error
}

func (e *LintError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "iteration %d: lint failed, %d error(s):", e.Iteration, len(e.Errors))
	for _, le := range e.Errors {
		b.WriteString("\n  ")
		b.WriteString(le.String())
	}
	return b.String()
}

// PendingError reports fields that could not be rendered (cycle or iteration limit).
type PendingError struct {
	Iteration int
	Max       int
	Cycle     bool
	Pending   []*Field
}

func (e *PendingError) Error() string {
	var b strings.Builder
	if e.Cycle {
		fmt.Fprintf(&b, "iteration %d: no progress, %d pending field(s), dependency cycle:", e.Iteration, len(e.Pending))
	} else {
		fmt.Fprintf(&b, "%d pending field(s) after %d iteration(s) (max-iterations=%d):", len(e.Pending), e.Iteration, e.Max)
	}
	for _, f := range e.Pending {
		fmt.Fprintf(&b, "\n  %s %s (%s)", f.Obj.ID, f.Path, f.location)
		if f.Blocker != nil {
			fmt.Fprintf(&b, " <- %s %s", f.Blocker.Obj.ID, f.Blocker.Path)
		}
	}
	return b.String()
}

type engine struct {
	opts     Options
	compiler *tmpl.Compiler
	objs     []*object.Object
	fields   []*Field
	index    map[string][]*object.Object // "ns/kind/name" (lower kind) -> objects of any group
	floating bool
	pending  map[*object.Object][]*Field
}

// Run renders objs in place and returns the result.
func Run(objs []*object.Object, opts Options) (*Result, error) {
	if opts.MaxIterations < 1 {
		return nil, fmt.Errorf("max-iterations must be >= 1, got %d", opts.MaxIterations)
	}
	e := &engine{opts: opts, compiler: tmpl.NewCompiler(opts.Template), objs: objs, index: map[string][]*object.Object{}}
	for _, o := range objs {
		if o.Floating {
			e.floating = true
			continue
		}
		k := indexKey(o.ID.Namespace, o.ID.Kind, o.ID.Name)
		e.index[k] = append(e.index[k], o)
	}
	if err := e.register(); err != nil {
		return nil, err
	}
	res := &Result{Objects: objs, Fields: e.fields}
	e.refreshPending()
	for _, o := range objs {
		if err := node.ExpandMergeKeys(o.Body()); err != nil {
			return nil, err
		}
	}
	if err := e.lint(0); err != nil {
		return nil, err
	}

	for k := 1; ; k++ {
		pending := e.pendingFields()
		if len(pending) == 0 {
			res.Complete = true
			return res, nil
		}
		rendered, err := e.iterate(k, pending)
		if err != nil {
			return nil, err
		}
		res.Iterations = k
		e.refreshPending()
		if err := e.lint(k); err != nil {
			return nil, err
		}
		remaining := e.pendingFields()
		if opts.Observer != nil {
			stop, err := opts.Observer.Iteration(&IterationReport{Iteration: k, Max: opts.MaxIterations, Rendered: rendered, Pending: remaining, Objects: objs})
			if err != nil {
				return nil, err
			}
			if stop && len(remaining) > 0 {
				return res, nil
			}
		}
		switch {
		case len(remaining) == 0:
			res.Complete = true
			return res, nil
		case len(rendered) == 0:
			return nil, &PendingError{Iteration: k, Cycle: true, Pending: remaining}
		case k == opts.StopAfter:
			return res, nil
		case k >= opts.MaxIterations:
			return nil, &PendingError{Iteration: k, Max: opts.MaxIterations, Pending: remaining}
		}
	}
}

func indexKey(ns, kind, name string) string {
	return ns + "/" + strings.ToLower(kind) + "/" + name
}

// register finds every templated string value, in document order, and compiles it.
func (e *engine) register() error {
	left := e.compiler.LeftDelim()
	for _, o := range e.objs {
		if o.Ignore {
			continue
		}
		var err error
		path.Walk(o.Body(), func(p path.Path, n *yaml.Node) bool {
			if err != nil || o.IsIgnored(p) {
				return false
			}
			if !node.IsString(n) || !strings.Contains(n.Value, left) {
				return true
			}
			f := &Field{Obj: o, Path: p, Node: n, location: o.Location(n)}
			t, cerr := e.compiler.Compile(p.String(), n.Value)
			if cerr != nil {
				err = &FieldError{Field: f, Err: cerr}
				return false
			}
			f.tmpl = t
			e.fields = append(e.fields, f)
			return false
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) pendingFields() []*Field {
	var out []*Field
	for _, f := range e.fields {
		if f.Iteration == 0 {
			out = append(out, f)
		}
	}
	return out
}

func (e *engine) refreshPending() {
	e.pending = map[*object.Object][]*Field{}
	for _, f := range e.fields {
		if f.Iteration == 0 {
			e.pending[f.Obj] = append(e.pending[f.Obj], f)
		}
	}
}

func (e *engine) isPending(o *object.Object, p path.Path) bool {
	return e.overlapping(o, p) != nil
}

func (e *engine) overlapping(o *object.Object, p path.Path) *Field {
	for _, f := range e.pending[o] {
		if f.Path.Overlaps(p) {
			return f
		}
	}
	return nil
}

func (e *engine) lint(k int) error {
	errs := lint.Run(&lint.State{IsPending: e.isPending}, e.objs)
	if len(errs) > 0 {
		return &LintError{Iteration: k, Errors: errs}
	}
	return nil
}

type outcome struct {
	field *Field
	value *yaml.Node
}

// iterate executes every pending field against the current state (= snapshot S(k-1)): results are
// buffered and applied only once all fields have been executed.
func (e *engine) iterate(k int, pending []*Field) ([]*Field, error) {
	var done []outcome
	for _, f := range pending {
		r := &resolver{e: e, sources: map[uintptr]*yaml.Node{}}
		out, err := f.tmpl.Execute(r)
		if errors.Is(err, errDeferred) {
			f.Blocker = r.blocker
			continue
		}
		if err != nil {
			return nil, &FieldError{Field: f, Iteration: k, Err: err}
		}
		n, err := r.toNode(out)
		if err != nil {
			return nil, &FieldError{Field: f, Iteration: k, Err: err}
		}
		done = append(done, outcome{field: f, value: n})
	}
	rendered := make([]*Field, 0, len(done))
	for _, d := range done {
		replace(d.field.Node, d.value)
		d.field.Iteration = k
		d.field.Blocker = nil
		if d.field.Obj.Floating {
			d.field.Obj.RefreshIdentity()
		}
		rendered = append(rendered, d.field)
	}
	for _, o := range e.objs {
		if err := node.ExpandMergeKeys(o.Body()); err != nil {
			return nil, err
		}
	}
	return rendered, nil
}

// replace overwrites dst with src, keeping dst's comments and position.
func replace(dst, src *yaml.Node) {
	head, line, foot := dst.HeadComment, dst.LineComment, dst.FootComment
	l, c := dst.Line, dst.Column
	node.SetLine(src, l, c)
	*dst = *src
	dst.HeadComment, dst.LineComment, dst.FootComment = head, line, foot
	dst.Line, dst.Column = l, c
}

// resolver implements tmpl.Resolver for one field execution.
type resolver struct {
	e       *engine
	blocker *Field
	sources map[uintptr]*yaml.Node // map/slice values returned by ref -> source node (order preservation)
}

func (r *resolver) Ref(id string, ps ...string) (any, error) {
	if len(ps) > 1 {
		return nil, &RefError{ID: id, Msg: fmt.Sprintf("expected at most 2 arguments, got %d", len(ps)+1)}
	}
	var rawPath *string
	p := path.Path{}
	if len(ps) == 1 {
		rawPath = &ps[0]
		var err error
		if p, err = path.Parse(ps[0]); err != nil {
			return nil, &RefError{ID: id, Path: rawPath, Msg: err.Error()}
		}
	}
	ref, err := object.ParseRef(id)
	if err != nil {
		return nil, &RefError{ID: id, Msg: err.Error()}
	}
	o, err := r.e.resolve(ref)
	if err != nil {
		return nil, &RefError{ID: id, Msg: err.Error()}
	}
	if b := r.e.overlapping(o, p); b != nil {
		r.blocker = b
		return nil, errDeferred
	}
	n, err := path.Get(o.Body(), p)
	if err != nil {
		var nf *path.NotFoundError
		if errors.As(err, &nf) {
			return nil, &RefError{ID: id, Path: rawPath, Msg: nf.Msg}
		}
		return nil, &RefError{ID: id, Path: rawPath, Msg: err.Error()}
	}
	v, err := convert.FromNode(n)
	if err != nil {
		return nil, &RefError{ID: id, Path: rawPath, Msg: err.Error()}
	}
	if k := pointerOf(v); k != 0 {
		r.sources[k] = n
	}
	return v, nil
}

func (e *engine) resolve(ref object.Ref) (*object.Object, error) {
	var matches []*object.Object
	for _, o := range e.index[indexKey(ref.Namespace, ref.Kind, ref.Name)] {
		if ref.Matches(o) {
			matches = append(matches, o)
		}
	}
	switch len(matches) {
	case 0:
		msg := "object not found"
		if e.floating {
			msg += " (objects with a templated metadata.name or metadata.namespace cannot be referenced)"
		}
		return nil, errors.New(msg)
	case 1:
		return matches[0], nil
	default:
		groups := make([]string, len(matches))
		for i, m := range matches {
			groups[i] = strings.ToLower(m.ID.Kind) + "." + m.ID.Group
		}
		return nil, fmt.Errorf("ambiguous kind %q, qualify it with its group: %s", ref.Kind, strings.Join(groups, ", "))
	}
}

// toNode converts an execution result to the node that replaces the template.
func (r *resolver) toNode(res tmpl.Result) (*yaml.Node, error) {
	if !res.Typed {
		return node.String(res.Value.(string)), nil
	}
	if k := pointerOf(res.Value); k != 0 {
		if src, ok := r.sources[k]; ok {
			// Same value as returned by ref: copy the source node to keep key order,
			// unless a function mutated it in place (e.g. sprig's set).
			if orig, err := convert.FromNode(src); err == nil && reflect.DeepEqual(orig, res.Value) {
				c := node.DeepCopy(src)
				node.ClearComments(c)
				return c, nil
			}
		}
	}
	return convert.ToNode(res.Value)
}

func pointerOf(v any) uintptr {
	switch v.(type) {
	case map[string]any, []any:
		rv := reflect.ValueOf(v)
		if rv.IsNil() {
			return 0
		}
		return rv.Pointer()
	}
	return 0
}
