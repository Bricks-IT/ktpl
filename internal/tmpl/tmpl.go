// Package tmpl compiles and executes the Go templates found in ktpl string values.
package tmpl

import (
	"maps"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"
)

// captureFunc is appended to the pipeline of single-action templates to capture the typed result.
const captureFunc = "__ktpl_capture"

// Options configures template compilation.
type Options struct {
	LeftDelim  string
	RightDelim string
	Hermetic   bool
}

// Resolver resolves `ref` calls. Implemented by the engine; bound for each execution.
type Resolver interface {
	Ref(id string, p ...string) (any, error)
}

// Compiler compiles templates sharing the same options and function map.
type Compiler struct {
	opts  Options
	funcs template.FuncMap
}

// NewCompiler returns a compiler. Empty delimiters default to "{{" and "}}".
func NewCompiler(opts Options) *Compiler {
	if opts.LeftDelim == "" {
		opts.LeftDelim = "{{"
	}
	if opts.RightDelim == "" {
		opts.RightDelim = "}}"
	}
	return &Compiler{opts: opts, funcs: FuncMap(opts.Hermetic)}
}

// LeftDelim returns the effective left delimiter.
func (c *Compiler) LeftDelim() string { return c.opts.LeftDelim }

// Template is a compiled field template.
type Template struct {
	mu       sync.Mutex
	tmpl     *template.Template
	single   bool
	res      Resolver
	captured any
	gotValue bool
}

// Result is the outcome of an execution.
type Result struct {
	Value any  // string for text templates, any Go value for single-action templates
	Typed bool // Value comes from a single action and keeps its native type
}

// Compile parses src. name is used in error messages (the field path).
func (c *Compiler) Compile(name, src string) (*Template, error) {
	t := &Template{}
	funcs := maps.Clone(c.funcs)
	funcs["ref"] = t.ref
	funcs[captureFunc] = t.capture
	tt, err := template.New(name).
		Delims(c.opts.LeftDelim, c.opts.RightDelim).
		Option("missingkey=error").
		Funcs(funcs).
		Parse(src)
	if err != nil {
		return nil, err
	}
	if act := singleAction(tt); act != nil {
		t.single = true
		ident := parse.NewIdentifier(captureFunc).SetTree(tt.Tree).SetPos(act.Pos)
		act.Pipe.Cmds = append(act.Pipe.Cmds, &parse.CommandNode{
			NodeType: parse.NodeCommand,
			Pos:      act.Pos,
			Args:     []parse.Node{ident},
		})
	}
	t.tmpl = tt
	return t, nil
}

// Single reports whether the template is exactly one action (typed result).
func (t *Template) Single() bool { return t.single }

// Execute runs the template with r bound to `ref`.
func (t *Template) Execute(r Resolver) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.res, t.captured, t.gotValue = r, nil, false
	defer func() { t.res, t.captured = nil, nil }()
	var b strings.Builder
	if err := t.tmpl.Execute(&b, nil); err != nil {
		return Result{}, err
	}
	if t.single && t.gotValue {
		return Result{Value: t.captured, Typed: true}, nil
	}
	return Result{Value: b.String()}, nil
}

func (t *Template) ref(id string, p ...string) (any, error) {
	return t.res.Ref(id, p...)
}

func (t *Template) capture(v any) string {
	t.captured, t.gotValue = v, true
	return ""
}

// singleAction returns the action node if the template body is exactly one action without declarations.
func singleAction(t *template.Template) *parse.ActionNode {
	if t.Tree == nil || t.Root == nil || len(t.Root.Nodes) != 1 {
		return nil
	}
	act, ok := t.Root.Nodes[0].(*parse.ActionNode)
	if !ok || act.Pipe == nil || len(act.Pipe.Decl) > 0 {
		return nil
	}
	return act
}
