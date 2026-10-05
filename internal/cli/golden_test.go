package cli

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var update = flag.Bool("update", false, "regenerate examples/*/rendered golden files")

// examples is collected before RunSpecs so that the golden entries exist at tree construction time.
var examples []string

func TestCLI(t *testing.T) {
	RegisterFailHandler(Fail)
	root, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			examples = append(examples, filepath.Join(root, e.Name()))
		}
	}
	RunSpecs(t, "cli")
}

// chdir switches the working directory for the current spec (specs using it must be Serial).
func chdir(dir string) {
	prev, err := os.Getwd()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, os.Chdir(dir)).To(Succeed())
	DeferCleanup(os.Chdir, prev)
}

// Golden tests: every examples/<case> is run in-process from the case folder; stdout must match
// rendered/output.yaml (exit 0) or stderr must match rendered/error.txt (exit 1).
// Regenerate with `make golden`, then review the diff.
var _ = Describe("golden examples", Serial, Label("golden"), func() {
	for _, dir := range examples {
		It(filepath.Base(dir), func() {
			chdir(dir)
			args := []string{"templates"}
			if raw, err := os.ReadFile("args"); err == nil {
				args = strings.Fields(string(raw))
			}
			var stdout, stderr bytes.Buffer
			code := Run(args, strings.NewReader(""), &stdout, &stderr)

			outFile := filepath.Join("rendered", "output.yaml")
			errFile := filepath.Join("rendered", "error.txt")
			if *update {
				writeGolden(code, stdout.Bytes(), stderr.Bytes(), outFile, errFile)
				return
			}
			if want, err := os.ReadFile(errFile); err == nil {
				Expect(code).To(Equal(ExitError), "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
				Expect(stderr.String()).To(Equal(string(want)), "diff:\n%s", lineDiff(string(want), stderr.String()))
				return
			}
			want, err := os.ReadFile(outFile)
			Expect(err).NotTo(HaveOccurred(), "missing golden file")
			Expect(code).To(Equal(ExitOK), "stderr:\n%s", stderr.String())
			Expect(stdout.String()).To(Equal(string(want)), "diff:\n%s", lineDiff(string(want), stdout.String()))
		})
	}
})

func writeGolden(code int, stdout, stderr []byte, outFile, errFile string) {
	Expect(os.MkdirAll("rendered", 0o755)).To(Succeed())
	keep, drop, content := outFile, errFile, stdout
	if code != ExitOK {
		keep, drop, content = errFile, outFile, stderr
	}
	Expect(os.WriteFile(keep, content, 0o644)).To(Succeed())
	if err := os.Remove(drop); err != nil && !errors.Is(err, os.ErrNotExist) {
		Fail(err.Error())
	}
}
