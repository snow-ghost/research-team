package researchweb

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func readPrivateSecrets(path, webDir string) (map[string]string, error) {
	fail := errors.New("secret file requires a private regular file outside the web directory")
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return nil, fail
	}
	web, err := filepath.EvalSymlinks(webDir)
	if err != nil {
		return nil, fail
	}
	rel, err := filepath.Rel(web, resolved)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil, fail
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fail
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0077 != 0 {
		return nil, fail
	}
	values := map[string]string{}
	if err := ReadJSON(path, &values); err != nil || len(values) == 0 || len(values) > 20 {
		return nil, fail
	}
	name := regexp.MustCompile("^[A-Z_][A-Z0-9_]*$")
	for key, value := range values {
		if !name.MatchString(key) || value == "" || len(value) > 16384 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, fail
		}
	}
	return values, nil
}
