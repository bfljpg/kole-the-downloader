package instagram

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/util"
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

func TestParseGraphQLResponse(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantRejected bool
		wantErr      bool
	}{
		{"data", `{"data":{"xdt_api__v1__media__shortcode__web_info":{"items":[]}}}`, false, false},
		{"rejected with prefix", `for (;;);{"error":1357004,"errorSummary":"Sorry, something went wrong"}`, true, true},
		{"graphql errors are not rejections", `{"data":null,"errors":[{"message":"field_exception"}]}`, false, false},
		{"not json", `<html>blocked</html>`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseGraphQLResponse([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if errors.Is(err, errGraphQLRejected) != tt.wantRejected {
				t.Fatalf("rejected = %v, want %v (err: %v)", errors.Is(err, errGraphQLRejected), tt.wantRejected, err)
			}
		})
	}
}

type slowReader struct {
	chunks [][]byte
	err    error
	reads  int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.reads >= len(r.chunks) {
		return 0, r.err
	}
	n := copy(p, r.chunks[r.reads])
	r.reads++
	return n, nil
}

func TestReadWebTokens(t *testing.T) {
	filler := bytes.Repeat([]byte("x"), 1000)
	tokensPart := []byte(`["LSD",[],{"token":"abc"}] "client_revision":123 "hsi":"456" "haste_session":"789"`)

	t.Run("stops once all tokens were seen", func(t *testing.T) {
		r := &slowReader{chunks: [][]byte{filler, tokensPart, filler, filler}, err: io.EOF}
		tokens, err := ReadWebTokens(r)
		if err != nil || tokens.LSD != "abc" || tokens.HSI != "456" {
			t.Fatalf("unexpected result: %+v, %v", tokens, err)
		}
		if r.reads != 2 {
			t.Fatalf("expected to stop after 2 reads, got %d", r.reads)
		}
	})
	t.Run("transport error is tagged", func(t *testing.T) {
		r := &slowReader{chunks: [][]byte{filler}, err: errors.New("deadline exceeded")}
		_, err := ReadWebTokens(r)
		if !errors.Is(err, errTransport) {
			t.Fatalf("expected a transport error, got %v", err)
		}
	})
	t.Run("page without tokens is not a transport error", func(t *testing.T) {
		r := &slowReader{chunks: [][]byte{filler}, err: io.EOF}
		_, err := ReadWebTokens(r)
		if err == nil || errors.Is(err, errTransport) {
			t.Fatalf("expected a parse error, got %v", err)
		}
	})
}

func TestShortcodeToMediaID(t *testing.T) {
	// pairs taken from real api responses
	tests := []struct {
		shortcode, want string
		wantErr         bool
	}{
		{"DdRSR0Et9Mb", "3986047534181634843", false},
		{"DeMYCMLiEm7", "4002679872459131323", false},
		{"Ddyz7R4N29Z", "3995484193449013081", false},
		{"B", "1", false},
		{"BA", "64", false},
		{"", "", true},
		{"has space", "", true},
		{"waytoolongshortcode", "", true},
	}
	for _, tt := range tests {
		got, err := ShortcodeToMediaID(tt.shortcode)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ShortcodeToMediaID(%q) = %q, %v; want %q (err %v)", tt.shortcode, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestParseMediaInfoResponse(t *testing.T) {
	item, err := ParseMediaInfoResponse([]byte(`{"status":"ok","items":[{"video_versions":[{"url":"https://cdn/v.mp4","width":720,"height":1280}]}]}`))
	if err != nil || len(item.VideoVersions) != 1 {
		t.Fatalf("unexpected result: %+v, %v", item, err)
	}
	if _, err := ParseMediaInfoResponse([]byte(`{"status":"ok","items":[]}`)); !errors.Is(err, util.ErrUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
	if _, err := ParseMediaInfoResponse([]byte(`{"status":"fail","message":"login_required"}`)); err == nil {
		t.Fatal("expected an error for a failed response")
	}
	if _, err := ParseMediaInfoResponse([]byte(`<html>`)); err == nil {
		t.Fatal("expected an error for a non-json body")
	}
}
