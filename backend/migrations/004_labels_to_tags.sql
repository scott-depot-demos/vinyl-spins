-- +goose Up
-- Rename categorization "labels" -> "tags" to avoid confusion with record labels.

alter table if exists labels rename to tags;
alter table if exists album_labels rename to album_tags;
alter table if exists album_tags rename column label_id to tag_id;

alter index if exists idx_labels_user_name rename to idx_tags_user_name;
alter index if exists idx_labels_user rename to idx_tags_user;
alter index if exists idx_album_labels_user rename to idx_album_tags_user;
alter index if exists idx_album_labels_label rename to idx_album_tags_tag;

-- +goose Down
alter index if exists idx_album_tags_tag rename to idx_album_labels_label;
alter index if exists idx_album_tags_user rename to idx_album_labels_user;
alter index if exists idx_tags_user rename to idx_labels_user;
alter index if exists idx_tags_user_name rename to idx_labels_user_name;

alter table if exists album_tags rename column tag_id to label_id;
alter table if exists album_tags rename to album_labels;
alter table if exists tags rename to labels;

