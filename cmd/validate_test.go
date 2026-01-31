package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeNNTPServer starts a simple server that sends a greeting and accepts
// AUTHINFO USER/PASS then responds with 281 on successful pass. Returns the
// listening address and a function to stop the server.
func fakeNNTPServer(t *testing.T, handleAuth bool, authOk bool) (string, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
					t.Logf("accept error: %v", err)
					return
				}
			}
			go func(c net.Conn) {
				defer c.Close()
				w := bufio.NewWriter(c)
				r := bufio.NewReader(c)
				// greeting
				fmt.Fprint(w, "200 fake.nntp NNTP server\r\n")
				w.Flush()
				if handleAuth {
					// Prompt for password immediately to avoid timing issues in tests.
					fmt.Fprint(w, "381 password required\r\n")
					w.Flush()
					for {
						line, err := r.ReadString('\n')
						if err != nil {
							return
						}
						if line == "QUIT\r\n" || line == "QUIT\n" {
							fmt.Fprint(w, "205 closing\r\n")
							w.Flush()
							return
						}
						if strings.HasPrefix(line, "AUTHINFO PASS ") {
							if authOk {
								fmt.Fprint(w, "281 auth accepted\r\n")
							} else {
								fmt.Fprint(w, "481 auth rejected\r\n")
							}
							w.Flush()
						}
					}
				} else {
					// simply wait for QUIT then close
					for {
						line, err := r.ReadString('\n')
						if err != nil {
							return
						}
						if line == "QUIT\r\n" || line == "QUIT\n" {
							fmt.Fprint(w, "205 closing\r\n")
							w.Flush()
							return
						}
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { close(stop); ln.Close() }
}

func TestValidateBasic(t *testing.T) {
	f, err := os.CreateTemp("", "cfg-*.json")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`{"default":"main","servers":[{"name":"main","hostname":"example","port":119}]}`)
	f.Close()
	// Should succeed without --check
	if err := validateCmd([]string{"--config", f.Name()}); err != nil {
		t.Fatalf("validate basic failed: %v", err)
	}
}

func TestValidateCheckConnectAndAuth(t *testing.T) {
	addr, stop := fakeNNTPServer(t, true, true)
	defer stop()
	host, portStr, _ := net.SplitHostPort(addr)
	cfgPath := filepath.Join(os.TempDir(), "test-nzb-config.json")
	os.WriteFile(cfgPath, []byte(fmt.Sprintf(`{"default":"main","servers":[{"name":"main","hostname":"%s","port":%s,"username":"u","password":"p"}]}`, host, portStr)), 0600)
	defer os.Remove(cfgPath)
	// with --check it should attempt to connect and auth
	if err := validateCmd([]string{"--config", cfgPath, "--check"}); err != nil {
		t.Fatalf("validate check failed: %v", err)
	}
}

func TestValidateCheckAuthFail(t *testing.T) {
	addr, stop := fakeNNTPServer(t, true, false)
	defer stop()
	host, portStr, _ := net.SplitHostPort(addr)
	cfgPath := filepath.Join(os.TempDir(), "test-nzb-config-bad.json")
	os.WriteFile(cfgPath, []byte(fmt.Sprintf(`{"default":"main","servers":[{"name":"main","hostname":"%s","port":%s,"username":"u","password":"p"}]}`, host, portStr)), 0600)
	defer os.Remove(cfgPath)
	err := validateCmd([]string{"--config", cfgPath, "--check"})
	if err == nil {
		t.Fatalf("expected validate to fail when auth is rejected")
	}
	// ensure error text mentions auth or failure
	if !(strings.Contains(err.Error(), "auth") || strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "failed")) {
		t.Fatalf("unexpected error: %v", err)
	}
}
