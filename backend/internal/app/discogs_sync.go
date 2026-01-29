package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	discogs "github.com/stmcallister/go-discogs"
	"github.com/jackc/pgx/v5"
)

func (a *App) syncDiscogsCollection(ctx context.Context, userID string) error {
	if a.db == nil {
		return errors.New("DATABASE_URL not configured")
	}

	sealer, err := newSealerFromEnv()
	if err != nil {
		return err
	}

	consumerKey := os.Getenv("DISCOGS_CONSUMER_KEY")
	consumerSecret := os.Getenv("DISCOGS_CONSUMER_SECRET")
	if consumerKey == "" || consumerSecret == "" {
		return errors.New("missing DISCOGS_CONSUMER_KEY or DISCOGS_CONSUMER_SECRET")
	}

	var discogsUsername string
	var accessTokenEnc, accessSecretEnc []byte
	err = a.db.QueryRow(ctx, `
select u.discogs_username, t.access_token_enc, t.access_secret_enc
from users u
join oauth_tokens t on t.user_id = u.id and t.provider = 'discogs'
where u.id = $1
`, userID).Scan(&discogsUsername, &accessTokenEnc, &accessSecretEnc)
	if err != nil {
		return fmt.Errorf("load oauth token: %w", err)
	}
	if discogsUsername == "" {
		return errors.New("missing discogs username for user")
	}

	accessTokenBytes, err := sealer.openFromBytes(accessTokenEnc)
	if err != nil {
		return fmt.Errorf("decrypt access token: %w", err)
	}
	accessSecretBytes, err := sealer.openFromBytes(accessSecretEnc)
	if err != nil {
		return fmt.Errorf("decrypt access secret: %w", err)
	}

	accessToken := string(accessTokenBytes)
	accessSecret := string(accessSecretBytes)
	if accessToken == "" || accessSecret == "" {
		return errors.New("missing decrypted discogs token")
	}

	c := discogs.NewOAuthClient(consumerKey, consumerSecret, accessToken, accessSecret).WithUserAgent(discogsUserAgent())
	rl, err := c.GetUserCollectionAllItemsByFolder(ctx, discogsUsername, "artist", 0)
	if err != nil {
		return fmt.Errorf("discogs collection: %w", err)
	}

	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("db begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, rel := range rl.Releases {
		if rel == nil || rel.BasicInformation == nil {
			continue
		}

		title := strings.TrimSpace(rel.BasicInformation.Title)
		artist := strings.TrimSpace(firstDiscogsArtist(rel.BasicInformation.Artists))
		year := rel.BasicInformation.Year
		thumb := strings.TrimSpace(rel.BasicInformation.Thumb)
		resourceURL := strings.TrimSpace(rel.BasicInformation.ResourceURL)

		var yearPtr *int
		if year != 0 {
			yearPtr = &year
		}
		var thumbPtr *string
		if thumb != "" {
			thumbPtr = &thumb
		}
		var resourcePtr *string
		if resourceURL != "" {
			resourcePtr = &resourceURL
		}

		_, err := tx.Exec(ctx, `
insert into albums (user_id, discogs_release_id, title, artist, year, thumb_url, resource_url, last_synced_at)
values ($1, $2, $3, $4, $5, $6, $7, now())
on conflict (user_id, discogs_release_id) do update
set title = excluded.title,
    artist = excluded.artist,
    year = excluded.year,
    thumb_url = excluded.thumb_url,
    resource_url = excluded.resource_url,
    last_synced_at = now(),
    updated_at = now()
`, userID, int64(rel.ID), title, artist, yearPtr, thumbPtr, resourcePtr)
		if err != nil {
			return fmt.Errorf("upsert album: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db commit: %w", err)
	}
	return nil
}

func firstDiscogsArtist(artists []*discogs.Artist) string {
	if len(artists) == 0 {
		return ""
	}
	if artists[0] == nil {
		return ""
	}
	return artists[0].Name
}

