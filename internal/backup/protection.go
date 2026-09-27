package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Protection struct {
	Recipient   string
	UploadURL   string
	UploadToken string
	Verify      bool
	Decrypt     func([]byte) ([]byte, error)
}
type ProtectionStatus struct {
	At        time.Time `json:"at"`
	File      string    `json:"file"`
	Encrypted bool      `json:"encrypted"`
	Uploaded  bool      `json:"uploaded"`
	Verified  bool      `json:"verified"`
	SHA256    string    `json:"sha256"`
	Error     string    `json:"error,omitempty"`
}

func (r *Runner) ProtectionStatus() ProtectionStatus {
	var s ProtectionStatus
	b, e := os.ReadFile(filepath.Join(r.Dir, "protection.json"))
	if e == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func (r *Runner) protect(ctx context.Context, e Entry) error {
	if r.Protection == nil {
		return nil
	}
	p := r.Protection
	s := ProtectionStatus{At: time.Now().UTC(), File: e.Name}
	defer func() {
		b, _ := json.Marshal(s)
		tmp, err := os.CreateTemp(r.Dir, ".protection-*")
		if err != nil {
			return
		}
		name := tmp.Name()
		defer func() { _ = os.Remove(name) }()
		if _, err = tmp.Write(b); err == nil {
			err = tmp.Sync()
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			_ = os.Rename(name, filepath.Join(r.Dir, "protection.json"))
		}
	}()
	fail := func(err error) error { s.Error = err.Error(); return err }
	if p.Verify {
		if err := r.Verify(ctx, e.Name); err != nil {
			return fail(err)
		}
		s.Verified = true
	}
	if p.Recipient == "" {
		if p.UploadURL != "" {
			return fail(errors.New("off-host upload requires encryption"))
		}
		return nil
	}
	recipient, err := age.ParseX25519Recipient(p.Recipient)
	if err != nil {
		return fail(errors.New("invalid backup age recipient"))
	}
	source, err := os.Open(e.Path)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = source.Close() }()
	encrypted, err := os.CreateTemp(r.Dir, ".encrypted-*")
	if err != nil {
		return fail(err)
	}
	temp := encrypted.Name()
	defer func() { _ = os.Remove(temp) }()
	writer, err := age.Encrypt(encrypted, recipient)
	if err != nil {
		_ = encrypted.Close()
		return fail(err)
	}
	_, err = io.Copy(writer, source)
	if ce := writer.Close(); err == nil {
		err = ce
	}
	if err == nil {
		err = encrypted.Sync()
	}
	if ce := encrypted.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return fail(err)
	}
	archive := e.Path + ".age"
	if err = os.Rename(temp, archive); err != nil {
		return fail(err)
	}
	s.Encrypted = true
	body, err := os.Open(archive)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = body.Close() }()
	sum := sha256.New()
	if _, err = io.Copy(sum, body); err != nil {
		return fail(err)
	}
	s.SHA256 = hex.EncodeToString(sum.Sum(nil))
	if _, err = body.Seek(0, 0); err != nil {
		return fail(err)
	}
	if p.UploadURL == "" {
		return nil
	}
	base, err := url.Parse(p.UploadURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" {
		return fail(errors.New("backup upload requires an HTTPS base URL without embedded credentials or query"))
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + filepath.Base(archive)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, base.String(), body)
	if err != nil {
		return fail(errors.New("invalid backup upload request"))
	}
	info, err := body.Stat()
	if err != nil {
		return fail(err)
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Content-SHA256", s.SHA256)
	if p.UploadToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.UploadToken)
	}
	client := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fail(errors.New("encrypted backup upload failed"))
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail(fmt.Errorf("backup upload returned HTTP %d", resp.StatusCode))
	}
	s.Uploaded = true
	return nil
}

// Verify restores into a freshly created database, never the configured live database.
func (r *Runner) Verify(ctx context.Context, name string) (resultErr error) {
	file, err := r.Path(name)
	if err != nil {
		return err
	}
	cfg, err := pgx.ParseConfig(r.DatabaseURL)
	if err != nil {
		return errors.New("invalid verification database configuration")
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return errors.New("cannot connect for restore verification")
	}
	defer func() { _ = admin.Close(context.Background()) }()
	dbName := "tgwp_restore_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ident := pgx.Identifier{dbName}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		return errors.New("cannot create isolated verification database; grant CREATEDB or run verification with an appropriate database role")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := admin.Exec(cleanup, "DROP DATABASE "+ident+" WITH (FORCE)"); e != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove isolated verification database %s manually: cleanup failed", dbName))
		}
	}()
	target := cfg.Copy()
	target.Database = dbName
	// pgx ConnString retains the original string, so override the database in the URL/DSN explicitly.
	dsn := r.DatabaseURL
	if u, e := url.Parse(dsn); e == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		u.Path = "/" + dbName
		q := u.Query()
		q.Del("dbname")
		q.Del("database")
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " dbname=" + dbName
	}
	out, err := r.run(ctx, "pg_restore", "--exit-on-error", "--no-owner", "--no-privileges", "--dbname", dsn, file)
	if err != nil {
		_ = out
		return errors.New("isolated pg_restore failed")
	}
	conn, err := pgx.ConnectConfig(ctx, target)
	if err != nil {
		return errors.New("cannot inspect restored database")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var count int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM nodes").Scan(&count); err != nil {
		return errors.New("restored schema validation failed")
	}
	if r.Protection != nil && r.Protection.Decrypt != nil {
		rows, e := conn.Query(ctx, "SELECT secret_enc FROM profiles UNION ALL SELECT secret_enc FROM access_keys UNION ALL SELECT totp_secret_enc FROM admin_users WHERE totp_secret_enc IS NOT NULL")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var encrypted []byte
			if e = rows.Scan(&encrypted); e != nil {
				return e
			}
			if _, e = r.Protection.Decrypt(encrypted); e != nil {
				return errors.New("restored secrets cannot be decrypted: retain every required master-key version")
			}
		}
		if e = rows.Err(); e != nil {
			return e
		}
	}
	return nil
}
