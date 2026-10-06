package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bricks-it/ktpl/internal/convert"
	"github.com/bricks-it/ktpl/internal/engine"
	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/render"
)

const previewMax = 80

// stepper implements engine.Observer for --step: it prints a summary after each iteration on w
// and reads a command from r.
type stepper struct {
	in   *bufio.Reader
	out  io.Writer
	prev map[*object.Object]string
}

func newStepper(r io.Reader, w io.Writer, objs []*object.Object) (*stepper, error) {
	s := &stepper{in: bufio.NewReader(r), out: w, prev: map[*object.Object]string{}}
	for _, o := range objs {
		y, err := render.EncodeObject(o)
		if err != nil {
			return nil, err
		}
		s.prev[o] = y
	}
	return s, nil
}

func (s *stepper) Iteration(r *engine.IterationReport) (bool, error) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(s.out, format, args...) }
	p("── Iteration %d/%d ── %d resolved · %d pending\n", r.Iteration, r.Max, len(r.Rendered), len(r.Pending))
	for _, f := range r.Rendered {
		p("  %-40s %-40s = %s\n", f.Obj.ID, f.Path, preview(f))
	}
	for _, f := range r.Pending {
		line := fmt.Sprintf("  pending: %s %s", f.Obj.ID, f.Path)
		if f.Blocker != nil {
			line += fmt.Sprintf(" <- %s %s", f.Blocker.Obj.ID, f.Blocker.Path)
		}
		p("%s\n", line)
	}
	current := map[*object.Object]string{}
	for _, o := range r.Objects {
		y, err := render.EncodeObject(o)
		if err != nil {
			return false, err
		}
		current[o] = y
	}
	defer func() { s.prev = current }()
	if len(r.Pending) == 0 {
		return false, nil
	}
	for {
		p("[Enter] next iteration · [d] diff · [q] quit\n")
		line, err := s.in.ReadString('\n')
		cmd := strings.TrimSpace(line)
		switch {
		case cmd == "q":
			return true, nil
		case cmd == "d":
			for _, o := range r.Objects {
				if d := lineDiff(s.prev[o], current[o]); d != "" {
					p("--- %s (%s)\n%s", o.ID, o.File, d)
				}
			}
		case cmd == "" && err == nil:
			return false, nil
		case err != nil: // EOF: continue without prompting again
			return false, nil
		default:
			p("unknown command %q\n", cmd)
		}
	}
}

func preview(f *engine.Field) string {
	if isSensitive(f) {
		return "[REDACTED]"
	}
	v, err := convert.FromNode(f.Node)
	if err != nil {
		return "?"
	}
	var s string
	if str, ok := v.(string); ok {
		s = str
	} else if b, err := json.Marshal(v); err == nil {
		s = string(b)
	}
	s = strings.ReplaceAll(s, "\n", `\n`)
	if len(s) > previewMax {
		s = s[:previewMax-1] + "…"
	}
	return s
}

func isSensitive(f *engine.Field) bool {
	if f == nil || f.Obj == nil {
		return false
	}
	if strings.EqualFold(f.Obj.ID.Kind, "Secret") {
		return true
	}
	lower := strings.ToLower(f.Path.String())
	return strings.Contains(lower, "password") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "privatekey")
}

// lineDiff returns a minimal line diff ("-"/"+" prefixed, unchanged lines omitted) or "" if equal.
func lineDiff(a, b string) string {
	if a == b {
		return ""
	}
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	// LCS table; manifests are small.
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out.WriteString("- " + x[i] + "\n")
			i++
		default:
			out.WriteString("+ " + y[j] + "\n")
			j++
		}
	}
	for ; i < len(x); i++ {
		out.WriteString("- " + x[i] + "\n")
	}
	for ; j < len(y); j++ {
		out.WriteString("+ " + y[j] + "\n")
	}
	return out.String()
}
