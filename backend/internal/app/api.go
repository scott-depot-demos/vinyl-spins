package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type ctxKey int

const ctxKeyUserID ctxKey = iota

func (a *App) handleMe() http.HandlerFunc {
	type resp struct {
		UserID         string `json:"user_id"`
		DiscogsUserID  int64  `json:"discogs_user_id"`
		DiscogsUsername string `json:"discogs_username"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}

		var out resp
		out.UserID = userID
		err = a.db.QueryRow(r.Context(), `
select discogs_user_id, discogs_username
from users
where id = $1
`, userID).Scan(&out.DiscogsUserID, &out.DiscogsUsername)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleAlbums() http.HandlerFunc {
	type label struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type album struct {
		ID              string    `json:"id"`
		DiscogsReleaseID int64     `json:"discogs_release_id"`
		Title           string    `json:"title"`
		Artist          string    `json:"artist"`
		Year            *int      `json:"year,omitempty"`
		ThumbURL        *string   `json:"thumb_url,omitempty"`
		ResourceURL     *string   `json:"resource_url,omitempty"`
		LastSyncedAt    *time.Time `json:"last_synced_at,omitempty"`
		SpinCount       int        `json:"spin_count"`
		LastSpunAt      *time.Time `json:"last_spun_at,omitempty"`
		Labels          []label    `json:"labels"`
	}

	type albumQuery struct {
		Q        string
		Artist   string
		LabelIDs []string
		Sort     string
		Order    string
	}

	parseAlbumQuery := func(v url.Values) albumQuery {
		q := albumQuery{
			Q:      strings.TrimSpace(v.Get("q")),
			Artist: strings.TrimSpace(v.Get("artist")),
			Sort:   strings.TrimSpace(v.Get("sort")),
			Order:  strings.ToLower(strings.TrimSpace(v.Get("order"))),
		}
		if q.Order != "desc" {
			q.Order = "asc"
		}
		if q.Sort == "" {
			q.Sort = "artist"
		}
		if raw := strings.TrimSpace(v.Get("label_ids")); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					q.LabelIDs = append(q.LabelIDs, part)
				}
			}
		}
		return q
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}

		// Optional: trigger a sync if requested.
		if r.URL.Query().Get("sync") == "1" {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
			defer cancel()
			if err := a.syncDiscogsCollection(ctx, userID); err != nil {
				writeJSONError(w, http.StatusBadGateway, err)
				return
			}
		}

		aq := parseAlbumQuery(r.URL.Query())

		var (
			args     []any
			argN     = 1
			whereSQL = "where a.user_id = $1"
		)
		args = append(args, userID)
		argN++

		if aq.Q != "" {
			whereSQL += " and (a.title ilike $" + strconv.Itoa(argN) + " or a.artist ilike $" + strconv.Itoa(argN) + ")"
			args = append(args, "%"+aq.Q+"%")
			argN++
		}
		if aq.Artist != "" {
			whereSQL += " and a.artist = $" + strconv.Itoa(argN)
			args = append(args, aq.Artist)
			argN++
		}
		if len(aq.LabelIDs) > 0 {
			whereSQL += " and exists (select 1 from album_labels al where al.user_id = a.user_id and al.album_id = a.id and al.label_id = any($" + strconv.Itoa(argN) + "::uuid[]))"
			args = append(args, aq.LabelIDs)
			argN++
		}

		orderCol := "a.artist"
		switch aq.Sort {
		case "title":
			orderCol = "a.title"
		case "artist":
			orderCol = "a.artist"
		case "spin_count":
			orderCol = "spin_count"
		case "last_spun_at":
			orderCol = "last_spun_at"
		}
		orderDir := "asc"
		if aq.Order == "desc" {
			orderDir = "desc"
		}

		rows, err := a.db.Query(r.Context(), `
select
  a.id,
  a.discogs_release_id,
  a.title,
  a.artist,
  a.year,
  a.thumb_url,
  a.resource_url,
  a.last_synced_at,
  count(s.id) as spin_count,
  max(s.spun_at) as last_spun_at
from albums a
left join spins s on s.album_id = a.id and s.user_id = a.user_id
`+whereSQL+`
group by a.id
order by `+orderCol+` `+orderDir+`, a.artist asc, a.title asc
limit 500
`, args...)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()

		var out []album
		var albumIDs []string
		for rows.Next() {
			var a album
			if err := rows.Scan(&a.ID, &a.DiscogsReleaseID, &a.Title, &a.Artist, &a.Year, &a.ThumbURL, &a.ResourceURL, &a.LastSyncedAt, &a.SpinCount, &a.LastSpunAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			a.Labels = []label{}
			out = append(out, a)
			albumIDs = append(albumIDs, a.ID)
		}
		if rows.Err() != nil {
			writeJSONError(w, http.StatusInternalServerError, rows.Err())
			return
		}

		// Attach labels in a second query (avoids complex aggregation).
		if len(albumIDs) > 0 {
			lrows, err := a.db.Query(r.Context(), `
select al.album_id, l.id, l.name
from album_labels al
join labels l on l.id = al.label_id and l.user_id = al.user_id
where al.user_id = $1 and al.album_id = any($2::uuid[])
order by l.name asc
`, userID, albumIDs)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			defer lrows.Close()

			byAlbum := make(map[string][]label, len(albumIDs))
			for lrows.Next() {
				var albumID, labelID, name string
				if err := lrows.Scan(&albumID, &labelID, &name); err != nil {
					writeJSONError(w, http.StatusInternalServerError, err)
					return
				}
				byAlbum[albumID] = append(byAlbum[albumID], label{ID: labelID, Name: name})
			}
			if lrows.Err() != nil {
				writeJSONError(w, http.StatusInternalServerError, lrows.Err())
				return
			}
			for i := range out {
				out[i].Labels = byAlbum[out[i].ID]
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleLabels() http.HandlerFunc {
	type label struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		AlbumCount int    `json:"album_count"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}
		rows, err := a.db.Query(r.Context(), `
select l.id, l.name, count(al.album_id) as album_count
from labels l
left join album_labels al on al.label_id = l.id and al.user_id = l.user_id
where l.user_id = $1
group by l.id
order by l.name asc
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		var out []label
		for rows.Next() {
			var x label
			if err := rows.Scan(&x.ID, &x.Name, &x.AlbumCount); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out = append(out, x)
		}
		if rows.Err() != nil {
			writeJSONError(w, http.StatusInternalServerError, rows.Err())
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleCreateLabel() http.HandlerFunc {
	type req struct {
		Name string `json:"name"`
	}
	type resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}
		var in req
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("invalid json"))
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("name is required"))
			return
		}
		var id string
		// Upsert by (user_id, name) unique index.
		err = a.db.QueryRow(r.Context(), `
insert into labels (user_id, name)
values ($1, $2)
on conflict (user_id, name) do update
set name = excluded.name,
    updated_at = now()
returning id
`, userID, name).Scan(&id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, resp{ID: id, Name: name})
	}
}

func (a *App) handleAddAlbumLabel() http.HandlerFunc {
	type req struct {
		LabelID string `json:"label_id,omitempty"`
		Name    string `json:"name,omitempty"` // optional: create/find label by name
	}
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}
		albumID := strings.TrimSpace(chi.URLParam(r, "albumID"))
		if albumID == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("albumID required"))
			return
		}

		// Ensure album belongs to user.
		var ok bool
		if err := a.db.QueryRow(r.Context(), `select exists(select 1 from albums where id=$1 and user_id=$2)`, albumID, userID).Scan(&ok); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, errors.New("album not found"))
			return
		}

		var in req
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("invalid json"))
			return
		}

		labelID := strings.TrimSpace(in.LabelID)
		if labelID == "" {
			name := strings.TrimSpace(in.Name)
			if name == "" {
				writeJSONError(w, http.StatusBadRequest, errors.New("label_id or name required"))
				return
			}
			if err := a.db.QueryRow(r.Context(), `
insert into labels (user_id, name)
values ($1, $2)
on conflict (user_id, name) do update
set name = excluded.name,
    updated_at = now()
returning id
`, userID, name).Scan(&labelID); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			// Ensure label belongs to user.
			var exists bool
			if err := a.db.QueryRow(r.Context(), `select exists(select 1 from labels where id=$1 and user_id=$2)`, labelID, userID).Scan(&exists); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			if !exists {
				writeJSONError(w, http.StatusNotFound, errors.New("label not found"))
				return
			}
		}

		_, err = a.db.Exec(r.Context(), `
insert into album_labels (user_id, album_id, label_id)
values ($1, $2, $3)
on conflict (album_id, label_id) do nothing
`, userID, albumID, labelID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleRemoveAlbumLabel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}
		albumID := strings.TrimSpace(chi.URLParam(r, "albumID"))
		labelID := strings.TrimSpace(chi.URLParam(r, "labelID"))
		if albumID == "" || labelID == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("albumID and labelID required"))
			return
		}
		ct, err := a.db.Exec(r.Context(), `
delete from album_labels
where user_id = $1 and album_id = $2 and label_id = $3
`, userID, albumID, labelID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSONError(w, http.StatusNotFound, errors.New("album label not found"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleSpins() http.HandlerFunc {
	type spin struct {
		ID         string     `json:"id"`
		AlbumID    string     `json:"album_id"`
		SpunAt     time.Time  `json:"spun_at"`
		Note       *string    `json:"note,omitempty"`
		AlbumTitle string     `json:"album_title"`
		AlbumArtist string    `json:"album_artist"`
		AlbumThumb *string    `json:"album_thumb_url,omitempty"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}

		rows, err := a.db.Query(r.Context(), `
select
  s.id,
  s.album_id,
  s.spun_at,
  nullif(s.note, '') as note,
  a.title as album_title,
  a.artist as album_artist,
  a.thumb_url as album_thumb_url
from spins s
join albums a on a.id = s.album_id and a.user_id = s.user_id
where s.user_id = $1
order by s.spun_at desc
limit 200
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()

		var out []spin
		for rows.Next() {
			var s spin
			if err := rows.Scan(&s.ID, &s.AlbumID, &s.SpunAt, &s.Note, &s.AlbumTitle, &s.AlbumArtist, &s.AlbumThumb); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out = append(out, s)
		}
		if rows.Err() != nil {
			writeJSONError(w, http.StatusInternalServerError, rows.Err())
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleCreateSpin() http.HandlerFunc {
	type req struct {
		AlbumID    string  `json:"album_id"`
		SpunAt     *string `json:"spun_at,omitempty"` // RFC3339
		Note       *string `json:"note,omitempty"`
	}
	type resp struct {
		ID string `json:"id"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}

		var in req
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("invalid json"))
			return
		}
		in.AlbumID = strings.TrimSpace(in.AlbumID)
		if in.AlbumID == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("album_id is required"))
			return
		}

		spunAt := time.Now()
		if in.SpunAt != nil && strings.TrimSpace(*in.SpunAt) != "" {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.SpunAt))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, errors.New("spun_at must be RFC3339"))
				return
			}
			spunAt = t
		}

		var note string
		if in.Note != nil {
			note = strings.TrimSpace(*in.Note)
		}

		// Ensure album belongs to user.
		var exists bool
		if err := a.db.QueryRow(r.Context(), `select exists(select 1 from albums where id=$1 and user_id=$2)`, in.AlbumID, userID).Scan(&exists); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		if !exists {
			writeJSONError(w, http.StatusBadRequest, errors.New("unknown album_id"))
			return
		}

		var id string
		err = a.db.QueryRow(r.Context(), `
insert into spins (user_id, album_id, spun_at, note)
values ($1, $2, $3, nullif($4, ''))
returning id
`, userID, in.AlbumID, spunAt, note).Scan(&id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, resp{ID: id})
	}
}

func (a *App) handleDeleteSpin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}

		spinID := chi.URLParam(r, "spinID")
		spinID = strings.TrimSpace(spinID)
		if spinID == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("spinID required"))
			return
		}

		ct, err := a.db.Exec(r.Context(), `delete from spins where id=$1 and user_id=$2`, spinID, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSONError(w, http.StatusNotFound, errors.New("spin not found"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleAlbumsSync() http.HandlerFunc {
	type resp struct {
		Status string `json:"status"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.requireSession(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err)
			return
		}
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured"))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if err := a.syncDiscogsCollection(ctx, userID); err != nil {
			writeJSONError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, resp{Status: "ok"})
	}
}

func (a *App) requireSession(r *http.Request) (string, error) {
	sealer, err := newSealerFromEnv()
	if err != nil {
		return "", err
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", errors.New("not authenticated")
	}
	b, err := sealer.openFromString(c.Value)
	if err != nil {
		return "", errors.New("invalid session")
	}
	userID := string(b)
	if userID == "" {
		return "", errors.New("invalid session")
	}
	return userID, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

