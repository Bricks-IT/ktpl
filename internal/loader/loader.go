// Package loader reads input folders and turns every YAML document into an object.Object.
package loader

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/oci"
)

// Options configures the loader.
type Options struct {
	LeftDelim string
	Stdin     io.Reader
	Insecure  bool
}

// Load reads every .yaml, .yml and .json file below each root (recursively, sorted by path) and
// returns the objects in command-line order, then path order, then document order.
// A root may be a folder, a single file, an OCI URL (oci://...), an OCI .tar archive, or "-" to read from stdin.
func Load(roots []string, opts Options) ([]*object.Object, error) {
	var objs []*object.Object
	for i, root := range roots {
		if root == "-" {
			docs, err := loadReader(opts.Stdin, "<stdin>", "-", i, opts)
			if err != nil {
				return nil, err
			}
			objs = append(objs, docs...)
			continue
		}
		if strings.HasPrefix(root, "oci://") {
			docs, err := loadOCI(root, i, opts)
			if err != nil {
				return nil, err
			}
			objs = append(objs, docs...)
			continue
		}
		ext := strings.ToLower(filepath.Ext(root))
		if ext == ".tar" || ext == ".tgz" {
			docs, err := loadArchive(root, i, opts)
			if err != nil {
				return nil, err
			}
			objs = append(objs, docs...)
			continue
		}
		files, err := listFiles(root)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			docs, err := loadFile(f, root, i, opts)
			if err != nil {
				return nil, err
			}
			objs = append(objs, docs...)
		}
	}
	return objs, nil
}

func listFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("input %q: %w", root, err)
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("input %q: %w", root, err)
	}
	return files, nil // WalkDir visits in lexical order
}

func loadFile(file, root string, folder int, opts Options) ([]*object.Object, error) {
	display := filepath.ToSlash(file)
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return decodeDocuments(f, display, filepath.ToSlash(root), folder, opts)
}

func loadReader(r io.Reader, display, root string, folder int, opts Options) ([]*object.Object, error) {
	if r == nil {
		return nil, errors.New("cannot read from stdin: stdin is nil")
	}
	return decodeDocuments(r, display, root, folder, opts)
}

func decodeDocuments(r io.Reader, display, root string, folder int, opts Options) ([]*object.Object, error) {
	var objs []*object.Object
	dec := yaml.NewDecoder(r)
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", display, err)
		}
		if len(doc.Content) == 0 || node.IsNull(doc.Content[0]) {
			continue // empty document
		}
		if err := checkDuplicateKeys(doc.Content[0], display); err != nil {
			return nil, err
		}
		if err := node.ExpandAliases(&doc); err != nil {
			return nil, fmt.Errorf("%s: %w", display, err)
		}
		obj, err := object.New(&doc, display, root, folder, opts.LeftDelim)
		if err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

func checkDuplicateKeys(n *yaml.Node, file string) error {
	var err error
	node.Walk(n, func(c *yaml.Node) {
		if err != nil || c.Kind != yaml.MappingNode {
			return
		}
		seen := map[string]int{}
		for i := 0; i+1 < len(c.Content); i += 2 {
			k := c.Content[i]
			if first, ok := seen[k.Value]; ok {
				err = &object.Error{File: file, Line: k.Line, Msg: fmt.Sprintf("duplicate key %q (first defined at line %d)", k.Value, first)}
				return
			}
			seen[k.Value] = k.Line
		}
	})
	return err
}

func loadArchive(tarPath string, folder int, opts Options) ([]*object.Object, error) {
	img, err := tarball.ImageFromPath(tarPath, nil)
	if err != nil {
		return nil, fmt.Errorf("loading OCI package %s: %w", tarPath, err)
	}
	tmpDir, err := os.MkdirTemp("", "ktpl-oci-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := oci.ExtractImage(img, tmpDir); err != nil {
		return nil, fmt.Errorf("extracting OCI package %s: %w", tarPath, err)
	}
	files, err := listFiles(tmpDir)
	if err != nil {
		return nil, err
	}
	var objs []*object.Object
	for _, f := range files {
		docs, err := loadFile(f, tmpDir, folder, opts)
		if err != nil {
			return nil, err
		}
		for _, o := range docs {
			rel := o.Rel()
			o.Root = tarPath
			o.File = strings.TrimSuffix(tarPath, "/") + "/" + rel
		}
		objs = append(objs, docs...)
	}
	return objs, nil
}

func loadOCI(root string, folder int, opts Options) ([]*object.Object, error) {
	tmpDir, err := os.MkdirTemp("", "ktpl-oci-pull-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pullOpts := oci.PullOptions{
		Insecure: opts.Insecure,
	}
	if err := oci.Pull(root, tmpDir, pullOpts); err != nil {
		return nil, fmt.Errorf("loading OCI artifact %s: %w", root, err)
	}

	files, err := listFiles(tmpDir)
	if err != nil {
		return nil, err
	}
	var objs []*object.Object
	for _, f := range files {
		docs, err := loadFile(f, tmpDir, folder, opts)
		if err != nil {
			return nil, err
		}
		for _, o := range docs {
			rel := o.Rel()
			o.Root = root
			o.File = strings.TrimSuffix(root, "/") + "/" + rel
		}
		objs = append(objs, docs...)
	}
	return objs, nil
}
