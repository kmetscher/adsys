package testutils

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/termie/go-shutil"
)

// MakeReadOnly makes dest read only and restore permission on cleanup.
func MakeReadOnly(t *testing.T, dest string) {
	t.Helper()

	// Get current dest permissions
	fi, err := os.Stat(dest)
	require.NoError(t, err, "Cannot stat %s", dest)
	mode := fi.Mode()

	err = os.Chmod(dest, 0444)
	require.NoError(t, err)

	t.Cleanup(func() {
		err := os.Chmod(dest, mode)
		require.NoError(t, err)
	})
}

const fileForEmptyDir = ".empty"

// CompareTreesWithFiltering allows comparing a goldPath directory to p. Those can be updated via the dedicated flag.
// It will filter dconf database and not commit it in the new golden directory.
func CompareTreesWithFiltering(t *testing.T, p, goldPath string, update bool) {
	t.Helper()

	// Update golden file
	if update {
		t.Logf("updating golden file %s", goldPath)
		require.NoError(t, os.RemoveAll(goldPath), "Cannot remove target golden directory")

		// check the source directory exists before trying to copy it
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			return
		}

		// Filter dconf generated DB files that are machine dependent
		require.NoError(t,
			shutil.CopyTree(
				p, goldPath,
				&shutil.CopyTreeOptions{Symlinks: true, Ignore: ignoreDconfDB, CopyFunction: shutil.Copy}),
			"Can’t update golden directory")
		require.NoError(t, addEmptyMarker(goldPath), "Cannot create empty file in empty directories")
	}

	var err error
	var gotContent map[string]string
	if _, err := os.Stat(p); err == nil {
		gotContent, err = treeContent(t, p, []byte("GVariant"))
		if err != nil {
			t.Fatalf("No generated content: %v", err)
		}
	}

	var goldContent map[string]string
	if _, err := os.Stat(goldPath); err == nil {
		goldContent, err = treeContent(t, goldPath, nil)
		if err != nil {
			t.Fatalf("No golden directory found: %v", err)
		}
	}
	assert.Equal(t, goldContent, gotContent, "got and expected content differs")

	// No more verification on p if it doesn’t exists
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return
	}

	// Verify that each <DB>.d has a corresponding gvariant db generated by dconf update
	// search for dconfDir
	dconfDir := p
	err = filepath.WalkDir(dconfDir, func(p string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}
		if info.Name() == "db" {
			dconfDir = filepath.Dir(p)
		}
		return nil
	})
	require.NoError(t, err, "can't find dconf directory")

	dbs, err := filepath.Glob(filepath.Join(dconfDir, "db", "*.d"))
	require.NoError(t, err, "Checking pattern for dconf db failed")
	for _, db := range dbs {
		_, err = os.Stat(strings.TrimSuffix(db, ".db"))
		assert.NoError(t, err, "Binary version of dconf DB should exists")
	}
}

// addEmptyMarker adds to any empty directory, fileForEmptyDir to it.
// That allows git to commit it.
func addEmptyMarker(p string) error {
	err := filepath.Walk(p, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			f, err := os.Create(filepath.Join(path, fileForEmptyDir))
			if err != nil {
				return err
			}
			f.Close()
		}
		return nil
	})

	return err
}

// treeContent builds a recursive file list of dir with their content
// It can ignore files starting with ignoreHeaders.
func treeContent(t *testing.T, dir string, ignoreHeaders []byte) (map[string]string, error) {
	t.Helper()

	r := make(map[string]string)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Ignore markers for empty directories
		if filepath.Base(path) == fileForEmptyDir {
			return nil
		}

		content := ""
		if !info.IsDir() {
			d, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// ignore given header
			if ignoreHeaders != nil && bytes.HasPrefix(d, ignoreHeaders) {
				return nil
			}
			content = string(d)
		}
		r[strings.TrimPrefix(path, dir)] = content
		return nil
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

// ignoreDconfDB is a utility function that returns the list of binary dconf db files to ignore during copy with shutils.CopyTree.
func ignoreDconfDB(src string, entries []os.FileInfo) []string {
	var r []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		d, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}

		if bytes.HasPrefix(d, []byte("GVariant")) {
			r = append(r, e.Name())
		}
	}
	return r
}
