// Package importer loads open place data for the United States into the
// places table: Overture divisions for STATE, CITY and TOWN, and Foursquare
// OS Places or Overture places for MALL and STORE, each with an IANA time
// zone and a parent link. It also folds stores and malls listed twice.
// placectl is its only caller; the server never imports this package.
//
// # Input format
//
// Overture and Foursquare publish GeoParquet. The importer does not read
// Parquet: it reads newline-delimited JSON extracts, one record per line,
// that DuckDB writes straight from the published files with the source
// column names kept. Reading JSON keeps the importer on the standard library
// and lets a fixture be written by hand. A line is either
//
//   - a flat object of source columns, as DuckDB's COPY ... (FORMAT JSON)
//     writes it, with coordinates in lng and lat (or Foursquare's longitude
//     and latitude); or
//   - a GeoJSON Feature whose properties hold the source columns and whose
//     geometry is a Point, as ogr2ogr -f GeoJSONSeq writes it. A leading
//     RFC 8142 record separator is ignored.
//
// Blank lines are skipped. Any other line is a *ParseError naming the file
// and line, and the import writes nothing.
//
// The divisions extract needs every division a locality's
// parent_division_id chain passes through on its way to its state, so it
// keeps counties and local admin areas:
//
//	INSTALL spatial; LOAD spatial;
//	COPY (
//	  SELECT id, subtype, class, country, region, names, parent_division_id,
//	         ST_X(geometry) AS lng, ST_Y(geometry) AS lat
//	  FROM read_parquet('s3://overturemaps-us-west-2/release/<release>/theme=divisions/type=division/*')
//	  WHERE country = 'US'
//	    AND subtype NOT IN ('macrohood', 'neighborhood', 'microhood')
//	) TO 'us-divisions.ndjson' (FORMAT JSON);
//
// The places extract is Overture places,
//
//	COPY (
//	  SELECT id, names, basic_category, taxonomy, addresses, confidence,
//	         ST_X(geometry) AS lng, ST_Y(geometry) AS lat
//	  FROM read_parquet('s3://overturemaps-us-west-2/release/<release>/theme=places/type=place/*')
//	  WHERE addresses[1].country = 'US'
//	) TO 'us-places.ndjson' (FORMAT JSON);
//
// or Foursquare OS Places,
//
//	COPY (
//	  SELECT fsq_place_id, name, latitude, longitude, locality, region, country,
//	         fsq_category_labels
//	  FROM read_parquet('<fsq-os-places release>/places/*.parquet')
//	  WHERE country = 'US' AND date_closed IS NULL
//	) TO 'us-places.ndjson' (FORMAT JSON);
//
// One places file may mix both sources; a line with fsq_place_id is
// Foursquare, any other is Overture.
//
// # Mapping
//
// A division with subtype region is a STATE. A locality is a CITY when its
// class is megacity or city (or absent) and a TOWN when it is town, village
// or hamlet. A CITY's parent is the nearest STATE up its parent_division_id
// chain; a TOWN's is the nearest CITY or STATE. A place is a MALL when its
// category is a shopping mall or a sports venue (a stadium, arena, ballpark,
// race track or sports complex; see category.go) and a STORE otherwise. A
// sports venue is filed under sports and recreation whatever group its source
// puts it in, and is a MALL so its stands and gates can be added inside it,
// as an admin's MakeVenue does by hand; a re-import never turns a MALL back
// into a STORE. Malls and stores are filed under the CITY or TOWN named by
// their address's locality in their region's state, compared without case,
// punctuation or extra whitespace and with St., Ste., Ft., Mt. and Mtn. read
// as the words they abbreviate, so "St. Petersburg" files under "Saint
// Petersburg". Records for any country but the US are skipped and counted. A
// US place whose address locality is empty or is not an imported city or
// town (a neighbourhood, borough or unincorporated area), or whose region is
// not a state, has no authoritative parent: it is left out and counted as
// unfiled, and the import log names the localities that left the most
// places unfiled. A division whose chain does not reach an imported state is
// an *UnresolvedParentError and the import writes nothing.
//
// A store or mall name is cleaned before it is filed: a trailing ", <its own
// locality>", ", <its own state>" or ", USA" is dropped (", Mexico" is not),
// trailing punctuation is dropped unless a full stop ends an abbreviation
// ("Co.") or initials, and a name written all in lower case is title-cased.
//
// The places_parent_name_key index allows one name per parent, so a record
// whose name is already taken under its parent is folded into that place
// (see Plan.Merged), and an existing row with that name, such as a
// user-added store, is reused rather than duplicated.
// A row re-imported by its source id whose new name, under its parent, is
// already another row's keeps its old name and parent and is counted as
// kept, so one collision never fails the import.
//
// # Duplicates
//
// Importer.Dedupe (placectl dedupe) folds stores and malls listed twice: one
// name, compared without case, punctuation or a trailing ", <a place above
// it>", within 200 m, or 2 km when one is a sports venue. The place with the
// most arcs, then the most perspectives, the cleanest name and the oldest id
// stays. Each other place with no arc and no perspective row, drafts
// included, is hidden; the places under it move to the one that stays, its
// followers follow that one, and an AUDIT_ACTION_MERGE_PLACE row with no
// actor records it. A copy whose places could not sit under the one that
// stays is skipped and reported, and a cluster where two places have stories
// or drafts is only reported. It is a dry run unless asked to apply, and a
// second run finds nothing more to merge.
//
// The time zone is resolved offline from the state, county and locality; see
// timezone.go for the rules and their known limits.
package importer
