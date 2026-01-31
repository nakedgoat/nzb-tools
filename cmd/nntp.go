package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// NNTPClient is a minimal NNTP client used to fetch article bodies.
type NNTPClient struct {
	conn net.Conn
	tr   *textproto.Reader
	tw   *textproto.Writer
	bw   *bufio.Writer
}

func DialNNTP(host string, port int, ssl bool) (*NNTPClient, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var c net.Conn
	var err error
	if ssl {
		c, err = tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	} else {
		c, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}

	r := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	tr := textproto.NewReader(r)
	tw := textproto.NewWriter(bw)

	// Read greeting
	line, err := tr.ReadLine()
	if err != nil {
		c.Close()
		return nil, err
	}
	// Accept 200/201 greetings
	if !strings.HasPrefix(line, "200") && !strings.HasPrefix(line, "201") {
		c.Close()
		return nil, fmt.Errorf("unexpected greeting: %s", line)
	}

	return &NNTPClient{conn: c, tr: tr, tw: tw, bw: bw}, nil
}

func (c *NNTPClient) Close() error {
	return c.conn.Close()
}

var NNTPReadTimeout = 10 * time.Second

func (c *NNTPClient) Auth(username, password string) error {
	if username == "" {
		return nil
	}
	// Send AUTHINFO USER
	if err := c.tw.PrintfLine("AUTHINFO USER %s", username); err != nil {
		return err
	}
	if err := c.flush(); err != nil {
		return err
	}
	// Set a read deadline so tests don't hang forever.
	_ = c.conn.SetReadDeadline(time.Now().Add(NNTPReadTimeout))
	line, err := c.tr.ReadLine()
	_ = c.conn.SetReadDeadline(time.Time{})
	if err != nil {
		return err
	}
	// 381 indicates password required
	if strings.HasPrefix(line, "381") {
		if err := c.tw.PrintfLine("AUTHINFO PASS %s", password); err != nil {
			return err
		}
		if err := c.flush(); err != nil {
			return err
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(NNTPReadTimeout))
		line, err = c.tr.ReadLine()
		_ = c.conn.SetReadDeadline(time.Time{})
		if err != nil {
			return err
		}
		if !strings.HasPrefix(line, "281") {
			return fmt.Errorf("auth failed: %s", line)
		}
		return nil
	}
	// Some servers immediately return 281
	if strings.HasPrefix(line, "281") {
		return nil
	}
	return fmt.Errorf("auth unexpected response: %s", line)
}

func (c *NNTPClient) flush() error {
	if c.bw != nil {
		return c.bw.Flush()
	}
	return nil
}

// Request sends an arbitrary command (e.g., STAT, HEAD, BODY, ARTICLE) with the
// given message-id and returns the response status code and the raw response line
// and, if present, any dot-terminated lines after a multiline response.
func (c *NNTPClient) Request(method, msgid string) (int, string, []string, error) {
	if err := c.tw.PrintfLine("%s %s", method, msgid); err != nil {
		return 0, "", nil, err
	}
	if err := c.flush(); err != nil {
		return 0, "", nil, err
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(NNTPReadTimeout))
	line, err := c.tr.ReadLine()
	_ = c.conn.SetReadDeadline(time.Time{})
	if err != nil {
		return 0, "", nil, err
	}
	// Parse status code
	parts := strings.SplitN(line, " ", 2)
	code := 0
	if len(parts) > 0 {
		if v, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			code = v
		}
	}
	var dotLines []string
	// Only methods that return dot-terminated multiline payloads should call ReadDotLines
	if code >= 200 && code < 300 {
		m := strings.ToUpper(method)
		if m == "BODY" || m == "HEAD" || m == "ARTICLE" {
			_ = c.conn.SetReadDeadline(time.Now().Add(NNTPReadTimeout))
			lines, err := c.tr.ReadDotLines()
			_ = c.conn.SetReadDeadline(time.Time{})
			if err == nil {
				dotLines = lines
			}
		}
	}
	return code, line, dotLines, nil
}

// Body fetches an article body by message-id and returns dot-terminated lines (compat wrapper).
func (c *NNTPClient) Body(msgid string) ([]string, error) {
	_, _, lines, err := c.Request("BODY", msgid)
	return lines, err
}

// Quit politely
func (c *NNTPClient) Quit() error {
	_ = c.tw.PrintfLine("QUIT")
	_ = c.flush()
	return c.Close()
}

// Post submits an article to the NNTP server. The article should include headers
// and body. Returns the Message-ID of the posted article (parsed from headers) or
// empty string if not present.
func (c *NNTPClient) Post(article string) (string, error) {
	if err := c.tw.PrintfLine("POST"); err != nil {
		return "", err
	}
	if err := c.flush(); err != nil {
		return "", err
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(NNTPReadTimeout))
	line, err := c.tr.ReadLine()
	_ = c.conn.SetReadDeadline(time.Time{})
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(line, "340") {
		return "", fmt.Errorf("server refused to accept post: %s", line)
	}

	// Send article lines, dot-stuffing any lines that begin with '.'
	for _, l := range strings.Split(article, "\n") {
		if strings.HasPrefix(l, ".") {
			l = "." + l
		}
		if err := c.tw.PrintfLine(l); err != nil {
			return "", err
		}
	}
	// Terminate with a single dot
	if err := c.tw.PrintfLine("."); err != nil {
		return "", err
	}
	if err := c.flush(); err != nil {
		return "", err
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(NNTPReadTimeout))
	line, err = c.tr.ReadLine()
	_ = c.conn.SetReadDeadline(time.Time{})
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(line, "240") {
		return "", fmt.Errorf("post failed: %s", line)
	}
	// Try to extract Message-ID header from article
	for _, l := range strings.Split(article, "\n") {
		if strings.HasPrefix(strings.ToLower(l), "message-id:") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}
	return "", nil
}
