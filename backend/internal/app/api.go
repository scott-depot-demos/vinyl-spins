package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	discogs "github.com/stmcallister/go-discogs"
	"github.com/jackc/pgx/v5"
)

type ctxKey int

const ctxKeyUserID ctxKey = iota

func (a *App) handleMe() http.HandlerFunc {
	type resp struct {
		UserID          string `json:"user_id"`
		DiscogsUserID   int64  `json:"discogs_user_id"`
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
	type tag struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type album struct {
		ID               string     `json:"id"`
		DiscogsReleaseID int64      `json:"discogs_release_id"`
		Title            string     `json:"title"`
		Artist           string     `json:"artist"`
		RecordLabel      *string    `json:"record_label,omitempty"`
		Year             *int       `json:"year,omitempty"`
		ThumbURL         *string    `json:"thumb_url,omitempty"`
		ResourceURL      *string    `json:"resource_url,omitempty"`
		LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
		SpinCount        int        `json:"spin_count"`
		LastSpunAt       *time.Time `json:"last_spun_at,omitempty"`
		Tags             []tag      `json:"tags"`
	}

	type albumQuery struct {
		Q      string
		Artist string
		TagIDs []string
		Sort   string
		Order  string
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
		if raw := strings.TrimSpace(v.Get("tag_ids")); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					q.TagIDs = append(q.TagIDs, part)
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
		if len(aq.TagIDs) > 0 {
			whereSQL += " and exists (select 1 from album_tags at where at.user_id = a.user_id and at.album_id = a.id and at.tag_id = any($" + strconv.Itoa(argN) + "::uuid[]))"
			args = append(args, aq.TagIDs)
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
  nullif(a.record_label, '') as record_label,
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
			if err := rows.Scan(&a.ID, &a.DiscogsReleaseID, &a.Title, &a.Artist, &a.RecordLabel, &a.Year, &a.ThumbURL, &a.ResourceURL, &a.LastSyncedAt, &a.SpinCount, &a.LastSpunAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			if a.ResourceURL != nil {
				u := discogsWebReleaseURL(a.DiscogsReleaseID, *a.ResourceURL)
				a.ResourceURL = &u
			} else if a.DiscogsReleaseID > 0 {
				u := discogsWebReleaseURL(a.DiscogsReleaseID, "")
				if u != "" {
					a.ResourceURL = &u
				}
			}
			a.Tags = []tag{}
			out = append(out, a)
			albumIDs = append(albumIDs, a.ID)
		}
		if rows.Err() != nil {
			writeJSONError(w, http.StatusInternalServerError, rows.Err())
			return
		}

		// Attach tags in a second query (avoids complex aggregation).
		if len(albumIDs) > 0 {
			lrows, err := a.db.Query(r.Context(), `
select at.album_id, t.id, t.name
from album_tags at
join tags t on t.id = at.tag_id and t.user_id = at.user_id
where at.user_id = $1 and at.album_id = any($2::uuid[])
order by t.name asc
`, userID, albumIDs)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			defer lrows.Close()

			byAlbum := make(map[string][]tag, len(albumIDs))
			for lrows.Next() {
				var albumID, tagID, name string
				if err := lrows.Scan(&albumID, &tagID, &name); err != nil {
					writeJSONError(w, http.StatusInternalServerError, err)
					return
				}
				byAlbum[albumID] = append(byAlbum[albumID], tag{ID: tagID, Name: name})
			}
			if lrows.Err() != nil {
				writeJSONError(w, http.StatusInternalServerError, lrows.Err())
				return
			}
			for i := range out {
				out[i].Tags = byAlbum[out[i].ID]
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) discogsAuthedClient(ctx context.Context, userID string) (*discogs.Client, error) {
	if a.db == nil {
		return nil, errors.New("DATABASE_URL not configured")
	}
	sealer, err := newSealerFromEnv()
	if err != nil {
		return nil, err
	}

	consumerKey := getenvDefault("DISCOGS_CONSUMER_KEY", "")
	consumerSecret := getenvDefault("DISCOGS_CONSUMER_SECRET", "")
	if consumerKey == "" || consumerSecret == "" {
		return nil, errors.New("missing DISCOGS_CONSUMER_KEY or DISCOGS_CONSUMER_SECRET")
	}

	var accessTokenEnc, accessSecretEnc []byte
	if err := a.db.QueryRow(ctx, `
select access_token_enc, access_secret_enc
from oauth_tokens
where user_id = $1 and provider = 'discogs'
`, userID).Scan(&accessTokenEnc, &accessSecretEnc); err != nil {
		return nil, fmt.Errorf("load oauth token: %w", err)
	}

	accessTokenBytes, err := sealer.openFromBytes(accessTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}
	accessSecretBytes, err := sealer.openFromBytes(accessSecretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt access secret: %w", err)
	}

	accessToken := string(accessTokenBytes)
	accessSecret := string(accessSecretBytes)
	if accessToken == "" || accessSecret == "" {
		return nil, errors.New("missing decrypted discogs token")
	}

	return discogs.NewOAuthClient(consumerKey, consumerSecret, accessToken, accessSecret).WithUserAgent(discogsUserAgent()), nil
}

func (a *App) handlePickAlbum() http.HandlerFunc {
	type resp struct {
		ID              string     `json:"id"`
		DiscogsReleaseID int64      `json:"discogs_release_id"`
		Title           string     `json:"title"`
		Artist          string     `json:"artist"`
		Year            *int       `json:"year,omitempty"`
		ThumbURL        *string    `json:"thumb_url,omitempty"`
		ResourceURL     *string    `json:"resource_url,omitempty"`
		SpinCount       int        `json:"spin_count"`
		LastSpunAt      *time.Time `json:"last_spun_at,omitempty"`
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

		// Same filters as /api/albums (optional).
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		artist := strings.TrimSpace(r.URL.Query().Get("artist"))
		var tagIDs []string
		if raw := strings.TrimSpace(r.URL.Query().Get("tag_ids")); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					tagIDs = append(tagIDs, part)
				}
			}
		}

		var (
			args     []any
			argN     = 1
			whereSQL = "where a.user_id = $1"
		)
		args = append(args, userID)
		argN++

		if q != "" {
			whereSQL += " and (a.title ilike $" + strconv.Itoa(argN) + " or a.artist ilike $" + strconv.Itoa(argN) + ")"
			args = append(args, "%"+q+"%")
			argN++
		}
		if artist != "" {
			whereSQL += " and a.artist = $" + strconv.Itoa(argN)
			args = append(args, artist)
			argN++
		}
		if len(tagIDs) > 0 {
			whereSQL += " and exists (select 1 from album_tags at where at.user_id = a.user_id and at.album_id = a.id and at.tag_id = any($" + strconv.Itoa(argN) + "::uuid[]))"
			args = append(args, tagIDs)
			argN++
		}

		// Weighted random pick biased toward older last_spun_at:
		// - compute age_days since last spun (or epoch if never spun)
		// - weight grows with age, capped so never-spun doesn't dominate infinitely
		// - choose weighted random using Efraimidis-Spirakis (minimize -ln(U)/w)
		var out resp
		err = a.db.QueryRow(r.Context(), `
with candidates as (
  select
    a.id,
    a.discogs_release_id,
    a.title,
    a.artist,
    a.year,
    a.thumb_url,
    a.resource_url,
    count(s.id) as spin_count,
    max(s.spun_at) as last_spun_at
  from albums a
  left join spins s on s.album_id = a.id and s.user_id = a.user_id
  `+whereSQL+`
  group by a.id
),
weighted as (
  select
    *,
    -- grows roughly weekly, capped at ~10 years
    (1 + power(2, least(extract(epoch from (now() - coalesce(last_spun_at, timestamptz '1970-01-01'))) / 86400.0, 3650.0) / 365.0)) as weight
  from candidates
)
select
  id,
  discogs_release_id,
  title,
  artist,
  year,
  thumb_url,
  resource_url,
  spin_count,
  last_spun_at
from weighted
order by (-ln(greatest(random(), 1e-12)) / weight) asc
limit 1
`, args...).Scan(&out.ID, &out.DiscogsReleaseID, &out.Title, &out.Artist, &out.Year, &out.ThumbURL, &out.ResourceURL, &out.SpinCount, &out.LastSpunAt)
		if err != nil {
			// No rows means no albums matched.
			if strings.Contains(err.Error(), "no rows") {
				writeJSONError(w, http.StatusNotFound, errors.New("no albums match filters"))
				return
			}
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		if out.ResourceURL != nil {
			u := discogsWebReleaseURL(out.DiscogsReleaseID, *out.ResourceURL)
			out.ResourceURL = &u
		} else if out.DiscogsReleaseID > 0 {
			u := discogsWebReleaseURL(out.DiscogsReleaseID, "")
			if u != "" {
				out.ResourceURL = &u
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleAlbumDetail() http.HandlerFunc {
	type tag struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type spin struct {
		ID     string     `json:"id"`
		SpunAt time.Time  `json:"spun_at"`
		Note   *string    `json:"note,omitempty"`
	}

	type discogsFormat struct {
		Name         string   `json:"name"`
		Qty          string   `json:"qty"`
		Text         string   `json:"text"`
		Descriptions []string `json:"descriptions"`
	}
	type discogsDetails struct {
		ReleaseID     int64          `json:"release_id"`
		Title         string         `json:"title"`
		Year          int            `json:"year"`
		Released      string         `json:"released,omitempty"`
		MasterID      int            `json:"master_id,omitempty"`
		OriginalYear  *int           `json:"original_year,omitempty"`
		Country       string         `json:"country,omitempty"`
		Formats       []discogsFormat `json:"formats"`
		Genres        []string       `json:"genres,omitempty"`
		Styles        []string       `json:"styles,omitempty"`
		Notes         string         `json:"notes,omitempty"`
	}

	type resp struct {
		ID              string     `json:"id"`
		DiscogsReleaseID int64      `json:"discogs_release_id"`
		Title           string     `json:"title"`
		Artist          string     `json:"artist"`
		Year            *int       `json:"year,omitempty"` // from collection basic info
		ThumbURL        *string    `json:"thumb_url,omitempty"`
		ResourceURL     *string    `json:"resource_url,omitempty"`
		LastSyncedAt    *time.Time `json:"last_synced_at,omitempty"`
		SpinCount       int        `json:"spin_count"`
		LastSpunAt      *time.Time `json:"last_spun_at,omitempty"`
		Tags            []tag      `json:"tags"`
		Spins           []spin     `json:"spins"`
		Discogs         *discogsDetails `json:"discogs,omitempty"`
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

		var out resp
		err = a.db.QueryRow(r.Context(), `
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
where a.user_id = $1 and a.id = $2
group by a.id
`, userID, albumID).Scan(&out.ID, &out.DiscogsReleaseID, &out.Title, &out.Artist, &out.Year, &out.ThumbURL, &out.ResourceURL, &out.LastSyncedAt, &out.SpinCount, &out.LastSpunAt)
		if err != nil {
			if strings.Contains(err.Error(), "no rows") {
				writeJSONError(w, http.StatusNotFound, errors.New("album not found"))
				return
			}
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		if out.ResourceURL != nil {
			u := discogsWebReleaseURL(out.DiscogsReleaseID, *out.ResourceURL)
			out.ResourceURL = &u
		} else if out.DiscogsReleaseID > 0 {
			u := discogsWebReleaseURL(out.DiscogsReleaseID, "")
			if u != "" {
				out.ResourceURL = &u
			}
		}

		// Tags
		lrows, err := a.db.Query(r.Context(), `
select t.id, t.name
from album_tags at
join tags t on t.id = at.tag_id and t.user_id = at.user_id
where at.user_id = $1 and at.album_id = $2
order by t.name asc
`, userID, albumID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer lrows.Close()
		out.Tags = []tag{}
		for lrows.Next() {
			var x tag
			if err := lrows.Scan(&x.ID, &x.Name); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.Tags = append(out.Tags, x)
		}
		if lrows.Err() != nil {
			writeJSONError(w, http.StatusInternalServerError, lrows.Err())
			return
		}

		// Spins for this album
		srows, err := a.db.Query(r.Context(), `
select id, spun_at, nullif(note, '') as note
from spins
where user_id = $1 and album_id = $2
order by spun_at desc
limit 500
`, userID, albumID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer srows.Close()
		out.Spins = []spin{}
		for srows.Next() {
			var s spin
			if err := srows.Scan(&s.ID, &s.SpunAt, &s.Note); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.Spins = append(out.Spins, s)
		}
		if srows.Err() != nil {
			writeJSONError(w, http.StatusInternalServerError, srows.Err())
			return
		}

		// Discogs details (best-effort; still return album even if Discogs fails)
		if out.DiscogsReleaseID != 0 {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			c, err := a.discogsAuthedClient(ctx, userID)
			if err == nil {
				rel, err := c.GetRelease(ctx, int(out.DiscogsReleaseID), "")
				if err == nil && rel != nil {
					d := &discogsDetails{
						ReleaseID: out.DiscogsReleaseID,
						Title:     rel.Title,
						Year:      rel.Year,
						Released:  strings.TrimSpace(rel.Released),
						MasterID:  rel.MasterID,
						Country:   strings.TrimSpace(rel.Country),
						Genres:    rel.Genres,
						Styles:    rel.Styles,
						Notes:     strings.TrimSpace(rel.Notes),
					}
					for _, f := range rel.Formats {
						if f == nil {
							continue
						}
						d.Formats = append(d.Formats, discogsFormat{
							Name:         f.Name,
							Qty:          f.Qty,
							Text:         f.Text,
							Descriptions: f.Descriptions,
						})
					}
					// "Original year": prefer master year if available.
					if rel.MasterID != 0 {
						if m, err := c.GetMasterRelease(ctx, rel.MasterID); err == nil && m != nil && m.Year != 0 {
							yy := m.Year
							d.OriginalYear = &yy
						}
					}
					out.Discogs = d
				}
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleImportOggerPlaylog() http.HandlerFunc {
	type resp struct {
		TotalRows        int      `json:"total_rows"`
		ParsedRows       int      `json:"parsed_rows"`
		DedupedRows      int      `json:"deduped_rows"`
		MatchedRows      int      `json:"matched_rows"`
		InsertedSpins    int      `json:"inserted_spins"`
		AlreadyExisted   int      `json:"already_existed"`
		UnmatchedRows    int      `json:"unmatched_rows"`
		UnmatchedReleaseIDs []int64 `json:"unmatched_release_ids,omitempty"`
		ParseErrors      int      `json:"parse_errors"`
		Timezone         string   `json:"timezone"`
	}

	type play struct {
		releaseID int64
		spunAt    time.Time
		note      string
	}

	normalizeHeader := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimPrefix(s, "\uFEFF") // BOM
		s = strings.ReplaceAll(s, "\u00A0", " ") // NBSP
		s = strings.TrimSpace(s)
		s = strings.ToLower(s)
		return s
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

		tzName := strings.TrimSpace(r.URL.Query().Get("tz"))
		if tzName == "" {
			tzName = time.Local.String()
		}
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("invalid tz (use e.g. America/Los_Angeles)"))
			return
		}

		// Limit CSV size to 50MB.
		r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("expected multipart form with file"))
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("missing file field 'file'"))
			return
		}
		defer f.Close()

		cr := csv.NewReader(f)
		cr.FieldsPerRecord = -1

		header, err := cr.Read()
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("empty csv"))
			return
		}

		col := map[string]int{}
		for i, h := range header {
			col[normalizeHeader(h)] = i
		}

		idxRelease, ok1 := col["release id"]
		idxPlayTime, ok2 := col["play time"]
		idxNotes := col["play notes"]
		if !ok1 || !ok2 {
			writeJSONError(w, http.StatusBadRequest, errors.New("csv missing required columns: Release ID, Play Time"))
			return
		}

		out := resp{Timezone: tzName}
		var plays []play
		seen := make(map[string]struct{})

		for {
			rec, err := cr.Read()
			if err == io.EOF {
				break
			}
			out.TotalRows++
			if err != nil {
				out.ParseErrors++
				continue
			}
			// Skip repeated header rows inside export.
			if idxRelease < len(rec) && normalizeHeader(rec[idxRelease]) == "release id" {
				continue
			}
			if idxRelease >= len(rec) || idxPlayTime >= len(rec) {
				out.ParseErrors++
				continue
			}

			releaseRaw := strings.TrimSpace(rec[idxRelease])
			releaseRaw = strings.ReplaceAll(releaseRaw, "\u00A0", "")
			if releaseRaw == "" {
				continue
			}
			releaseID, err := strconv.ParseInt(releaseRaw, 10, 64)
			if err != nil || releaseID <= 0 {
				out.ParseErrors++
				continue
			}

			playTimeRaw := strings.TrimSpace(rec[idxPlayTime])
			if playTimeRaw == "" {
				out.ParseErrors++
				continue
			}
			// Ogger Club export uses "YYYY-MM-DD HH:MM:SS" (no timezone).
			t, err := time.ParseInLocation("2006-01-02 15:04:05", playTimeRaw, loc)
			if err != nil {
				out.ParseErrors++
				continue
			}

			note := ""
			if idxNotes >= 0 && idxNotes < len(rec) {
				note = strings.TrimSpace(rec[idxNotes])
			}

			out.ParsedRows++
			key := fmt.Sprintf("%d|%s|%s", releaseID, t.Format(time.RFC3339Nano), note)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			plays = append(plays, play{releaseID: releaseID, spunAt: t, note: note})
		}

		out.DedupedRows = len(plays)

		if len(plays) == 0 {
			writeJSON(w, http.StatusOK, out)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tx, err := a.db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `
create temporary table if not exists tmp_ogger_playlog_import (
  release_id bigint not null,
  spun_at timestamptz not null,
  note text
) on commit drop
`); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		rows := make([][]any, 0, len(plays))
		for _, p := range plays {
			rows = append(rows, []any{p.releaseID, p.spunAt, p.note})
		}

		_, err = tx.CopyFrom(
			ctx,
			pgx.Identifier{"tmp_ogger_playlog_import"},
			[]string{"release_id", "spun_at", "note"},
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Errorf("copy import rows: %w", err))
			return
		}

		// How many rows match existing albums?
		var matched int
		if err := tx.QueryRow(ctx, `
select count(*)
from tmp_ogger_playlog_import t
join albums a on a.user_id = $1 and a.discogs_release_id = t.release_id
`, userID).Scan(&matched); err == nil {
			out.MatchedRows = matched
		} else {
			// ignore; still proceed
		}

		// Insert spins (skip if already exists exact (album_id, spun_at, note)).
		insRows, err := tx.Query(ctx, `
with resolved as (
  select
    a.id as album_id,
    t.spun_at,
    nullif(t.note, '') as note
  from tmp_ogger_playlog_import t
  join albums a on a.user_id = $1 and a.discogs_release_id = t.release_id
)
insert into spins (user_id, album_id, spun_at, note)
select
  $1,
  r.album_id,
  r.spun_at,
  r.note
from resolved r
where not exists (
  select 1
  from spins s
  where s.user_id = $1
    and s.album_id = r.album_id
    and s.spun_at = r.spun_at
    and coalesce(s.note, '') = coalesce(r.note, '')
)
returning id
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for insRows.Next() {
			out.InsertedSpins++
		}
		insRows.Close()

		// Unmatched release IDs (up to 25) for debugging.
		umRows, err := tx.Query(ctx, `
select distinct t.release_id
from tmp_ogger_playlog_import t
left join albums a on a.user_id = $1 and a.discogs_release_id = t.release_id
where a.id is null
order by t.release_id asc
limit 25
`, userID)
		if err == nil {
			for umRows.Next() {
				var rid int64
				if err := umRows.Scan(&rid); err == nil {
					out.UnmatchedReleaseIDs = append(out.UnmatchedReleaseIDs, rid)
				}
			}
			umRows.Close()
		}

		if err := tx.QueryRow(ctx, `
select count(*)
from tmp_ogger_playlog_import t
left join albums a on a.user_id = $1 and a.discogs_release_id = t.release_id
where a.id is null
`, userID).Scan(&out.UnmatchedRows); err != nil {
			// ignore
		}

		if out.MatchedRows == 0 {
			out.MatchedRows = out.DedupedRows - out.UnmatchedRows
			if out.MatchedRows < 0 {
				out.MatchedRows = 0
			}
		}

		out.AlreadyExisted = out.DedupedRows - out.UnmatchedRows - out.InsertedSpins
		if out.AlreadyExisted < 0 {
			out.AlreadyExisted = 0
		}

		if err := tx.Commit(ctx); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func discogsWebReleaseURL(releaseID int64, stored string) string {
	// If we've already stored a human-facing Discogs URL, keep it.
	if s := strings.TrimSpace(stored); s != "" {
		if strings.Contains(s, "www.discogs.com/release/") || strings.Contains(s, "discogs.com/release/") {
			return s
		}
		// Legacy stored value: API resource URL like "https://api.discogs.com/releases/123"
		if strings.Contains(s, "api.discogs.com/releases/") && releaseID > 0 {
			return fmt.Sprintf("https://www.discogs.com/release/%d", releaseID)
		}
	}
	if releaseID > 0 {
		return fmt.Sprintf("https://www.discogs.com/release/%d", releaseID)
	}
	return ""
}

func (a *App) handleLabels() http.HandlerFunc {
	// Backwards-compatible alias: historically called these "labels".
	return a.handleTags()
}

func (a *App) handleTags() http.HandlerFunc {
	type tag struct {
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
select t.id, t.name, count(at.album_id) as album_count
from tags t
left join album_tags at on at.tag_id = t.id and at.user_id = t.user_id
where t.user_id = $1
group by t.id
order by t.name asc
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		var out []tag
		for rows.Next() {
			var x tag
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

func (a *App) handleCreateTag() http.HandlerFunc {
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
insert into tags (user_id, name)
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

func (a *App) handleAddAlbumTag() http.HandlerFunc {
	type req struct {
		TagID string `json:"tag_id,omitempty"`
		Name  string `json:"name,omitempty"` // optional: create/find tag by name
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

		tagID := strings.TrimSpace(in.TagID)
		if tagID == "" {
			name := strings.TrimSpace(in.Name)
			if name == "" {
				writeJSONError(w, http.StatusBadRequest, errors.New("tag_id or name required"))
				return
			}
			if err := a.db.QueryRow(r.Context(), `
insert into tags (user_id, name)
values ($1, $2)
on conflict (user_id, name) do update
set name = excluded.name,
    updated_at = now()
returning id
`, userID, name).Scan(&tagID); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			// Ensure tag belongs to user.
			var exists bool
			if err := a.db.QueryRow(r.Context(), `select exists(select 1 from tags where id=$1 and user_id=$2)`, tagID, userID).Scan(&exists); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			if !exists {
				writeJSONError(w, http.StatusNotFound, errors.New("tag not found"))
				return
			}
		}

		_, err = a.db.Exec(r.Context(), `
insert into album_tags (user_id, album_id, tag_id)
values ($1, $2, $3)
on conflict (album_id, tag_id) do nothing
`, userID, albumID, tagID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleRemoveAlbumTag() http.HandlerFunc {
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
		tagID := strings.TrimSpace(chi.URLParam(r, "tagID"))
		if albumID == "" || tagID == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("albumID and tagID required"))
			return
		}
		ct, err := a.db.Exec(r.Context(), `
delete from album_tags
where user_id = $1 and album_id = $2 and tag_id = $3
`, userID, albumID, tagID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSONError(w, http.StatusNotFound, errors.New("album tag not found"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleSpins() http.HandlerFunc {
	type spin struct {
		ID          string    `json:"id"`
		AlbumID     string    `json:"album_id"`
		SpunAt      time.Time `json:"spun_at"`
		Note        *string   `json:"note,omitempty"`
		AlbumTitle  string    `json:"album_title"`
		AlbumArtist string    `json:"album_artist"`
		AlbumThumb  *string   `json:"album_thumb_url,omitempty"`
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
		AlbumID string  `json:"album_id"`
		SpunAt  *string `json:"spun_at,omitempty"` // RFC3339
		Note    *string `json:"note,omitempty"`
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
