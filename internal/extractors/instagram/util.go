package instagram

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/govdbot/govd/internal/database"
	"github.com/govdbot/govd/internal/logger"
	"github.com/govdbot/govd/internal/models"
	"github.com/govdbot/govd/internal/networking"
	"github.com/govdbot/govd/internal/util"

	"github.com/bytedance/sonic"
	"github.com/titanous/json5"
)

const (
	postPageURL     = "https://www.instagram.com/p/%s/"
	graphQLEndpoint = "https://www.instagram.com/graphql/query"
	webInfoQuery    = "PolarisPostRootQuery"
	// doc_id of webInfoQuery, taken from the web client bundle.
	// it goes stale when instagram deploys a new web client.
	webInfoDocID = "29131271483123635"
	webAppID     = "936619743392459"
	webUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	igramHostname = "api-wh.igram.world"
	igramAPIBase  = "api.igram.world"
	igramHMACKey  = "75f2d70d3724f98e4a7d1ffd0ba9cfd907f3ae2632ee159980e2c521bff62358"
	igramStaticTS = 1771418815381 // parseInt("mls10xp1", 36)
)

var (
	embedPattern = regexp.MustCompile(
		`new ServerJS\(\)\);s\.handle\(({.*})\);requireLazy`)

	lsdPattern          = regexp.MustCompile(`\["LSD",\[\],\{"token":"([^"]+)"`)
	revPattern          = regexp.MustCompile(`"client_revision":(\d+)`)
	hsiPattern          = regexp.MustCompile(`"hsi":"(\d+)"`)
	hasteSessionPattern = regexp.MustCompile(`"haste_session":"([^"]+)"`)

	pageHeaders = map[string]string{
		"User-Agent":      webUserAgent,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
	}

	webHeaders = map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Language":           "en-GB,en;q=0.9",
		"Cache-Control":             "max-age=0",
		"Dnt":                       "1",
		"Priority":                  "u=0, i",
		"Sec-Ch-Ua":                 `Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99`,
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        "macOS",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}

	igramHeaders = map[string]string{
		"Referer": "https://igram.world/",
	}
)

func ParseGQLMedia(ctx *models.ExtractorContext, data *Media) (*models.Media, error) {
	var caption string
	if data.EdgeMediaToCaption != nil && len(data.EdgeMediaToCaption.Edges) > 0 {
		caption = data.EdgeMediaToCaption.Edges[0].Node.Text
	}

	media := ctx.NewMedia()
	media.SetCaption(caption)

	if data.EdgeSidecarToChildren != nil && len(data.EdgeSidecarToChildren.Edges) > 0 {
		for i, edge := range data.EdgeSidecarToChildren.Edges {
			format := gqlMediaFormat(edge.Node)
			if format == nil {
				return nil, fmt.Errorf("no media url found for sidecar item at index %d", i)
			}
			media.NewItem().AddFormats(format)
		}
		return media, nil
	}

	format := gqlMediaFormat(data)
	if format == nil {
		return nil, fmt.Errorf("no media url found")
	}
	media.NewItem().AddFormats(format)
	return media, nil
}

// gqlMediaFormat builds a format from a single GQL media node, or
// returns nil if the node has no usable url. embed pages omit
// __typename on sidecar children, so rely on the data itself.
func gqlMediaFormat(node *Media) *models.MediaFormat {
	if node == nil {
		return nil
	}
	var width, height int32
	if node.Dimensions != nil {
		width, height = node.Dimensions.Width, node.Dimensions.Height
	}
	isVideo := node.IsVideo || node.VideoURL != "" ||
		node.Typename == "GraphVideo" || node.Typename == "XDTGraphVideo"
	if isVideo {
		if node.VideoURL == "" {
			return nil
		}
		return &models.MediaFormat{
			FormatID:     "video",
			Type:         database.MediaTypeVideo,
			VideoCodec:   database.MediaCodecAvc,
			AudioCodec:   database.MediaCodecAac,
			URL:          []string{node.VideoURL},
			ThumbnailURL: []string{node.DisplayURL},
			Width:        width,
			Height:       height,
		}
	}
	if node.DisplayURL == "" {
		return nil
	}
	return &models.MediaFormat{
		FormatID: "image",
		Type:     database.MediaTypePhoto,
		URL:      []string{node.DisplayURL},
	}
}

