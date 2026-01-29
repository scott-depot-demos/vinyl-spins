-- +goose Up
-- Add labels and album_labels for categorization (many-to-many)

create table if not exists labels (
  id uuid primary key default uuid_generate_v4(),
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

-- Enforce uniqueness per user.
create unique index if not exists idx_labels_user_name on labels(user_id, name);
create index if not exists idx_labels_user on labels(user_id);

create table if not exists album_labels (
  user_id uuid not null references users(id) on delete cascade,
  album_id uuid not null references albums(id) on delete cascade,
  label_id uuid not null references labels(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key (album_id, label_id)
);

create index if not exists idx_album_labels_user on album_labels(user_id);
create index if not exists idx_album_labels_label on album_labels(label_id);

-- +goose Down
drop table if exists album_labels;
drop table if exists labels;

