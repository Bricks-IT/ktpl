// Package lint validates objects after loading and after every iteration.
package lint

import (
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/path"
)

// Error is a located lint error.
type Error struct {
	ID       string
	Path     string
	Location string // file:line
	Msg      string
}

func (e Error) String() string {
	if e.Path == "" {
		return fmt.Sprintf("%s (%s): %s", e.ID, e.Location, e.Msg)
	}
	return fmt.Sprintf("%s %s (%s): %s", e.ID, e.Path, e.Location, e.Msg)
}

// Context gives rules access to the engine state.
type Context struct {
	// IsPending reports whether the value at p (or any of its ancestors or descendants) is still pending.
	IsPending func(o *object.Object, p path.Path) bool
}

func (c *Context) pending(o *object.Object, p path.Path) bool {
	return c != nil && c.IsPending != nil && c.IsPending(o, p)
}

// Rule checks one object.
type Rule interface {
	Name() string
	Check(ctx *Context, o *object.Object) []Error
}

// GlobalRule checks all objects at once.
type GlobalRule interface {
	Name() string
	CheckAll(ctx *Context, objs []*object.Object) []Error
}

// Rules returns the per-object rules in their fixed execution order.
func Rules() []Rule {
	return []Rule{structureRule{}, nameRule{}, labelsRule{}, annotationsRule{}, roundTripRule{}}
}

// GlobalRules returns the global rules in their fixed execution order.
func GlobalRules() []GlobalRule {
	return []GlobalRule{duplicateRule{}}
}

// Run applies every rule and returns all errors, in object order then rule order.
func Run(ctx *Context, objs []*object.Object) []Error {
	var errs []Error
	rules := Rules()
	for _, o := range objs {
		for _, r := range rules {
			errs = append(errs, r.Check(ctx, o)...)
		}
	}
	for _, r := range GlobalRules() {
		errs = append(errs, r.CheckAll(ctx, objs)...)
	}
	return errs
}

func newError(o *object.Object, p path.Path, n *yaml.Node, format string, args ...any) Error {
	return Error{ID: o.ID.String(), Path: p.String(), Location: o.Location(n), Msg: fmt.Sprintf(format, args...)}
}
