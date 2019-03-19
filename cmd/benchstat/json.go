// Copyright 2017 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/perf/benchstat"
	"io"
	"os"
	"unicode/utf8"
)
type Message struct {
	Name string
	Body string
	Time int64
}

// FormatJson appends a json formatting of the tables to w.
func FormatJson(w io.Writer, tables []*benchstat.Table) {
	var textTables [][]*textRow
	for _, t := range tables {
		textTables = append(textTables, toText(t))
	}

	var max []int
	for _, table := range textTables {
		for _, row := range table {
			if len(row.Cols) == 1 {
				// Header row
				continue
			}
			for len(max) < len(row.Cols) {
				max = append(max, 0)
			}
			for i, s := range row.Cols {
				n := utf8.RuneCountInString(s)
				if max[i] < n {
					max[i] = n
				}
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(textTables)
}

// A textRow is a row of printed text columns.
type textRow struct {
	Cols []string
}

func newTextRow(cols ...string) *textRow {
	return &textRow{Cols: cols}
}

func (r *textRow) add(col string) {
	r.Cols = append(r.Cols, col)
}

func (r *textRow) trim() {
	for len(r.Cols) > 0 && r.Cols[len(r.Cols)-1] == "" {
		r.Cols = r.Cols[:len(r.Cols)-1]
	}
}

// toText converts the Table to a textual grid of cells,
// which can then be printed in fixed-width output.
func toText(t *benchstat.Table) []*textRow {
	var textRows []*textRow
	switch len(t.Configs) {
	case 1:
		textRows = append(textRows, newTextRow("name", "value", t.Metric, "diff"))
	case 2:
		textRows = append(textRows, newTextRow("name", "old value", "old "+t.Metric, "diff", "new value", "new "+t.Metric, "diff", "delta", "significance"))
	default:
		row := newTextRow("name \\ " + t.Metric)
		row.Cols = append(row.Cols, t.Configs...)
		textRows = append(textRows, row)
	}

	var group string

	for _, row := range t.Rows {
		if row.Group != group {
			group = row.Group
			textRows = append(textRows, newTextRow(group))
		}

		text := newTextRow(row.Benchmark)
		for _, m := range row.Metrics {
			mean, unit, diff := Format(m)
			text.Cols = append(text.Cols, mean, unit, diff)
		}
		if len(t.Configs) == 2 {
			delta := row.Delta
			if delta == "~" {
				delta = "~"
			}
			text.Cols = append(text.Cols, delta)
			text.Cols = append(text.Cols, row.Note)
		}
		textRows = append(textRows, text)
	}
	for _, r := range textRows {
		r.trim()
	}
	return textRows
}

// Format returns a textual formatting of "Mean ±Diff" using scaler.
func Format(m *benchstat.Metrics) (string, string, string) {
	if m.Unit == "" {
		return "", "", ""
	}

	mean := fmt.Sprintf("%.f", m.Mean)
	diff := m.FormatDiff()
	if diff == "" {
		return mean, m.Unit, ""
	}
	return mean, m.Unit, diff
}