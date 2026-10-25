package leancheck

import "path/filepath"

// ResolvePaths is explicit: normal validation continues to reject symlink mounts.
func ResolvePaths(c Config) (Config, error) {
	var err error
	c.Toolchain, err = filepath.EvalSymlinks(c.Toolchain)
	if err != nil {
		return Config{}, err
	}
	c.Libraries = append([]string{}, c.Libraries...)
	for i, path := range c.Libraries {
		c.Libraries[i], err = filepath.EvalSymlinks(path)
		if err != nil {
			return Config{}, err
		}
	}
	return c, c.Validate()
}
