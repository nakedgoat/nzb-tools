package main

import (
	"os"
	"testing"
)

func TestLoadJSONConfig(t *testing.T) {
	f, err := os.CreateTemp("", "nzb-*.json")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`{
  "default": "main",
  "servers": [
    { "name": "main", "hostname": "news.example.com", "port": 563, "ssl": true, "username": "u", "password": "p" },
    { "name": "backup", "hostname": "news2.example.com", "port": 119 }
  ]
}`)
	f.Close()

	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Default != "main" {
		t.Fatalf("expected default main, got %q", cfg.Default)
	}
	if s := cfg.Server("main"); s == nil || s.Hostname != "news.example.com" || s.Port != 563 || !s.SSL {
		t.Fatalf("unexpected main server: %+v", s)
	}
}

func TestLoadEnvSingle(t *testing.T) {
	f, err := os.CreateTemp("", "nzb-*.env")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`NNTP_HOSTNAME=news.example.com
NNTP_PORT=563
NNTP_SSL=true
NNTP_USERNAME=u
NNTP_PASSWORD=p
NNTP_NAME=main
`)
	f.Close()

	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if s := cfg.Server("main"); s == nil || s.Hostname != "news.example.com" || s.Port != 563 || !s.SSL {
		t.Fatalf("unexpected server from env: %+v", s)
	}
}

func TestLoadEnvIndexed(t *testing.T) {
	f, err := os.CreateTemp("", "nzb-*.env")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`NNTP_0_HOSTNAME=news.example.com
NNTP_0_PORT=563
NNTP_0_SSL=true
NNTP_0_NAME=main
NNTP_1_HOSTNAME=news2.example.com
NNTP_1_PORT=119
NNTP_1_NAME=backup
`)
	f.Close()

	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cfg.Servers))
	}
	if cfg.Servers[1].Name != "backup" || cfg.Servers[1].Port != 119 {
		t.Fatalf("unexpected indexed server: %+v", cfg.Servers[1])
	}
}
