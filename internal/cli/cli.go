// Package cli implements the ktpl command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/bricks-it/ktpl/internal/engine"
	"github.com/bricks-it/ktpl/internal/loader"
	"github.com/bricks-it/ktpl/internal/overlay"
	"github.com/bricks-it/ktpl/internal/render"
	"github.com/bricks-it/ktpl/internal/tmpl"
)

// version is set at build time with -ldflags "-X github.com/bricks-it/ktpl/internal/cli.version=...".
var version = "dev"

// Exit codes.
const (
	ExitOK     = 0
	ExitError  = 1 // rendering, lint or I/O error
	ExitUsage  = 2 // invalid flags or arguments
	defaultDel = "{{"
)

type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

type options struct {
	maxIterations int
	step          bool
	stopAfter     int
	output        string
	noAnnotations bool
	keepLocal     bool
	hermetic      bool
	leftDelim     string
	rightDelim    string
}

// Run executes ktpl with args (without the program name) and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "ktpl [flags] <folder>...",
		Short: "Render native Kubernetes manifests with cross-object Go templates",
		Long: `ktpl renders native Kubernetes YAML manifests whose string values may contain Go templates
(Sprig + ref). ref "<ns>/<kind>/<name>" "<path>" reads a field of another object. Rendering is
iterative: a field is rendered once every value it references is resolved.`,
		Version:       resolveVersion(),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return &usageError{errors.New("at least one input folder is required")}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if err := opts.validate(); err != nil {
				return &usageError{err}
			}
			return execute(args, opts, stdin, stdout, stderr)
		},
	}
	cmd.SetArgs(args)
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetVersionTemplate("ktpl {{.Version}}\n")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &usageError{err} })

	f := cmd.Flags()
	f.SortFlags = false
	f.IntVarP(&opts.maxIterations, "max-iterations", "i", engine.DefaultMaxIterations, "maximum number of iterations")
	f.BoolVarP(&opts.step, "step", "s", false, "interactive mode, pause after each iteration")
	f.IntVar(&opts.stopAfter, "stop-after", 0, "stop after N iterations and emit the partial state")
	f.StringVarP(&opts.output, "output", "o", "", "output folder (default: stdout, multi-document stream)")
	f.BoolVar(&opts.noAnnotations, "no-annotations", false, "do not write the ktpl.io/rendered annotation")
	f.BoolVar(&opts.keepLocal, "keep-local", false, "also emit local objects")
	f.BoolVar(&opts.hermetic, "hermetic", false, "forbid non-deterministic template functions")
	f.StringVar(&opts.leftDelim, "left-delim", defaultDel, "left template delimiter")
	f.StringVar(&opts.rightDelim, "right-delim", "}}", "right template delimiter")

	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		var ue *usageError
		if errors.As(err, &ue) {
			_, _ = fmt.Fprintln(stderr, "Run 'ktpl --help' for usage.")
			return ExitUsage
		}
		return ExitError
	}
	return ExitOK
}

func (o *options) validate() error {
	switch {
	case o.maxIterations < 1:
		return fmt.Errorf("--max-iterations must be >= 1, got %d", o.maxIterations)
	case o.stopAfter < 0:
		return fmt.Errorf("--stop-after must be >= 0, got %d", o.stopAfter)
	case o.stopAfter > o.maxIterations:
		return fmt.Errorf("--stop-after (%d) cannot exceed --max-iterations (%d)", o.stopAfter, o.maxIterations)
	case o.leftDelim == "" || o.rightDelim == "":
		return errors.New("--left-delim and --right-delim must not be empty")
	}
	return nil
}

func execute(roots []string, opts *options, stdin io.Reader, stdout, stderr io.Writer) error {
	tmplOpts := tmpl.Options{LeftDelim: opts.leftDelim, RightDelim: opts.rightDelim, Hermetic: opts.hermetic}

	objs, err := loader.Load(roots, loader.Options{LeftDelim: opts.leftDelim})
	if err != nil {
		return err
	}
	objs, err = overlay.Merge(objs, opts.leftDelim)
	if err != nil {
		return err
	}

	engOpts := engine.Options{MaxIterations: opts.maxIterations, StopAfter: opts.stopAfter, Template: tmplOpts}
	if opts.step {
		if !isTerminal(stdin) {
			return &usageError{errors.New("--step requires an interactive terminal on stdin")}
		}
		st, err := newStepper(stdin, stderr, objs)
		if err != nil {
			return err
		}
		engOpts.Observer = st
	}

	res, err := engine.Run(objs, engOpts)
	if err != nil {
		return err
	}
	if !opts.noAnnotations {
		if err := render.Annotate(res); err != nil {
			return err
		}
	}
	selected := render.Selected(res.Objects, render.Options{KeepLocal: opts.keepLocal})
	if opts.output != "" {
		return render.WriteDir(opts.output, selected)
	}
	return render.Encode(stdout, selected)
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true // injected reader (tests)
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}
