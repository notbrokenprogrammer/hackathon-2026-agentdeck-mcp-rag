package web

import _ "embed"

// InteriorLayout is shared by the renderer and authoritative movement checks.
//
//go:embed src/interior/layout.json
var InteriorLayout []byte
