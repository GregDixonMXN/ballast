// Command ballast is the CLI client of the control-plane API.
// Usage: ballast [--server URL --token TOK] <project|task|workspace|changeset> ...
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	server := flag.String("server", envOr("BALLAST_SERVER", "http://localhost:8080"), "")
	token := flag.String("token", envOr("BALLAST_TOKEN", ""), "")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	fmt.Printf("ballast → %s (token %s...)\n", *server, short(*token))
	switch args[0] {
	case "project", "task", "workspace", "changeset", "runner":
		fmt.Println("see API docs; full subcommands land with Milestone 9 wiring.")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("ballast [--server URL --token TOK] project|task|workspace|changeset|runner ...")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func short(s string) string {
	if len(s) > 6 {
		return s[:6]
	}
	return s
}
