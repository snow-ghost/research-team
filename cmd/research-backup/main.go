package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	config := flag.String("config", "", "Server configuration")
	output := flag.String("out", "", "New private backup directory")
	compose := flag.String("compose", "compose.yaml", "Compose file")
	env := flag.String("compose-env", ".env.postgres", "Compose environment file")
	restore := flag.String("restore", "", "Verified backup to restore into a new database and data directory")
	portable := flag.String("portable-from", "", "Export a verified backup without operational configuration")
	flag.Parse()
	if *portable != "" {
		if *output == "" || *restore != "" || *config != "" {
			fmt.Fprintln(os.Stderr, "Pass only -portable-from and -out.")
			os.Exit(2)
		}
		m, err := researchweb.ExportPortableBackup(*portable, *output)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"revision": m.Revision, "files": len(m.Files), "tables": len(m.Tables), "restore_verified": m.RestoreVerified})
		return
	}
	if *config == "" || (*output == "" && *restore == "") || (*output != "" && *restore != "") {
		fmt.Fprintln(os.Stderr, "Pass -config and -out; stop the server first.")
		os.Exit(2)
	}
	o, err := researchweb.LoadOptions(*config)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		var m researchweb.BackupManifest
		runner := func(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
			prefix := []string{"compose", "-f", *compose, "--env-file", *env, "exec", "-T", "postgres"}
			cmd := exec.CommandContext(ctx, "docker", append(prefix, args...)...)
			cmd.Stdin, cmd.Stdout = in, out
			if cmd.Run() != nil {
				return errors.New("PostgreSQL container operation failed; no completed backup manifest was written")
			}
			return nil
		}
		if *restore != "" {
			m, err = researchweb.RestorePostgres(ctx, o.Config, o.Lookup, *restore, runner)
		} else {
			m, err = researchweb.BackupPostgres(ctx, o.Config, o.Lookup, *output, runner)
		}
		if err == nil {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"revision": m.Revision, "files": len(m.Files), "tables": len(m.Tables), "restore_verified": m.RestoreVerified})
			return
		}
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
