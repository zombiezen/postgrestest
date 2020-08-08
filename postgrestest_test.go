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

package postgrestest

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

const singleTestTime = 30 * time.Second

func TestStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), singleTestTime)
	defer cancel()
	srv, err := Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Cleanup)
	db, err := sql.Open("postgres", srv.DefaultDatabase())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1;").Scan(&result); err != nil {
		t.Fatal("Test query:", err)
	}
	if result != 1 {
		t.Errorf("Query returned %d; want 1", result)
	}
}

func TestNewDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), singleTestTime)
	defer cancel()
	srv, err := Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Cleanup)

	const createTableStmt = `CREATE TABLE foo (id SERIAL PRIMARY KEY);`
	db1, err := srv.NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	_, err = db1.ExecContext(ctx, createTableStmt)
	if err != nil {
		t.Fatal("CREATE TABLE in database #1:", err)
	}

	db2, err := srv.NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	// If this fails, it likely means that the server is returning the same database.
	_, err = db2.ExecContext(ctx, createTableStmt)
	if err != nil {
		t.Fatal("CREATE TABLE in database #2:", err)
	}
}

func BenchmarkCreateDatabase(b *testing.B) {
	ctx := context.Background()
	srv, err := Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(srv.Cleanup)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := srv.CreateDatabase(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}
