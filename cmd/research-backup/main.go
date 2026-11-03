package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	config := flag.String("config", "", "Server configuration")
	output := flag.String("out", "", "New private backup directory")
	compose := flag.String("compose", "compose.yaml", "Compose file")
	env := flag.String("compose-env", ".env.postgres", "Compose environment file")
	restore := flag.String("restore", "", "Verified backup to restore into a new database and data directory")
	restoreDatabase := flag.String("restore-database", "", "New research_restore_ database name")
	portable := flag.String("portable-from", "", "Export a verified backup without operational configuration")
	localPostgres := flag.Bool("local-postgres", false, "Run PostgreSQL client commands directly against the host gateway")
	flag.Parse()
	if *restoreDatabase != "" && *restore == "" {
		fmt.Fprintln(os.Stderr, "-restore-database requires -restore.")
		os.Exit(2)
	}
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
	lookup, err := privateEnvLookup(*env)
	o := researchweb.Options{}
	if err == nil {
		o, err = researchweb.LoadMaintenanceOptions(*config, lookup)
	}
	if err == nil && *localPostgres {
		dsn, _ := o.Lookup(o.Config.Database.DSNEnv)
		dsnURL, parseErr := url.Parse(dsn)
		switch {
		case parseErr != nil:
			err = errors.New("invalid database URL")
		case dsnURL.Hostname() == "":
			err = errors.New("invalid database URL")
		default:
			port := dsnURL.Port()
			if port == "" {
				port = "5432"
			}
			dsnURL.Host = "host.docker.internal:" + port
			if *restoreDatabase != "" {
				dsnURL.Path = "/" + *restoreDatabase
			}
			localLookup := o.Lookup
			o.Lookup = func(key string) (string, bool) {
				if key == o.Config.Database.DSNEnv {
					return dsnURL.String(), true
				}
				return localLookup(key)
			}
		}
	}
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		var m researchweb.BackupManifest
		runner := func(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
			var cmd *exec.Cmd
			if *localPostgres {
				dsn, _ := o.Lookup(o.Config.Database.DSNEnv)
				dbConfig, parseErr := pgx.ParseConfig(dsn)
				if parseErr != nil {
					return parseErr
				}
				user := dbConfig.User
				for i, arg := range args {
					if arg == "-U" && i+1 < len(args) {
						user = args[i+1]
					}
				}
				passwordKey := "RESEARCH_DB_PASSWORD"
				if user == "postgres" {
					passwordKey = "POSTGRES_PASSWORD"
				}
				password, _ := o.Lookup(passwordKey)
				cmd = exec.CommandContext(ctx, args[0], args[1:]...)
				cmd.Env = []string{"PATH=/usr/lib/postgresql/18/bin:/usr/bin:/bin", "PGHOST=host.docker.internal", "PGPORT=" + fmt.Sprint(dbConfig.Port), "PGPASSWORD=" + password}
			} else {
				prefix := []string{"compose", "-f", *compose, "--env-file", *env, "exec", "-T", "postgres"}
				cmd = exec.CommandContext(ctx, "docker", append(prefix, args...)...)
			}
			cmd.Stdin, cmd.Stdout = in, out
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if cmd.Run() != nil {
				diagnostics := strings.TrimSpace(stderr.String())
				if len(diagnostics) > 4000 {
					diagnostics = diagnostics[:4000]
				}
				if diagnostics == "" {
					diagnostics = "no diagnostics"
				}
				return fmt.Errorf("PostgreSQL container operation failed; no completed backup manifest was written: %s", diagnostics)
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
