// Copyright 2015 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Benchstat computes and compares statistics about benchmarks.
//
// Usage:
//
//	benchstat [-delta-test name] [-geomean] [-output name] old.txt [new.txt] [more.txt ...]
//
// Each input file should contain the concatenated output of a number
// of runs of ``go test -bench.'' For each different benchmark listed in an input file,
// benchstat computes the mean, minimum, and maximum run time,
// after removing outliers using the interquartile range rule.
//
// If invoked on a single input file, benchstat prints the per-benchmark statistics
// for that file.
//
// If invoked on a pair of input files, benchstat adds to the output a column
// showing the statistics from the second file and a column showing the
// percent change in mean from the first to the second file.
// Next to the percent change, benchstat shows the p-value and sample
// sizes from a test of the two distributions of benchmark times.
// Small p-values indicate that the two distributions are significantly different.
// If the test indicates that there was no significant change between the two
// benchmarks (defined as p > 0.05), benchstat displays a single ~ instead of
// the percent change.
//
// The -delta-test option controls which significance test is applied:
// utest (Mann-Whitney U-test), ttest (two-sample Welch t-test), or none.
// The default is the U-test, sometimes also referred to as the Wilcoxon rank
// sum test.
//
// If invoked on more than two input files, benchstat prints the per-benchmark
// statistics for all the files, showing one column of statistics for each file,
// with no column for percent change or statistical significance.
//
// The -output option causes benchstat to print the results as an either text,
// HTML, or json table.
//
// The -raw option causes benchstat to print results as unscaled values.
//
// Example
//
// Suppose we collect benchmark results from running ``go test -bench=Encode''
// five times before and after a particular change.
//
// The file old.txt contains:
//
//	BenchmarkGobEncode   	100	  13552735 ns/op	  56.63 MB/s
//	BenchmarkJSONEncode  	 50	  32395067 ns/op	  59.90 MB/s
//	BenchmarkGobEncode   	100	  13553943 ns/op	  56.63 MB/s
//	BenchmarkJSONEncode  	 50	  32334214 ns/op	  60.01 MB/s
//	BenchmarkGobEncode   	100	  13606356 ns/op	  56.41 MB/s
//	BenchmarkJSONEncode  	 50	  31992891 ns/op	  60.65 MB/s
//	BenchmarkGobEncode   	100	  13683198 ns/op	  56.09 MB/s
//	BenchmarkJSONEncode  	 50	  31735022 ns/op	  61.15 MB/s
//
// The file new.txt contains:
//
//	BenchmarkGobEncode   	 100	  11773189 ns/op	  65.19 MB/s
//	BenchmarkJSONEncode  	  50	  32036529 ns/op	  60.57 MB/s
//	BenchmarkGobEncode   	 100	  11942588 ns/op	  64.27 MB/s
//	BenchmarkJSONEncode  	  50	  32156552 ns/op	  60.34 MB/s
//	BenchmarkGobEncode   	 100	  11786159 ns/op	  65.12 MB/s
//	BenchmarkJSONEncode  	  50	  31288355 ns/op	  62.02 MB/s
//	BenchmarkGobEncode   	 100	  11628583 ns/op	  66.00 MB/s
//	BenchmarkJSONEncode  	  50	  31559706 ns/op	  61.49 MB/s
//	BenchmarkGobEncode   	 100	  11815924 ns/op	  64.96 MB/s
//	BenchmarkJSONEncode  	  50	  31765634 ns/op	  61.09 MB/s
//
// The order of the lines in the file does not matter, except that the
// output lists benchmarks in order of appearance.
//
// If run with just one input file, benchstat summarizes that file:
//
//	$ benchstat old.txt
//	name        time/op
//	GobEncode   13.6ms ± 1%
//	JSONEncode  32.1ms ± 1%
//	$
//
// If run with two input files, benchstat summarizes and compares:
//
//	$ benchstat old.txt new.txt
//	name        old time/op  new time/op  delta
//	GobEncode   13.6ms ± 1%  11.8ms ± 1%  -13.31% (p=0.016 n=4+5)
//	JSONEncode  32.1ms ± 1%  31.8ms ± 1%     ~    (p=0.286 n=4+5)
//	$
//
// Note that the JSONEncode result is reported as
// statistically insignificant instead of a -0.93% delta.
//
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"golang.org/x/perf/benchstat"
)

