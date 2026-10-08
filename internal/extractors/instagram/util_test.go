package instagram

import (
	"testing"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
)

func TestGQLMediaFormat(t *testing.T) {
	tests := []struct {
		name     string
		node     *Media
		wantType database.MediaType
		wantNil  bool
	}{
		// embed pages omit __typename on sidecar children
		{"image without typename", &Media{DisplayURL: "https://cdn/i.jpg"}, database.MediaTypePhoto, false},
		{"video without typename", &Media{IsVideo: true, VideoURL: "https://cdn/v.mp4", DisplayURL: "https://cdn/t.jpg"}, database.MediaTypeVideo, false},
		{"typed video", &Media{Typename: "XDTGraphVideo", VideoURL: "https://cdn/v.mp4"}, database.MediaTypeVideo, false},
		{"video without url", &Media{IsVideo: true, DisplayURL: "https://cdn/t.jpg"}, "", true},
		{"empty node", &Media{}, "", true},
		{"nil node", nil, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gqlMediaFormat(tt.node)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil || got.Type != tt.wantType {
				t.Fatalf("expected %s format, got %+v", tt.wantType, got)
			}
		})
	}
}

func TestParseWebTokens(t *testing.T) {
	page := []byte(`..["LSD",[],{"token":"abc123"},323]..{"client_revision":1049680277}..` +
		`"hsi":"7694259793429087481"..."haste_session":"20734.HYP:instagram_web_pkg.2.1...0"`)
	tokens, err := ParseWebTokens(page)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokens.LSD != "abc123" || tokens.Rev != "1049680277" ||
		tokens.HSI != "7694259793429087481" || tokens.HasteSession == "" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
	if _, err := ParseWebTokens([]byte("<html>login</html>")); err == nil {
		t.Fatal("expected error for a page without tokens")
	}
}

func TestParseWebInfoMedia(t *testing.T) {
	const (
		photo = `{"caption":{"text":"hi"},"image_versions2":{"candidates":[{"url":"https://cdn/small.jpg","width":320},{"url":"https://cdn/big.jpg","width":800}]}}`
		video = `{"video_versions":[{"url":"https://cdn/v1.mp4","width":1276,"height":718,"type":101}],"image_versions2":{"candidates":[{"url":"https://cdn/t.jpg","width":720}]}}`
	)
	tests := []struct {
		name      string
		body      string
		wantTypes []database.MediaType
		wantErr   bool
	}{
		{"photo", photo, []database.MediaType{database.MediaTypePhoto}, false},
		{"reel", video, []database.MediaType{database.MediaTypeVideo}, false},
		{"carousel", `{"carousel_media":[` + photo + `,` + video + `]}`,
			[]database.MediaType{database.MediaTypePhoto, database.MediaTypeVideo}, false},
		{"carousel child without url", `{"carousel_media":[` + photo + `,{}]}`, nil, true},
		{"empty item", `{}`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var item WebInfoItem
			if err := sonic.ConfigFastest.UnmarshalFromString(tt.body, &item); err != nil {
				t.Fatal(err)
			}
			media, err := ParseWebInfoMedia(&models.ExtractorContext{Extractor: Extractor}, &item)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(media.Items) != len(tt.wantTypes) {
				t.Fatalf("expected %d items, got %d", len(tt.wantTypes), len(media.Items))
			}
			for i, want := range tt.wantTypes {
				formats := media.Items[i].Formats
				if len(formats) != 1 || formats[0].Type != want {
					t.Fatalf("item %d: expected one %s format, got %+v", i, want, formats)
				}
			}
			if tt.name == "photo" && media.Items[0].Formats[0].URL[0] != "https://cdn/big.jpg" {
				t.Fatalf("expected the widest image, got %v", media.Items[0].Formats[0].URL)
			}
		})
	}
}
