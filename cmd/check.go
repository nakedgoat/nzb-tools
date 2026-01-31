package main

import (
	"errors"
	"flag"
	"fmt"
	"time"
)

func checkCmd(args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	h := flags.String("hostname", "", "NNTP hostname")
	p := flags.Int("port", 0, "NNTP port")
	ssl := flags.Bool("ssl", false, "Whether to use SSL")
	u := flags.String("username", "", "username")
	pass := flags.String("password", "", "password")
	server := flags.String("server", "", "server name from config (overrides hostname/port)")
	cfgPath := flags.String("config", "", "path to config file")
	method := flags.String("method", "STAT", "method to check articles: STAT, HEAD, BODY, ARTICLE")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// config override
	if *server != "" {
		cfg, err := LoadConfig(*cfgPath)
		if err != nil {
			return err
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
		if !*ssl && srv.SSL {
			*ssl = srv.SSL
		}
	}

	if flags.NArg() < 1 {
		return errors.New("usage: check [--server name | --hostname host --port P] <input> [filename]")
	}

	input := flags.Arg(0)
	var filename string
	if flags.NArg() > 1 {
		filename = flags.Arg(1)
	}

	nzb, err := fetchNZB(input)
	if err != nil {
		return err
	}

	var files []File
	if filename != "" {
		for i := range nzb.Files {
			if nzb.Files[i].Name == filename {
				files = append(files, nzb.Files[i])
				break
			}
		}
		if len(files) == 0 {
			return fmt.Errorf("file %s not found", filename)
		}
	} else {
		files = nzb.Files
	}

	client, err := DialNNTP(*h, *p, *ssl)
	if err != nil {
		return err
	}
	defer client.Quit()
	if *u != "" {
		if err := client.Auth(*u, *pass); err != nil {
			return err
		}
	}

	for _, f := range files {
		start := time.Now()
		fmt.Printf("Checking %s\n", f.Name)
		for _, seg := range f.Segments {
			// send request for each segment
			code, line, _, err := client.Request(*method, seg.ID)
			if err != nil {
				return fmt.Errorf("request %s %s: %w", *method, seg.ID, err)
			}
			if code == 430 {
				fmt.Printf("Article %s of file %s is missing (response: %s)\n", seg.ID, f.Name, line)
			}
		}
		fmt.Printf("Checked %s in %v\n", f.Name, time.Since(start))
	}

	return nil
}
