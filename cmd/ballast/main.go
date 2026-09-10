// Command ballast is the authenticated local API client.
package main

import (
	"ballast/internal/auth"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var version = "1.0.0-rc.1"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	server := flag.String("server", envOr("BALLAST_SERVER", "http://127.0.0.1:8080"), "control plane URL")
	tokenFile := flag.String("token-file", envOr("BALLAST_TOKEN_FILE", "./.ballast/operator.token"), "private operator credential file")
	flag.Parse()
	if *showVersion {
		fmt.Println("ballast " + version)
		return
	}
	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	token, err := auth.ReadCredential(*tokenFile)
	if err != nil {
		fatal(err)
	}
	method, path := "GET", ""
	var body io.Reader
	switch args[0] {
	case "projects":
		path = "/projects"
	case "runners":
		path = "/runners"
	case "activity":
		path = "/activity"
	case "project", "task", "workspace", "changeset":
		if len(args) < 3 {
			fatal(fmt.Errorf("resource command requires an action and ID (or JSON for create)"))
		}
		resource, action, value := args[0], args[1], args[2]
		id := url.PathEscape(value)
		switch resource + " " + action {
		case "project get":
			path = "/projects/" + id
		case "project create":
			method, path, body = "POST", "/projects", strings.NewReader(value)
		case "task get":
			path = "/tasks/" + id
		case "task list":
			path = "/projects/" + id + "/tasks"
		case "workspace list":
			path = "/projects/" + id + "/workspaces"
		case "workspace get":
			path = "/workspaces/" + id
		case "changeset list":
			path = "/projects/" + id + "/changesets"
		case "changeset get":
			path = "/changesets/" + id
		case "task create":
			method, path = "POST", "/projects/"+id+"/tasks"
		case "task assign":
			method, path = "POST", "/tasks/"+id+"/assign"
		case "changeset approve", "changeset reject":
			payload, _ := json.Marshal(map[string]string{"decision": action})
			method, path, body = "POST", "/changesets/"+id+"/decision", bytes.NewReader(payload)
		case "changeset integrate":
			method, path = "POST", "/changesets/"+id+"/integrate"
		default:
			fatal(fmt.Errorf("unknown resource action"))
		}
		if resource == "task" && (action == "create" || action == "assign") {
			if len(args) != 4 {
				fatal(fmt.Errorf("JSON body required (or - for stdin)"))
			}
			if args[3] == "-" {
				body = io.LimitReader(os.Stdin, 4<<20)
			} else {
				body = strings.NewReader(args[3])
			}
		}
	case "request":
		if len(args) < 3 {
			usage()
			os.Exit(2)
		}
		method, path = args[1], args[2]
		if len(args) > 3 {
			if args[3] == "-" {
				body = io.LimitReader(os.Stdin, 4<<20)
			} else {
				body = bytes.NewBufferString(args[3])
			}
		}
	default:
		usage()
		os.Exit(2)
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		fatal(fmt.Errorf("API path must start with a single slash"))
	}
	if method == "POST" && path == "/runners" {
		fatal(fmt.Errorf("use ballast-runner to register privately"))
	}
	req, err := http.NewRequest(method, strings.TrimRight(*server, "/")+path, body)
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		fatal(err)
	}
	defer res.Body.Close()
	// Runner registration contains a credential and is intentionally excluded
	// from generic CLI output. Register through the runner daemon instead.
	if method == "POST" && path == "/runners" {
		fatal(fmt.Errorf("use ballast-runner to register privately"))
	}
	_, _ = io.Copy(os.Stdout, io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 400 {
		os.Exit(1)
	}
}
func usage() {
	fmt.Fprintln(os.Stderr, "ballast [--server URL --token-file PATH] projects|runners|activity\nballast [flags] project|task|workspace|changeset ACTION ID [JSON|-]\nballast [flags] request METHOD /path [JSON|-]")
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "ballast:", err); os.Exit(1) }
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
