package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}
func main() {
	root := flag.String("root", "", "Absolute project directory")
	config := flag.String("config", "", "Absolute private server configuration")
	env := flag.String("env", "", "Absolute private environment file")
	out := flag.String("out", "", "New private directory for rendered systemd units")
	flag.Parse()
	for _, p := range []string{*root, *config, *env, *out} {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") {
			fmt.Fprintln(os.Stderr, "Absolute paths without line breaks required.")
			os.Exit(2)
		}
	}
	info, err := os.Lstat(*env)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		fmt.Fprintln(os.Stderr, "Environment file must be private.")
		os.Exit(2)
	}
	if err = os.Mkdir(*out, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "Output directory must be new.")
		os.Exit(1)
	}
	replacer := strings.NewReplacer("@WORKDIR@", strings.ReplaceAll(*root, "%", "%%"), "@ENVFILE@", strings.ReplaceAll(*env, "%", "%%"), "@BINARY@", quote(filepath.Join(*root, "bin/research-server")), "@CONFIG@", quote(*config), "@CONFIG_ENV@", quote("RESEARCH_TEAM_CONFIG="+*config), "@BACKUP_ENV@", quote("RESEARCH_BACKUP_ROOT="+filepath.Join(*root, ".research-backups")), "@BACKUP_SCRIPT@", quote(filepath.Join(*root, "scripts/backup-service.sh")))
	for _, name := range []string{"research-team.service", "research-team-backup.service", "research-team-backup.timer"} {
		file := name
		if !strings.HasSuffix(name, ".timer") {
			file += ".in"
		}
		body, readErr := os.ReadFile(filepath.Join(*root, "deploy/systemd", file))
		if readErr != nil {
			err = readErr
			break
		}
		err = os.WriteFile(filepath.Join(*out, name), []byte(replacer.Replace(string(body))), 0600)
		if err != nil {
			break
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Service rendering failed.")
		os.Exit(1)
	}
	fmt.Println("Private units rendered; installation and enablement are separate operator actions.")
}
