package tiktok

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// tiktok sometimes serves a tiny "Please wait..." page (slardar waf)
// instead of the real one. the page's script brute-forces a sha256
// proof of work, stores the answer in a short-lived cookie and reloads.
// we do the same here without needing a browser.

const wafMaxAttempts = 1_000_000

var wafFieldPattern = regexp.MustCompile(`<p id="(wci|cs|rci|rs|rs_id)" class="([^"]*)"`)

type wafChallenge struct {
	V struct {
		A string `json:"a"` // base64 prefix
		C string `json:"c"` // base64 expected sha256
	} `json:"v"`
}

// wafPayload keeps the original challenge fields untouched
// and appends the solution, as the page script does.
type wafPayload struct {
	V json.RawMessage `json:"v"`
	S json.RawMessage `json:"s,omitempty"`
	D string          `json:"d"`
}

// SolveWAFChallenge returns the cookies needed to pass the challenge
// served in body, or nil if body is not a challenge page.
func SolveWAFChallenge(body []byte) ([]*http.Cookie, error) {
	fields := make(map[string]string)
	for _, m := range wafFieldPattern.FindAllSubmatch(body, -1) {
		fields[string(m[1])] = string(m[2])
	}
	if fields["wci"] == "" || fields["cs"] == "" {
		return nil, nil
	}

	raw, err := decodeBase64(fields["cs"])
	if err != nil {
		return nil, fmt.Errorf("failed to decode challenge: %w", err)
	}
	var challenge wafChallenge
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return nil, fmt.Errorf("failed to parse challenge: %w", err)
	}
	var payload wafPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse challenge: %w", err)
	}
	prefix, err := decodeBase64(challenge.V.A)
	if err != nil {
		return nil, fmt.Errorf("failed to decode challenge prefix: %w", err)
	}
	expected, err := decodeBase64(challenge.V.C)
	if err != nil {
		return nil, fmt.Errorf("failed to decode challenge hash: %w", err)
	}

	answer, ok := solveProofOfWork(prefix, expected)
	if !ok {
		return nil, errors.New("failed to solve challenge")
	}
	payload.D = base64.StdEncoding.EncodeToString([]byte(answer))
	value, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode challenge solution: %w", err)
	}

	cookies := []*http.Cookie{
		{Name: fields["wci"], Value: base64.StdEncoding.EncodeToString(value)},
	}
	if rci := fields["rci"]; rci != "" {
		cookies = append(cookies, &http.Cookie{Name: rci, Value: fields["rs"]})
	}
	if id := fields["rs_id"]; id != "" {
		cookies = append(cookies, &http.Cookie{Name: "waforigin_id", Value: id})
	}
	return cookies, nil
}

func solveProofOfWork(prefix, expected []byte) (string, bool) {
	hasher := sha256.New()
	sum := make([]byte, 0, sha256.Size)
	for i := range wafMaxAttempts {
		answer := strconv.Itoa(i)
		hasher.Reset()
		hasher.Write(prefix)
		hasher.Write([]byte(answer))
		if bytes.Equal(hasher.Sum(sum[:0]), expected) {
			return answer, true
		}
	}
	return "", false
}

// the page serves base64 without padding
func decodeBase64(s string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}
