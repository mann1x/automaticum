package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// cohortLs lists the declared cohorts (D120).
func cohortLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cohort ls", flag.ExitOnError)
	_, dial := addClient(fs)
	parse(fs, args)
	c, err := dial()
	if err != nil {
		return err
	}
	var out []daemon.CohortSummary
	if err := c.do(ctx, "GET", "/v1/cohorts", nil, &out); err != nil {
		return err
	}
	if c.json {
		return printJSON(out)
	}
	w := table()
	fmt.Fprintln(w, "COHORT\tVARIABLE\tCELLS\tDECLARED")
	for _, co := range out {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", co.Name, co.Variable, co.Cells, human.Time(co.Created))
	}
	return w.Flush()
}

// cohortShow puts a cohort's cells side by side: each cell's headline and
// value of the variable, the basis facts in which they differ, and whether
// the cohort is confounded.
func cohortShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cohort show", flag.ExitOnError)
	_, dial := addClient(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum cohort show <name>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.CohortView
	if err := c.do(ctx, "GET", "/v1/cohorts/"+url.PathEscape(rest[0]), nil, &v); err != nil {
		return err
	}
	if c.json {
		return printJSON(v)
	}
	label := "clean: only the variable differs"
	if v.Confounded {
		label = "CONFOUNDED: " + strings.Join(v.ConfoundedBy, ", ") + " also differ"
	}
	fmt.Printf("cohort %s, variable %s (basis: %s): %d cells, %s\n", v.Name, v.Variable, strings.Join(v.VariableFacts, ", "), len(v.Cells), label)
	w := table()
	fmt.Fprintln(w, "\nCELL\tARM\tVALUE\tHEADLINE\tCOUNTS\tCOMPLETE")
	for _, cell := range v.Cells {
		head := cell.Metric + " " + human.Number(cell.Headline)
		if cell.Rescored {
			head += " (rescored)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\n", cell.Batch, cell.Arm, cell.Value, head, countsText(cell.Counts), cell.Complete)
	}
	w.Flush()
	if len(v.Differs) > 0 {
		fmt.Println("\nthe cells differ in:")
		for _, f := range v.Differs {
			mark := ""
			if f.Variable {
				mark = " (the variable)"
			}
			fmt.Printf("  %s%s\n", f.Name, mark)
			for _, cell := range v.Cells {
				fmt.Printf("    %s: %s\n", cell.Batch, strings.Join(f.Values[cell.Batch], " | "))
			}
		}
	}
	for _, wn := range v.Warnings {
		fmt.Printf("warning: %s\n", wn)
	}
	return nil
}

// exportCohort writes every cell of a cohort under <out>/<batch>/, the
// cohort's view as <out>/cohort.json, and one row per run of every cell,
// with every per-run number as a column, as <out>/runs.jsonl (and runs.csv
// with --format csv) (065 §5).
func exportCohort(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export cohort", flag.ExitOnError)
	_, dial := addClient(fs)
	out := fs.String("out", "", "the directory to write into")
	format := fs.String("format", "jsonl", "jsonl, or csv to also write runs.csv; Parquet is not built")
	dry := fs.Bool("dry-run", false, "say which cells would be written; write nothing")
	rest := parse(fs, args)
	if len(rest) != 1 || *out == "" {
		return errors.New("usage: automaticum export cohort <name> --out D [--format jsonl|csv]")
	}
	if err := checkFormat(*format); err != nil {
		return err
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var v daemon.CohortView
	if err := c.do(ctx, "GET", "/v1/cohorts/"+url.PathEscape(rest[0]), nil, &v); err != nil {
		return err
	}
	if *dry {
		for _, cell := range v.Cells {
			fmt.Printf("would write %s\n", filepath.Join(*out, cell.Batch))
		}
		fmt.Printf("would write %s and %s\n", filepath.Join(*out, "cohort.json"), filepath.Join(*out, "runs.jsonl"))
		return nil
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	var rows []daemon.CellRow
	var lead [][]string
	var lines strings.Builder
	codes := map[string]int{}
	for _, cell := range v.Cells {
		dir := filepath.Join(*out, cell.Batch)
		code, err := writeBatchCell(ctx, c, cell.Batch, dir, *format)
		if err != nil {
			return fmt.Errorf("cell %s: %w", cell.Batch, err)
		}
		codes[cell.Batch] = code
		var cv daemon.CellView
		if err := c.do(ctx, "GET", "/v1/batches/"+url.PathEscape(cell.Batch)+"/cell", nil, &cv); err != nil {
			return err
		}
		for _, r := range cv.Rows {
			rows = append(rows, r)
			lead = append(lead, []string{v.Name, cell.Batch, cell.Arm, cell.ArmID, cell.Value})
			m := map[string]any{"cohort": v.Name, "variable": v.Variable, "batch": cell.Batch, "arm": cell.Arm, "arm_id": cell.ArmID, "value": cell.Value,
				"run_id": r.RunID, "task": r.Task, "verdict": r.Verdict, "counted_as": r.CountedAs}
			for k, x := range r.Metrics {
				m[k] = x
			}
			for k, n := range r.Findings {
				m["finding."+k] = n
			}
			b, _ := json.Marshal(m)
			lines.Write(append(b, '\n'))
		}
	}
	if err := writeWhole(filepath.Join(*out, "runs.jsonl"), []byte(lines.String())); err != nil {
		return err
	}
	if *format == "csv" {
		if err := writeRowsCSV(filepath.Join(*out, "runs.csv"), rows, []string{"cohort", "batch", "arm", "arm_id", "value"}, func(i int) []string { return lead[i] }); err != nil {
			return err
		}
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	if err := writeWhole(filepath.Join(*out, "cohort.json"), append(data, '\n')); err != nil {
		return err
	}
	if c.json {
		return printJSON(map[string]any{"cohort": v.Name, "out": *out, "cells": codes, "confounded": v.Confounded})
	}
	fmt.Printf("cohort %s: %d cells, %d runs written to %s; confounded %v\n", v.Name, len(codes), len(rows), *out, v.Confounded)
	return nil
}
