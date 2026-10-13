package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	dir := flag.String("sqlite-dir", "", "Private source SQLite directory (read only)")
	id := flag.String("result", "", "Completed result identifier")
	out := flag.String("out", "", "New private package file")
	flag.Parse()
	if err := export(*dir, *id, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func export(dir, id, out string) error {
	if dir == "" || id == "" || out == "" {
		return fmt.Errorf("pass -sqlite-dir, -result and -out")
	}
	v, err := researchweb.ReadSQLiteResultSnapshot(dir)
	if err != nil {
		return err
	}
	p, err := researchweb.ExportResultPackageSnapshot(v, dir, id)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("package destination must be new")
	}
	_, writeErr := f.Write(body)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Printf("Package %s: %d completed attempts. A new native check and separate acceptance are required.\n", p.SHA256, len(p.Attempts))
	return nil
}
