package extractors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/govdbot/govd/internal/config"
	"github.com/govdbot/govd/internal/logger"
	"go.uber.org/zap"
)

func TestNewSessionClient(t *testing.T) {
	logger.L = zap.NewNop().Sugar()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "private", "cookies"), 0o755); err != nil {
		t.Fatal(err)
	}
	cookie := ".instagram.com\tTRUE\t/\tTRUE\t0\tsessionid\tsecret\n"
	if err := os.WriteFile(filepath.Join(dir, "private", "cookies", "demo-session.txt"), []byte(cookie), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if c := newSessionClient("demo", &config.ExtractorConfig{}); c != nil {
		t.Fatal("session client must not exist without session_proxy")
	}
	if c := newSessionClient("other", &config.ExtractorConfig{SessionProxy: "http://1.2.3.4:8080"}); c != nil {
		t.Fatal("session client must not exist without session cookies")
	}
	c := newSessionClient("demo", &config.ExtractorConfig{SessionProxy: "http://1.2.3.4:8080"})
	if c == nil || c.Proxy != "http://1.2.3.4:8080" || len(c.Cookies) != 1 {
		t.Fatalf("unexpected session client: %+v", c)
	}
}
