package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	corsAllowedMethods               = "GET, POST, OPTIONS"
	corsAllowedHeaders               = "Content-Type, Idempotency-Key"
	privateNetworkRequestHeader      = "Access-Control-Request-Private-Network"
	privateNetworkAllowedHeaderValue = "true"
)

type originMatcher struct {
	allowAll      bool
	exact         map[string]struct{}
	wildcardPorts []originWildcardPort
}

type originWildcardPort struct {
	scheme string
	host   string
}

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	matcher := newOriginMatcher(allowedOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && matcher.allows(origin) {
			writeCORSHeaders(w, r, origin, matcher.allowAll)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeCORSHeaders(w http.ResponseWriter, r *http.Request, origin string, allowAll bool) {
	allowedOrigin := origin
	if allowAll {
		allowedOrigin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
	w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
	if strings.EqualFold(r.Header.Get(privateNetworkRequestHeader), privateNetworkAllowedHeaderValue) {
		w.Header().Set("Access-Control-Allow-Private-Network", privateNetworkAllowedHeaderValue)
	}
	w.Header().Add("Vary", "Origin")
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
	w.Header().Add("Vary", privateNetworkRequestHeader)
}

func newOriginMatcher(allowedOrigins []string) originMatcher {
	matcher := originMatcher{exact: make(map[string]struct{}, len(allowedOrigins))}
	for _, allowedOrigin := range allowedOrigins {
		allowedOrigin = strings.TrimSpace(allowedOrigin)
		if allowedOrigin == "*" {
			matcher.allowAll = true
			continue
		}
		if strings.HasSuffix(allowedOrigin, ":*") {
			pattern := strings.TrimSuffix(allowedOrigin, ":*")
			parsed, err := url.Parse(pattern)
			if err != nil {
				continue
			}
			matcher.wildcardPorts = append(matcher.wildcardPorts, originWildcardPort{
				scheme: strings.ToLower(parsed.Scheme),
				host:   strings.ToLower(parsed.Hostname()),
			})
			continue
		}
		matcher.exact[allowedOrigin] = struct{}{}
	}
	return matcher
}

func (m originMatcher) allows(origin string) bool {
	if m.allowAll {
		return true
	}
	if _, ok := m.exact[origin]; ok {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	for _, wildcard := range m.wildcardPorts {
		if strings.EqualFold(parsed.Scheme, wildcard.scheme) &&
			strings.EqualFold(parsed.Hostname(), wildcard.host) {
			return true
		}
	}
	return false
}
