package httpx

import (
	"context"
	"net"
	"net/http"
	"strings"
)

const clientIPKey ctxKey = iota + 100

// ClientIP resolves the caller's IP without trusting spoofable headers: with
// trustedHops = 0 it uses the TCP peer; with N > 0 (N proxies/load balancers
// in front of us that each append to X-Forwarded-For) it takes the Nth entry
// from the right, which only our own infrastructure could have written.
func ClientIP(trustedHops int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peerIP(r.RemoteAddr)
			if trustedHops > 0 {
				var hops []string
				for _, h := range r.Header.Values("X-Forwarded-For") {
					for p := range strings.SplitSeq(h, ",") {
						if p = strings.TrimSpace(p); p != "" {
							hops = append(hops, p)
						}
					}
				}
				if i := len(hops) - trustedHops; i >= 0 && i < len(hops) {
					if parsed := net.ParseIP(hops[i]); parsed != nil {
						ip = parsed.String()
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}

func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// ClientIPFrom returns the IP resolved by ClientIP (falls back to the peer).
func ClientIPFrom(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey).(string); ok && ip != "" {
		return ip
	}
	return peerIP(r.RemoteAddr)
}

// KeyByClientIP is an httprate key func using ClientIPFrom.
func KeyByClientIP(r *http.Request) (string, error) { return ClientIPFrom(r), nil }
