// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Command freeccrd serves the FreeCCR-go web UI over HTTP. The UI itself lives
// in internal/ui and is shared with the Wails desktop app.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/zhengli/freeccr-go/internal/ui"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8422", "listen address")
	open := flag.String("dir", "", "optional roll directory to load at startup")
	flag.Parse()

	srv := ui.NewServer()
	if *open != "" {
		if err := srv.LoadDir(*open); err != nil {
			log.Printf("initial load %s: %v", *open, err)
		}
	}
	fmt.Printf("FreeCCR-go UI → http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
