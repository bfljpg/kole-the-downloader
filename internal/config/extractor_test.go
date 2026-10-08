package config

import "testing"

func TestValidateSessionProxy(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{"", false},
		{"http://user:pass@1.2.3.4:8080", false},
		{"https://1.2.3.4:8080", false},
		{"socks5://user:pass@1.2.3.4:1080", false},
		{"socks5h://1.2.3.4:1080", false},
		{"ftp://1.2.3.4:21", true},
		{"1.2.3.4:8080", true},
		{"http://1.2.3.4", true},
		{"http://:8080", true},
		{"://bad", true},
	}
	for _, tt := range tests {
		err := validateSessionProxy(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateSessionProxy(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
	}
}
