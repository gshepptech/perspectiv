//go:build integration

package jobs_test

import (
	"testing"

	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) {
	testdb.Main(m)
}
