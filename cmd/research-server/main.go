package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	config := flag.String("config", "examples/server/local.json", "Trusted server configuration")
	flag.Parse()
	o, err := researchweb.LoadOptions(*config)
	if err != nil {
		return err
	}
	service, err := researchweb.NewService(o)
	if err != nil {
		return err
	}
	defer service.Close()
	key, err := researchweb.AccessKey(o.Config, o.Lookup)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", o.Config.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	service.Options.Config.Listen = listener.Addr().String()
	handler, err := researchweb.NewHTTP(service, key)
	if err != nil {
		return err
	}
	defer handler.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	fmt.Printf("Research Team: http://%s\n", listener.Addr())
	if o.Config.TokenEnv == "" {
		fmt.Printf("Access key file: %s\n", filepath.Join(o.Config.DataDir, "access-token"))
	}
	err = server.Serve(listener)
	close(stopped)
	<-shutdownDone
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
