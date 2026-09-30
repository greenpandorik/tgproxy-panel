package keys

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

var ErrRevoked = errors.New("key is revoked")

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// ValidSlug reports whether s can be a short subscription address.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

func (s *Service) issueSubscriptionTx(ctx context.Context, q *db.Queries, keyID uuid.UUID) (string, error) {
	token, err := crypto.NewToken(32)
	if err != nil {
		return "", err
	}
	enc, err := s.box.EncryptString(token)
	if err != nil {
		return "", err
	}
	if err := q.RevokeSubscriptionTokensForKey(ctx, keyID); err != nil {
		return "", err
	}
	_, err = q.CreateSubscriptionToken(ctx, db.CreateSubscriptionTokenParams{TokenHash: crypto.HashToken(token), AccessKeyID: keyID, TokenEnc: enc})
	return token, err
}

// IssueSubscription gives the key a new subscription link; the previous one stops opening.
func (s *Service) IssueSubscription(ctx context.Context, keyID uuid.UUID) (string, error) {
	var token string
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		k, err := q.GetKey(ctx, keyID)
		if err != nil {
			return ErrNotFound
		}
		if k.Status == db.KeyStatusRevoked {
			return ErrRevoked
		}
		token, err = s.issueSubscriptionTx(ctx, q, keyID)
		return err
	})
	return token, err
}

// SubscriptionToken returns the plain token of a stored link, or false for links issued
// before tokens were kept encrypted.
func (s *Service) SubscriptionToken(sub db.SubscriptionToken) (string, bool) {
	if len(sub.TokenEnc) == 0 {
		return "", false
	}
	token, err := s.box.DecryptString(sub.TokenEnc)
	if err != nil {
		return "", false
	}
	return token, true
}

// SetDisabled turns a key off on every server without deleting it, or back on.
func (s *Service) SetDisabled(ctx context.Context, keyID uuid.UUID, disabled bool) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		k, err := q.GetKey(ctx, keyID)
		if err != nil {
			return ErrNotFound
		}
		if k.Status == db.KeyStatusRevoked {
			return ValidationError{"status": "key is revoked"}
		}
		if (k.DisabledAt != nil) == disabled {
			return nil
		}
		var at *time.Time
		if disabled {
			now := time.Now()
			at = &now
		}
		if err := q.SetKeyDisabled(ctx, db.SetKeyDisabledParams{ID: keyID, DisabledAt: at}); err != nil {
			return err
		}
		return s.dirtyKeyNodes(ctx, q, keyID)
	})
}

// Expire takes a key whose time is up off its servers. It stays editable, and a later
// expiry date brings it back.
func (s *Service) Expire(ctx context.Context, keyID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		if err := q.SetKeyExpired(ctx, keyID); err != nil {
			return err
		}
		return s.dirtyKeyNodes(ctx, q, keyID)
	})
}

// SetSlug sets or clears the short subscription address of a shared key.
func (s *Service) SetSlug(ctx context.Context, keyID uuid.UUID, slug string) error {
	if slug != "" && !ValidSlug(slug) {
		return ValidationError{"sub_slug": "3-32 characters: a-z, 0-9 and dashes, not at the ends"}
	}
	k, err := s.st.Q.GetKey(ctx, keyID)
	if err != nil {
		return ErrNotFound
	}
	if slug != "" && k.Type != db.KeyTypeSHARED {
		return ValidationError{"sub_slug": "only shared keys have a short address"}
	}
	var v *string
	if slug != "" {
		v = &slug
	}
	err = s.st.Q.SetKeySlug(ctx, db.SetKeySlugParams{ID: keyID, SubSlug: v})
	if isUnique(err) {
		return ValidationError{"sub_slug": "taken"}
	}
	return err
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
