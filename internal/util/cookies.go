package util

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/aki237/nscjar"
	"github.com/govdbot/govd/internal/logger"
)

var cookiesCache = make(map[string][]*http.Cookie)

func GetExtractorCookies(extractorID string) []*http.Cookie {
	if extractorID == "" {
		return nil
	}
	cookieFile := extractorID + ".txt"
	return ParseCookieFile(cookieFile)
}

// GetExtractorSessionCookies returns the cookies of a logged in session,
// kept in a separate file so they are never attached to regular requests.
func GetExtractorSessionCookies(extractorID string) []*http.Cookie {
	if extractorID == "" {
		return nil
	}
	return ParseCookieFile(extractorID + "-session.txt")
}

func ParseCookieFile(fileName string) []*http.Cookie {
	cachedCookies, ok := cookiesCache[fileName]
	if ok {
		return cachedCookies
	}

	cookiePath := filepath.Join("private/cookies", fileName)

	cookieFile, err := os.Open(cookiePath)
	if err != nil {
		return nil
	}
	defer cookieFile.Close()

	var parser nscjar.Parser
	cookies, err := parser.Unmarshal(cookieFile)
	if err != nil {
		logger.L.Warnf("failed parsing cookie file %s: %v", fileName, err)
		return nil
	}
	cookiesCache[fileName] = cookies

	logger.L.Debugf("parsed cookie file: %s", fileName)
	return cookies
}
