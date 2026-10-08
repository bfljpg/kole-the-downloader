package tiktok

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// challenge page captured from tiktok.com
const wafChallengePage = `<html><body>Please wait...
<p id="wci" class="_wafchallengeid"></p>
<p id="cs" class="eyJ2Ijp7ImEiOiJZYStsTEtMaFN5Q0p5QjZEMXhmWHhEUmo2UEtkNU9qL0FxTW10TWZKYnFNPSIsImIiOjE3OTE0NDkwNTUsImMiOiJyWitSZ0VwQnhhK1Vlczd5enZkYTZnVkk4Wmh5U3hsOEZJdEpyZVowN0FvPSJ9LCJzIjoiL1ExOXU2d0FXeXVXMjJsVXJWcmtDOEdNWVovN1hKOENjcHB4em00OTl5Yz0ifQ"></p>
<p id="rci" class="waforiginalreid"></p>
<p id="rs" class=""></p>
<p id="rs_id" class="50"></p>
</body></html>`

func TestSolveWAFChallenge(t *testing.T) {
	cookies, err := SolveWAFChallenge([]byte(wafChallengePage))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(cookies))
	}
	if cookies[0].Name != "_wafchallengeid" ||
		cookies[1].Name != "waforiginalreid" ||
		cookies[2].Name != "waforigin_id" || cookies[2].Value != "50" {
		t.Fatalf("unexpected cookies: %+v", cookies)
	}

	raw, err := base64.StdEncoding.DecodeString(cookies[0].Value)
	if err != nil {
		t.Fatalf("cookie is not base64: %v", err)
	}
	var solved struct {
		V struct{ A, C string } `json:"v"`
		D string                `json:"d"`
	}
	if err := json.Unmarshal(raw, &solved); err != nil {
		t.Fatalf("cookie is not json: %v", err)
	}
	prefix, _ := decodeBase64(solved.V.A)
	expected, _ := decodeBase64(solved.V.C)
	answer, _ := base64.StdEncoding.DecodeString(solved.D)

	sum := sha256.Sum256(append(prefix, answer...))
	if string(sum[:]) != string(expected) {
		t.Fatalf("solution %q does not match the expected hash", answer)
	}
}

func TestSolveWAFChallengeIgnoresRegularPage(t *testing.T) {
	cookies, err := SolveWAFChallenge([]byte(`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{}</script>`))
	if err != nil || cookies != nil {
		t.Fatalf("expected no-op, got cookies=%v err=%v", cookies, err)
	}
}
