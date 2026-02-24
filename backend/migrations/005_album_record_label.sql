-- +goose Up
-- Store the record label (publisher) from Discogs on albums.

alter table if exists albums
  add column if not exists record_label text;

-- +goose Down
alter table if exists albums
  drop column if exists record_label;

