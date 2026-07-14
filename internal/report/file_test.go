package report

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hardik-aws/driftlens/internal/dag"
	"github.com/hardik-aws/driftlens/internal/model"
)

var genAt = time.Date(2026, 6, 16, 9, 30, 0, 0, time.UTC)

func TestHTMLRendersDependencyGraph(t *testing.T) {
	g, err := dag.Parse(`digraph {
        "vpc" ;
        "eks-cluster" ;
        "eks-cluster" -> "vpc";
}`)
	if err != nil {
		t.Fatal(err)
	}
	view := BuildDependencyView(g, sample(), "")
	var buf bytes.Buffer
	if err := HTML(&buf, sample(), genAt, 90*time.Second, view); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"depgraph", "<svg", "deptable", "Depends on", "Required by",
		"eks-cluster", "vpc",
		// SVG rects need an explicit fill (they ignore the badge CSS background);
		// without a status fill rule the nodes render as illegible black boxes.
		".st-drift rect", ".st-clean rect",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dependency-graph HTML missing %q", want)
		}
	}
}

func TestPDFWithDependencyView(t *testing.T) {
	g, err := dag.Parse(`digraph { "vpc" ; "eks" ; "eks" -> "vpc"; }`)
	if err != nil {
		t.Fatal(err)
	}
	view := BuildDependencyView(g, sample(), "")
	var buf bytes.Buffer
	if err := PDF(&buf, sample(), genAt, 90*time.Second, view); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty PDF with dependency view")
	}
}

func TestHTMLNoDependencyGraphWhenNil(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, sample(), genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	// `.depgraph` always appears in the stylesheet; assert the rendered section
	// marker is absent, per the codebase's substring-collision convention.
	if strings.Contains(buf.String(), `class="depgraph"`) {
		t.Error("nil view must not emit a dependency-graph section")
	}
}

func TestHTMLContainsRowsAndSummary(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, sample(), genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Completed in",            // total elapsed time in hero
		"Resource", "Plan detail", // per-module resource table headers
		"svc-a", "svc-b", "svc-c", // each module gets a section
		"update", "aws_s3_bucket.x", // drifted resource: action + address
		"replace", "aws_instance.y", // second drifted resource
		"init exit 1: boom", "2026", // error message + generated timestamp
		"~ acl = &#34;private&#34; -&gt; &#34;public&#34;", // raw plan diff, html-escaped
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q\n%s", want, out)
		}
	}
	// one <section> per module: svc-a, svc-b, svc-c
	if n := strings.Count(out, `class="module`); n != 3 {
		t.Errorf("want 3 module sections, got %d", n)
	}
	// one resource row per drifted resource: svc-b has 2
	if n := strings.Count(out, `class="res-row"`); n != 2 {
		t.Errorf("want 2 resource rows, got %d", n)
	}
	// the error module renders its message in an error box
	if n := strings.Count(out, `class="errbox"`); n != 1 {
		t.Errorf("want 1 error box, got %d", n)
	}
	// the clean module renders a no-drift note
	if n := strings.Count(out, `class="noinfo"`); n != 1 {
		t.Errorf("want 1 no-drift note, got %d", n)
	}
	// summary counts: 1 clean, 1 drift, 1 error
	if !strings.Contains(out, "Drift: 1") {
		t.Errorf("HTML missing drift summary count\n%s", out)
	}
}

func TestHTMLHasSearchAndFilter(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, sample(), genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`id="search"`,       // search input
		`data-filter="all"`, // status filter buttons
		`data-filter="drift"`,
		`data-filter="error"`,
		`data-filter="clean"`,
		`data-status="drift"`,                       // sections carry their status for filtering
		`data-search="svc-b aws_s3_bucket.x update`, // each drift row is searchable by address/action/diff
		"<script>",                                  // client-side filtering logic
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q\n%s", want, out)
		}
	}
	// each module section is filterable by status
	if n := strings.Count(out, `data-status="`); n != 3 {
		t.Errorf("want 3 filterable sections, got %d", n)
	}
	// search granularity is per-resource: 2 drift rows + 2 module-level (clean+error)
	if n := strings.Count(out, `data-search="`); n != 4 {
		t.Errorf("want 4 searchable nodes (2 rows + clean + error), got %d", n)
	}
}

