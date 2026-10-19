// Package testdb gives integration tests a real, isolated PostgreSQL database.
//
// One postgres:18-alpine container serves every test binary of a `go test`
// run; the first binary to reach Main starts it. New clones a fresh database
// from a template that db.Migrate built once, so every test sees the
// production schema and nothing another test wrote. The harness is
// compiled only with the integration build tag:
//
//	func TestMain(m *testing.M) { testdb.Main(m) }
//
//	func TestSomething(t *testing.T) {
//		pool := testdb.New(t)
//		...
//	}
package testdb
