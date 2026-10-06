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
})
