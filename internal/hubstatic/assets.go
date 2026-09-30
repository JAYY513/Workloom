package hubstatic

import "embed"

// Files contains the single-page Hub shell. Runtime data comes only from the Hub API.
//
//go:embed index.html app.js style.css
var Files embed.FS
