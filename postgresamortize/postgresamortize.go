package postgresamortize

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"zombiezen.com/go/postgrestest"
)

func createDatabase(forOthers bool) (dir string, _ error) {
	ctx := context.Background() // TODO: cap init time
	srv, err := postgrestest.Start(ctx)
	if err != nil {
		return "", err
	}
	if forOthers {
		if err := os.WriteFile(filepath.Join(srv.Dir, "NEW"), nil, 0644); err != nil {
			return "", err
		}
	}
	return srv.Dir, nil
}

func pgtmp() (dsn string, cleanup func(), _ error) {
	// find an already-prepared data dir in /tmp/postgrestest*/
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "postgrestest*", "NEW"))
	if err != nil {
		return "", nil, err
	}
	var dir string
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			if os.IsNotExist(err) {
				continue // another process raced us
			}
			return "", nil, fmt.Errorf("could not grab prepared database: %v", err)
		}
		// deletion of NEW file succeeded, this database is now ours
		dir = filepath.Dir(match)
		break
	}

	// if none found: create a new database instance (without a NEW file)
	if dir == "" {
		var err error
		dir, err = createDatabase(false)
		if err != nil {
			return "", nil, err
		}
	}
	srv, err := postgrestest.Resume(context.Background(), dir)
	if err != nil {
		return "", nil, err
	}

	// in any case: create another database instance for future invocations
	if err := background("-prepare=0", srv.Dir); err != nil {
		return "", nil, err
	}

	// create test database
	dsn, err = srv.CreateDatabase(context.Background())
	if err != nil {
		return "", nil, err
	}

	return dsn, srv.Cleanup, nil
}

func background(args ...string) error {
	child := exec.Command(os.Args[0], args...)
	// Intentionally do not set Stdout and Stderr,
	// as make(1) will wait until all file descriptors are closed.
	//
	// Run() will return quickly, as the child process starts the grandchild
	// process and immediately exits.
	return child.Run()
}

// Main returns an error (caller should report) or nil (caller code can
// continue). For amortize-internal subprocesses, Main calls os.Exit(0).
func Main() (dsn string, cleanup func(), _ error) {
	var (
		prepare = flag.Int("prepare",
			-1,
			"prepare a new database? (invoked in a subprocess)")
	)
	flag.Parse()
	if *prepare > -1 {
		if flag.NArg() == 0 {
			return "", nil, fmt.Errorf("syntax: -prepare=n <dir>")
		}
		if *prepare == 0 {
			child := exec.Command(os.Args[0], append([]string{"-prepare=1"}, flag.Args()...)...)
			child.Start()
			os.Exit(0)
		}
		if _, err := createDatabase(true); err != nil {
			return "", nil, err
		}
		os.Exit(0)
	}
	return pgtmp()
}
