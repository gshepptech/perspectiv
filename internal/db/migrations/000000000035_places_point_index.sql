-- +goose Up
-- An empty place page offers its twin: another listing of the same store or
-- mall, with stories, within 2 km. The twin lookup found candidates by
-- reading every arc of the place's state; at beta scale (90k days, 225k
-- stories, nearly all in Florida) that took 1.1 s warm for a store in
-- downtown Miami and 0.8 s for one in a small town. It now reads the places
-- in a 2 km box through this index and probes each in arcs_place_path_idx:
-- 25 to 43 ms warm in downtown Miami (5,300 places in the box) and 0.1 ms in
-- a small town, with the same candidates inside 2 km.
CREATE INDEX IF NOT EXISTS places_point_idx ON places USING gist (point(lng, lat));
-- Those probes, like every place_path read, scanned the GIN pending list
-- on each lookup: 90k fresh arcs made one probe cost 12 ms instead of
-- 1 µs, and the old lookup 33 s. Arcs are written once per new day or
-- hour, so each is put straight into the index instead.
ALTER INDEX arcs_place_path_idx SET (fastupdate = off);
SELECT gin_clean_pending_list('arcs_place_path_idx'::regclass);

-- +goose Down
ALTER INDEX arcs_place_path_idx RESET (fastupdate);
DROP INDEX IF EXISTS places_point_idx;
