package researchweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func atomicFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func readBoundedFile(path string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := openSource(root, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("regular file required")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrLimit
	}
	return data, nil
}

type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

func copyWorkspace(ctx context.Context, source, destination string) ([]FileDigest, error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, errors.New("workspace unavailable")
	}
	defer root.Close()
	if err = os.Mkdir(destination, 0700); err != nil {
		return nil, err
	}
	files := []FileDigest{}
	total := 0
	visited := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		visited++
		if visited > 2000 {
			return ErrLimit
		}
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("workspace symlinks are not supported")
		}
		name := entry.Name()
		if name == ".git" || name == ".env" || strings.HasPrefix(name, ".env.") || name == ".coddy" || name == "node_modules" || name == ".research-server" || name == ".research-coddy" {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		dst := filepath.Join(destination, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.Mkdir(dst, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("workspace special files are not supported")
		}
		f, err := openSource(root, path)
		if err != nil {
			return err
		}
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			return errors.New("workspace file changed")
		}
		data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		f.Close()
		if err != nil {
			return err
		}
		total += len(data)
		if total > 8<<20 || len(files) >= 500 {
			return ErrLimit
		}
		sum := sha256.Sum256(data)
		if err = os.WriteFile(dst, data, 0400); err != nil {
			return err
		}
		files = append(files, FileDigest{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: len(data)})
		return nil
	})
	return files, err
}
