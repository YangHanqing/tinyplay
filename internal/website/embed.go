package website

import _ "embed"

// ControllerJS is the canonical website-page controller injected by both
// desktop shells. Keep a single source here — do not fork copies into Swift/Go
// UI packages.
//
//go:embed controller.js
var ControllerJS string
