package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const chain = `apiVersion: v1
kind: ConfigMap
metadata:
  name: a
  namespace: demo
data:
  v: a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: b
  namespace: demo
data:
  v: '{{ ref "demo/configmap/a" "data.v" }}b'
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: c
  namespace: demo
data:
  v: '{{ ref "demo/configmap/b" "data.v" }}c'
`

type result struct {
	code           int
	stdout, stderr string
}

func run(stdin string, args ...string) result {
	var out, errb bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &out, &errb)
	return result{code, out.String(), errb.String()}
}

var _ = Describe("Run", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "chain.yaml"), []byte(chain), 0o644)).To(Succeed())
	})

	DescribeTable("usage errors exit with code 2",
		func(args []string, msg string) {
			r := run("", args...)
			Expect(r.code).To(Equal(ExitUsage))
			Expect(r.stderr).To(ContainSubstring(msg))
			Expect(r.stderr).To(ContainSubstring("Run 'ktpl --help' for usage."))
		},
		Entry("no folder", []string{}, "at least one input folder is required"),
		Entry("unknown flag", []string{"--nope", "x"}, "unknown flag: --nope"),
		Entry("max-iterations < 1", []string{"-i", "0", "x"}, "--max-iterations must be >= 1"),
		Entry("stop-after > max", []string{"--stop-after", "6", "x"}, "--stop-after (6) cannot exceed --max-iterations (5)"),
		Entry("empty delimiter", []string{"--left-delim", "", "x"}, "must not be empty"),
	)

	It("prints the version", func() {
		r := run("", "--version")
		Expect(r.code).To(Equal(ExitOK))
		Expect(r.stdout).To(HavePrefix("ktpl "))
	})

	It("exits 1 on a missing input folder", func() {
		r := run("", filepath.Join(dir, "nope"))
		Expect(r.code).To(Equal(ExitError))
		Expect(r.stderr).To(HavePrefix("Error: input "))
	})

	It("renders and annotates", func() {
		r := run("", dir)
		Expect(r.code).To(Equal(ExitOK), r.stderr)
		Expect(r.stdout).To(ContainSubstring("v: abc"))
		Expect(r.stdout).To(ContainSubstring(`ktpl.io/rendered: '{"data.v":2}'`))
	})

	It("omits annotations with --no-annotations", func() {
		r := run("", "--no-annotations", dir)
		Expect(r.code).To(Equal(ExitOK), r.stderr)
		Expect(r.stdout).NotTo(ContainSubstring("ktpl.io/rendered"))
	})

	It("emits the partial state with --stop-after", func() {
		r := run("", "--stop-after", "1", dir)
		Expect(r.code).To(Equal(ExitOK), r.stderr)
		Expect(r.stdout).To(ContainSubstring("v: ab\n"))
		Expect(r.stdout).To(ContainSubstring(`v: '{{ ref "demo/configmap/b" "data.v" }}c'`))
	})

	It("writes files mirroring the input tree with -o", func() {
		out := filepath.Join(GinkgoT().TempDir(), "out")
		r := run("", "-o", out, dir)
		Expect(r.code).To(Equal(ExitOK), r.stderr)
		Expect(r.stdout).To(BeEmpty())
		content, err := os.ReadFile(filepath.Join(out, "chain.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("v: abc"))
	})

	It("forbids non-deterministic functions with --hermetic", func() {
		Expect(os.WriteFile(filepath.Join(dir, "now.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: t\ndata:\n  t: '{{ now }}'\n"), 0o644)).To(Succeed())
		r := run("", "--hermetic", dir)
		Expect(r.code).To(Equal(ExitError))
		Expect(r.stderr).To(ContainSubstring(`function "now" not defined`))
	})

	Describe("--step", func() {
		It("pauses after each iteration and continues on Enter", func() {
			r := run("\n\n", "--step", dir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stderr).To(ContainSubstring("── Iteration 1/5 ── 1 resolved · 1 pending"))
			Expect(r.stderr).To(ContainSubstring("pending: demo/ConfigMap/c data.v <- demo/ConfigMap/b data.v"))
			Expect(r.stderr).To(ContainSubstring("── Iteration 2/5 ── 1 resolved · 0 pending"))
			Expect(r.stdout).To(ContainSubstring("v: abc"))
		})

		It("shows a diff on d", func() {
			r := run("d\n\n", "--step", dir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stderr).To(ContainSubstring("--- demo/ConfigMap/b"))
			Expect(r.stderr).To(ContainSubstring("+   v: ab"))
		})

		It("stops with the partial state on q", func() {
			r := run("q\n", "--step", dir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stderr).NotTo(ContainSubstring("Iteration 2"))
			Expect(r.stdout).To(ContainSubstring(`{{ ref "demo/configmap/b"`))
		})
	})
})

var _ = Describe("lineDiff", func() {
	It("returns nothing for equal inputs", func() {
		Expect(lineDiff("a\nb", "a\nb")).To(BeEmpty())
	})
	It("lists removed and added lines", func() {
		Expect(lineDiff("a\nb\nc", "a\nx\nc")).To(Equal("- b\n+ x\n"))
	})
})
