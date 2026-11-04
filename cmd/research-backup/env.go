package main

import "github.com/snow-ghost/research-team/internal/researchweb"

func privateEnvLookup(path string) (func(string) (string, bool), error) {
	return researchweb.PrivateEnvLookup(path)
}
