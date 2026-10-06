package cli

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bricks-it/ktpl/internal/oci"
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
		Entry("invalid render-dst", []string{"--render-dst", "ftp", "x"}, "invalid --render-dst \"ftp\": must be 'stdout' or 'dir://<dir>'"),
		Entry("empty dir render-dst", []string{"--render-dst", "dir://", "x"}, "--render-dst dir:// requires a target directory"),
		Entry("conflicting output and render-dst", []string{"-o", "a", "--render-dst", "dir://b", "x"}, "cannot specify both -o/--output and --render-dst"),
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

	It("writes files mirroring the input tree with --render-dst dir://<dir>", func() {
		out := filepath.Join(GinkgoT().TempDir(), "out")
		r := run("", "--render-dst", "dir://"+out, dir)
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

		It("redacts sensitive and Secret fields in preview", func() {
			sec := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: my-sec\n  namespace: demo\ndata:\n  password: '{{ ref \"demo/configmap/a\" \"data.v\" }}'\n"
			Expect(os.WriteFile(filepath.Join(dir, "sec.yaml"), []byte(sec), 0o644)).To(Succeed())
			r := run("\n\n", "--step", dir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stderr).To(ContainSubstring("[REDACTED]"))
		})
	})

	It("renders manifests provided on stdin with '-'", func() {
		r := run(chain, "-")
		Expect(r.code).To(Equal(ExitOK), r.stderr)
		Expect(r.stdout).To(ContainSubstring("v: abc"))
	})

	It("rejects --step with '-'", func() {
		r := run(chain, "--step", "-")
		Expect(r.code).To(Equal(ExitUsage), r.stderr)
		Expect(r.stderr).To(ContainSubstring("--step cannot be used when reading from stdin"))
	})

	Describe("OCI subcommands", func() {
		It("packages a template folder into an OCI archive with ktpl package", func() {
			outTar := filepath.Join(GinkgoT().TempDir(), "pkg.tar")
			r := run("", "package", dir, "-o", outTar)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(outTar).To(BeAnExistingFile())
		})

		It("shows usage errors when arguments are missing", func() {
			Expect(run("", "package").code).To(Equal(ExitUsage))
			Expect(run("", "push").code).To(Equal(ExitUsage))
			Expect(run("", "pull").code).To(Equal(ExitUsage))
		})

		It("shows help for subcommands", func() {
			r := run("", "package", "--help")
			Expect(r.code).To(Equal(ExitOK))
			Expect(r.stdout).To(ContainSubstring("Package a template folder into an OCI artifact archive"))

			r = run("", "help", "push")
			Expect(r.code).To(Equal(ExitOK))
			Expect(r.stdout).To(ContainSubstring("Push an OCI artifact archive or template folder"))
		})

		It("pulls an OCI artifact using ktpl pull", func() {
			srv := httptest.NewServer(registry.New())
			defer srv.Close()

			host := srv.Listener.Addr().String()
			ref := host + "/test/cli-pull:v1"
			Expect(oci.Push(dir, ref, true)).To(Succeed())

			destDir := filepath.Join(GinkgoT().TempDir(), "pulled")
			r := run("", "pull", ref, "-o", destDir, "--insecure")
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(filepath.Join(destDir, "chain.yaml")).To(BeAnExistingFile())
		})

		It("renders an OCI artifact as base layer with local overlay: ktpl oci://... prod/", func() {
			srv := httptest.NewServer(registry.New())
			defer srv.Close()

			host := srv.Listener.Addr().String()
			ref := "oci://" + host + "/test/base:v1"

			// Base manifests in baseDir
			baseDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(baseDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  env: dev\n  tier: frontend\n"), 0o644)).To(Succeed())
			Expect(oci.Push(baseDir, host+"/test/base:v1", true)).To(Succeed())

			// Overlay manifests in prodDir
			prodDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(prodDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  env: prod\n"), 0o644)).To(Succeed())

			r := run("", "--insecure", ref, prodDir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stdout).To(ContainSubstring("env: prod"))
			Expect(r.stdout).To(ContainSubstring("tier: frontend"))
			Expect(r.stdout).To(ContainSubstring(`ktpl.io/sources: '["` + ref + `","` + prodDir + `"]'`))
		})

		It("renders an OCI artifact as overlay on top of local base: ktpl base/ oci://...", func() {
			srv := httptest.NewServer(registry.New())
			defer srv.Close()

			host := srv.Listener.Addr().String()
			ref := "oci://" + host + "/test/patch:v1"

			baseDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(baseDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  env: dev\n  tier: frontend\n"), 0o644)).To(Succeed())

			patchDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(patchDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  env: staging\n"), 0o644)).To(Succeed())
			Expect(oci.Push(patchDir, host+"/test/patch:v1", true)).To(Succeed())

			r := run("", "--insecure", baseDir, ref)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stdout).To(ContainSubstring("env: staging"))
			Expect(r.stdout).To(ContainSubstring("tier: frontend"))
			Expect(r.stdout).To(ContainSubstring(`ktpl.io/sources: '["` + baseDir + `","` + ref + `"]'`))
		})

		It("applies --name-prefix and --name-suffix to emitted objects", func() {
			baseDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(baseDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  k: v\n"), 0o644)).To(Succeed())

			r := run("", "--name-prefix", "prod-", "--name-suffix", "-v1", baseDir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stdout).To(ContainSubstring("name: prod-app-v1"))

			// Test alias flags
			r = run("", "--nameprefix", "dev-", "--namesuffix", "-test", baseDir)
			Expect(r.code).To(Equal(ExitOK), r.stderr)
			Expect(r.stdout).To(ContainSubstring("name: dev-app-test"))
		})

		It("rejects names exceeding 63 characters with prefix and suffix", func() {
			baseDir := GinkgoT().TempDir()
			longName := strings.Repeat("a", 55)
			Expect(os.WriteFile(filepath.Join(baseDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: "+longName+"\ndata:\n  k: v\n"), 0o644)).To(Succeed())

			r := run("", "--name-prefix", "very-long-prefix-", baseDir)
			Expect(r.code).To(Equal(ExitError))
			Expect(r.stderr).To(ContainSubstring("exceeds Kubernetes limit of 63 characters"))
		})

		It("rejects invalid characters in --name-prefix or --name-suffix", func() {
			baseDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(baseDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  k: v\n"), 0o644)).To(Succeed())

			r := run("", "--name-prefix", "Prod ", baseDir)
			Expect(r.code).To(Equal(ExitUsage))
			Expect(r.stderr).To(ContainSubstring("--name-prefix \"Prod \": must consist of lowercase alphanumerics, '-' or '.'"))
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
