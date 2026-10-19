// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prompts_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-pi/pkg/prompts"
)

var _ = Describe("BuildInstructions", func() {
	It("contains the workflow module", func() {
		Expect(prompts.BuildInstructions()).To(ContainSubstring("task execution agent"))
	})

	It("contains the output-format module", func() {
		Expect(prompts.BuildInstructions()).To(ContainSubstring("single JSON object"))
	})

	It("places the workflow module before the output-format module", func() {
		instructions := prompts.BuildInstructions()
		Expect(strings.Index(instructions, "task execution agent")).
			To(BeNumerically("<", strings.Index(instructions, "single JSON object")))
	})
})
