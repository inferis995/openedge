-- Add sort_order column to tags table
ALTER TABLE tags ADD COLUMN IF NOT EXISTS sort_order FLOAT DEFAULT 0.0;

-- Initialize sort_order with id value to maintain creation order initially
UPDATE tags SET sort_order = id::float;

-- Add index for easier sorting
CREATE INDEX IF NOT EXISTS idx_tags_sort_order ON tags(sort_order);