// ParseWebInfoMedia builds media from a PolarisPostRootQuery item.
func ParseWebInfoMedia(ctx *models.ExtractorContext, item *WebInfoItem) (*models.Media, error) {
	media := ctx.NewMedia()
	if item.Caption != nil {
		media.SetCaption(item.Caption.Text)
	}

	if len(item.CarouselMedia) > 0 {
		for i, child := range item.CarouselMedia {
			format := webInfoFormat(child)
			if format == nil {
				return nil, fmt.Errorf("no media url found for sidecar item at index %d", i)
			}
			media.NewItem().AddFormats(format)
		}
		return media, nil
	}

	format := webInfoFormat(item)
	if format == nil {
		return nil, fmt.Errorf("no media url found")
	}
	media.NewItem().AddFormats(format)
	return media, nil
}

func webInfoFormat(item *WebInfoItem) *models.MediaFormat {
	var thumbnail *Candidates
	if item.ImageVersions != nil {
		thumbnail = GetBestCandidate(item.ImageVersions.Candidates)
	}
	if video := GetBestVideoVersion(item.VideoVersions); video != nil && video.URL != "" {
		format := &models.MediaFormat{
			FormatID:   "video",
			Type:       database.MediaTypeVideo,
			VideoCodec: database.MediaCodecAvc,
			AudioCodec: database.MediaCodecAac,
			URL:        []string{video.URL},
			Width:      int32(video.Width),
			Height:     int32(video.Height),
		}
		if thumbnail != nil && thumbnail.URL != "" {
			format.ThumbnailURL = []string{thumbnail.URL}
		}
		return format
	}
	if thumbnail != nil && thumbnail.URL != "" {
		return &models.MediaFormat{
			FormatID: "image",
			Type:     database.MediaTypePhoto,
			URL:      []string{thumbnail.URL},
		}
	}
	return nil
}

