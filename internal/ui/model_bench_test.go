// SPDX-License-Identifier: MIT
package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

func BenchmarkModelViewLiveGrid(b *testing.B) {
	panels := []*process.Panel{
		process.New("one", "echo one", "", "."),
		process.New("two", "echo two", "", "."),
		process.New("three", "echo three", "", "."),
		process.New("four", "echo four", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = 0
	model.width = 160
	model.height = 48
	model.panelRunning = func(*process.Panel) bool { return true }
	view := process.DisplayState{Output: "ready\n" + strings.Repeat("line\n", 20)}
	model.displayForView = func(*process.Panel) process.DisplayState {
		return view
	}
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		b.Fatal("live rendering should not build history viewports")
		return nil
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = model.View()
	}
}

func BenchmarkUpdateMouseWheelLargeHistory(b *testing.B) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 120
	model.height = 40
	model.panelRunning = func(*process.Panel) bool { return true }
	history := benchmarkHistoryLines(5000)
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return history
	}
	msg := mouseWheel(1, 1, tea.MouseWheelUp)

	b.ReportAllocs()
	for b.Loop() {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
}

func BenchmarkUpdatePrintableKeyInsert(b *testing.B) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }
	model.sendInput = func(*process.Panel, []byte) error { return nil }
	msg := keyRune('a')

	b.ReportAllocs()
	for b.Loop() {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
}

func BenchmarkViewportForPanelScrolledLargeHistory(b *testing.B) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 120
	model.height = 40
	model.panelRunning = func(*process.Panel) bool { return true }
	history := benchmarkHistoryLines(5000)
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return history
	}
	model.ensureScrollState()
	model.scrollOffsets[0] = 100

	b.ReportAllocs()
	for b.Loop() {
		_ = model.viewportForPanel(0, 39)
	}
}

func BenchmarkSelectionLinesForPanelHistory(b *testing.B) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 120
	model.height = 40
	model.panelRunning = func(*process.Panel) bool { return true }
	history := benchmarkHistoryLines(5000)
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return history
	}
	model.ensureScrollState()
	model.scrollOffsets[0] = 100
	model.selections[0] = panelSelection{Active: true, Source: selectSourceHistory}

	b.ReportAllocs()
	for b.Loop() {
		_, _ = model.selectionLinesForPanel(0, model.activePaneLineCapacity())
	}
}

func benchmarkHistoryLines(n int) []process.HistoryLine {
	lines := make([]process.HistoryLine, n)
	for i := range lines {
		lines[i] = process.HistoryLine{
			ID:   uint64(i + 1),
			Text: "line " + strconv.Itoa(i),
		}
	}
	return lines
}
