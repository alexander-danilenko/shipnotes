// Package fileoutput writes the rendered release notes to disk. It is the
// adapter behind the application's Writer port: given the Markdown text and a
// destination path, it creates any missing parent directories, writes the file,
// and reports back the real (symlink-resolved) path.
package fileoutput

import (
	"os"
	"path/filepath"
)

const (
	// outputDirPerm and outputFilePerm are the permissions for the directory and
	// file we create. Release notes are committed to a repository, so they use
	// ordinary, world-readable permissions.
	outputDirPerm  = 0o750
	outputFilePerm = 0o644
)

// Writer writes Markdown files to the local filesystem.
type Writer struct{}

// New returns a Writer.
func New() *Writer {
	return &Writer{}
}

// Write saves content to target (creating parent directories as needed) and
// returns the absolute, symlink-resolved path of the file written.
func (*Writer) Write(content, target string) (string, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(absolute), outputDirPerm); err != nil {
		return "", err
	}

	if err := os.WriteFile(absolute, []byte(content), outputFilePerm); err != nil {
		return "", err
	}

	return resolveSymlinks(absolute), nil
}

// resolveSymlinks resolves any symlinks in the path so the reported path is the
// real, canonical one. It resolves the parent directory (which exists by now)
// and rejoins the file name; on any error it returns the path unchanged.
func resolveSymlinks(path string) string {
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path
	}

	return filepath.Join(dir, filepath.Base(path))
}
