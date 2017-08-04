// Copyright 2017 Mathieu Lonjaret

package main

import (
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/mpl/basicauth"
	"github.com/mpl/simpletls"
)

const (
	idstring = "http://golang.org/pkg/http/#ListenAndServe"
)

var (
	host         = flag.String("host", "0.0.0.0:8080", "listening port and hostname")
	help         = flag.Bool("h", false, "show this help")
	flagUserpass = flag.String("userpass", "", "optional username:password protection")
	flagTLS      = flag.Bool("tls", false, `For https. Uses autocert, or "key.pem" and "cert.pem" in $HOME/keys/`)
)

var (
	rootdir, _ = os.Getwd()
	up         *basicauth.UserPass
	uploadTmpl *template.Template

	lastServerMu sync.Mutex
	lastServer   int
)

func usage() {
	fmt.Fprintf(os.Stderr, "\t osmcache \n")
	flag.PrintDefaults()
	os.Exit(2)
}

func makeHandler(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if e, ok := recover().(error); ok {
				http.Error(w, e.Error(), http.StatusInternalServerError)
				return
			}
		}()
		title := r.URL.Path
		w.Header().Set("Server", idstring)
		if isAllowed(r) {
			fn(w, r, title)
		} else {
			basicauth.SendUnauthorized(w, r, "simpleHttpd")
		}
	}
}

func isAllowed(r *http.Request) bool {
	if *flagUserpass == "" {
		return true
	}
	return up.IsAllowed(r)
}

func pickServer() string {
	servers := []string{"a", "b", "c"}
	lastServerMu.Lock()
	defer lastServerMu.Unlock()
	lastServer++
	if lastServer >= len(servers) {
		lastServer = 0
	}
	return servers[lastServer]
}

const (
	osmBaseURL = "https://%s.tile.openstreetmap.org"
	userAgent  = "github.com/mpl/osmcache/1.0"
)

func fetchTile(url string) error {
	server := pickServer()

	// TODO(mpl): Contact them and figure out what the fuck they want from
	// me in addition to a user-agent. using curl in the meantime.

	fullPath := filepath.Join(rootdir, url)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
		return err
	}
	fullURL := fmt.Sprintf(osmBaseURL, server) + url
	cmd := exec.Command("curl", fullURL, "-o", fullPath)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil

	req, err := http.NewRequest("GET", fmt.Sprintf(osmBaseURL, server)+url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		tile, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return errors.New("no fetchy")
		}
		if err := ioutil.WriteFile(filepath.Join(rootdir, "error.dat"), tile, 0700); err != nil {
			return errors.New("no fetchy")
		}
		return errors.New("no fetchy")
	}
	tile, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
		return err
	}
	if err := ioutil.WriteFile(fullPath, tile, 0700); err != nil {
		return err
	}
	return nil
}

func serveTile(w http.ResponseWriter, r *http.Request, url string) {
	fullPath := filepath.Join(rootdir, url)
	if _, err := os.Stat(fullPath); err != nil {
		if !os.IsNotExist(err) {
			http.Error(w, "nope", 500)
			log.Print(err)
			return
		}
		if err := fetchTile(url); err != nil {
			http.Error(w, "nope", 500)
			log.Print(err)
			return
		}
	}
	http.ServeFile(w, r, fullPath)
}

func initUserPass() {
	if *flagUserpass == "" {
		return
	}
	var err error
	up, err = basicauth.New(*flagUserpass)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Usage = usage
	flag.Parse()
	if *help {
		usage()
	}

	nargs := flag.NArg()
	if nargs > 0 {
		usage()
	}

	initUserPass()

	*simpletls.FlagAutocert = false

	if !*flagTLS && *simpletls.FlagAutocert {
		*flagTLS = true
	}

	var err error
	var listener net.Listener
	if *flagTLS {
		listener, err = simpletls.Listen(*host)
	} else {
		listener, err = net.Listen("tcp", *host)
	}
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *host, err)
	}

	http.Handle("/", makeHandler(serveTile))
	if err = http.Serve(listener, nil); err != nil {
		log.Fatalf("Error in http server: %v\n", err)
	}
}
