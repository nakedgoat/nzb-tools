package main

import (
	"bufio"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Minimal NZB model for parsing and generating NZB XML.

type NZB struct {
	XMLName xml.Name `xml:"nzb"`
	Head    Head     `xml:"head"`
	Files   []File   `xml:"file"`
}

type Head struct {
	Meta []Meta `xml:"meta"`
}

type Meta struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type File struct {
	Poster   string    `xml:"poster,attr"`
	Date     string    `xml:"date,attr"`
	Subject  string    `xml:"subject,attr"`
	Groups   []string  `xml:"groups>group"`
	Segments []Segment `xml:"segments>segment"`
	Name     string    `xml:"-"`
	SizeNum  int64     `xml:"-"`
}

type Segment struct {
	Bytes  int64  `xml:"bytes,attr"`
	Number int    `xml:"number,attr"`
	ID     string `xml:",chardata"`
}

// yEnc subject regex similar to the original.
var subjectRe = regexp.MustCompile(`"(?P<name>[^"]+)"(?: yEnc)?(?: \((?P<partnum>[\d]+)\/(?P<numparts>[\d]+)\))?(?: yEnc)?[^\d]?(?P<size>[\d]+)?`)

func yEncParse(subject string) (name string, size int64) {
	m := subjectRe.FindStringSubmatch(subject)
	if m == nil {
		return "", 0
	}
	for i, nameK := range subjectRe.SubexpNames() {
		if nameK == "name" {
			name = m[i]
		}
		if nameK == "size" && m[i] != "" {
			fmt.Sscan(m[i], &size)
		}
	}
	return
}

// fetchNZB supports local paths and http(s) URLs and gzipped NZBs.
func fetchNZB(input string) (*NZB, error) {
	var r io.ReadCloser
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		resp, err := http.Get(input)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("http status %d", resp.StatusCode)
		}
		r = resp.Body
	} else {
		// treat as file (relative or absolute)
		f, err := os.Open(input)
		if err != nil {
			return nil, err
		}
		r = f
	}
	defer r.Close()

	if strings.HasSuffix(strings.ToLower(input), ".gz") {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		return parseNZB(gr)
	}
	return parseNZB(r)
}

func parseNZB(r io.Reader) (*NZB, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var nzb NZB
	if err := xml.Unmarshal(b, &nzb); err != nil {
		return nil, err
	}
	// Postprocess: populate file Name and Size from subject and segments.
	for i := range nzb.Files {
		name, size := yEncParse(nzb.Files[i].Subject)
		if name != "" {
			nzb.Files[i].Name = name
		}
		if size > 0 {
			nzb.Files[i].SizeNum = size
		} else {
			var s int64
			for _, seg := range nzb.Files[i].Segments {
				s += seg.Bytes
			}
			nzb.Files[i].SizeNum = s
		}
	}
	return &nzb, nil
}

func (n *NZB) String() string {
	out := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"
	out += `<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">\n`
	out += "<nzb xmlns=\"http://www.newzbin.com/DTD/2003/nzb\">\n"
	out += "  <head>\n"
	for _, m := range n.Head.Meta {
		out += fmt.Sprintf("    <meta type=\"%s\">%s</meta>\n", m.Type, m.Value)
	}
	out += "  </head>\n"
	for _, f := range n.Files {
		out += fmt.Sprintf("  <file poster=\"%s\" date=\"%s\" subject=\"%s\">\n", escapeXml(f.Poster), f.Date, escapeXml(f.Subject))
		out += "    <groups>\n"
		for _, g := range f.Groups {
			out += fmt.Sprintf("      <group>%s</group>\n", g)
		}
		out += "    </groups>\n"
		out += "    <segments>\n"
		for _, s := range f.Segments {
			out += fmt.Sprintf("      <segment bytes=\"%d\" number=\"%d\">%s</segment>\n", s.Bytes, s.Number, s.ID)
		}
		out += "    </segments>\n"
		out += "  </file>\n"
	}
	out += "</nzb>\n"
	return out
}

func escapeXml(unsafe string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&apos;", "\"", "&quot;")
	return replacer.Replace(unsafe)
}

func combineCmd(args []string) error {
	flags := flag.NewFlagSet("combine", flag.ContinueOnError)
	_ = flags // no options now
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 2 {
		return errors.New("usage: combine <target> <source> [source ...]")
	}
	target := flags.Arg(0)
	sources := flags.Args()[1:]
	root, err := fetchNZB(target)
	if err != nil {
		return fmt.Errorf("fetch target: %w", err)
	}
	for _, s := range sources {
		n, err := fetchNZB(s)
		if err != nil {
			return fmt.Errorf("fetch source %s: %w", s, err)
		}
		// Append source files to target
		root.Files = append(root.Files, n.Files...)
	}
	fmt.Print(root.String())
	return nil
}

