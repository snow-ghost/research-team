package main

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

func privateEnvLookup(path string) (func(string) (string, bool), error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("compose environment must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, errors.New("compose environment is too large")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		_, duplicate := values[key]
		if !ok || key == "" || strings.ContainsAny(key, " \t\r\n") || duplicate {
			return nil, errors.New("invalid compose environment entry")
		}
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		} else if len(value) >= 2 && value[0] == '"' {
			value, err = strconv.Unquote(value)
			if err != nil {
				return nil, errors.New("invalid quoted compose environment entry")
			}
		}
		values[key] = value
	}
	return func(key string) (string, bool) {
		if value, ok := values[key]; ok {
			return value, true
		}
		return os.LookupEnv(key)
	}, nil
}
