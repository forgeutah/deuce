// Package extension embeds the Pi extensions Deuce provisions into each
// workspace container. Currently just the ask-user tool that backs the
// awaiting-input lifecycle (KTD15).
package extension

import _ "embed"

// AskUser is the source of the ask-user Pi extension, provisioned to
// ~/.pi/agent/extensions/ask-user.ts in the container so Pi auto-discovers it.
//
//go:embed ask-user.ts
var AskUser string

// AskUserFilename is the auto-discovery filename used in the container.
const AskUserFilename = "ask-user.ts"
