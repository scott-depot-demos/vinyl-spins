package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	discogs "github.com/stmcallister/go-discogs"
	"github.com/jackc/pgx/v5"
)

const (
	discogsAuthorizeURL    = "https://www.discogs.com/oauth/authorize"

	discogsTmpCookieName = "dlt_discogs_oauth_tmp"
	sessionCookieName    = "dlt_session"
)

func (a *App) handleDiscogsOAuthStart() http.HandlerFunc {
	type tmpPayload struct {
		Token  string `json:"t"`
		Secret string `json:"s"`
		TS     int64  `json:"ts"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		sealer, err := newSealerFromEnv()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		consumerKey := os.Getenv("DISCOGS_CONSUMER_KEY")
		consumerSecret := os.Getenv("DISCOGS_CONSUMER_SECRET")
		callbackURL := os.Getenv("DISCOGS_OAUTH_CALLBACK_URL")
		if consumerKey == "" || consumerSecret == "" || callbackURL == "" {
			writeJSONError(w, http.StatusInternalServerError, errors.New("missing DISCOGS_CONSUMER_KEY, DISCOGS_CONSUMER_SECRET, or DISCOGS_OAUTH_CALLBACK_URL"))
			return
		}

		c := discogs.NewOAuthClient(consumerKey, consumerSecret, "", "").WithUserAgent(discogsUserAgent())
		reqToken, reqSecret, err := c.RequestToken(r.Context(), callbackURL)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Errorf("discogs request token: %w", err))
			return
		}

		payloadBytes, _ := json.Marshal(tmpPayload{
			Token:  reqToken,
			Secret: reqSecret,
			TS:     time.Now().Unix(),
		})
		enc, err := sealer.sealToString(payloadBytes)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		setCookie(w, &http.Cookie{
			Name:     discogsTmpCookieName,
			Value:    enc,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   cookieSecure(r),
			// OAuth temp cookie, 10 minutes.
			MaxAge: int((10 * time.Minute).Seconds()),
		})

		u, _ := url.Parse(discogsAuthorizeURL)
		q := u.Query()
		q.Set("oauth_token", reqToken)
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
}

func (a *App) handleDiscogsOAuthCallback() http.HandlerFunc {
	type tmpPayload struct {
		Token  string `json:"t"`
		Secret string `json:"s"`
		TS     int64  `json:"ts"`
	}

	type identityResponse struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if a.db == nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("DATABASE_URL not configured; cannot persist auth"))
			return
		}

		sealer, err := newSealerFromEnv()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		consumerKey := os.Getenv("DISCOGS_CONSUMER_KEY")
		consumerSecret := os.Getenv("DISCOGS_CONSUMER_SECRET")
		if consumerKey == "" || consumerSecret == "" {
			writeJSONError(w, http.StatusInternalServerError, errors.New("missing DISCOGS_CONSUMER_KEY or DISCOGS_CONSUMER_SECRET"))
			return
		}

		oauthToken := r.URL.Query().Get("oauth_token")
		verifier := r.URL.Query().Get("oauth_verifier")
		if oauthToken == "" || verifier == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("missing oauth_token or oauth_verifier"))
			return
		}

		tmpCookie, err := r.Cookie(discogsTmpCookieName)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("missing temporary oauth cookie; restart login"))
			return
		}

		tmpBytes, err := sealer.openFromString(tmpCookie.Value)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("invalid temporary oauth cookie; restart login"))
			return
		}

		var tmp tmpPayload
		if err := json.Unmarshal(tmpBytes, &tmp); err != nil {
			writeJSONError(w, http.StatusBadRequest, errors.New("corrupt temporary oauth cookie; restart login"))
			return
		}
		if tmp.Token != oauthToken {
			writeJSONError(w, http.StatusBadRequest, errors.New("oauth_token mismatch; restart login"))
			return
		}
		if tmp.TS != 0 && time.Since(time.Unix(tmp.TS, 0)) > 10*time.Minute {
			writeJSONError(w, http.StatusBadRequest, errors.New("oauth login expired; restart login"))
			return
		}

		c := discogs.NewOAuthClient(consumerKey, consumerSecret, "", "").WithUserAgent(discogsUserAgent())
		accessToken, accessSecret, err := c.AccessToken(r.Context(), oauthToken, tmp.Secret, verifier)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Errorf("discogs access token: %w", err))
			return
		}

		authed := discogs.NewOAuthClient(consumerKey, consumerSecret, accessToken, accessSecret).WithUserAgent(discogsUserAgent())
		ident, err := authed.Identity(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Errorf("discogs identity: %w", err))
			return
		}
		if ident.ID == 0 || ident.Username == "" {
			writeJSONError(w, http.StatusBadGateway, errors.New("discogs identity response missing id/username"))
			return
		}

		accessTokenEnc, err := sealer.sealToBytes([]byte(accessToken))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		accessSecretEnc, err := sealer.sealToBytes([]byte(accessSecret))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		userID, err := upsertDiscogsUserAndToken(r.Context(), a.db, ident.ID, ident.Username, accessTokenEnc, accessSecretEnc)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		// Clear temp cookie.
		setCookie(w, &http.Cookie{
			Name:     discogsTmpCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   cookieSecure(r),
			MaxAge:   -1,
		})

		// Set session cookie (encrypted + authenticated).
		sessionVal, err := sealer.sealToString([]byte(userID))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		setCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    sessionVal,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   cookieSecure(r),
			// 30 days.
			MaxAge: int((30 * 24 * time.Hour).Seconds()),
		})

		// Best-effort initial sync in the background so the DB isn't empty.
		go func(userID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = a.syncDiscogsCollection(ctx, userID)
		}(userID)

		http.Redirect(w, r, frontendRedirectURL(), http.StatusFound)
	}
}

func (a *App) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   cookieSecure(r),
			MaxAge:   -1,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

func upsertDiscogsUserAndToken(ctx context.Context, db interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, discogsUserID int64, discogsUsername string, accessTokenEnc, accessSecretEnc []byte) (string, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("db begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	if err := tx.QueryRow(ctx, `
insert into users (discogs_user_id, discogs_username)
values ($1, $2)
on conflict (discogs_user_id) do update
set discogs_username = excluded.discogs_username,
    updated_at = now()
returning id
`, discogsUserID, discogsUsername).Scan(&userID); err != nil {
		return "", fmt.Errorf("upsert user: %w", err)
	}

	_, err = tx.Exec(ctx, `
insert into oauth_tokens (user_id, provider, access_token_enc, access_secret_enc)
values ($1, 'discogs', $2, $3)
on conflict (user_id) do update
set access_token_enc = excluded.access_token_enc,
    access_secret_enc = excluded.access_secret_enc,
    updated_at = now()
`, userID, accessTokenEnc, accessSecretEnc)
	if err != nil {
		return "", fmt.Errorf("upsert oauth token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("db commit: %w", err)
	}
	return userID, nil
}

func discogsUserAgent() string {
	if ua := os.Getenv("DISCOGS_USER_AGENT"); ua != "" {
		return ua
	}
	// Discogs requires a User-Agent; keep it descriptive.
	return "vinyl-spin-tracker/0.1 (local)"
}

func frontendRedirectURL() string {
	if v := os.Getenv("FRONTEND_URL"); v != "" {
		return v
	}
	return "http://localhost:5173/"
}

// ---- Cookie + JSON helpers ----

func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return os.Getenv("COOKIE_SECURE") == "1"
}

func setCookie(w http.ResponseWriter, c *http.Cookie) {
	http.SetCookie(w, c)
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"error":%q}`, err.Error())))
}

// ---- AES-GCM sealer ----

type sealer struct {
	gcm cipher.AEAD
}

func newSealerFromEnv() (*sealer, error) {
	keyB64 := os.Getenv("APP_ENC_KEY")
	if keyB64 == "" {
		return nil, errors.New("missing APP_ENC_KEY")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, errors.New("APP_ENC_KEY must be base64")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("APP_ENC_KEY must decode to 32 bytes (got %d)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{gcm: gcm}, nil
}

func (s *sealer) sealToBytes(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := s.gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func (s *sealer) openFromBytes(in []byte) ([]byte, error) {
	ns := s.gcm.NonceSize()
	if len(in) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce := in[:ns]
	ct := in[ns:]
	pt, err := s.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errors.New("invalid ciphertext")
	}
	return pt, nil
}

func (s *sealer) sealToString(plaintext []byte) (string, error) {
	b, err := s.sealToBytes(plaintext)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *sealer) openFromString(v string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil, errors.New("invalid base64")
	}
	return s.openFromBytes(b)
}

