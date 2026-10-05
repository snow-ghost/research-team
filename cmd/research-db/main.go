package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	config := flag.String("config", "examples/server/postgres.json", "Trusted server configuration")
	importSQLite := flag.Bool("import-sqlite", false, "Import the stopped SQLite workspace, preserving its source")
	initLocal := flag.Bool("init-local", false, "Create private local PostgreSQL credentials without overwriting")
	flag.Parse()
	if err := run(*config, *importSQLite, *initLocal); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path string, importSQLite, initLocal bool) error {
	if initLocal {
		key := func() string {
			var b [32]byte
			if _, err := rand.Read(b[:]); err != nil {
				panic(err)
			}
			return hex.EncodeToString(b[:])
		}
		admin, password := key(), key()
		file, err := os.OpenFile(".env.postgres", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("cannot create .env.postgres: file may already exist")
		}
		defer file.Close()
		_, err = fmt.Fprintf(file, "POSTGRES_PASSWORD=%s\nRESEARCH_DB_PASSWORD=%s\nPOSTGRES_PORT=55433\nRESEARCH_DATABASE_URL=postgres://research:%s@127.0.0.1:55433/research?sslmode=disable\n", admin, password, password)
		if err != nil {
			return err
		}
		if err = file.Sync(); err != nil {
			return err
		}
		fmt.Println("Created private .env.postgres; credentials were not printed.")
		return nil
	}
	o, err := researchweb.LoadOptions(path)
	if err != nil {
		return err
	}
	if o.Config.Database.Driver != "postgres" {
		return fmt.Errorf("this command requires PostgreSQL configuration")
	}
	store, err := researchweb.OpenDatabase(o.Config, o.Lookup)
	if err != nil {
		return err
	}
	defer store.Close()
	if importSQLite {
		if err = store.ImportSQLite(); err != nil {
			return err
		}
		fmt.Println("SQLite workspace imported; source and local artifacts preserved.")
	} else {
		fmt.Println("PostgreSQL migrations applied.")
	}
	return nil
}
