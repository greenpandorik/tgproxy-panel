package sitekit

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// UpstreamMarkerPath is an internal panel-to-agent representation. It is never
// written into the public website directory.
const UpstreamMarkerPath = ".tgproxy-http-upstream"

var httpOriginHostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// ValidateHTTPUpstream keeps Telemt's decoy proxy away from the public network.
// The legacy default requires a private IP literal. Startup resolution allows
// hostnames; Telemt validates every DNS answer and pins them in its prepared config.
func ValidateHTTPUpstream(raw string, resolve ...string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Host == "" {
		return "", errors.New("origin must be an http:// URL with a private IP address")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("origin must not contain a path, query, credentials, or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	allowDNS := len(resolve) > 0 && resolve[0] == "startup"
	if ip == nil && allowDNS {
		if !httpOriginHostPattern.MatchString(u.Hostname()) || len(u.Hostname()) > 253 {
			return "", errors.New("origin hostname must be a valid ASCII hostname")
		}
	}
	if (ip == nil && !allowDNS) || (ip != nil && (ip.IsUnspecified() || (!ip.IsLoopback() && !ip.IsPrivate()))) {
		return "", errors.New("origin host must be a loopback or private IP literal")
	}
	if port := u.Port(); port != "" {
		parsed, err := net.LookupPort("tcp", port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", errors.New("origin port must be between 1 and 65535")
		}
	}
	u.Path = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

func UpstreamBundle(origin string, resolve ...string) (Bundle, error) {
	origin, err := ValidateHTTPUpstream(origin, resolve...)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Files: map[string][]byte{UpstreamMarkerPath: []byte(origin)}}, nil
}

func UpstreamFromBundle(bundle Bundle, resolve ...string) (string, bool, error) {
	raw, ok := bundle.Files[UpstreamMarkerPath]
	if !ok {
		return "", false, nil
	}
	if len(bundle.Files) != 1 {
		return "", true, errors.New("an HTTP upstream bundle cannot contain public files")
	}
	origin, err := ValidateHTTPUpstream(string(raw), resolve...)
	return origin, true, err
}
