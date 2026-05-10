package app

import (
	"errors"
	"net/http"
	"time"
)

func (a *App) handleCollectionReport() http.HandlerFunc {
	type yearCount struct {
		Year  int `json:"year"`
		Count int `json:"count"`
	}
	type artistCount struct {
		Artist string `json:"artist"`
		Count  int    `json:"count"`
	}
	type labelCount struct {
		Label string `json:"label"`
		Count int    `json:"count"`
	}
	type formatCount struct {
		Format string `json:"format"`
		Count  int    `json:"count"`
	}
	type response struct {
		ByYear         []yearCount   `json:"by_year"`
		ByOriginalYear []yearCount   `json:"by_original_year"`
		ByArtist       []artistCount `json:"by_artist"`
		ByLabel        []labelCount  `json:"by_label"`
		ByFormat       []formatCount `json:"by_format"`
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

		out := response{
			ByYear:         []yearCount{},
			ByOriginalYear: []yearCount{},
			ByArtist:       []artistCount{},
			ByLabel:        []labelCount{},
			ByFormat:       []formatCount{},
		}

		// ── Records by pressing year ──────────────────────────────────────────
		rows, err := a.db.Query(r.Context(), `
select year, count(*)::int
from albums
where user_id = $1 and year is not null
group by year
order by year
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var yc yearCount
			if err := rows.Scan(&yc.Year, &yc.Count); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.ByYear = append(out.ByYear, yc)
		}
		rows.Close()

		// ── Records by original year ──────────────────────────────────────────
		rows, err = a.db.Query(r.Context(), `
select original_year, count(*)::int
from albums
where user_id = $1 and original_year is not null
group by original_year
order by original_year
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var yc yearCount
			if err := rows.Scan(&yc.Year, &yc.Count); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.ByOriginalYear = append(out.ByOriginalYear, yc)
		}
		rows.Close()

		// ── Records by artist ─────────────────────────────────────────────────
		rows, err = a.db.Query(r.Context(), `
select artist, count(*)::int
from albums
where user_id = $1
group by artist
order by count(*) desc, artist
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var ac artistCount
			if err := rows.Scan(&ac.Artist, &ac.Count); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.ByArtist = append(out.ByArtist, ac)
		}
		rows.Close()

		// ── Records by label ──────────────────────────────────────────────────
		rows, err = a.db.Query(r.Context(), `
select coalesce(record_label, 'Unknown'), count(*)::int
from albums
where user_id = $1
group by record_label
order by count(*) desc, record_label
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var lc labelCount
			if err := rows.Scan(&lc.Label, &lc.Count); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.ByLabel = append(out.ByLabel, lc)
		}
		rows.Close()

		// ── Records by format ─────────────────────────────────────────────────
		rows, err = a.db.Query(r.Context(), `
select coalesce(format, 'Unknown'), count(*)::int
from albums
where user_id = $1
group by format
order by count(*) desc, format
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var fc formatCount
			if err := rows.Scan(&fc.Format, &fc.Count); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.ByFormat = append(out.ByFormat, fc)
		}
		rows.Close()

		writeJSON(w, http.StatusOK, out)
	}
}

func (a *App) handleReports() http.HandlerFunc {
	type spinOverTime struct {
		Period    string `json:"period"`
		SpinCount int    `json:"spin_count"`
	}
	type topArtist struct {
		Artist      string `json:"artist"`
		SpinCount   int    `json:"spin_count"`
		RecordCount int    `json:"record_count"`
	}
	type reportRecord struct {
		ID         string     `json:"id"`
		Title      string     `json:"title"`
		Artist     string     `json:"artist"`
		Year       *int       `json:"year,omitempty"`
		ThumbURL   *string    `json:"thumb_url,omitempty"`
		LastSpunAt *time.Time `json:"last_spun_at,omitempty"`
		SpinCount  int        `json:"spin_count"`
	}
	type collectionStats struct {
		TotalRecords   int     `json:"total_records"`
		PlayedRecords  int     `json:"played_records"`
		NeverPlayed    int     `json:"never_played"`
		UtilizationPct float64 `json:"utilization_pct"`
	}
	type response struct {
		SpinsOverTime []spinOverTime  `json:"spins_over_time"`
		TopArtists    []topArtist     `json:"top_artists"`
		NeverPlayed   []reportRecord  `json:"never_played"`
		Neglected     []reportRecord  `json:"neglected"`
		Stats         collectionStats `json:"stats"`
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

		period := r.URL.Query().Get("period")
		if period != "month" {
			period = "week"
		}
		lookback := "52 weeks"
		if period == "month" {
			lookback = "24 months"
		}

		out := response{
			SpinsOverTime: []spinOverTime{},
			TopArtists:    []topArtist{},
			NeverPlayed:   []reportRecord{},
			Neglected:     []reportRecord{},
		}

		// ── Spins over time ──────────────────────────────────────────────────
		rows, err := a.db.Query(r.Context(), `
select
  date_trunc($1, spun_at)::date as period,
  count(*)::int as spin_count
from spins
where user_id = $2
  and spun_at >= now() - $3::interval
group by period
order by period
`, period, userID, lookback)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var s spinOverTime
			var t time.Time
			if err := rows.Scan(&t, &s.SpinCount); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			s.Period = t.Format("2006-01-02")
			out.SpinsOverTime = append(out.SpinsOverTime, s)
		}
		rows.Close()

		// ── Top artists ──────────────────────────────────────────────────────
		rows, err = a.db.Query(r.Context(), `
select
  a.artist,
  count(s.id)::int as spin_count,
  count(distinct a.id)::int as record_count
from albums a
left join spins s on s.album_id = a.id and s.user_id = $1
where a.user_id = $1
group by a.artist
order by spin_count desc, a.artist
limit 20
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var ta topArtist
			if err := rows.Scan(&ta.Artist, &ta.SpinCount, &ta.RecordCount); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.TopArtists = append(out.TopArtists, ta)
		}
		rows.Close()

		// ── Never played ─────────────────────────────────────────────────────
		rows, err = a.db.Query(r.Context(), `
select a.id, a.title, a.artist, a.year, a.thumb_url
from albums a
where a.user_id = $1
  and not exists (
    select 1 from spins s where s.album_id = a.id and s.user_id = $1
  )
order by a.artist, a.title
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var rr reportRecord
			if err := rows.Scan(&rr.ID, &rr.Title, &rr.Artist, &rr.Year, &rr.ThumbURL); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.NeverPlayed = append(out.NeverPlayed, rr)
		}
		rows.Close()

		// ── Neglected (played but not in 6 months, excludes 7" singles) ────────
		rows, err = a.db.Query(r.Context(), `
select
  a.id, a.title, a.artist, a.year, a.thumb_url,
  max(s.spun_at) as last_spun_at,
  count(s.id)::int as spin_count
from albums a
join spins s on s.album_id = a.id and s.user_id = $1
where a.user_id = $1
  and (a.format is null or a.format not like '7%')
group by a.id, a.title, a.artist, a.year, a.thumb_url
having max(s.spun_at) < now() - interval '6 months'
order by last_spun_at asc
`, userID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		for rows.Next() {
			var rr reportRecord
			if err := rows.Scan(&rr.ID, &rr.Title, &rr.Artist, &rr.Year, &rr.ThumbURL, &rr.LastSpunAt, &rr.SpinCount); err != nil {
				rows.Close()
				writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			out.Neglected = append(out.Neglected, rr)
		}
		rows.Close()

		// ── Collection stats ─────────────────────────────────────────────────
		err = a.db.QueryRow(r.Context(), `
select
  count(distinct a.id)::int,
  count(distinct case when s.id is not null then a.id end)::int
from albums a
left join spins s on s.album_id = a.id and s.user_id = $1
where a.user_id = $1
`, userID).Scan(&out.Stats.TotalRecords, &out.Stats.PlayedRecords)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		out.Stats.NeverPlayed = out.Stats.TotalRecords - out.Stats.PlayedRecords
		if out.Stats.TotalRecords > 0 {
			out.Stats.UtilizationPct = float64(out.Stats.PlayedRecords) / float64(out.Stats.TotalRecords) * 100
		}

		writeJSON(w, http.StatusOK, out)
	}
}
