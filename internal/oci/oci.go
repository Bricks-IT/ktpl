// Package oci implements packaging, pushing and pulling ktpl templates as OCI artifacts.
package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// MediaTypeLayer is the media type for a ktpl template layer tarball.
const MediaTypeLayer = types.MediaType("application/vnd.ktpl.content.v1.tar+gzip")

// ParseOCIURL parses an oci:// URL into an image reference and an optional layer selector.
// Examples:
//
//	"oci://ghcr.io/org/repo:v1"         -> "ghcr.io/org/repo:v1", ""
//	"oci://ghcr.io/org/repo:v1#layer=0" -> "ghcr.io/org/repo:v1", "layer=0"
//	"oci://ghcr.io/org/repo:v1#1"       -> "ghcr.io/org/repo:v1", "1"
func ParseOCIURL(raw string) (ref string, selector string) {
	s := strings.TrimPrefix(raw, "oci://")
	if idx := strings.IndexAny(s, "#?"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

// ArchiveDir writes a gzipped tar archive of dir to w.
// It skips hidden directories and files (e.g. .git).
func ArchiveDir(dir string, w io.Writer) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	cleanDir := filepath.Clean(dir)
	info, err := os.Stat(cleanDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	err = filepath.Walk(cleanDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(fi.Name(), ".") && p != cleanDir {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(cleanDir, p)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p) //nolint:gosec // walk inside clean source directory
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

// ExtractLayer extracts the files from layer into destDir.
// It prevents directory traversal attacks ("Zip Slip").
func ExtractLayer(l v1.Layer, destDir string) error {
	rc, err := l.Uncompressed()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cleanDest := filepath.Clean(destDir)
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(hdr.Name, "/")
		target := filepath.Join(cleanDest, filepath.FromSlash(name))
		cleanTarget := filepath.Clean(target)
		rel, err := filepath.Rel(cleanDest, cleanTarget)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("security: path %q escapes destination %q", hdr.Name, destDir)
		}
		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(tr, 100<<20)); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}
	return nil
}

// ExtractImage extracts all layers of img in order into destDir.
func ExtractImage(img v1.Image, destDir string) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("reading image layers: %w", err)
	}
	if len(layers) == 0 {
		return errors.New("artifact has no layers")
	}

	for _, l := range layers {
		if err := ExtractLayer(l, destDir); err != nil {
			return err
		}
	}
	return nil
}

// LayerFromDir creates a v1.Layer from a directory.
func LayerFromDir(dir string) (v1.Layer, error) {
	var buf bytes.Buffer
	if err := ArchiveDir(dir, &buf); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	}, tarball.WithMediaType(MediaTypeLayer))
}

// BuildImage creates an OCI image from a directory.
func BuildImage(dir string) (v1.Image, error) {
	layer, err := LayerFromDir(dir)
	if err != nil {
		return nil, err
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, err
	}
	return mutate.MediaType(img, types.OCIManifestSchema1), nil
}

// Package archives dir into an OCI image tarball at outPath.
func Package(dir, outPath, tagRef string) error {
	img, err := BuildImage(dir)
	if err != nil {
		return err
	}
	if tagRef == "" {
		tagRef = "ktpl-artifact:latest"
	}
	tag, err := name.NewTag(tagRef, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("invalid tag %q: %w", tagRef, err)
	}
	if outPath == "" {
		base := filepath.Base(filepath.Clean(dir))
		outPath = base + ".tar"
	}
	return tarball.WriteToFile(outPath, tag, img)
}

// Push pushes an artifact (directory or .tar package) to a remote OCI registry.
func Push(sourcePath, remoteRef string, insecure bool) error {
	var (
		img v1.Image
		err error
	)
	fi, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		img, err = BuildImage(sourcePath)
		if err != nil {
			return err
		}
	} else {
		img, err = tarball.ImageFromPath(sourcePath, nil)
		if err != nil {
			return fmt.Errorf("loading package %s: %w", sourcePath, err)
		}
	}

	opts := []name.Option{name.StrictValidation}
	if insecure {
		opts = append(opts, name.Insecure)
	}
	ref, err := name.ParseReference(remoteRef, opts...)
	if err != nil {
		return fmt.Errorf("invalid reference %q: %w", remoteRef, err)
	}

	remoteOpts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	if insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
		remoteOpts = append(remoteOpts, remote.WithTransport(tr))
	}

	return remote.Write(ref, img, remoteOpts...)
}

// PullOptions configures pulling an OCI artifact.
type PullOptions struct {
	Insecure bool
}

// Pull pulls an artifact from a remote OCI registry and extracts its content into destDir.
func Pull(remoteRef, destDir string, opts PullOptions) error {
	refStr := strings.TrimPrefix(remoteRef, "oci://")

	nameOpts := []name.Option{name.StrictValidation}
	if opts.Insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}
	ref, err := name.ParseReference(refStr, nameOpts...)
	if err != nil {
		return fmt.Errorf("invalid reference %q: %w", refStr, err)
	}

	remoteOpts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	if opts.Insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
		remoteOpts = append(remoteOpts, remote.WithTransport(tr))
	}

	img, err := remote.Image(ref, remoteOpts...)
	if err != nil {
		return fmt.Errorf("pulling %s: %w", refStr, err)
	}

	if destDir == "" {
		destDir = filepath.Base(ref.Context().RepositoryStr())
	}

	return ExtractImage(img, destDir)
}
