package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLoader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "loader")
}

func write(dir, rel, content string) {
	p := filepath.Join(dir, rel)
	ExpectWithOffset(1, os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(p, []byte(content), 0o644)).To(Succeed())
}

func names(dir string) []string {
	objs, err := Load([]string{dir}, Options{LeftDelim: "{{"})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID.Name
	}
	return out
}

var _ = Describe("Load", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	It("walks recursively in path order, then document order", func() {
		write(dir, "b.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b1\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b2\n")
		write(dir, "a/z.yml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: az\n")
		write(dir, "c.json", `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"c"}}`)
		write(dir, "ignored.txt", "nope")
		write(dir, ".hidden/x.yaml", "nope: [")
		Expect(names(dir)).To(Equal([]string{"az", "b1", "b2", "c"}))
	})

	It("skips empty documents", func() {
		write(dir, "a.yaml", "---\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n")
		Expect(names(dir)).To(Equal([]string{"a"}))
	})

	It("expands aliases so the tree can be mutated safely", func() {
		write(dir, "a.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  labels: &l\n    x: y\n  annotations: *l\n")
		objs, err := Load([]string{dir}, Options{LeftDelim: "{{"})
		Expect(err).NotTo(HaveOccurred())
		Expect(objs[0].Body().Content[5].Content[5].Kind).NotTo(BeZero())
	})

	It("accepts a single file as input", func() {
		write(dir, "a.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n")
		objs, err := Load([]string{filepath.Join(dir, "a.yaml")}, Options{LeftDelim: "{{"})
		Expect(err).NotTo(HaveOccurred())
		Expect(objs).To(HaveLen(1))
	})

	DescribeTable("located errors",
		func(content, want string) {
			write(dir, "a.yaml", content)
			_, err := Load([]string{dir}, Options{LeftDelim: "{{"})
			Expect(err).To(MatchError(ContainSubstring(want)))
			Expect(err).To(MatchError(ContainSubstring("a.yaml")))
		},
		Entry("syntax error", "a: [", "did not find expected"),
		Entry("duplicate key", "apiVersion: v1\nkind: A\nkind: B\nmetadata:\n  name: a\n", `a.yaml:3: duplicate key "kind" (first defined at line 2)`),
		Entry("missing name", "apiVersion: v1\nkind: A\nmetadata: {}\n", "metadata.name must be a non-empty string"),
	)

	It("fails on a missing input", func() {
		_, err := Load([]string{filepath.Join(dir, "nope")}, Options{LeftDelim: "{{"})
		Expect(err).To(MatchError(ContainSubstring("no such file or directory")))
	})

	It("loads manifests from stdin when '-' is provided", func() {
		input := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: from-stdin\n"
		objs, err := Load([]string{"-"}, Options{LeftDelim: "{{", Stdin: strings.NewReader(input)})
		Expect(err).NotTo(HaveOccurred())
		Expect(objs).To(HaveLen(1))
		Expect(objs[0].ID.Name).To(Equal("from-stdin"))
		Expect(objs[0].File).To(Equal("<stdin>"))
	})

	It("fails when stdin is nil and '-' is requested", func() {
		_, err := Load([]string{"-"}, Options{LeftDelim: "{{"})
		Expect(err).To(MatchError(ContainSubstring("stdin is nil")))
	})
})