func matchesPattern(name, pattern string, isRegex bool) (bool, error) {
	if isRegex {
		r, err := regexp.Compile(pattern)
		if err != nil {
			return false, err
		}
		return r.MatchString(name), nil
	}
	// Use filepath.Match for glob (pattern may contain path separators; treat just as pattern)
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		// If pattern is not a valid glob, fall back to substring
		return strings.Contains(name, pattern), nil
	}
	return ok, nil
}

func extractCmd(args []string) error {
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	isRegex := flags.Bool("regex", false, "treat pattern as regular expression")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: extract <input> <glob|regex>")
	}
	input := flags.Arg(0)
	pattern := flags.Arg(1)
	root, err := fetchNZB(input)
	if err != nil {
		return err
	}
	out := NZB{Head: root.Head}
	for _, f := range root.Files {
		ok, err := matchesPattern(f.Name, pattern, *isRegex)
		if err != nil {
			return err
		}
		if ok {
			out.Files = append(out.Files, f)
		}
	}
	fmt.Print(out.String())
	return nil
}

func serveCmd(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("address", ":8000", "address to bind (e.g. :8000)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: serve [--address :8000] <input>")
	}
	input := flags.Arg(0)
	nzb, err := fetchNZB(input)
	if err != nil {
		return err
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><head><title>NZB Index</title></head><body>")
		fmt.Fprintf(w, "<h1>NZB Index</h1><ul>")
		for _, f := range nzb.Files {
			fmt.Fprintf(w, "<li><b>%s</b> (%d bytes) - <a href=\"/nzb?name=%s\">nzb</a></li>", f.Name, f.SizeNum, urlSafe(f.Name))
		}
		fmt.Fprintf(w, "</ul></body></html>")
	})

	http.HandleFunc("/nzb", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			fmt.Fprint(w, nzb.String())
			return
		}
		// Return NZB containing only that file
		for _, f := range nzb.Files {
			if f.Name == name {
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				single := NZB{Head: nzb.Head, Files: []File{f}}
				fmt.Fprint(w, single.String())
				return
			}
		}
		http.Error(w, "file not found", http.StatusNotFound)
	})

	fmt.Fprintf(os.Stderr, "Serving on %s\n", *addr)
	return http.ListenAndServe(*addr, nil)
}

func urlSafe(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "/", "%2F")
}

func getCmd(args []string) error {
	flags := flag.NewFlagSet("get", flag.ContinueOnError)
	h := flags.String("hostname", "", "NNTP hostname")
	p := flags.Int("port", 0, "NNTP port")
	u := flags.String("username", "", "username")
	pass := flags.String("password", "", "password")
	server := flags.String("server", "", "server name from config (overrides hostname/port)")
	sslFlag := flags.Bool("ssl", false, "Whether to use SSL")
	start := flags.Int64("start", 0, "start byte offset")
	end := flags.Int64("end", 0, "end byte offset (inclusive), 0 means to end of file")
	out := flags.String("out", "", "output file path (or '-' for stdout)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// If a server name is provided, try to load it from configuration.
	useSSL := *sslFlag
	if *server != "" {
		cfg, err := LoadConfig("")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		srv := cfg.Server(*server)
		if srv == nil {
			return fmt.Errorf("server %s not found in config", *server)
		}
		if *h == "" {
			*h = srv.Hostname
		}
		if *p == 0 && srv.Port != 0 {
			*p = srv.Port
		}
		if *u == "" {
			*u = srv.Username
		}
		if *pass == "" {
			*pass = srv.Password
		}
		if !useSSL && srv.SSL {
			useSSL = srv.SSL
		}
	}

	if flags.NArg() < 2 {
		return errors.New("usage: get [--server name | --hostname host --port P] [--start N] [--end N] [--out path] <input> <filename>")
	}

	input := flags.Arg(0)
	filename := flags.Arg(1)
	nzb, err := fetchNZB(input)
	if err != nil {
		return err
	}

	var file *File
	for i := range nzb.Files {
		if nzb.Files[i].Name == filename {
			file = &nzb.Files[i]
			break
		}
	}
	if file == nil {
		return fmt.Errorf("file %s not found", filename)
	}

	// Determine end default to file size-1 if zero
	if *end == 0 {
		*end = file.SizeNum - 1
	}
	if *start < 0 || *end < 0 || *start > *end || *end >= file.SizeNum {
		return fmt.Errorf("invalid range %d-%d for file size %d", *start, *end, file.SizeNum)
	}

	// Build list of segments covering the requested range.
	type piece struct {
		id    string
		start int64
		end   int64
	}

	var pieces []piece
	var offset int64 = 0
	for _, seg := range file.Segments {
		segSize := seg.Bytes
		if offset+segSize-1 < *start {
			offset += segSize
			continue
		}
		p := piece{id: seg.ID, start: 0, end: segSize - 1}
		if len(pieces) == 0 {
			p.start = *start - offset
		}
		// last piece
		if offset+segSize-1 >= *end {
			p.end = segSize - (offset + segSize - 1 - *end) - 1
			pieces = append(pieces, p)
			break
		}
		pieces = append(pieces, p)
		offset += segSize
	}

	// Prepare output writer
	var w *bufio.Writer
	var outFile *os.File
	if *out == "" || *out == "-" {
		w = bufio.NewWriter(os.Stdout)
	} else {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("create out: %w", err)
		}
		outFile = f
		w = bufio.NewWriter(f)
	}
	defer func() {
		w.Flush()
		if outFile != nil {
			outFile.Close()
		}
	}()

	// Connect to NNTP
	if *h == "" || *p == 0 {
		return fmt.Errorf("missing NNTP host/port; provide --hostname/--port or --server name")
	}
	client, err := DialNNTP(*h, *p, useSSL)
	if err != nil {
		return err
	}
	defer client.Quit()
	if *u != "" {
		if err := client.Auth(*u, *pass); err != nil {
			return err
		}
	}

	for _, pc := range pieces {
		lines, err := client.Body(pc.id)
		if err != nil {
			return fmt.Errorf("fetch body %s: %w", pc.id, err)
		}
		var segWritten int64
		for _, line := range lines {
			// Skip yEnc headers/trailer
			if strings.HasPrefix(line, "=ybegin") || strings.HasPrefix(line, "=ypart") || strings.HasPrefix(line, "=yend") {
				continue
			}
			// Un-dot-stuff
			if strings.HasPrefix(line, "..") {
				line = line[1:]
			}
			decoded, err := decodeYEncLine([]byte(line))
			if err != nil {
				return err
			}
			if len(decoded) == 0 {
				continue
			}
			// Determine slice within this decoded chunk
			chunkLen := int64(len(decoded))
			startOff := int64(0)
			endOff := chunkLen - 1
			if pc.start > segWritten {
				startOff = pc.start - segWritten
			}
			if pc.end < segWritten+chunkLen-1 {
				endOff = pc.end - segWritten
			}
			if startOff <= endOff {
				if _, err := w.Write(decoded[startOff : endOff+1]); err != nil {
					return err
				}
			}
			segWritten += chunkLen
			// If we've reached the end of this piece, break
			if segWritten > pc.end {
				break
			}
		}
	}

	return w.Flush()
}

