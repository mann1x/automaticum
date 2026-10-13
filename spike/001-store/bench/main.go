//go:build lance

// Command bench runs the measurements of the store spike (spec 080 §7.3).
// It is throwaway: it exists to produce the numbers in ../REPORT.md.
//
// Each subcommand prints one JSON object. Paths are flags; nothing about
// this installation is compiled in.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: bench <vectors|state|range|chunks|text|regex|archive-half|embed|dedup> [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "vectors":
		err = cmdVectors(os.Args[2:])
	case "state":
		err = cmdState(os.Args[2:])
	case "range":
		err = cmdRange(os.Args[2:])
	case "chunks":
		err = cmdChunks(os.Args[2:])
	case "text":
		err = cmdText(os.Args[2:])
	case "regex":
		err = cmdRegex(os.Args[2:])
	case "archive-half":
		err = cmdArchiveHalf(os.Args[2:])
	case "embed":
		err = cmdEmbed(os.Args[2:])
	case "dedup":
		err = cmdDedup(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func emit(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// latency summarises a set of durations in milliseconds.
type latency struct {
	N                  int
	P50, P95, P99, Max float64
}

func summarise(ds []time.Duration) latency {
	if len(ds) == 0 {
		return latency{}
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	at := func(q float64) float64 {
		i := int(q * float64(len(s)-1))
		return float64(s[i].Microseconds()) / 1000
	}
	return latency{N: len(s), P50: at(0.50), P95: at(0.95), P99: at(0.99), Max: at(1)}
}

// memory reads the process's current and peak resident set, in MiB.
func memory() (rss, peak float64) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "VmRSS:":
			rss = kb / 1024
		case "VmHWM:":
			peak = kb / 1024
		}
	}
	return rss, peak
}

// dirSize returns the bytes under a directory.
func dirSize(dir string) int64 {
	var total int64
	var walk func(string)
	walk = func(d string) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				walk(d + "/" + e.Name())
				continue
			}
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
	}
	walk(dir)
	return total
}

// freeGB returns the free space of the filesystem holding dir, in GiB.
func freeGB(dir string) float64 { return float64(freeBytes(dir)) / (1 << 30) }
