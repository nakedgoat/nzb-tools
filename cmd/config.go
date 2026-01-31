package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ServerConfig holds connection information for an NNTP server.
type ServerConfig struct {
	Name        string `json:"name"`
	Hostname    string `json:"hostname"`
	Port        int    `json:"port"`
	SSL         bool   `json:"ssl"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Connections int    `json:"connections,omitempty"`
}

// Config represents the top-level configuration file.
type Config struct {
	Default string         `json:"default"`
	Servers []ServerConfig `json:"servers"`
}

// Server returns the server configuration with the given name, or nil if not found.
func (c *Config) Server(name string) *ServerConfig {
	if name == "" && c.Default != "" {
		name = c.Default
	}
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			return &c.Servers[i]
		}
	}
	return nil
}

// LoadConfig tries to load configuration from a provided path, or from common locations
// if path is empty. It supports JSON files describing multiple servers and simple
// .env-style files. Use the environment variable NZB_CONFIG to override the path.
func LoadConfig(path string) (*Config, error) {
	// Environment override
	if env := os.Getenv("NZB_CONFIG"); env != "" && path == "" {
		path = env
	}

	// Default search paths
	if path == "" {
		candidates := []string{
			"./nzb.json",
			"./nzb.config.json",
		}
		if dir, err := os.UserConfigDir(); err == nil {
			candidates = append(candidates, filepath.Join(dir, "nzb", "config.json"))
		}
		candidates = append(candidates, "/etc/nzb/config.json")
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}

	if path == "" {
		return nil, errors.New("no config path provided and none found in default locations")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	if strings.HasSuffix(strings.ToLower(path), ".env") {
		m, err := parseEnvFile(f)
		if err != nil {
			return nil, err
		}
		cfg := configFromEnvMap(m)
		return cfg, nil
	}

	var cfg Config
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

var envIndexedKeyRe = regexp.MustCompile(`^NNTP_(?:([0-9]+)_)?([A-Z0-9_]+)$`)

// parseEnvFile reads KEY=VALUE lines into a map.
func parseEnvFile(f *os.File) (map[string]string, error) {
	s := bufio.NewScanner(f)
	m := make(map[string]string)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		m[k] = v
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// configFromEnvMap turns a flat env map into a Config. Supports keys like
// NNTP_HOSTNAME, NNTP_PORT (single default server) or NNTP_1_HOSTNAME, NNTP_2_HOSTNAME
// for indexed servers.
func configFromEnvMap(m map[string]string) *Config {
	indexed := make(map[int]map[string]string)
	base := make(map[string]string)

	for k, v := range m {
		if !strings.HasPrefix(k, "NNTP_") {
			continue
		}
		if matches := envIndexedKeyRe.FindStringSubmatch(k); matches != nil {
			if matches[1] == "" {
				base[matches[2]] = v
				continue
			}
			idx, _ := strconv.Atoi(matches[1])
			mm := indexed[idx]
			if mm == nil {
				mm = make(map[string]string)
				indexed[idx] = mm
			}
			mm[matches[2]] = v
		}
	}

	cfg := &Config{}

	// Populate indexed servers first
	for i := 0; ; i++ {
		mm := indexed[i]
		if mm == nil {
			break
		}
		srv := serverFromMap(mm)
		srv.Name = mm["NAME"]
		if srv.Name == "" {
			srv.Name = fmt.Sprintf("server-%d", i)
		}
		cfg.Servers = append(cfg.Servers, *srv)
	}

	// If we have base keys and no indexed servers, use base as single server
	if len(cfg.Servers) == 0 && len(base) > 0 {
		srv := serverFromMap(base)
		srv.Name = base["NAME"]
		if srv.Name == "" {
			srv.Name = "default"
		}
		cfg.Servers = append(cfg.Servers, *srv)
	}

	return cfg
}

// serverFromMap creates a ServerConfig from a map with keys like HOSTNAME, PORT, SSL, etc.
func serverFromMap(m map[string]string) *ServerConfig {
	s := &ServerConfig{}
	if v := m["HOSTNAME"]; v != "" {
		s.Hostname = v
	}
	if v := m["PORT"]; v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			s.Port = p
		}
	}
	if v := m["SSL"]; v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			s.SSL = b
		}
	}
	if v := m["USERNAME"]; v != "" {
		s.Username = v
	}
	if v := m["PASSWORD"]; v != "" {
		s.Password = v
	}
	if v := m["CONNECTIONS"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Connections = n
		}
	}
	return s
}