func validateCmd(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	cfgPath := flags.String("config", "", "path to config file")
	check := flags.Bool("check", false, "attempt to connect and authenticate to each server")
	server := flags.String("server", "", "only validate a specific server by name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	servers := cfg.Servers
	if *server != "" {
		s := cfg.Server(*server)
		if s == nil {
			return fmt.Errorf("server %s not found in config", *server)
		}
		servers = []ServerConfig{*s}
	}
	if len(servers) == 0 {
		return fmt.Errorf("no servers configured")
	}
	ok := true
	for _, s := range servers {
		fmt.Printf("Server %s: ", s.Name)
		if s.Hostname == "" || s.Port == 0 {
			fmt.Printf("invalid (missing hostname/port)\n")
			ok = false
			continue
		}
		if !*check {
			fmt.Printf("ok (host=%s:%d ssl=%t)\n", s.Hostname, s.Port, s.SSL)
			continue
		}
		client, err := DialNNTP(s.Hostname, s.Port, s.SSL)
		if err != nil {
			fmt.Printf("connect failed: %v\n", err)
			ok = false
			continue
		}
		if s.Username != "" {
			if err := client.Auth(s.Username, s.Password); err != nil {
				fmt.Printf("auth failed: %v\n", err)
				client.Quit()
				ok = false
				continue
			}
		}
		fmt.Printf("ok\n")
		client.Quit()
	}
	if !ok {
		return fmt.Errorf("some checks failed")
	}
	return nil
}

func usage() {
	fmt.Printf(`nzb - NZB tools (single-file Go port)

Usage:
  nzb <command> [options] <args>

Commands:
  combine <target> <source>...    Combine NZBs into <target>
  extract [--regex] <input> <pattern>  Extract files matching pattern
  serve [--address :8000] <input> Serve NZB index over HTTP
  get [--server name | --hostname host --port P] <input> <filename>  (stub) show file
  validate [--config path] [--check] [--server name] Validate configured servers

Configuration:
  Set NZB_CONFIG to point to a config file (JSON or .env). Default locations checked:
    ./nzb.json, ./nzb.config.json, $XDG_CONFIG_HOME/nzb/config.json, /etc/nzb/config.json

Use "nzb <command> --help" for command-specific help.
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "combine":
		err = combineCmd(args)
	case "extract":
		err = extractCmd(args)
	case "serve":
		err = serveCmd(args)
	case "get":
		err = getCmd(args)
	case "check":
		err = checkCmd(args)
	case "validate":
		err = validateCmd(args)
	case "--help", "-h", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
