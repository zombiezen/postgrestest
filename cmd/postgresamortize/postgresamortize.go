package main

import (
	"log"
	"os"
	"os/exec"

	"zombiezen.com/go/postgrestest/postgresamortize"
)

func main() {
	dsn, cleanup, err := postgresamortize.Main()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	wrapped := exec.Command(os.Args[2], os.Args[3:]...)
	wrapped.Stdin = os.Stdin
	wrapped.Stdout = os.Stdout
	wrapped.Stderr = os.Stderr
	wrapped.Env = append(os.Environ(), "PGURL="+dsn)
	if err := wrapped.Run(); err != nil {
		log.Fatalf("%v: %v", wrapped.Args, err)
	}
}
