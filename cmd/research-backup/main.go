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
	flag.Parse()
	if *config == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "Pass -config and -out; stop the server first.")
		os.Exit(2)
	}
	o, err := researchweb.LoadOptions(*config)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		var m researchweb.BackupManifest
		m, err = researchweb.BackupPostgres(ctx, o.Config, o.Lookup, *output, func(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
			prefix := []string{"compose", "-f", *compose, "--env-file", *env, "exec", "-T", "postgres"}
			cmd := exec.CommandContext(ctx, "docker", append(prefix, args...)...)
			cmd.Stdin, cmd.Stdout = in, out
			if cmd.Run() != nil {
				return errors.New("PostgreSQL container operation failed; no completed backup manifest was written")
			}
			return nil
		})
		if err == nil {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"revision": m.Revision, "files": len(m.Files), "tables": len(m.Tables), "restore_verified": m.RestoreVerified})
			return
		}
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
