package importer

import (
	"strings"
	"testing"
	"time"
)

func TestResolveZone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state, county, locality, want string
	}{
		{"single-zone state", "NY", "Kings County", "Brooklyn", "America/New_York"},
		{"state default with no county", "TX", "", "Austin", "America/Chicago"},
		{"Texas mountain county", "TX", "El Paso County", "El Paso", "America/Denver"},
		{"Florida panhandle", "FL", "Escambia County", "Pensacola", "America/Chicago"},
		{"Florida peninsula", "FL", "Miami-Dade County", "Miami", "America/New_York"},
		{"Gulf County override", "FL", "Gulf County", "Wewahitchka", "America/Chicago"},
		{"Arizona without DST", "AZ", "Maricopa County", "Phoenix", "America/Phoenix"},
		{"Navajo Nation observes DST", "AZ", "Apache County", "Window Rock", "America/Denver"},
		{"Indiana central county", "IN", "Lake County", "Gary", "America/Chicago"},
		{"Indiana LaPorte spelled apart", "IN", "La Porte County", "Michigan City", "America/Chicago"},
		{"Indiana Starke", "IN", "Starke County", "Knox", "America/Indiana/Knox"},
		{"Indiana default", "IN", "Marion County", "Indianapolis", "America/Indiana/Indianapolis"},
		{"Kentucky central", "KY", "Warren County", "Bowling Green", "America/Chicago"},
		{"Louisville", "KY", "Jefferson County", "Louisville", "America/Kentucky/Louisville"},
		{"Tennessee east", "TN", "Knox County", "Knoxville", "America/New_York"},
		{"Tennessee middle", "TN", "Davidson County", "Nashville", "America/Chicago"},
		{"Idaho panhandle", "ID", "Kootenai County", "Coeur d'Alene", "America/Los_Angeles"},
		{"Idaho south", "ID", "Ada County", "Boise", "America/Boise"},
		{"Oregon Malheur", "OR", "Malheur County", "Ontario", "America/Boise"},
		{"Michigan UP", "MI", "Menominee County", "Menominee", "America/Menominee"},
		{"North Dakota Mercer", "ND", "Mercer County", "Beulah", "America/North_Dakota/Beulah"},
		{"South Dakota west", "SD", "Pennington County", "Rapid City", "America/Denver"},
		{"Fort Pierre", "SD", "Stanley County", "Fort Pierre", "America/Chicago"},
		{"Nebraska panhandle", "NE", "Scotts Bluff County", "Scottsbluff", "America/Denver"},
		{"Kansas west", "KS", "Sherman County", "Goodland", "America/Denver"},
		{"West Wendover", "NV", "Elko County", "West Wendover", "America/Denver"},
		{"Juneau borough", "AK", "City and Borough of Juneau", "Juneau", "America/Juneau"},
		{"Unalaska keeps Alaska time", "AK", "Aleutians West Census Area", "Unalaska", "America/Anchorage"},
		{"Adak", "AK", "Aleutians West Census Area", "Adak", "America/Adak"},
		{"Hawaii", "HI", "Honolulu County", "Honolulu", "Pacific/Honolulu"},
		{"county name case and spacing", "TX", "  el   PASO county ", "x", "America/Denver"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveZone(tc.state, tc.county, tc.locality)
			if err != nil {
				t.Fatalf("resolveZone: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveZone(%q, %q, %q) = %q, want %q", tc.state, tc.county, tc.locality, got, tc.want)
			}
		})
	}
}

func TestResolveZoneUnknownState(t *testing.T) {
	t.Parallel()
	_, err := resolveZone("ZZ", "", "")
	if err == nil || !strings.Contains(err.Error(), "ZZ") {
		t.Fatalf("resolveZone(ZZ) error = %v, want one naming the state", err)
	}
}

// Every zone the tables can return is a real IANA zone.
func TestZoneTablesAreIANA(t *testing.T) {
	t.Parallel()
	zones := map[string]bool{}
	for _, z := range stateZones {
		zones[z] = true
	}
	for _, m := range []map[string]map[string]string{countyZones, localityZones} {
		for state, names := range m {
			if _, ok := stateZones[state]; !ok {
				t.Errorf("override table names unknown state %q", state)
			}
			for _, z := range names {
				zones[z] = true
			}
		}
	}
	for z := range zones {
		if _, err := time.LoadLocation(z); err != nil {
			t.Errorf("zone %q: %v", z, err)
		}
	}
}
