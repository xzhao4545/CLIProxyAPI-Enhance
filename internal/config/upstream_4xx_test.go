package config

import "testing"

func TestUpstream4xxTo502IsBooleanOnly(t *testing.T) {
	for _, tc := range []struct {
		yaml string
		want bool
	}{
		{"port: 8317", false}, {"upstream-4xx-to-502: false", false}, {"upstream-4xx-to-502: true", true},
	} {
		cfg, err := ParseConfigBytes([]byte(tc.yaml))
		if err != nil || cfg.Upstream4xxTo502 != tc.want {
			t.Fatalf("parse %q: cfg=%+v err=%v", tc.yaml, cfg, err)
		}
	}
	if _, err := ParseConfigBytes([]byte("upstream-4xx-to-502: 502")); err == nil {
		t.Fatal("numeric status accepted for boolean setting")
	}
}
