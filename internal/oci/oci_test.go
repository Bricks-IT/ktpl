package oci_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bricks-it/ktpl/internal/oci"
)

func TestOCI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "oci")
}

var _ = Describe("OCI", func() {
	var srcDir string

	BeforeEach(func() {
		srcDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(srcDir, "app.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(srcDir, "sub", "deploy.yaml"), []byte("apiVersion: apps/v1\nkind: Deployment\n"), 0o644)).To(Succeed())
		// Hidden file should be ignored
		Expect(os.WriteFile(filepath.Join(srcDir, ".hidden"), []byte("secret"), 0o644)).To(Succeed())
	})

	It("packages a directory into an OCI image tarball and extracts it back", func() {
		tarPath := filepath.Join(GinkgoT().TempDir(), "artifact.tar")
		Expect(oci.Package(srcDir, tarPath, "example.com/test:v1.0.0")).To(Succeed())

		tag, err := name.NewTag("example.com/test:v1.0.0")
		Expect(err).NotTo(HaveOccurred())

		img, err := tarball.ImageFromPath(tarPath, &tag)
		Expect(err).NotTo(HaveOccurred())

		layers, err := img.Layers()
		Expect(err).NotTo(HaveOccurred())
		Expect(layers).To(HaveLen(1))

		destDir := filepath.Join(GinkgoT().TempDir(), "extracted")
		Expect(oci.ExtractLayer(layers[0], destDir)).To(Succeed())

		appContent, err := os.ReadFile(filepath.Join(destDir, "app.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(appContent)).To(Equal("apiVersion: v1\nkind: ConfigMap\n"))

		deployContent, err := os.ReadFile(filepath.Join(destDir, "sub", "deploy.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(deployContent)).To(Equal("apiVersion: apps/v1\nkind: Deployment\n"))

		Expect(filepath.Join(destDir, ".hidden")).NotTo(BeAnExistingFile())
	})

	It("rejects directory traversal attempts during extraction", func() {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)

		hdr := &tar.Header{
			Name: "../../../evil.txt",
			Mode: 0o644,
			Size: 4,
		}
		Expect(tw.WriteHeader(hdr)).To(Succeed())
		_, err := tw.Write([]byte("evil"))
		Expect(err).NotTo(HaveOccurred())
		Expect(tw.Close()).To(Succeed())
		Expect(gw.Close()).To(Succeed())
		b := buf.Bytes()
		layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		})
		Expect(err).NotTo(HaveOccurred())

		dest := GinkgoT().TempDir()
		err = oci.ExtractLayer(layer, dest)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("escapes destination"))
	})

	It("fails when packaging a non-existent directory", func() {
		err := oci.Package(filepath.Join(srcDir, "nonexistent"), "out.tar", "")
		Expect(err).To(HaveOccurred())
	})

	It("pushes and pulls an artifact using an in-memory OCI registry", func() {
		srv := httptest.NewServer(registry.New())
		defer srv.Close()

		host := srv.Listener.Addr().String()
		ref := host + "/test/artifact:v1.0.0"

		// Push directory directly
		Expect(oci.Push(srcDir, ref, true)).To(Succeed())

		// Pull into new destination
		pulledDir := filepath.Join(GinkgoT().TempDir(), "pulled")
		Expect(oci.Pull("oci://"+ref, pulledDir, oci.PullOptions{Insecure: true})).To(Succeed())

		content, err := os.ReadFile(filepath.Join(pulledDir, "app.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("apiVersion: v1\nkind: ConfigMap\n"))
	})

	It("rejects symlink and dangerous entries in layers", func() {
		for _, flag := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo, tar.TypeChar, tar.TypeBlock} {
			var buf bytes.Buffer
			gw := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gw)

			hdr := &tar.Header{
				Typeflag: flag,
				Name:     "entry.yaml",
				Linkname: "/etc/passwd",
			}
			Expect(tw.WriteHeader(hdr)).To(Succeed())
			Expect(tw.Close()).To(Succeed())
			Expect(gw.Close()).To(Succeed())

			b := buf.Bytes()
			layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(b)), nil
			})
			Expect(err).NotTo(HaveOccurred())

			dest := GinkgoT().TempDir()
			err = oci.ExtractLayer(layer, dest)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported or dangerous entry type"))
		}
	})

	It("skips symlinks when archiving a directory", func() {
		symlinkPath := filepath.Join(srcDir, "dangling-symlink")
		_ = os.Symlink("/nonexistent/target", symlinkPath)

		tarPath := filepath.Join(GinkgoT().TempDir(), "archive.tar")
		Expect(oci.Package(srcDir, tarPath, "example.com/test:v1.0.0")).To(Succeed())

		destDir := filepath.Join(GinkgoT().TempDir(), "extracted")
		tag, err := name.NewTag("example.com/test:v1.0.0")
		Expect(err).NotTo(HaveOccurred())
		img, err := tarball.ImageFromPath(tarPath, &tag)
		Expect(err).NotTo(HaveOccurred())
		Expect(oci.ExtractImage(img, destDir)).To(Succeed())

		Expect(filepath.Join(destDir, "dangling-symlink")).NotTo(BeAnExistingFile())
	})

	It("parses OCI URLs correctly", func() {
		ref, sel := oci.ParseOCIURL("oci://ghcr.io/org/repo:v1")
		Expect(ref).To(Equal("ghcr.io/org/repo:v1"))
		Expect(sel).To(BeEmpty())

		ref, sel = oci.ParseOCIURL("oci://ghcr.io/org/repo:v1#layer=0")
		Expect(ref).To(Equal("ghcr.io/org/repo:v1"))
		Expect(sel).To(Equal("layer=0"))

		ref, sel = oci.ParseOCIURL("oci://ghcr.io/org/repo:v1?query=1")
		Expect(ref).To(Equal("ghcr.io/org/repo:v1"))
		Expect(sel).To(Equal("query=1"))

		ref, sel = oci.ParseOCIURL("ghcr.io/org/repo:v1")
		Expect(ref).To(Equal("ghcr.io/org/repo:v1"))
		Expect(sel).To(BeEmpty())
	})

	It("returns error when archiving non-directory or nonexistent path", func() {
		var buf bytes.Buffer
		filePath := filepath.Join(srcDir, "app.yaml")
		Expect(oci.ArchiveDir(filePath, &buf)).To(MatchError(ContainSubstring("is not a directory")))

		Expect(oci.ArchiveDir(filepath.Join(srcDir, "notfound"), &buf)).To(HaveOccurred())
	})

	It("handles Package defaults and tag validation", func() {
		// Invalid tag
		Expect(oci.Package(srcDir, "out.tar", "INVALID TAG WITH SPACES")).To(MatchError(ContainSubstring("invalid tag")))

		// Empty outPath and empty tagRef
		cwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		tmp := GinkgoT().TempDir()
		Expect(os.Chdir(tmp)).To(Succeed())
		defer func() { _ = os.Chdir(cwd) }()

		dirToPack := filepath.Join(tmp, "mydir")
		Expect(os.MkdirAll(dirToPack, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dirToPack, "f.txt"), []byte("hi"), 0o644)).To(Succeed())

		Expect(oci.Package(dirToPack, "", "")).To(Succeed())
		Expect(filepath.Join(tmp, "mydir.tar")).To(BeAnExistingFile())
	})

	It("fails ExtractImage when artifact has no layers", func() {
		destDir := filepath.Join(GinkgoT().TempDir(), "extracted")
		err := oci.ExtractImage(empty.Image, destDir)
		Expect(err).To(MatchError("artifact has no layers"))
	})

	It("handles Push and Pull error cases and push of .tar file", func() {
		// Push non-existent source
		Expect(oci.Push("/nonexistent/file/path", "localhost:5000/test:v1", true)).To(HaveOccurred())

		// Push with invalid reference
		Expect(oci.Push(srcDir, "INVALID REF WITH SPACES", true)).To(MatchError(ContainSubstring("invalid reference")))

		// Pull with invalid reference
		Expect(oci.Pull("INVALID REF WITH SPACES", "", oci.PullOptions{})).To(MatchError(ContainSubstring("invalid reference")))

		// Push a .tar package directly to in-memory registry and pull with empty destDir
		srv := httptest.NewServer(registry.New())
		defer srv.Close()

		tarPath := filepath.Join(GinkgoT().TempDir(), "pkg.tar")
		Expect(oci.Package(srcDir, tarPath, "example.com/test:v1")).To(Succeed())

		host := srv.Listener.Addr().String()
		ref := host + "/test/pkg-artifact:v1.0.0"
		Expect(oci.Push(tarPath, ref, true)).To(Succeed())

		// Pull into empty destDir (defaults to repo name)
		cwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		pullTmp := GinkgoT().TempDir()
		Expect(os.Chdir(pullTmp)).To(Succeed())
		defer func() { _ = os.Chdir(cwd) }()

		Expect(oci.Pull("oci://"+ref, "", oci.PullOptions{Insecure: true})).To(Succeed())
		Expect(filepath.Join(pullTmp, "pkg-artifact", "app.yaml")).To(BeAnExistingFile())
	})
})
