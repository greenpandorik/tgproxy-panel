// Package nodeinstall renders the one-shot node installer script.
package nodeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	"tgwebproxy/internal/domain"
)

//go:embed script.sh.tmpl
var scriptTmpl string

//go:embed fallback-site/*
var fallback embed.FS

type Params struct {
	PanelURL, InstallToken, Hostname, ACMEEmail, Secret, TProxyCommit, AgentSHA256 string
	Site                                                                           map[string][]byte

	Engine                                          domain.Engine
	WebUser, TLSDomain, TelemtVersion, TelemtSHA256 string
	ClassicPort                                     int

	PublicIP string

	NoSysctlTuning bool
}

// IsTelemt drives the engine branch in the template.
func (p Params) IsTelemt() bool { return p.Engine == domain.EngineTelemt }

// SysctlTuning drives the sysctl branch in the template; on unless NoSysctlTuning is set.
func (p Params) SysctlTuning() bool { return !p.NoSysctlTuning }

// DefaultClassicPort matches the nodes.classic_port column default.
const DefaultClassicPort = 8443

var (
	reTelemtVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reTelemtSHA256  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reWebUser       = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// sq renders v as a single-quoted shell word.
func sq(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

var tmpl = template.Must(template.New("install").Funcs(template.FuncMap{"sq": sq}).Parse(scriptTmpl))

func Render(p Params) (string, error) {
	if _, ok := p.Site["index.html"]; !ok {
		return "", errors.New("site must contain index.html")
	}
	if p.IsTelemt() {
		if !reTelemtVersion.MatchString(p.TelemtVersion) {
			return "", fmt.Errorf("telemt version %q is not a pinned release (set TELEMT_VERSION)", p.TelemtVersion)
		}
		if !reTelemtSHA256.MatchString(p.TelemtSHA256) {
			return "", errors.New("telemt release checksum missing or malformed (set TELEMT_SHA256_X86_64 to 64 lowercase hex characters)")
		}
		if !reWebUser.MatchString(p.WebUser) {
			return "", fmt.Errorf("telemt web user %q is not a valid user name", p.WebUser)
		}
		if p.TLSDomain == "" {
			p.TLSDomain = p.Hostname
		}
		if p.ClassicPort <= 0 || p.ClassicPort > 65535 {
			p.ClassicPort = DefaultClassicPort
		}
	}
	tarB64, err := tarGzBase64(p.Site)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	err = tmpl.Execute(&buf, struct {
		Params
		SiteTarB64 string
	}{p, tarB64})
	return buf.String(), err
}

func FallbackSite() map[string][]byte {
	out := map[string][]byte{}
	for _, name := range []string{"index.html", "styles.css"} {
		b, _ := fallback.ReadFile("fallback-site/" + name)
		out[name] = b
	}
	return out
}

func tarGzBase64(files map[string][]byte) (string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), ModTime: time.Unix(0, 0)}); err != nil {
			return "", err
		}
		if _, err := tw.Write(files[n]); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// TelemtReleaseAsset is the release asset a telemt node installs: the x86_64 glibc tarball.
const TelemtReleaseAsset = "telemt-x86_64-linux-gnu.tar.gz"

// TelemtReleaseURL is the download URL of the pinned telemt release.
func TelemtReleaseURL(version string) string {
	return "https://github.com/telemt/telemt/releases/download/" + version + "/" + TelemtReleaseAsset
}
