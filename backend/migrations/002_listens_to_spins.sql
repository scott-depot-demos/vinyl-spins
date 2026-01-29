-- +goose Up
-- Rename listens -> spins (Vinyl Spin Tracker terminology)

alter table if exists listens rename to spins;
alter table if exists spins rename column listened_at to spun_at;

-- Index renames (safe even if already renamed; use IF EXISTS)
alter index if exists idx_listens_user rename to idx_spins_user;
alter index if exists idx_listens_album rename to idx_spins_album;
alter index if exists idx_listens_listened_at rename to idx_spins_spun_at;

-- +goose Down
alter index if exists idx_spins_user rename to idx_listens_user;
alter index if exists idx_spins_album rename to idx_listens_album;
alter index if exists idx_spins_spun_at rename to idx_listens_listened_at;

alter table if exists spins rename column spun_at to listened_at;
alter table if exists spins rename to listens;

