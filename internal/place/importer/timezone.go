package importer

import (
	"fmt"
	"strings"
	"time"

	// Embeds the IANA database so zone ids are validated the same way on a
	// host without /usr/share/zoneinfo (the distroless runtime image).
	_ "time/tzdata"
)

// Timezone resolution, decided for this importer: offline, from the
// administrative chain rather than from coordinates.
//
// Every imported STATE, CITY and TOWN is an Overture division whose
// parent_division_id chain names its state (region code) and, almost always,
// its county. US time zone boundaries follow county lines except in a handful
// of split counties, so the zone is looked up, in order:
//
//  1. a locality override for the state (a place known to differ from its
//     county, such as the Navajo Nation communities in Arizona, which observe
//     daylight saving time while the rest of the state does not);
//  2. a county override for the state (for example El Paso County, Texas);
//  3. the state's default zone.
//
// A MALL or STORE inherits the zone of the locality it is filed under.
//
// No polygon data, PostGIS or network service is involved, so an import runs
// with no third-party call. The cost is accuracy inside a split county: the
// county's majority zone wins. The known split counties are Gulf (FL, handled
// by the Wewahitchka override), Idaho County (ID, north of the Salmon River is
// Pacific; the whole county resolves to Pacific with Grangeville), Cherry (NE,
// resolves to Central), Stanley (SD, Fort Pierre overridden to Central),
// Dunn, McKenzie and Sioux (ND), Culberson (TX, resolves to Central),
// Malheur (OR, resolves to Mountain) and Aleutians West (AK, Adak and Atka
// overridden to Hawaii-Aleutian). A town in the minority part of one of these
// counties gets its county's majority zone; an admin can correct the row.

// stateZones is the default zone for each state, DC and the inhabited
// territories, keyed by the ISO 3166-2 suffix ("TX" for US-TX).
var stateZones = map[string]string{
	"AL": "America/Chicago", "AK": "America/Anchorage", "AZ": "America/Phoenix",
	"AR": "America/Chicago", "CA": "America/Los_Angeles", "CO": "America/Denver",
	"CT": "America/New_York", "DE": "America/New_York", "DC": "America/New_York",
	"FL": "America/New_York", "GA": "America/New_York", "HI": "Pacific/Honolulu",
	"ID": "America/Boise", "IL": "America/Chicago", "IN": "America/Indiana/Indianapolis",
	"IA": "America/Chicago", "KS": "America/Chicago", "KY": "America/New_York",
	"LA": "America/Chicago", "ME": "America/New_York", "MD": "America/New_York",
	"MA": "America/New_York", "MI": "America/Detroit", "MN": "America/Chicago",
	"MS": "America/Chicago", "MO": "America/Chicago", "MT": "America/Denver",
	"NE": "America/Chicago", "NV": "America/Los_Angeles", "NH": "America/New_York",
	"NJ": "America/New_York", "NM": "America/Denver", "NY": "America/New_York",
	"NC": "America/New_York", "ND": "America/Chicago", "OH": "America/New_York",
	"OK": "America/Chicago", "OR": "America/Los_Angeles", "PA": "America/New_York",
	"RI": "America/New_York", "SC": "America/New_York", "SD": "America/Chicago",
	"TN": "America/Chicago", "TX": "America/Chicago", "UT": "America/Denver",
	"VT": "America/New_York", "VA": "America/New_York", "WA": "America/Los_Angeles",
	"WV": "America/New_York", "WI": "America/Chicago", "WY": "America/Denver",
	"PR": "America/Puerto_Rico", "VI": "America/St_Thomas", "GU": "Pacific/Guam",
	"MP": "Pacific/Saipan", "AS": "Pacific/Pago_Pago",
}

