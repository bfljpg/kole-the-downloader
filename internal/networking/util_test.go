package networking

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCookieMatchesHost(t *testing.T) {
	tests := []struct {
		domain, host string
		want         bool
	}{
		{".instagram.com", "www.instagram.com", true},
		{".instagram.com", "instagram.com", true},
		{"instagram.com", "i.instagram.com", true},
		{".Instagram.com", "WWW.instagram.com", true},
		{".instagram.com", "api-wh.igram.world", false},
		{".instagram.com", "scontent.cdninstagram.com", false},
		{".instagram.com", "notinstagram.com", false},
		{".instagram.com", "instagram.com.evil.com", false},
		{"", "anything.example", true},
	}
	for _, tt := range tests {
		got := cookieMatchesHost(&http.Cookie{Domain: tt.domain}, tt.host)
		if got != tt.want {
			t.Errorf("domain %q host %q: got %v, want %v", tt.domain, tt.host, got, tt.want)
		}
	}
}

func TestFetchClientCookieScope(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
	}))
	defer srv.Close()

	tests := []struct {
		name   string
		cookie *http.Cookie
		want   string
	}{
		{"other domain is not sent", &http.Cookie{Name: "sessionid", Value: "secret", Domain: ".instagram.com"}, ""},
		{"no domain is sent", &http.Cookie{Name: "sid", Value: "v"}, "sid=v"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got = ""
			client := DefaultHTTPClient(&NewHTTPClientOptions{Cookies: []*http.Cookie{tt.cookie}})
			resp, err := client.FetchWithContext(context.Background(), http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if got != tt.want {
				t.Fatalf("Cookie header = %q, want %q", got, tt.want)
			}
		})
	}
}
