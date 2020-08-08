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

package postgrestest_test

import (
	"context"
	"testing"

	"zombiezen.com/go/postgrestest"
)

func Example() {
	var t *testing.T // passed into your testing function

	// Start up the PostgreSQL server. This can take a few seconds, so better to
	// do it once per test run.
	ctx := context.Background()
	srv, err := postgrestest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Cleanup)

	// Each of your subtests can have their own database:
	t.Run("Test1", func(t *testing.T) {
		db, err := srv.NewDatabase(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE foo (id SERIAL PRIMARY KEY);`); err != nil {
			t.Fatal(err)
		}
		// ...
	})

	t.Run("Test2", func(t *testing.T) {
		db, err := srv.NewDatabase(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE foo (id SERIAL PRIMARY KEY);`); err != nil {
			t.Fatal(err)
		}
		// ...
	})
}