// zoneList expands one zone over many county or locality names.
func zoneList(zone string, names ...string) map[string]string {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[n] = zone
	}
	return m
}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// countyZones overrides the state default for whole counties, keyed by state
// and then by the county name as countyKey normalises it.
var countyZones = map[string]map[string]string{
	"AK": {
		"juneau":  "America/Juneau",
		"sitka":   "America/Sitka",
		"yakutat": "America/Yakutat",
		"nome":    "America/Nome",
	},
	"FL": zoneList("America/Chicago",
		"escambia", "santa rosa", "okaloosa", "walton", "holmes", "washington",
		"bay", "jackson", "calhoun"),
	"ID": zoneList("America/Los_Angeles",
		"benewah", "bonner", "boundary", "clearwater", "kootenai", "latah",
		"lewis", "nez perce", "shoshone", "idaho"),
	"IN": merge(
		zoneList("America/Chicago",
			"jasper", "lake", "laporte", "la porte", "newton", "porter",
			"gibson", "posey", "spencer", "vanderburgh", "warrick"),
		zoneList("America/Indiana/Vincennes", "daviess", "dubois", "knox", "martin"),
		zoneList("America/Kentucky/Louisville", "clark", "floyd", "harrison"),
		zoneList("America/New_York", "dearborn", "ohio"),
		map[string]string{
			"starke":      "America/Indiana/Knox",
			"perry":       "America/Indiana/Tell_City",
			"pike":        "America/Indiana/Petersburg",
			"pulaski":     "America/Indiana/Winamac",
			"crawford":    "America/Indiana/Marengo",
			"switzerland": "America/Indiana/Vevay",
		},
	),
	"KS": zoneList("America/Denver", "greeley", "hamilton", "sherman", "wallace"),
	"KY": merge(
		zoneList("America/Chicago",
			"adair", "allen", "ballard", "barren", "breckinridge", "butler",
			"caldwell", "calloway", "carlisle", "christian", "clinton", "crittenden",
			"cumberland", "daviess", "edmonson", "fulton", "graves", "grayson",
			"green", "hancock", "hart", "henderson", "hickman", "hopkins",
			"livingston", "logan", "lyon", "marshall", "mccracken", "mclean",
			"metcalfe", "monroe", "muhlenberg", "ohio", "russell", "simpson",
			"todd", "trigg", "union", "warren", "webster"),
		map[string]string{
			"jefferson": "America/Kentucky/Louisville",
			"wayne":     "America/Kentucky/Monticello",
		},
	),
	"MI": zoneList("America/Menominee", "gogebic", "iron", "dickinson", "menominee"),
	"ND": merge(
		zoneList("America/Denver",
			"adams", "billings", "bowman", "dunn", "golden valley", "grant",
			"hettinger", "slope", "stark"),
		map[string]string{
			"oliver": "America/North_Dakota/Center",
			"morton": "America/North_Dakota/New_Salem",
			"mercer": "America/North_Dakota/Beulah",
		},
	),
	"NE": zoneList("America/Denver",
		"arthur", "banner", "box butte", "chase", "cheyenne", "dawes", "deuel",
		"dundy", "garden", "grant", "hooker", "keith", "kimball", "morrill",
		"perkins", "scotts bluff", "sheridan", "sioux"),
	"OR": zoneList("America/Boise", "malheur"),
	"SD": zoneList("America/Denver",
		"bennett", "butte", "corson", "custer", "dewey", "fall river", "haakon",
		"harding", "jackson", "lawrence", "meade", "oglala lakota", "shannon",
		"pennington", "perkins", "stanley", "ziebach"),
	"TN": zoneList("America/New_York",
		"anderson", "blount", "bradley", "campbell", "carter", "claiborne",
		"cocke", "grainger", "greene", "hamblen", "hamilton", "hancock",
		"hawkins", "jefferson", "johnson", "knox", "loudon", "mcminn", "meigs",
		"monroe", "morgan", "polk", "rhea", "roane", "scott", "sevier",
		"sullivan", "unicoi", "union", "washington"),
	"TX": zoneList("America/Denver", "el paso", "hudspeth"),
}

// localityZones overrides the county for named localities, keyed by state and
// then by the locality name as nameKey normalises it.
var localityZones = map[string]map[string]string{
	// Navajo Nation communities observe daylight saving time; the rest of
	// Arizona, including the Hopi Reservation, does not.
	"AZ": zoneList("America/Denver",
		"window rock", "fort defiance", "st. michaels", "saint michaels",
		"chinle", "many farms", "ganado", "kayenta", "tuba city", "shonto",
		"kaibeto", "kaibito", "lukachukai", "tsaile", "rough rock", "round rock",
		"rock point", "dennehotso", "teec nos pos", "red mesa", "pinon", "piñon",
		"cameron", "leupp", "dilkon", "lechee", "chilchinbito"),
	"AK": zoneList("America/Adak", "adak", "atka"),
	"FL": zoneList("America/Chicago", "wewahitchka"),
	"NV": zoneList("America/Denver", "west wendover"),
	"SD": zoneList("America/Chicago", "fort pierre"),
}

// countySuffixes are stripped from a county's name before lookup, longest
// first so "city and borough" wins over "borough".
var countySuffixes = []string{
	" city and borough", " census area", " municipality", " borough",
	" county", " parish",
}

var countyPrefixes = []string{"city and borough of ", "municipality of ", "borough of "}

// countyKey normalises a county name: "LaPorte County" -> "laporte",
// "City and Borough of Juneau" -> "juneau".
func countyKey(name string) string {
	k := nameKey(name)
	for _, p := range countyPrefixes {
		k = strings.TrimPrefix(k, p)
	}
	for _, s := range countySuffixes {
		if t, ok := strings.CutSuffix(k, s); ok {
			k = t
			break
		}
	}
	return k
}

// resolveZone returns the IANA zone for a place in state (ISO 3166-2 suffix),
// optionally in county, with the locality name. An unknown state is an error:
// every imported row must carry a zone.
func resolveZone(state, county, locality string) (string, error) {
	zone, ok := stateZones[state]
	if !ok {
		return "", fmt.Errorf("no time zone known for state %q", state)
	}
	if z, ok := localityZones[state][nameKey(locality)]; ok && locality != "" {
		zone = z
	} else if z, ok := countyZones[state][countyKey(county)]; ok && county != "" {
		zone = z
	}
	if err := checkZone(zone); err != nil {
		return "", err
	}
	return zone, nil
}

// checkZone confirms the zone id exists in the IANA database.
func checkZone(zone string) error {
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("time zone %q: %w", zone, err)
	}
	return nil
}
