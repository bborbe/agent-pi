// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prompts

import (
	_ "embed"
)

//go:embed workflow.md
var workflow string

//go:embed output-format.md
var outputFormat string

// BuildInstructions assembles the full agent prompt from embedded modules.
func BuildInstructions() string {
	return workflow + "\n\n" + outputFormat
}
