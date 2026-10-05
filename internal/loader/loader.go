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

	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
)

// Options configures the loader.
type Options struct {
	LeftDelim string
}

// Load reads every .yaml, .yml and .json file below each root (recursively, sorted by path) and
// returns the objects in command-line order, then path order, then document order.
// A root may also be a single file.
func Load(roots []string, opts Options) ([]*object.Object, error) {
	var objs []*object.Object
	for i, root := range roots {
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

	var objs []*object.Object
	dec := yaml.NewDecoder(f)
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
		node.ExpandAliases(&doc)
		obj, err := object.New(&doc, display, filepath.ToSlash(root), folder, opts.LeftDelim)
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
