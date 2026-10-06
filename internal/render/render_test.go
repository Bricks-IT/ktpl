package render

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/engine"
	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/path"
)

func TestRender(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "render")
}

func parseObj(src, file, root string) *object.Object {
	var doc yaml.Node
	ExpectWithOffset(1, yaml.Unmarshal([]byte(src), &doc)).To(Succeed())
	obj, err := object.New(&doc, file, root, 0, "{{")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return obj
}

var _ = Describe("Render", func() {
	Context("Selected", func() {
		It("filters out local objects unless KeepLocal is set", func() {
			normal := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: normal\n", "a.yaml", ".")
			local := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: local\n  annotations:\n    ktpl.io/local: 'true'\n", "b.yaml", ".")

			objs := []*object.Object{normal, local}
			Expect(Selected(objs, Options{KeepLocal: false})).To(Equal([]*object.Object{normal}))
			Expect(Selected(objs, Options{KeepLocal: true})).To(Equal([]*object.Object{normal, local}))
		})
	})

	Context("Encode and EncodeObject", func() {
		It("encodes objects with indentation", func() {
			o := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n", "a.yaml", ".")
			s, err := EncodeObject(o)
			Expect(err).NotTo(HaveOccurred())
			Expect(s).To(ContainSubstring("kind: ConfigMap"))
			Expect(s).To(ContainSubstring("name: test"))

			var buf bytes.Buffer
			Expect(Encode(&buf, nil)).To(Succeed())
			Expect(buf.String()).To(BeEmpty())

			Expect(Encode(&buf, []*object.Object{o})).To(Succeed())
			Expect(buf.String()).To(Equal(s))
		})
	})

	Context("Annotate", func() {
		It("annotates rendered fields and multiple sources", func() {
			o := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\ndata:\n  k: v\n", "cm.yaml", ".")
			o.Sources = []string{"base", "overlay"}

			f1 := &engine.Field{
				Obj:       o,
				Path:      path.MustParse("data.k"),
				Node:      node.String("v"),
				Iteration: 1,
			}
			f2 := &engine.Field{
				Obj:       o,
				Path:      path.MustParse("data.k2"),
				Node:      node.String("v2"),
				Iteration: 2,
			}
			oUnrendered := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unrendered\n", "u.yaml", ".")
			res := &engine.Result{
				Objects: []*object.Object{o, oUnrendered},
				Fields:  []*engine.Field{f1, f2},
			}

			Expect(Annotate(res)).To(Succeed())

			meta := node.MapValue(o.Body(), "metadata")
			ann := node.MapValue(meta, "annotations")
			Expect(ann).NotTo(BeNil())

			renderedVal := node.MapValue(ann, object.AnnotationRendered)
			Expect(renderedVal).NotTo(BeNil())
			Expect(renderedVal.Value).To(Equal(`{"data.k":1,"data.k2":2}`))

			sourcesVal := node.MapValue(ann, object.AnnotationSources)
			Expect(sourcesVal).NotTo(BeNil())
			Expect(sourcesVal.Value).To(Equal(`["base","overlay"]`))
		})
	})

	Context("WriteDir", func() {
		var tmpDir string

		BeforeEach(func() {
			tmpDir = GinkgoT().TempDir()
		})

		It("writes objects to their relative paths", func() {
			root := filepath.Join(tmpDir, "input")
			o1 := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n", filepath.Join(root, "sub/a.yaml"), root)
			o2 := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm2\n", filepath.Join(root, "sub/a.yaml"), root)

			outDir := filepath.Join(tmpDir, "out")
			Expect(WriteDir(outDir, []*object.Object{o1, o2})).To(Succeed())

			targetFile := filepath.Join(outDir, "sub/a.yaml")
			content, err := os.ReadFile(targetFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("name: cm1"))
			Expect(string(content)).To(ContainSubstring("name: cm2"))
		})

		It("blocks directory traversal attempts", func() {
			o := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: evil\n", "/tmp/other/evil.yaml", "/tmp/somewhere")
			o.File = "/tmp/somewhere/../../evil.yaml"

			outDir := filepath.Join(tmpDir, "out")
			Expect(WriteDir(outDir, []*object.Object{o})).To(Succeed())
			Expect(filepath.Join(outDir, "evil.yaml")).To(BeAnExistingFile())
		})

		It("fails when writing to an invalid target directory", func() {
			blockedFile := filepath.Join(tmpDir, "blocked")
			Expect(os.WriteFile(blockedFile, []byte("file"), 0o644)).To(Succeed())

			root := filepath.Join(tmpDir, "input")
			o := parseObj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n", filepath.Join(root, "sub/a.yaml"), root)

			// target is blocked by the file
			err := WriteDir(filepath.Join(blockedFile, "sub"), []*object.Object{o})
			Expect(err).To(HaveOccurred())
		})
	})
})
