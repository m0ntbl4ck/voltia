// Package data embeds the dataset so the binary seeds itself without files on disk.
package data

import "embed"

//go:embed meters.yaml readings.csv events.csv
var FS embed.FS
