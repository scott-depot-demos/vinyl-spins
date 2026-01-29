-- +goose Up
-- Basic schema for Discogs Listen Tracker (MVP)

create extension if not exists "uuid-ossp";

create table if not exists users (
  id uuid primary key default uuid_generate_v4(),
  discogs_user_id bigint unique,
  discogs_username text unique,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists oauth_tokens (
  user_id uuid primary key references users(id) on delete cascade,
  provider text not null default 'discogs',
  access_token_enc bytea not null,
  access_secret_enc bytea not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists albums (
  id uuid primary key default uuid_generate_v4(),
  user_id uuid not null references users(id) on delete cascade,
  discogs_release_id bigint not null,
  title text not null,
  artist text not null,
  year int,
  thumb_url text,
  resource_url text,
  last_synced_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique(user_id, discogs_release_id)
);

create index if not exists idx_albums_user on albums(user_id);

create table if not exists listens (
  id uuid primary key default uuid_generate_v4(),
  user_id uuid not null references users(id) on delete cascade,
  album_id uuid not null references albums(id) on delete cascade,
  listened_at timestamptz not null,
  note text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_listens_user on listens(user_id);
create index if not exists idx_listens_album on listens(album_id);
create index if not exists idx_listens_listened_at on listens(listened_at desc);

create table if not exists groups (
  id uuid primary key default uuid_generate_v4(),
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique(user_id, name)
);

create table if not exists group_albums (
  group_id uuid not null references groups(id) on delete cascade,
  album_id uuid not null references albums(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key (group_id, album_id)
);

-- +goose Down
drop table if exists group_albums;
drop table if exists groups;
drop table if exists listens;
drop table if exists albums;
drop table if exists oauth_tokens;
drop table if exists users;
