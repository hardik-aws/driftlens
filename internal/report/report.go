// Package report renders drift results as a console table or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hardik-aws/driftlens/internal/model"
)

// Console writes an aligned table of results to w, followed by total elapsed time.
func Console(w io.Writer, results []model.Result, elapsed time.Duration) {
	sorted := append([]model.Result(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Dir < sorted[j].Dir })

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "DIR\tTOOL\tSTATUS\tDETAIL")
	for _, r := range sorted {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Dir, r.Tool, r.Status, detail(r))
	}
	tw.Flush()
	fmt.Fprintf(w, "\nCompleted in %s\n", formatElapsed(elapsed))
}

func detail(r model.Result) string {
	switch r.Status {
	case model.StatusError:
		return r.Err
	case model.StatusDrift:
		if len(r.Drifted) > 0 {
			lines := make([]string, len(r.Drifted))
			for i, rc := range r.Drifted {
				lines[i] = resourceLine(rc)
			}
			return strings.Join(lines, "; ")
		}
		return "drift detected"
	default:
		return ""
	}
}

// resourceLine renders one drifted resource: "<action> <address> (<attrs>)".
func resourceLine(rc model.ResourceChange) string {
	s := rc.Action + " " + rc.Address
	if len(rc.Changed) > 0 {
		s += " (" + strings.Join(rc.Changed, ", ") + ")"
	}
	return s
}

// JSON writes results plus total elapsed time as an indented JSON object to w.
func JSON(w io.Writer, results []model.Result, elapsed time.Duration) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		ElapsedSeconds float64        `json:"elapsed_seconds"`
		Results        []model.Result `json:"results"`
	}{elapsed.Seconds(), results})
}