func ParseEmbedGQL(body []byte) (*Media, error) {
	match := embedPattern.FindSubmatch(body)
	if len(match) < 2 {
		return nil, fmt.Errorf("gql json not found")
	}
	jsonData := match[1]

	var data map[string]any
	if err := json5.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	igCtx := util.TraverseJSON(data, "contextJSON")
	if igCtx == nil {
		return nil, fmt.Errorf("contextJSON not found")
	}
	var ctxJSON ContextJSON
	switch v := igCtx.(type) {
	case string:
		if err := json5.Unmarshal([]byte(v), &ctxJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal contextJSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unexpected type for contextJSON: %T", v)
	}
	if ctxJSON.GqlData == nil {
		return nil, fmt.Errorf("gql_data not found")
	}
	if ctxJSON.GqlData.ShortcodeMedia == nil {
		return nil, fmt.Errorf("shortcode_media not found")
	}
	return ctxJSON.GqlData.ShortcodeMedia, nil
}

func IGramBodyFromURL(contentURL string) (io.Reader, error) {
	return igramBuildPayload(map[string]string{
		"target_url": contentURL,
	})
}

func IGramBodyFromParams(params map[string]string) (io.Reader, error) {
	return igramBuildPayload(params)
}

func igramBuildPayload(urlParams map[string]string) (io.Reader, error) {
	nowMs := time.Now().UnixMilli()
	serverMs := getIGramServerTime()

	drift := serverMs - nowMs
	var correction int64
	if drift >= 60000 || drift <= -60000 {
		correction = drift
	}
	ts := nowMs + correction

	// partial payload fields that get signed
	partial := map[string]any{
		"_sc": 0,
		"_ef": 0,
		"_df": 0,
	}
	for k, v := range urlParams {
		partial[k] = v
	}

	sig, err := igramSign(partial, ts)
	if err != nil {
		return nil, err
	}

	// assemble final payload
	final := make(map[string]any, len(partial)+5)
	for k, v := range partial {
		final[k] = v
	}
	final["ts"] = ts
	final["_ts"] = igramStaticTS
	final["_tsc"] = correction
	final["_sv"] = 2
	final["_s"] = sig

	jsonBytes, err := sonic.ConfigFastest.Marshal(final)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	return strings.NewReader(string(jsonBytes)), nil
}

func igramSign(partial map[string]any, ts int64) (string, error) {
	// sonic.ConfigStd sorts map keys alphabetically, matching
	// the signing: JSON.stringify(sorted_partial) + String(ts)
	jsonBytes, err := sonic.ConfigStd.Marshal(partial)
	if err != nil {
		return "", fmt.Errorf("failed to marshal partial payload: %w", err)
	}

	data := string(jsonBytes) + strconv.FormatInt(ts, 10)

	keyBytes, err := hex.DecodeString(igramHMACKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode HMAC key: %w", err)
	}

	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func getIGramServerTime() int64 {
	apiURL := fmt.Sprintf("https://%s/msec", igramAPIBase)
	resp, err := http.Get(apiURL)
	if err != nil {
		return time.Now().UnixMilli()
	}
	defer resp.Body.Close()

	var result struct {
		Msec float64 `json:"msec"`
	}
	decoder := sonic.ConfigFastest.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return time.Now().UnixMilli()
	}
	return int64(result.Msec * 1000)
}

func ParseIGramResponse(body []byte) (*IGramResponse, error) {
	// try to unmarshal as a single IGramMedia and then as a slice
	var media IGramMedia

	if err := sonic.ConfigFastest.Unmarshal(body, &media); err != nil {
		// try with slice
		var mediaList []*IGramMedia
		if err := sonic.ConfigFastest.Unmarshal(body, &mediaList); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		return &IGramResponse{
			Items: mediaList,
		}, nil
	}
	if media.Success != nil && !(*media.Success) {
		return nil, util.ErrUnavailable
	}
	return &IGramResponse{
		Items: []*IGramMedia{&media},
	}, nil
}

func GetCDNURL(contentURL string) (string, error) {
	parsedURL, err := url.Parse(contentURL)
	if err != nil {
		return "", fmt.Errorf("can't parse igram URL: %w", err)
	}
	queryParams, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		return "", fmt.Errorf("can't unescape igram URL: %w", err)
	}
	cdnURL := queryParams.Get("uri")
	return cdnURL, nil
}

// webTokens are the values the web client reads from the post page
// before sending a query. a logged out visitor gets them too.
type webTokens struct {
	LSD, Rev, HSI, HasteSession string
}

func ParseWebTokens(body []byte) (*webTokens, error) {
	lsd := lsdPattern.FindSubmatch(body)
	rev := revPattern.FindSubmatch(body)
	if lsd == nil || rev == nil {
		return nil, fmt.Errorf("lsd or client revision not found")
	}
	tokens := &webTokens{LSD: string(lsd[1]), Rev: string(rev[1])}
	if m := hsiPattern.FindSubmatch(body); m != nil {
		tokens.HSI = string(m[1])
	}
	if m := hasteSessionPattern.FindSubmatch(body); m != nil {
		tokens.HasteSession = string(m[1])
	}
	return tokens, nil
}

// errGraphQLRejected means instagram answered with an error payload
// instead of data, which can be transient.
var errGraphQLRejected = errors.New("graphql request rejected")

func GetGQLData(ctx *models.ExtractorContext) (*WebInfoItem, error) {
	var (
		item *WebInfoItem
		err  error
	)
	for range 2 {
		item, err = fetchWebInfo(ctx)
		if !errors.Is(err, errGraphQLRejected) {
			return item, err
		}
		ctx.Debugf("graphql request rejected, retrying: %v", err)
	}
	return nil, err
}

// ParseGraphQLResponse decodes a graphql body, which instagram
// prefixes with "for (;;);" on rejected requests.
func ParseGraphQLResponse(body []byte) (*GraphQLResponse, error) {
	trimmed := bytes.TrimPrefix(bytes.TrimSpace(body), []byte("for (;;);"))
	var response GraphQLResponse
	if err := sonic.ConfigFastest.Unmarshal(trimmed, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %.200q)", err, body)
	}
	if response.ErrorCode != 0 {
		return nil, fmt.Errorf("%w: %d %s", errGraphQLRejected, response.ErrorCode, response.ErrorSummary)
	}
	return &response, nil
}

