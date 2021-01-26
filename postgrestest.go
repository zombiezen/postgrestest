// Copyright 2020 Ross Light
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// Package postgrestest provides a test harness that starts an ephemeral
// PostgreSQL server. PostgreSQL must be installed locally for this package to
// work.
package postgrestest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	_ "github.com/lib/pq"
)

const superuserName = "postgres"

// A Server represents a running PostgreSQL server.
type Server struct {
	dir     string
	baseURL string
	conn    *sql.DB

	exited  <-chan struct{}
	waitErr error
}

// Start starts a PostgreSQL server with an empty database and waits for it to
// accept connections.
//
// Start looks for the programs "pg_ctl" and "initdb" in PATH. If these are not
// found, then Start searches for them in /usr/lib/postgresql/*/bin, preferring
// the highest version found.
func Start(ctx context.Context) (_ *Server, err error) {
	// Prepare data directory.
	dir, err := ioutil.TempDir("", "postgrestest")
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	superuserPassword, err := randomString(16)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	pwFile := filepath.Join(dir, "password")
	err = ioutil.WriteFile(pwFile, []byte(superuserPassword), 0600)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	dataDir := filepath.Join(dir, "data")
	err = runCommand("initdb",
		"--no-sync",
		"--username="+superuserName,
		"--pwfile="+pwFile,
		"-D", dataDir)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	port, err := findUnusedTCPPort()
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	const configFormat = "" +
		"listen_addresses = localhost\n" +
		"port = %d\n" +
		"unix_socket_directories = ''\n" +
		"fsync = off\n" +
		"synchronous_commit = off\n" +
		"full_page_writes = off\n"
	err = ioutil.WriteFile(
		filepath.Join(dataDir, "postgresql.conf"),
		[]byte(fmt.Sprintf(configFormat, port)),
		0666)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}

	// Start server process.
	// On Unix systems, pg_ctl runs as a daemon.
	// On Windows systems, pg_ctl runs in the foreground (not well-documented) and
	// drops privileges as needed.
	logFile := filepath.Join(dir, "log.txt")
	proc, err := command("pg_ctl", "start", "--no-wait", "--pgdata="+dataDir, "--log="+logFile)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	if err := proc.Start(); err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	exited := make(chan struct{})
	srv := &Server{
		dir: dir,
		baseURL: (&url.URL{
			Scheme: "postgres",
			Host:   fmt.Sprintf("localhost:%d", port),
			User:   url.UserPassword(superuserName, superuserPassword),
			Path:   "/",
		}).String(),
		exited: exited,
	}
	go func() {
		defer close(exited)
		srv.waitErr = proc.Wait()
	}()

	// Wait for server to come up healthy.
	srv.conn, err = sql.Open("postgres", srv.DefaultDatabase())
	if err != nil {
		// Failure to open means the DSN is invalid. Connections aren't created
		// until we ping.
		srv.stop()
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	defer func() {
		if err != nil {
			srv.conn.Close()
		}
	}()
	srv.conn.SetMaxOpenConns(1)
	for {
		select {
		case <-ctx.Done():
			srv.stop()
			logOutput, _ := ioutil.ReadFile(logFile)
			if len(logOutput) == 0 {
				return nil, fmt.Errorf("start postgres: %w", ctx.Err())
			}
			return nil, fmt.Errorf("start postgres: %w\n%s", ctx.Err(), logOutput)
		default:
			if err := srv.conn.PingContext(ctx); err == nil {
				return srv, nil
			}
		}
	}
}

// DefaultDatabase returns the data source name of the default "postgres" database.
func (srv *Server) DefaultDatabase() string {
	return srv.dsn("postgres")
}

func (srv *Server) dsn(dbName string) string {
	return srv.baseURL + dbName + "?sslmode=disable"
}

// NewDatabase opens a connection to a freshly created database on the server.
func (srv *Server) NewDatabase(ctx context.Context) (*sql.DB, error) {
	dsn, err := srv.CreateDatabase(ctx)
	if err != nil {
		return nil, err
	}
	return sql.Open("postgres", dsn)
}

// CreateDatabase creates a new database on the server and returns its
// data source name.
func (srv *Server) CreateDatabase(ctx context.Context) (string, error) {
	dbName, err := randomString(16)
	if err != nil {
		return "", fmt.Errorf("new database: %w", err)
	}
	_, err = srv.conn.ExecContext(ctx, "CREATE DATABASE \""+dbName+"\";")
	if err != nil {
		return "", fmt.Errorf("new database: %w", err)
	}
	return srv.dsn(dbName), nil
}

// Cleanup shuts down the server and deletes any on-disk files the server used.
func (srv *Server) Cleanup() {
	if srv.conn != nil {
		srv.conn.Close()
	}
	srv.stop()
	os.RemoveAll(srv.dir)
}

func (srv *Server) stop() {
	// Use Immediate Shutdown mode. We don't care about data corruption.
	// https://www.postgresql.org/docs/current/server-shutdown.html
	//
	// TODO(someday): What happens if this fails?
	runCommand("pg_ctl", "stop",
		"--pgdata="+filepath.Join(srv.dir, "data"),
		"--mode=immediate",
		"--wait")
	<-srv.exited
}

// command creates an *exec.Cmd for the given PostgreSQL program. If it it
// cannot find the program on the PATH, then it searches some well-known
// PostgreSQL installation paths.
func command(name string, args ...string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p, lookErr := exec.LookPath(name)
	if lookErr == nil {
		return exec.Command(p, args...), nil
	}
	// Find PostgreSQL installation path. If this doesn't work, return the
	// original LookPath error, since the runner of the test should add the binary
	// to their PATH if it can't be found.
	postgresBin.init.Do(findPostgresBin)
	if postgresBin.dir == "" {
		return nil, lookErr
	}
	p = filepath.Join(postgresBin.dir, name)
	if _, err := os.Stat(p); err != nil {
		return nil, lookErr
	}
	return exec.Command(p, args...), nil
}

func findPostgresBin() {
	dir := "/usr/lib/postgresql"
	if runtime.GOOS == "windows" {
		dir = `C:\Program Files\PostgreSQL`
	}
	listing, err := ioutil.ReadDir(dir)
	if err != nil {
		return
	}
	maxVersion := -1
	for _, ent := range listing {
		v, err := strconv.ParseInt(ent.Name(), 10, 0)
		if err != nil || v <= 0 {
			continue
		}
		if int(v) > maxVersion {
			maxVersion = int(v)
		}
	}
	if maxVersion < 0 {
		return
	}
	postgresBin.dir = filepath.Join(dir, strconv.Itoa(maxVersion), "bin")
}

var postgresBin struct {
	init sync.Once
	dir  string
}

func runCommand(name string, args ...string) error {
	c, err := command(name, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	out, err := c.CombinedOutput()
	if errors.As(err, new(*exec.ExitError)) {
		return fmt.Errorf("%s: %s", name, out)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func findUnusedTCPPort() (int, error) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP: net.IPv4(127, 0, 0, 1),
	})
	if err != nil {
		return 0, fmt.Errorf("find unused tcp port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return 0, fmt.Errorf("find unused tcp port: %w", err)
	}
	return port, nil
}

func randomString(n int) (string, error) {
	enc := base64.RawURLEncoding
	bits := make([]byte, enc.DecodedLen(n))
	if _, err := rand.Read(bits); err != nil {
		return "", fmt.Errorf("generate random string: %w", err)
	}
	return enc.EncodeToString(bits), nil
}
