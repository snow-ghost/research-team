package main

import (
	"bytes"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestDatabaseBDD_PrivateCredentialsAreNotOverwritten(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := run("", false, true); err != nil {
		t.Fatal(err)
	}
	path := ".env.postgres"
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(before), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	admin, app := values["POSTGRES_PASSWORD"], values["RESEARCH_DB_PASSWORD"]
	if len(admin) != 64 || len(app) != 64 || admin == app {
		t.Fatal("credentials were not generated independently")
	}
	u, err := url.Parse(values["RESEARCH_DATABASE_URL"])
	if err != nil {
		t.Fatal("invalid connection URL")
	}
	password, _ := u.User.Password()
	if password != app || u.User.Username() != "research" {
		t.Fatal("application uses administrative credentials")
	}
	if err := run("", false, true); err == nil {
		t.Fatal("existing credentials overwritten")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("credentials changed")
	}
}
