package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// simple fake NNTP that responds to STAT with 430 for a given message-id.
func fakeStatServer(t *testing.T, missing map[string]bool) (string, func()) {
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
				fmt.Fprint(w, "200 fake.nntp NNTP server\r\n")
				w.Flush()
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					// Debug log what we received from client
					t.Logf("server received: %q", line)
					if line == "QUIT\r\n" || line == "QUIT\n" {
						fmt.Fprint(w, "205 closing\r\n")
						w.Flush()
						return
					}
					// Expect e.g. STAT <msgid>
					var cmd, arg string
					fmt.Sscanf(line, "%s %s", &cmd, &arg)
					if _, ok := missing[arg]; ok {
						fmt.Fprintf(w, "430 no such article\r\n")
					} else {
						fmt.Fprintf(w, "223 %s OK\r\n", arg)
					}
					w.Flush()
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { close(stop); ln.Close() }
}

func TestCheckMissing(t *testing.T) {
	// create simple NZB with one file and two segments
	nzb := `<?xml version="1.0"?>
	<nzb>
	  <head></head>
	  <file poster="p" date="0" subject="f 2">
	    <groups><group>alt.test</group></groups>
	    <segments>
      <segment bytes="100" number="1">msg1</segment>
      <segment bytes="100" number="2">msg2</segment>
	    </segments>
	  </file>
	</nzb>`
	f, _ := os.CreateTemp("", "t-*.nzb")
	defer os.Remove(f.Name())
	f.WriteString(nzb)
	f.Close()

	addr, stop := fakeStatServer(t, map[string]bool{"<msg2>": true})
	defer stop()
	host, port, _ := net.SplitHostPort(addr)
	cfgPath := filepath.Join(os.TempDir(), "test-nzb-config.json")
	os.WriteFile(cfgPath, []byte(fmt.Sprintf(`{"default":"main","servers":[{"name":"main","hostname":"%s","port":%s}]}`, host, port)), 0600)
	defer os.Remove(cfgPath)

	// run check with --server and input file (pass config path)
	if err := checkCmd([]string{"--config", cfgPath, "--server", "main", f.Name()}); err != nil {
		t.Fatalf("check failed: %v", err)
	}
}
