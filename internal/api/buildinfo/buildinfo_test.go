package buildinfo_test

import (
	"runtime"
	"testing"

	"perspectiv/internal/api/buildinfo"
)

func TestGetReportsStampedValues(t *testing.T) {
	t.Parallel()
	got := buildinfo.Get()
	want := buildinfo.Info{Version: buildinfo.Version, Commit: buildinfo.Commit, Date: buildinfo.Date, GoVersion: runtime.Version()}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
	if got.Version == "" || got.Commit == "" || got.Date == "" {
		t.Fatalf("unstamped build reports empty fields: %+v", got)
	}
}
