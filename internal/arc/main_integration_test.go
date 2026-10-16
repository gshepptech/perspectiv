//go:build integration

package arc_test

import (
	"testing"

	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }
