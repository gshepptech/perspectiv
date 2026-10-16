// Package importer loads open place data for the United States into the
// places table: Overture divisions for STATE, CITY and TOWN, and Foursquare
// OS Places or Overture places for MALL and STORE, each with an IANA time
// zone and a parent link. placectl is its only caller; the server never
// imports this package.
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
//	  SELECT id, names, basic_category, taxonomy, addresses,
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
// category is a shopping mall and a STORE otherwise; both are filed under
// the CITY or TOWN named by their address's locality in their region's
// state. Records for any country but the US are skipped and counted. A
// division whose chain does not reach an imported state, or a place whose
// locality is not an imported city or town, is an *UnresolvedParentError and
// the import writes nothing.
//
// The places_parent_name_key index allows one name per parent, so a record
// whose name is already taken under its parent is folded into that place
// (see Plan.Merged), and an existing row with that name, such as a
// user-added store, is reused rather than duplicated.
//
// The time zone is resolved offline from the state, county and locality; see
// timezone.go for the rules and their known limits.
package importer
