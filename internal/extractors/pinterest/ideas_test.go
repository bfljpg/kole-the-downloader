package pinterest

import (
	"net/url"
	"testing"

	"github.com/bytedance/sonic"
)

func TestIdeasURLPattern(t *testing.T) {
	tests := []struct {
		url      string
		ideas    bool
		wantID   string
		otherHit bool // matched by the regular pin extractor
	}{
		{"https://www.pinterest.com/ideas/home-decor/935749051732/", true, "home-decor", false},
		{"https://tr.pinterest.com/ideas/kedi/123456", true, "kedi", false},
		{"https://www.pinterest.com/pin/1234567890/", false, "", true},
	}
	for _, tt := range tests {
		m := IdeasExtractor.URLPattern.FindStringSubmatch(tt.url)
		if (m != nil) != tt.ideas {
			t.Errorf("%s: ideas match = %v, want %v", tt.url, m != nil, tt.ideas)
		}
		if m != nil && m[IdeasExtractor.URLPattern.SubexpIndex("id")] != tt.wantID {
			t.Errorf("%s: id = %q, want %q", tt.url, m[IdeasExtractor.URLPattern.SubexpIndex("id")], tt.wantID)
		}
		if got := Extractor.URLPattern.MatchString(tt.url); got != tt.otherHit {
			t.Errorf("%s: pin extractor match = %v, want %v", tt.url, got, tt.otherHit)
		}
	}
}

func TestBuildSearchRequestParams(t *testing.T) {
	values, err := url.ParseQuery(BuildSearchRequestParams("home decor"))
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Options struct {
			Query    string `json:"query"`
			Scope    string `json:"scope"`
			PageSize int    `json:"page_size"`
		} `json:"options"`
	}
	if err := sonic.ConfigFastest.UnmarshalFromString(values.Get("data"), &data); err != nil {
		t.Fatal(err)
	}
	if data.Options.Query != "home decor" || data.Options.Scope != "pins" || data.Options.PageSize != 1 {
		t.Fatalf("unexpected options: %+v", data.Options)
	}
}
