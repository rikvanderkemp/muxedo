// SPDX-License-Identifier: MIT
package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rikvanderkemp/muxedo/internal/process"
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

func benchmarkExitModel(panelCount int) Model {
	panels := make([]*process.Panel, panelCount)
	statuses := make([]string, panelCount)
	for i := range panelCount {
		panels[i] = process.New(fmt.Sprintf("panel-%02d", i+1), "echo one", "", ".")
		switch i % 3 {
		case 0:
			statuses[i] = "stopping..."
		case 1:
			statuses[i] = "stopped"
		default:
			statuses[i] = "kill command failed: context deadline exceeded"
		}
	}

	model := NewModel(panels)
	model.width = 160
	model.height = 48
	model.exiting = true
	model.exitStatuses = statuses
	model.exitReceived = make([]bool, panelCount)
	model.exitCompleted = panelCount / 2
	return model
}

func BenchmarkWrapExiting(b *testing.B) {
	for _, panelCount := range []int{8, 32} {
		b.Run(fmt.Sprintf("panels_%d", panelCount), func(b *testing.B) {
			model := benchmarkExitModel(panelCount)
			b.ReportAllocs()
			for b.Loop() {
				_ = model.wrapExiting("body")
			}
		})
	}
}

func BenchmarkUpdateExitProgressMsg(b *testing.B) {
	b.Run("non_final", func(b *testing.B) {
		model := benchmarkExitModel(8)
		msg := exitProgressMsg{panelIdx: 4, panelName: "panel-05", status: "stopped"}
		b.ReportAllocs()
		for b.Loop() {
			model.exitCompleted = 3
			model.exitDelayPending = false
			model.exitStatuses[msg.panelIdx] = "stopping..."
			model.exitReceived[4] = false
			next, cmd := model.Update(msg)
			model = next.(Model)
			if model.exitCompleted != 4 {
				b.Fatalf("exitCompleted = %d, want 4", model.exitCompleted)
			}
			if cmd != nil {
				b.Fatal("expected nil cmd for non-final exitProgressMsg")
			}
		}
	})

	b.Run("final_schedules_delay", func(b *testing.B) {
		model := benchmarkExitModel(8)
		msg := exitProgressMsg{panelIdx: 7, panelName: "panel-08", status: "stopped"}
		for i := 0; i < 7; i++ {
			model.exitReceived[i] = true
		}
		b.ReportAllocs()
		for b.Loop() {
			model.exitCompleted = 7
			model.exitDelayPending = false
			model.exitStatuses[msg.panelIdx] = "stopping..."
			model.exitReceived[msg.panelIdx] = false
			next, cmd := model.Update(msg)
			model = next.(Model)
			if model.exitCompleted != 8 {
				b.Fatalf("exitCompleted = %d, want 8", model.exitCompleted)
			}
			if cmd == nil {
				b.Fatal("expected delay cmd for final exitProgressMsg")
			}
		}
	})
}
