package pinterest

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util/parser/m3u8"

	"github.com/bytedance/sonic"
)

var headers = map[string]string{
	// fix 403 error
	"X-Pinterest-Pws-Handler": "www/[username].js",
}

func ParseVideoObject(ctx *models.ExtractorContext, videoObj *Videos) ([]*models.MediaFormat, error) {
	var formats []*models.MediaFormat

	for key, video := range videoObj.VideoList {
		if !strings.Contains(key, "HLS") {
			formats = append(formats, &models.MediaFormat{
				FormatID:     key,
				URL:          []string{video.URL},
				Type:         database.MediaTypeVideo,
				VideoCodec:   database.MediaCodecAvc,
				AudioCodec:   database.MediaCodecAac,
				Width:        video.Width,
				Height:       video.Height,
				Duration:     video.Duration / 1000,
				ThumbnailURL: []string{video.Thumbnail},
			})
		} else {
			hlsFormats, err := m3u8.ParseM3U8FromURL(ctx, video.URL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to extract hls formats: %w", err)
			}

			formats = make([]*models.MediaFormat, 0, len(hlsFormats))
			for _, hlsFormat := range hlsFormats {
				hlsFormat.Duration = video.Duration / 1000
				hlsFormat.ThumbnailURL = []string{video.Thumbnail}
				formats = append(formats, hlsFormat)
			}
		}
	}

	return formats, nil
}

func BuildPinRequestParams(pinID string) string {
	options := map[string]any{
		"options": map[string]any{
			"field_set_key": "unauth_react_main_pin",
			"id":            pinID,
		},
	}
	jsonData, _ := sonic.ConfigFastest.Marshal(options)
	params := map[string]string{
		"data": string(jsonData),
	}
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return values.Encode()
}

func BuildSearchRequestParams(query string) string {
	options := map[string]any{
		"options": map[string]any{
			"query":         query,
			"scope":         "pins",
			"page_size":     1,
			"field_set_key": "react_grid_pin",
		},
	}
	jsonData, _ := sonic.ConfigFastest.Marshal(options)
	values := url.Values{}
	values.Set("data", string(jsonData))
	return values.Encode()
}

func GetFirstPinIDFromIdeas(ctx *models.ExtractorContext, keyword string) (string, error) {
	resp, err := ctx.Fetch(
		http.MethodGet,
		searchResourceEndpoint+"?"+BuildSearchRequestParams(keyword),
		&networking.RequestParams{
			Headers: headers,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to send search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad search response: %s", resp.Status)
	}

	var searchResponse SearchResponse
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	if err := decoder.Decode(&searchResponse); err != nil {
		return "", fmt.Errorf("failed to parse search response: %w", err)
	}

	pins := searchResponse.ResourceResponse.Data
	if len(pins) == 0 {
		return "", fmt.Errorf("no pins found for keyword: %s", keyword)
	}
	return pins[0].ID, nil
}