const (
  _text = "text"
  _html = "html"
  _json = "json"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: benchstat [options] old.txt [new.txt] [more.txt ...]\n")
	fmt.Fprintf(os.Stderr, "options:\n")
	flag.PrintDefaults()
	os.Exit(2)
}

var (
	flagDeltaTest = flag.String("delta-test", "utest", "significance `test` to apply to delta: utest, ttest, or none")
	flagAlpha     = flag.Float64("alpha", 0.05, "consider change significant if p < `α`")
	flagGeomean   = flag.Bool("geomean", false, "print the geometric mean of each file")
	flagSplit     = flag.String("split", "pkg,goos,goarch", "split benchmarks by `labels`")
	flagUnits     = flag.String("units", "b,allocs,ns", "prints only the given units")
	flagOnlyDiff  = flag.Bool("diff", false, "prints only if differences appears")
	flagRawValues  = flag.Bool("raw", false, "the raw unscaled values are printed")
	flagOutput = flag.String("output", "text", "output format: text (default), html, or json")
)

var deltaTestNames = map[string]benchstat.DeltaTest{
	"none":   benchstat.NoDeltaTest,
	"u":      benchstat.UTest,
	"u-test": benchstat.UTest,
	"utest":  benchstat.UTest,
	"t":      benchstat.TTest,
	"t-test": benchstat.TTest,
	"ttest":  benchstat.TTest,
}

var unitNames = map[string]string{
	"b":      "B/op",
	"ns":     "ns/op",
	"allocs": "allocs/op",
}

var outputFormatNames = map[string]string{
	"text": _text,
	"html": _html,
	"json": _json,
}

func filterDiff(tables []*benchstat.Table) []*benchstat.Table {
	for i := 0; i < len(tables); i++ {
		for j := 0; j < len(tables[i].Rows); j++ {
			if tables[i].Rows[j].Change == 0 {
				tables[i].Rows = append(tables[i].Rows[:j], tables[i].Rows[j+1:]...)
				j--
			}
		}
		if len(tables[i].Rows) == 0 {
			tables = append(tables[:i], tables[i+1:]...)
			i--
		}
	}
	return tables
}

func main() {
	log.SetPrefix("benchstat: ")
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()
	deltaTest := deltaTestNames[strings.ToLower(*flagDeltaTest)]
	if flag.NArg() < 1 || deltaTest == nil {
		flag.Usage()
	}

	outputFormat := outputFormatNames[strings.ToLower(*flagOutput)]

	c := &benchstat.Collection{
		Alpha:      *flagAlpha,
		AddGeoMean: *flagGeomean,
		DeltaTest:  deltaTest,
	}
	if *flagSplit != "" {
		c.SplitBy = strings.Split(*flagSplit, ",")
	}

	units := []string{}
	if *flagUnits != "" {
		unitSet := strings.Split(*flagUnits, ",")
		for _, u := range unitSet {
			if n, ok := unitNames[strings.ToLower(u)]; ok {
				units = append(units, n)
			}
		}
	}

	for _, file := range flag.Args() {
		data, err := ioutil.ReadFile(file)
		if err != nil {
			log.Fatal(err)
		}
		c.AddConfig(file, data)
	}

	if len(units) > 0 {
		c.Units = units
	}

	tables := c.Tables()

	if *flagRawValues {
		for _, table := range tables {
			for _, row := range table.Rows {
				row.Scaler = NewNoopScaler(row.Metrics[0].Unit)
			}
		}
	}

	if *flagOnlyDiff {
		tables = filterDiff(tables)
		if len(tables) == 0 && outputFormat == _text {
			os.Stdout.WriteString("No significant differences in benchmarks\n")
			return
		}
	}

	var buf bytes.Buffer
	switch outputFormat {
	case _html:
		buf.WriteString(htmlStyle)
		benchstat.FormatHTML(&buf, tables)
	case _json:
		FormatJson(&buf, tables)
	case _text:
		benchstat.FormatText(&buf, tables)
	}
	os.Stdout.Write(buf.Bytes())
}

var htmlStyle = `<style>
.benchstat { border-collapse: collapse; }
.benchstat th:nth-child(1) { text-align: left; }
.benchstat tbody td:nth-child(1n+2):not(.note) { text-align: right; padding: 0em 1em; }
.benchstat tr:not(.configs) th { border-top: 1px solid #666; border-bottom: 1px solid #ccc; }
.benchstat .nodelta { text-align: center !important; }
.benchstat .better td.delta { font-weight: bold; }
.benchstat .worse td.delta { font-weight: bold; color: #c00; }
</style>
`