func fetchWebInfo(ctx *models.ExtractorContext) (*WebInfoItem, error) {
	pageResp, err := ctx.Fetch(
		http.MethodGet,
		fmt.Sprintf(postPageURL, ctx.ContentID),
		&networking.RequestParams{Headers: pageHeaders},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch post page: %w", err)
	}
	defer pageResp.Body.Close()

	if pageResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch post page: %s", pageResp.Status)
	}
	page, err := io.ReadAll(pageResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read post page: %w", err)
	}
	tokens, err := ParseWebTokens(page)
	if err != nil {
		return nil, fmt.Errorf("failed to parse post page: %w", err)
	}
	cookies := pageResp.Cookies()
	var csrfToken string
	for _, cookie := range cookies {
		if cookie.Name == "csrftoken" {
			csrfToken = cookie.Value
		}
	}

	variables, err := sonic.ConfigFastest.Marshal(map[string]any{
		"shortcode":               ctx.ContentID,
		"fetch_tagged_user_count": nil,
		"hoisted_comment_id":      nil,
		"hoisted_reply_id":        nil,
		"__relay_internal__pv__PolarisShortDramaEnabledrelayprovider":           false,
		"__relay_internal__pv__PolarisMultiCaptionCarouselEnabledrelayprovider": false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal variables: %w", err)
	}
	form := url.Values{
		"av":                       {"0"},
		"__d":                      {"www"},
		"__user":                   {"0"},
		"__a":                      {"1"},
		"__req":                    {"1"},
		"__hs":                     {tokens.HasteSession},
		"__hsi":                    {tokens.HSI},
		"__rev":                    {tokens.Rev},
		"__spin_r":                 {tokens.Rev},
		"__spin_b":                 {"trunk"},
		"__ccg":                    {"EXCELLENT"},
		"__comet_req":              {"7"},
		"dpr":                      {"1"},
		"lsd":                      {tokens.LSD},
		"jazoest":                  {"2999"},
		"fb_api_caller_class":      {"RelayModern"},
		"fb_api_req_friendly_name": {webInfoQuery},
		"variables":                {string(variables)},
		"server_timestamps":        {"true"},
		"doc_id":                   {webInfoDocID},
	}
	resp, err := ctx.Fetch(
		http.MethodPost,
		graphQLEndpoint,
		&networking.RequestParams{
			Headers: map[string]string{
				"User-Agent":         webUserAgent,
				"Accept-Language":    "en-US,en;q=0.9",
				"Content-Type":       "application/x-www-form-urlencoded",
				"Origin":             "https://www.instagram.com",
				"Referer":            fmt.Sprintf(postPageURL, ctx.ContentID),
				"Sec-Fetch-Site":     "same-origin",
				"X-FB-LSD":           tokens.LSD,
				"X-CSRFToken":        csrfToken,
				"X-IG-App-ID":        webAppID,
				"X-ASBD-ID":          "359341",
				"X-FB-Friendly-Name": webInfoQuery,
			},
			Cookies: cookies,
			Body:    strings.NewReader(form.Encode()),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	logger.WriteFile("iggql_api_response", resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid response code: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	response, err := ParseGraphQLResponse(body)
	if err != nil {
		return nil, err
	}
	if response.Data == nil || response.Data.WebInfo == nil {
		if len(response.Errors) > 0 {
			return nil, fmt.Errorf("graphql error: %s", response.Errors[0].Message)
		}
		return nil, fmt.Errorf("data is nil")
	}
	if len(response.Data.WebInfo.Items) == 0 {
		return nil, util.ErrUnavailable
	}
	return response.Data.WebInfo.Items[0], nil
}

func GetBestCandidate(candidates []*Candidates) *Candidates {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, candidate := range candidates {
		if candidate.Width > best.Width {
			best = candidate
		}
	}
	return best
}

func GetBestVideoVersion(versions []*VideoVersions) *VideoVersions {
	if len(versions) == 0 {
		return nil
	}
	best := versions[0]
	for _, version := range versions {
		if version.Width > best.Width {
			best = version
		}
	}
	return best
}
