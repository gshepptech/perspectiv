-- +goose Up
-- What kind of place a store or mall is, from the place data, as the
-- PlaceCategory enum name. NULL above the store level and for user places.
ALTER TABLE places ADD COLUMN IF NOT EXISTS category text NULL
    CONSTRAINT places_category_check CHECK (category IN (
        'PLACE_CATEGORY_FOOD_AND_DRINK', 'PLACE_CATEGORY_NIGHTLIFE', 'PLACE_CATEGORY_SHOPPING',
        'PLACE_CATEGORY_OUTDOORS', 'PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT',
        'PLACE_CATEGORY_SPORTS_AND_RECREATION', 'PLACE_CATEGORY_CULTURE_AND_HISTORY',
        'PLACE_CATEGORY_EDUCATION', 'PLACE_CATEGORY_COMMUNITY', 'PLACE_CATEGORY_TRAVEL',
        'PLACE_CATEGORY_LODGING', 'PLACE_CATEGORY_SERVICES', 'PLACE_CATEGORY_HEALTH'));
-- A city's places of one category, in name order.
CREATE INDEX IF NOT EXISTS places_parent_category_sort_idx
    ON places (parent_id, category, (lower(regexp_replace(name, '^[^[:alnum:]]+', ''))), id)
    WHERE hidden_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS places_parent_category_sort_idx;
ALTER TABLE places DROP COLUMN IF EXISTS category;
