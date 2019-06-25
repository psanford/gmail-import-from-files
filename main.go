package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

var dir = flag.String("dir", "", "Directory of messages")
var label = flag.String("label", "", "Gmail label to use")

func main() {
	flag.Parse()

	if *dir == "" {
		fmt.Fprintf(os.Stderr, "-dir required\n")
		flag.Usage()
		os.Exit(1)
	}

	if *label == "" {
		fmt.Fprintf(os.Stderr, "-label required\n")
		flag.Usage()
		os.Exit(1)
	}

	c, err := NewClient()
	if err != nil {
		panic(err)
	}

	req := c.svc.Labels.List("me")
	res, err := req.Do()
	if err != nil {
		panic(err)
	}

	var lid string
	for _, l := range res.Labels {
		if l.Name == *label {
			lid = l.Id
			fmt.Printf("lid %s\n", lid)
			break
		}
	}

	os.MkdirAll(filepath.Join(*dir, "imported"), 0777)

	if lid == "" {
		log.Fatal("Label not found")
	}

	files, err := ioutil.ReadDir(*dir)
	if err != nil {
		panic(err)
	}

	for _, fi := range files {
		if fi.IsDir() {
			continue
		}

		fiPath := filepath.Join(*dir, fi.Name())
		raw, err := ioutil.ReadFile(fiPath)
		if err != nil {
			panic(err)
		}

		var msg = gmail.Message{
			LabelIds: []string{lid},
		}

		importCmd := c.svc.Messages.Import("me", &msg)
		importCmd.InternalDateSource("dateHeader")
		importCmd.NeverMarkSpam(true)
		importCmd.Media(bytes.NewReader(raw), googleapi.ContentType("message/rfc822"))
		log.Printf("import %s", fiPath)
		got, err := importCmd.Do()
		if err != nil {
			panic(err)
		}

		log.Printf("msg: %+v\n", got)
		os.Rename(fiPath, filepath.Join(*dir, "imported", fi.Name()))
	}
}

func NewClient() (*Client, error) {
	const OOB = "urn:ietf:wg:oauth:2.0:oob"
	conf := &oauth2.Config{
		ClientID: "693482145935-1q2s7qsv89kdf28kkj7hu3dq65qb8c78.apps.googleusercontent.com",

		// https://developers.google.com/identity/protocols/OAuth2InstalledApp
		// says: "The client ID and client secret obtained
		// from the Developers Console are embedded in the
		// source code of your application. In this context,
		// the client secret is obviously not treated as a
		// secret."
		ClientSecret: "pvXBU_ecl_TnCjDD2nzU5czg",

		Endpoint:    google.Endpoint,
		RedirectURL: OOB,
		Scopes:      []string{gmail.MailGoogleComScope},
	}

	cacheDir := filepath.Join(userCacheDir(), "gmail-import-from-dir")
	gmailTokenFile := filepath.Join(cacheDir, "gmail.token")

	slurp, err := ioutil.ReadFile(gmailTokenFile)
	var ts oauth2.TokenSource
	if err == nil {
		f := strings.Fields(strings.TrimSpace(string(slurp)))
		if len(f) == 2 {
			ts = conf.TokenSource(context.Background(), &oauth2.Token{
				AccessToken:  f[0],
				TokenType:    "Bearer",
				RefreshToken: f[1],
				Expiry:       time.Unix(1, 0),
			})
			if _, err := ts.Token(); err != nil {
				log.Printf("Cached token invalid: %v", err)
				ts = nil
			}
		}
	}

	if ts == nil {
		authCode := conf.AuthCodeURL("state")
		log.Printf("Go to %v", authCode)
		io.WriteString(os.Stdout, "Enter code> ")

		bs := bufio.NewScanner(os.Stdin)
		if !bs.Scan() {
			return nil, errors.New("Failed to read code")
		}
		code := strings.TrimSpace(bs.Text())
		t, err := conf.Exchange(context.Background(), code)
		if err != nil {
			return nil, err
		}
		os.MkdirAll(cacheDir, 0700)
		ioutil.WriteFile(gmailTokenFile, []byte(t.AccessToken+" "+t.RefreshToken), 0600)
		ts = conf.TokenSource(context.Background(), t)
	}

	client := oauth2.NewClient(context.Background(), ts)
	svc, err := gmail.New(client)
	if err != nil {
		return nil, err
	}

	return &Client{
		svc: svc.Users,
	}, nil
}

type Client struct {
	svc *gmail.UsersService
}

func userCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return xdg
	}
	return filepath.Join(os.Getenv("HOME"), ".cache")
}
