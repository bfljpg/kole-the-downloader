package instagram

import (
	"testing"

	"github.com/govdbot/govd/internal/database"
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