func TestHTMLShowsChangedAttrsWhenNoDetail(t *testing.T) {
	rs := []model.Result{
		{Dir: "waf", Tool: "terragrunt", Status: model.StatusDrift, Drifted: []model.ResourceChange{
			// drift captured from plan JSON but no human-readable diff block matched
			{Address: "aws_wafv2_web_acl.waf", Action: "update", Changed: []string{"rule", "tags"}},
		}},
	}
	var buf bytes.Buffer
	if err := HTML(&buf, rs, genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "rule, tags") {
		t.Errorf("HTML should list changed attrs as fallback\n%s", out)
	}
	if strings.Contains(out, "no diff captured") {
		t.Errorf("changed attrs known; should not show 'no diff captured'\n%s", out)
	}
	// searchable by attribute name
	if !strings.Contains(out, `data-search="waf aws_wafv2_web_acl.waf update rule tags`) {
		t.Errorf("changed attrs should be part of the row's data-search\n%s", out)
	}
}

func TestHTMLNoDiffWhenNothingCaptured(t *testing.T) {
	rs := []model.Result{
		{Dir: "svc", Tool: "terraform", Status: model.StatusDrift, Drifted: []model.ResourceChange{
			{Address: "aws_instance.y", Action: "replace"}, // no Detail, no Changed
		}},
	}
	var buf bytes.Buffer
	if err := HTML(&buf, rs, genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no diff captured") {
		t.Errorf("with no detail and no changed attrs, expected 'no diff captured'\n%s", buf.String())
	}
}

func TestHTMLEscapesContent(t *testing.T) {
	var buf bytes.Buffer
	rs := []model.Result{{Dir: "<script>alert(1)</script>", Tool: "terraform", Status: model.StatusError, Err: "<b>bad</b>"}}
	if err := HTML(&buf, rs, genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// the injected payload must not survive as live markup anywhere
	for _, bad := range []string{"<script>alert", "alert(1)</script>", "<b>bad</b>"} {
		if strings.Contains(out, bad) {
			t.Errorf("HTML did not escape user content: found %q", bad)
		}
	}
}

func TestPDFWritesValidHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := PDF(&buf, sample(), genAt, 90*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Fatalf("output is not a PDF (prefix %q)", buf.Bytes()[:min(4, len(buf.Bytes()))])
	}
	if buf.Len() < 500 {
		t.Errorf("PDF suspiciously small: %d bytes", buf.Len())
	}
}

func TestWriteReportsModes(t *testing.T) {
	cases := map[string][]string{
		"none": nil,
		"html": {"drift-report.html"},
		"pdf":  {"drift-report.pdf"},
		"both": {"drift-report.html", "drift-report.pdf"},
	}
	for mode, wantFiles := range cases {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			paths, err := WriteReports(dir, mode, sample(), genAt, 90*time.Second, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				got = append(got, e.Name())
			}
			sort.Strings(got)
			sort.Strings(wantFiles)
			if strings.Join(got, ",") != strings.Join(wantFiles, ",") {
				t.Fatalf("files = %v, want %v", got, wantFiles)
			}
			if len(paths) != len(wantFiles) {
				t.Errorf("returned %d paths, want %d", len(paths), len(wantFiles))
			}
			for _, p := range paths {
				if filepath.Dir(p) != dir {
					t.Errorf("path %q not in dir %q", p, dir)
				}
			}
		})
	}
}

func TestWriteReportsRejectsBadMode(t *testing.T) {
	if _, err := WriteReports(t.TempDir(), "xml", sample(), genAt, 90*time.Second, nil); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
