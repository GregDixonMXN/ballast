// Package migrations embeds the append-only SQL schema so the server
// binary carries it. Store applies these on boot.
package migrations

import "embed"

// FS holds *.sql in filename order at apply time.
//
//go:embed *.sql
var FS embed.FS
