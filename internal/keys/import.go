package keys

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"tgwebproxy/internal/store/db"
)

// MaxImport is how many users one import may carry.
const MaxImport = 500

var secretRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ImportItem is one person carried over from another proxy, with the secret their links already use.
type ImportItem struct {
	Label      string
	OwnerLabel string
	Secret     string
}

// NormalizeSecret lowercases a 32-hex secret and reports whether it is one.
func NormalizeSecret(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	return s, secretRe.MatchString(s)
}

// Import creates every item with its own secret and the shared settings of in, or none of them.
func (s *Service) Import(ctx context.Context, in CreateInput, items []ImportItem) ([]db.AccessKey, error) {
	if len(items) == 0 || len(items) > MaxImport {
		return nil, ValidationError{"items": fmt.Sprintf("1..%d users", MaxImport)}
	}
	in.SubSlug = ""
	ve := ValidationError{}
	seen := map[string]int{}
	for i, it := range items {
		secret, ok := NormalizeSecret(it.Secret)
		switch {
		case strings.TrimSpace(it.Label) == "":
			ve[fmt.Sprintf("items.%d", i)] = "name required"
		case !ok:
			ve[fmt.Sprintf("items.%d", i)] = "secret must be 32 hex characters"
		default:
			if j, dup := seen[secret]; dup {
				ve[fmt.Sprintf("items.%d", i)] = fmt.Sprintf("same secret as line %d", j+1)
			}
			seen[secret] = i
		}
		items[i].Secret = secret
	}
	if len(ve) > 0 {
		return nil, ve
	}
	probe := in
	probe.Label = items[0].Label
	if err := probe.validate(true); err != nil {
		return nil, err
	}

	var out []db.AccessKey
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if err := q.LockSecretImport(ctx); err != nil {
			return err
		}
		used, err := q.ListSecretsInUse(ctx)
		if err != nil {
			return err
		}
		for _, enc := range used {
			plain, err := s.box.DecryptString(enc)
			if err != nil {
				continue
			}
			if i, taken := seen[plain]; taken {
				ve[fmt.Sprintf("items.%d", i)] = "secret already belongs to another user"
			}
		}
		if len(ve) > 0 {
			return ve
		}
		if err := s.checkCapacity(ctx, q, in.NodeIDs, len(items)); err != nil {
			return err
		}
		out = make([]db.AccessKey, 0, len(items))
		for _, it := range items {
			item := in
			item.Label, item.OwnerLabel, item.Secret = strings.TrimSpace(it.Label), strings.TrimSpace(it.OwnerLabel), it.Secret
			k, err := s.createTx(ctx, q, item)
			if err != nil {
				return err
			}
			out = append(out, k)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
