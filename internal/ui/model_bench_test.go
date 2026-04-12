// SPDX-License-Identifier: MIT
package ui

import (
	"strings"
	"testing"

	"muxedo/internal/process"
)

func BenchmarkSelectionLinesForPanelLive(b *testing.B) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 120
	model.height = 40
	model.panelRunning = func(*process.Panel) bool { return true }

	var out strings.Builder
	for i := 0; i < 48; i++ {
		if i%3 == 0 {
			out.WriteString("\x1b[32m")
		}
		out.WriteString("line ")
		out.WriteString(strings.Repeat("x", 40))
		if i%3 == 0 {
			out.WriteString("\x1b[0m")
		}
		if i < 47 {
			out.WriteByte('\n')
		}
	}
	view := process.DisplayState{Output: out.String()}
	model.displayForView = func(*process.Panel) process.DisplayState {
		return view
	}

	b.ReportAllocs()
	for b.Loop() {
		_, _ = model.selectionLinesForPanel(0, model.activePaneLineCapacity())
	}
}
