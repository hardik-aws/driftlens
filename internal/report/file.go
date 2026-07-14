package report

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"
	"github.com/hardik-aws/driftlens/internal/model"
)

// summary tallies results by status.
type summary struct {
	Clean, Drift, Error int
}

func summarize(results []model.Result) summary {
	var s summary
	for _, r := range results {
		switch r.Status {
		case model.StatusClean:
			s.Clean++
		case model.StatusDrift:
			s.Drift++
		case model.StatusError:
			s.Error++
		}
	}
	return s
}

func sortedCopy(results []model.Result) []model.Result {
	out := append([]model.Result(nil), results...)
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// ModuleSection is one module's slice of the report: its identity, overall
// status, and the per-resource changes (if any) found within it.
type ModuleSection struct {
	Dir       string
	Tool      string
	Status    string                 // clean | drift | error
	Resources []model.ResourceChange // populated for drifted modules
	Err       string                 // populated for errored modules
}

// Count returns the number of drifted resources in the section.
func (m ModuleSection) Count() int { return len(m.Resources) }

// Search returns a lowercased haystack (dir, tool, status, resource addresses
// and diffs, error text) the HTML report filters against client-side.
func (m ModuleSection) Search() string {
	var b strings.Builder
	b.WriteString(m.Dir)
	b.WriteByte(' ')
	b.WriteString(m.Tool)
	b.WriteByte(' ')
	b.WriteString(m.Status)
	b.WriteByte(' ')
	b.WriteString(m.Err)
	for _, c := range m.Resources {
		b.WriteByte(' ')
		b.WriteString(c.Address)
		b.WriteByte(' ')
		b.WriteString(c.Action)
		b.WriteByte(' ')
		b.WriteString(strings.Join(c.Changed, " "))
		b.WriteByte(' ')
		b.WriteString(c.Detail)
	}
	return strings.ToLower(b.String())
}

// sections groups results into one section per module, sorted by directory and
// with each module's drifted resources sorted by address.
func sections(results []model.Result) []ModuleSection {
	var out []ModuleSection
	for _, r := range sortedCopy(results) {
		sec := ModuleSection{Dir: r.Dir, Tool: r.Tool}
		switch r.Status {
		case model.StatusDrift:
			sec.Status = "drift"
			rc := append([]model.ResourceChange(nil), r.Drifted...)
			sort.Slice(rc, func(i, j int) bool { return rc[i].Address < rc[j].Address })
			sec.Resources = rc
		case model.StatusError:
			sec.Status = "error"
			sec.Err = r.Err
		default:
			sec.Status = "clean"
		}
		out = append(out, sec)
	}
	return out
}

// badgeClass maps a status/action word to its CSS badge class.
func badgeClass(status string) string {
	switch status {
	case "create", "update", "replace", "delete", "read":
		return "act-" + status
	case "clean":
		return "st-clean"
	case "error":
		return "st-error"
	default:
		return "st-drift"
	}
}

type svgNode struct {
	Name, Status, Anchor string
	X, Y, W, H           float64
}

type svgEdge struct {
	X1, Y1, X2, Y2 float64
}

type svgLayout struct {
	Width, Height float64
	Nodes         []svgNode
	Edges         []svgEdge
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a module dir into a stable HTML id fragment.
func slug(s string) string {
	s = strings.ToLower(s)
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// buildSVGLayout positions nodes in columns by topological level and draws an
// edge from each node's left edge to each dependency's right edge. Returns nil
// when there is no view.
func buildSVGLayout(v *DependencyView) *svgLayout {
	if v == nil {
		return nil
	}
	const (
		colW  = 240.0
		rowH  = 34.0
		nodeW = 200.0
		nodeH = 24.0
		mX    = 16.0
		mY    = 16.0
	)
	type pt struct{ x, y float64 }
	center := map[string]pt{}
	byName := map[string]DepNode{}
	for _, n := range v.Nodes {
		byName[n.Name] = n
	}
	var nodes []svgNode
	maxRows := 0
	for lvl, names := range v.Levels {
		if len(names) > maxRows {
			maxRows = len(names)
		}
		for i, name := range names {
			x := mX + float64(lvl)*colW
			y := mY + float64(i)*rowH
			anchor := ""
			if d := byName[name].Dir; d != "" {
				anchor = "#mod-" + slug(d)
			}
			nodes = append(nodes, svgNode{
				Name: name, Status: byName[name].Status, Anchor: anchor,
				X: x, Y: y, W: nodeW, H: nodeH,
			})
			center[name] = pt{x, y + nodeH/2}
		}
	}
	var edges []svgEdge
	for _, n := range v.Nodes {
		from, ok := center[n.Name]
		if !ok {
			continue
		}
		for _, dep := range n.DependsOn {
			to, ok := center[dep]
			if !ok {
				continue
			}
			edges = append(edges, svgEdge{X1: from.x, Y1: from.y, X2: to.x + nodeW, Y2: to.y})
		}
	}
	return &svgLayout{
		Width:  mX*2 + float64(len(v.Levels))*colW,
		Height: mY*2 + float64(maxRows)*rowH,
		Nodes:  nodes,
		Edges:  edges,
	}
}

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"badge": badgeClass,
	"lower": strings.ToLower,
	"join":  strings.Join,
	"slug":  slug,
}).Parse(htmlSource))

// HTML renders an HTML drift report to w.
func HTML(w io.Writer, results []model.Result, generatedAt time.Time, elapsed time.Duration, view *DependencyView) error {
	return htmlTmpl.Execute(w, struct {
		Generated string
		Elapsed   string
		Summary   summary
		Total     int
		Sections  []ModuleSection
		Dep       *DependencyView
		SVG       *svgLayout
	}{
		Generated: generatedAt.Format("2006-01-02 15:04:05 MST"),
		Elapsed:   formatElapsed(elapsed),
		Summary:   summarize(results),
		Total:     len(results),
		Sections:  sections(results),
		Dep:       view,
		SVG:       buildSVGLayout(view),
	})
}

// actionFill returns the RGB fill for an action/status badge in the PDF.
func actionFill(status string) (int, int, int) {
	switch status {
	case "create":
		return 220, 252, 231
	case "update", "drift":
		return 254, 243, 199
	case "replace", "delete", "error":
		return 254, 226, 226
	case "read":
		return 224, 242, 254
	case "clean":
		return 220, 252, 231
	default:
		return 241, 245, 249
	}
}

// PDF renders a PDF drift report to w using a pure-Go engine, organised into
// one band per module.
func PDF(w io.Writer, results []model.Result, generatedAt time.Time, elapsed time.Duration, view *DependencyView) error {
	pdf := fpdf.New("L", "mm", "A4", "") // landscape for the wide diff column
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 12)
	pdf.AddPage()

	const pageW = 273.0 // usable width on A4 landscape with 12mm margins

	// Title band
	pdf.SetFillColor(15, 23, 42)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 18)
	pdf.CellFormat(pageW, 13, "  Terraform Drift Report", "", 1, "L", true, 0, "")
	pdf.SetTextColor(15, 23, 42)
	pdf.Ln(3)

	s := summarize(results)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(100, 116, 139)
	pdf.CellFormat(0, 5, fmt.Sprintf("Generated %s  -  %d module(s) scanned  -  Completed in %s",
		generatedAt.Format("2006-01-02 15:04:05 MST"), len(results), formatElapsed(elapsed)), "", 1, "L", false, 0, "")
	pdf.Ln(2)

	// Summary chips
	pdf.SetTextColor(15, 23, 42)
	chips := []struct {
		label string
		n     int
		key   string
	}{{"CLEAN", s.Clean, "clean"}, {"DRIFT", s.Drift, "drift"}, {"ERROR", s.Error, "error"}}
	for _, c := range chips {
		r, g, b := actionFill(c.key)
		pdf.SetFillColor(r, g, b)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(40, 8, fmt.Sprintf(" %d  %s", c.n, c.label), "1", 0, "L", true, 0, "")
		pdf.CellFormat(6, 8, "", "", 0, "L", false, 0, "")
	}
	pdf.Ln(13)

	const detailLineH = 3.6
	for _, sec := range sections(results) {
		drawModuleHeader(pdf, sec, pageW)

		switch sec.Status {
		case "error":
			pdf.SetFont("Courier", "", 8)
			pdf.SetFillColor(254, 242, 242)
			pdf.SetTextColor(153, 27, 27)
			pdf.MultiCell(pageW, 5, sec.Err, "1", "L", true)
			pdf.SetTextColor(15, 23, 42)
		case "clean":
			pdf.SetFont("Helvetica", "I", 9)
			pdf.SetTextColor(22, 101, 52)
			pdf.CellFormat(pageW, 7, "  No drift detected", "1", 1, "L", false, 0, "")
			pdf.SetTextColor(15, 23, 42)
		default: // drift
			drawResourceTable(pdf, sec, detailLineH)
		}
		pdf.Ln(5)
	}

	drawDependencySection(pdf, view, pageW)

	return pdf.Output(w)
}

// drawDependencySection appends the dependency graph to the PDF: a per-level
// listing (fpdf can't lay out dozens of nodes as a drawn graph) followed by a
// status table. No-op when v is nil.
func drawDependencySection(pdf *fpdf.Fpdf, v *DependencyView, pageW float64) {
	if v == nil {
		return
	}
	pdf.AddPage()
	pdf.SetTextColor(15, 23, 42)
	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(pageW, 10, "  Dependency Graph", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	// Per-level listing
	for i, lv := range v.Levels {
		x, y := pdf.GetXY()
		pdf.SetFont("Helvetica", "B", 9)
		pdf.CellFormat(24, 6, fmt.Sprintf("Level %d", i), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetXY(x+24, y)
		pdf.MultiCell(pageW-24, 6, strings.Join(lv, ", "), "", "L", false)
	}
	pdf.Ln(3)

	// Status table: Module | Status | Depends on | Required by = 273mm
	widths := []float64{70, 24, 90, 89}
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetFillColor(226, 232, 240)
	pdf.SetTextColor(15, 23, 42)
	for i, h := range []string{"Module", "Status", "Depends on", "Required by"} {
		pdf.CellFormat(widths[i], 6, " "+h, "1", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	for _, n := range v.Nodes {
		dep := strings.Join(n.DependsOn, ", ")
		req := strings.Join(n.RequiredBy, ", ")
		lines := pdf.SplitLines([]byte(n.Name+" "+dep+" "+req), widths[2])
		h := float64(len(lines)) * 4
		if h < 6 {
			h = 6
		}
		if _, pageH := pdf.GetPageSize(); pdf.GetY()+h > pageH-12 {
			pdf.AddPage()
		}
		x, y := pdf.GetXY()
		pdf.SetFont("Helvetica", "", 7)
		pdf.MultiCell(widths[0], 4, n.Name, "1", "L", false)
		pdf.SetXY(x+widths[0], y)
		r, g, b := actionFill(n.Status)
		pdf.SetFillColor(r, g, b)
		pdf.SetFont("Helvetica", "B", 7)
		pdf.CellFormat(widths[1], h, strings.ToUpper(n.Status), "1", 0, "C", true, 0, "")
		pdf.SetFont("Helvetica", "", 7)
		pdf.MultiCell(widths[2], 4, dep, "1", "L", false)
		pdf.SetXY(x+widths[0]+widths[1]+widths[2], y)
		pdf.MultiCell(widths[3], 4, req, "1", "L", false)
		pdf.SetXY(x, y+h)
	}
}

// drawModuleHeader renders a module's directory/tool/status header band.
func drawModuleHeader(pdf *fpdf.Fpdf, sec ModuleSection, pageW float64) {
	if _, pageH := pdf.GetPageSize(); pdf.GetY()+24 > pageH-12 {
		pdf.AddPage()
	}
	pdf.SetFillColor(241, 245, 249)
	pdf.SetDrawColor(203, 213, 225)
	pdf.SetFont("Helvetica", "B", 11)
	x, y := pdf.GetXY()
	pdf.CellFormat(pageW, 9, "  "+sec.Dir, "1", 1, "L", true, 0, "")

	// status badge + tool/count, drawn over the header band's right side
	label := strings.ToUpper(sec.Status)
	if sec.Status == "drift" {
		label = fmt.Sprintf("%s  -  %d resource(s)", label, sec.Count())
	}
	r, g, b := actionFill(sec.Status)
	pdf.SetXY(x+pageW-70, y+1.5)
	pdf.SetFillColor(r, g, b)
	pdf.SetFont("Helvetica", "B", 8)
	pdf.CellFormat(68, 6, sec.Tool+"  "+label+" ", "", 0, "R", true, 0, "")
	pdf.SetXY(x, y+9)
}

// drawResourceTable renders a drifted module's per-resource rows.
func drawResourceTable(pdf *fpdf.Fpdf, sec ModuleSection, detailLineH float64) {
	widths := []float64{26, 82, 165} // Action | Resource | Plan detail = 273
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetFillColor(226, 232, 240)
	pdf.SetTextColor(15, 23, 42)
	for i, h := range []string{"Action", "Resource", "Plan detail"} {
		pdf.CellFormat(widths[i], 6, " "+h, "1", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	const addrLineH = 4.0
	for _, c := range sec.Resources {
		detail := planDetailText(c)
		// Measure both wrapping columns; the row is as tall as the taller one so
		// neither overflows into its neighbour.
		pdf.SetFont("Courier", "", 6.5)
		detailLines := pdf.SplitLines([]byte(detail), widths[2])
		pdf.SetFont("Helvetica", "", 8)
		addrLines := pdf.SplitLines([]byte(" "+c.Address), widths[1])
		h := float64(len(detailLines)) * detailLineH
		if ah := float64(len(addrLines)) * addrLineH; ah > h {
			h = ah
		}
		if h < 7 {
			h = 7
		}
		if _, pageH := pdf.GetPageSize(); pdf.GetY()+h > pageH-12 {
			pdf.AddPage()
		}
		x, y := pdf.GetXY()
		r, g, b := actionFill(c.Action)
		pdf.SetFillColor(r, g, b)
		pdf.SetFont("Helvetica", "B", 7)
		pdf.CellFormat(widths[0], h, strings.ToUpper(c.Action), "1", 0, "C", true, 0, "")
		pdf.SetFont("Helvetica", "", 8)
		pdf.MultiCell(widths[1], addrLineH, " "+c.Address, "1", "L", false)
		pdf.SetXY(x+widths[0]+widths[1], y)
		pdf.SetFont("Courier", "", 6.5)
		pdf.MultiCell(widths[2], detailLineH, detail, "1", "L", false)
		pdf.SetXY(x, y+h)
	}
}

// planDetailText picks what to show in a report's Plan-detail column: the
// human-readable diff when available, else the JSON-derived changed attribute
// names, else a note that nothing was captured. The HTML template applies the
// same precedence inline.
func planDetailText(c model.ResourceChange) string {
	switch {
	case c.Detail != "":
		return c.Detail
	case len(c.Changed) > 0:
		return "changed: " + strings.Join(c.Changed, ", ")
	default:
		return "no diff captured"
	}
}

// WriteReports writes report files to dir per mode (none|html|pdf|both),
// creating dir if needed. Returns the paths written.
func WriteReports(dir, mode string, results []model.Result, generatedAt time.Time, elapsed time.Duration, view *DependencyView) ([]string, error) {
	type job struct {
		name string
		fn   func(io.Writer, []model.Result, time.Time, time.Duration, *DependencyView) error
	}
	var jobs []job
	switch mode {
	case "none":
		return nil, nil
	case "html":
		jobs = []job{{"drift-report.html", HTML}}
	case "pdf":
		jobs = []job{{"drift-report.pdf", PDF}}
	case "both":
		jobs = []job{{"drift-report.html", HTML}, {"drift-report.pdf", PDF}}
	default:
		return nil, fmt.Errorf("invalid report mode %q: want none, html, pdf, or both", mode)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var paths []string
	for _, j := range jobs {
		p := filepath.Join(dir, j.name)
		f, err := os.Create(p)
		if err != nil {
			return paths, err
		}
		if err := j.fn(f, results, generatedAt, elapsed, view); err != nil {
			f.Close()
			return paths, fmt.Errorf("write %s: %w", j.name, err)
		}
		if err := f.Close(); err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

const htmlSource = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Terraform Drift Report</title>
<style>
  :root {
    --border:#e2e8f0; --bg:#f8fafc; --ink:#0f172a; --muted:#64748b;
    --clean:#16a34a; --drift:#d97706; --error:#dc2626;
  }
  * { box-sizing:border-box; }
  body { font-family:-apple-system,Segoe UI,Roboto,sans-serif; margin:0;
         background:#f1f5f9; color:var(--ink); line-height:1.5; }
  .wrap { max-width:1100px; margin:0 auto; padding:2rem 1.5rem 4rem; }
  header.hero { background:linear-gradient(120deg,#0f172a,#1e293b); color:#fff;
                border-radius:14px; padding:1.5rem 1.75rem; margin-bottom:1.5rem; }
  header.hero h1 { margin:0 0 .25rem; font-size:1.6rem; letter-spacing:-.01em; }
  header.hero .meta { color:#94a3b8; font-size:.85rem; }
  .cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr));
           gap:1rem; margin-bottom:2rem; }
  .card { background:#fff; border:1px solid var(--border); border-radius:12px;
          padding:1rem 1.25rem; box-shadow:0 1px 2px rgba(15,23,42,.04); }
  .card .n { font-size:1.9rem; font-weight:800; font-variant-numeric:tabular-nums; line-height:1; }
  .card .l { font-size:.72rem; color:var(--muted); text-transform:uppercase;
             letter-spacing:.06em; margin-top:.35rem; font-weight:600; }
  .card.clean .n { color:var(--clean); }
  .card.drift .n { color:var(--drift); }
  .card.error .n { color:var(--error); }
  .module { background:#fff; border:1px solid var(--border); border-radius:12px;
            margin-bottom:1.25rem; overflow:hidden; box-shadow:0 1px 2px rgba(15,23,42,.04); }
  .module > .mhead { display:flex; align-items:center; gap:.75rem;
            padding:.85rem 1.1rem; border-bottom:1px solid var(--border);
            background:var(--bg); }
  .module.s-clean > .mhead { border-left:4px solid var(--clean); }
  .module.s-drift > .mhead { border-left:4px solid var(--drift); }
  .module.s-error > .mhead { border-left:4px solid var(--error); }
  .mhead .dir { font-family:ui-monospace,Menlo,monospace; font-weight:700; font-size:.95rem; }
  .mhead .tool { color:var(--muted); font-size:.78rem; }
  .mhead .spacer { flex:1; }
  .mbody { padding:0; }
  table { border-collapse:collapse; width:100%; font-size:.88rem; }
  th,td { border-bottom:1px solid var(--border); padding:.6rem .9rem;
          text-align:left; vertical-align:top; }
  th { background:#fff; color:var(--muted); font-size:.72rem; text-transform:uppercase;
       letter-spacing:.04em; }
  tr:last-child td { border-bottom:none; }
  code { font-family:ui-monospace,Menlo,monospace; font-size:.82rem; }
  .badge { display:inline-block; min-width:56px; text-align:center; font-size:.7rem;
           font-weight:700; text-transform:uppercase; letter-spacing:.03em;
           padding:.18rem .45rem; border-radius:5px; white-space:nowrap; }
  .act-create { background:#dcfce7; color:#166534; }
  .act-update { background:#fef3c7; color:#92400e; }
  .act-replace { background:#fee2e2; color:#991b1b; }
  .act-delete { background:#fee2e2; color:#991b1b; }
  .act-read { background:#e0f2fe; color:#075985; }
  .st-clean { background:#dcfce7; color:#166534; }
  .st-drift { background:#fef3c7; color:#92400e; }
  .st-error { background:#fee2e2; color:#991b1b; }
  pre.detail { margin:0; font-family:ui-monospace,Menlo,monospace; font-size:.75rem;
               line-height:1.45; white-space:pre-wrap; word-break:break-word; color:var(--ink); }
  .errbox { margin:0; padding:.9rem 1.1rem; font-family:ui-monospace,Menlo,monospace;
            font-size:.8rem; white-space:pre-wrap; word-break:break-word;
            background:#fef2f2; color:#991b1b; }
  .noinfo { padding:.9rem 1.1rem; color:var(--clean); font-style:italic; font-size:.9rem; }
  .nodiff { color:var(--muted); font-style:italic; font-size:.82rem; }
  .attrs { color:var(--muted); font-size:.85rem; }
  .attrs::before { content:""; }
  .controls { display:flex; gap:.75rem; align-items:center; flex-wrap:wrap; margin-bottom:1.25rem; }
  #search { flex:1; min-width:220px; padding:.6rem .85rem; font-size:.9rem;
            border:1px solid var(--border); border-radius:9px; background:#fff; color:var(--ink); }
  #search:focus { outline:2px solid #2563eb; outline-offset:1px; border-color:#2563eb; }
  .filters { display:flex; gap:.4rem; }
  .fbtn { padding:.5rem .85rem; font-size:.8rem; font-weight:600; cursor:pointer;
          border:1px solid var(--border); border-radius:8px; background:#fff; color:var(--muted); }
  .fbtn:hover { background:var(--bg); }
  .fbtn.active { background:var(--ink); color:#fff; border-color:var(--ink); }
  .fbtn:focus-visible { outline:2px solid #2563eb; outline-offset:1px; }
  .empty { color:var(--muted); font-style:italic; padding:1rem 0; }
  .depgraph { background:#fff; border:1px solid var(--border); border-radius:12px;
              margin-bottom:1.5rem; padding:1rem 1.1rem; box-shadow:0 1px 2px rgba(15,23,42,.04); }
  .depgraph h2 { margin:0 0 .75rem; font-size:1.1rem; }
  .depscroll { overflow:auto; max-height:480px; border:1px solid var(--border); border-radius:8px; }
  .depscroll svg { display:block; }
  /* SVG rects need fill (they ignore the badge classes' CSS background); keep
     fills light so the dark node text stays legible. */
  .depgraph .node rect { fill:#f1f5f9; stroke:#cbd5e1; stroke-width:1; }
  .depgraph .st-clean rect { fill:#dcfce7; }
  .depgraph .st-drift rect { fill:#fef3c7; }
  .depgraph .st-error rect { fill:#fee2e2; }
  .depgraph .edge { stroke:#94a3b8; stroke-width:1; opacity:.35; }
  .depgraph text { font-family:ui-monospace,Menlo,monospace; font-size:10px; fill:var(--ink); }
  table.deptable { margin-top:1rem; }
  .dep-row[hidden] { display:none; }
  .module[hidden], .res-row[hidden] { display:none; }
  .res-cell { width:1%; white-space:nowrap; }
  @media (prefers-reduced-motion: reduce) { * { animation:none!important; transition:none!important; } }
</style>
</head>
<body>
  <div class="wrap">
    <header class="hero">
      <h1>Terraform Drift Report</h1>
      <div class="meta">Generated {{ .Generated }} &middot; {{ .Total }} module(s) scanned &middot; Completed in {{ .Elapsed }} &middot; Clean: {{ .Summary.Clean }} | Drift: {{ .Summary.Drift }} | Error: {{ .Summary.Error }}</div>
    </header>
    <div class="cards">
      <div class="card clean"><div class="n">{{ .Summary.Clean }}</div><div class="l">Clean</div></div>
      <div class="card drift"><div class="n">{{ .Summary.Drift }}</div><div class="l">Drift</div></div>
      <div class="card error"><div class="n">{{ .Summary.Error }}</div><div class="l">Error</div></div>
    </div>

    <div class="controls">
      <input id="search" type="search" placeholder="Search modules, resources, diffs…" autocomplete="off" aria-label="Search report">
      <div class="filters" role="group" aria-label="Filter by status">
        <button class="fbtn active" data-filter="all">All</button>
        <button class="fbtn" data-filter="drift">Drift</button>
        <button class="fbtn" data-filter="error">Error</button>
        <button class="fbtn" data-filter="clean">Clean</button>
      </div>
    </div>
    <p id="empty" class="empty" hidden>No modules match.</p>

    {{- if .SVG }}
    <section class="depgraph">
      <h2>Dependency graph</h2>
      <div class="depscroll">
        <svg width="{{ .SVG.Width }}" height="{{ .SVG.Height }}" viewBox="0 0 {{ .SVG.Width }} {{ .SVG.Height }}" xmlns="http://www.w3.org/2000/svg">
          {{- range .SVG.Edges }}
          <line class="edge" x1="{{ .X1 }}" y1="{{ .Y1 }}" x2="{{ .X2 }}" y2="{{ .Y2 }}"/>
          {{- end }}
          {{- range .SVG.Nodes }}
          <a{{ if .Anchor }} href="{{ .Anchor }}"{{ end }}>
            <g class="node badge {{ badge .Status }}">
              <rect x="{{ .X }}" y="{{ .Y }}" width="{{ .W }}" height="{{ .H }}" rx="5"/>
              <text x="{{ .X }}" y="{{ .Y }}" dx="6" dy="16">{{ .Name }}</text>
            </g>
          </a>
          {{- end }}
        </svg>
      </div>
      <table class="deptable">
        <thead><tr><th>Module</th><th>Status</th><th>Depends on</th><th>Required by</th></tr></thead>
        <tbody>
        {{- range .Dep.Nodes }}
          <tr class="dep-row" data-search="{{ lower (printf "%s %s %s %s" .Name .Status (join .DependsOn " ") (join .RequiredBy " ")) }}">
            <td>{{ if .Dir }}<a href="#mod-{{ slug .Dir }}"><code>{{ .Name }}</code></a>{{ else }}<code>{{ .Name }}</code>{{ end }}</td>
            <td><span class="badge {{ badge .Status }}">{{ .Status }}</span></td>
            <td class="attrs">{{ join .DependsOn ", " }}</td>
            <td class="attrs">{{ join .RequiredBy ", " }}</td>
          </tr>
        {{- end }}
        </tbody>
      </table>
    </section>
    {{- end }}

    {{- range .Sections }}
    {{- $dir := .Dir }}
    <section id="mod-{{ slug .Dir }}" class="module s-{{ .Status }}" data-status="{{ .Status }}"{{ if ne .Status "drift" }} data-search="{{ .Search }}"{{ end }}>
      <div class="mhead">
        <span class="dir">{{ .Dir }}</span>
        <span class="tool">{{ .Tool }}</span>
        <span class="spacer"></span>
        {{- if eq .Status "drift" }}
        <span class="badge st-drift">{{ .Count }} drifted</span>
        {{- else if eq .Status "error" }}
        <span class="badge st-error">error</span>
        {{- else }}
        <span class="badge st-clean">clean</span>
        {{- end }}
      </div>
      <div class="mbody">
        {{- if eq .Status "drift" }}
        <table>
          <thead><tr><th class="res-cell">Action</th><th>Resource</th><th>Plan detail</th></tr></thead>
          <tbody>
          {{- range .Resources }}
            <tr class="res-row" data-search="{{ lower (printf "%s %s %s %s %s" $dir .Address .Action (join .Changed " ") .Detail) }}">
              <td class="res-cell"><span class="badge {{ badge .Action }}">{{ .Action }}</span></td>
              <td><code>{{ .Address }}</code></td>
              <td>
                {{- if .Detail }}<pre class="detail">{{ .Detail }}</pre>
                {{- else if .Changed }}<span class="attrs">changed: {{ join .Changed ", " }}</span>
                {{- else }}<span class="nodiff">no diff captured</span>{{ end }}</td>
            </tr>
          {{- end }}
          </tbody>
        </table>
        {{- else if eq .Status "error" }}
        <pre class="errbox">{{ .Err }}</pre>
        {{- else }}
        <div class="noinfo">No drift detected</div>
        {{- end }}
      </div>
    </section>
    {{- end }}
  </div>
  <script>
  (function () {
    var search = document.getElementById('search');
    var empty = document.getElementById('empty');
    var btns = Array.prototype.slice.call(document.querySelectorAll('.fbtn'));
    var mods = Array.prototype.slice.call(document.querySelectorAll('.module'));
    var depRows = Array.prototype.slice.call(document.querySelectorAll('.dep-row'));
    var status = 'all';

    function apply() {
      var q = search.value.trim().toLowerCase();
      var shown = 0;
      mods.forEach(function (m) {
        if (status !== 'all' && m.getAttribute('data-status') !== status) {
          m.hidden = true;
          return;
        }
        var rows = Array.prototype.slice.call(m.querySelectorAll('tr.res-row'));
        if (rows.length) {
          // drifted module: filter individual resource rows
          var vis = 0;
          rows.forEach(function (r) {
            var ok = q === '' || (r.getAttribute('data-search') || '').indexOf(q) !== -1;
            r.hidden = !ok;
            if (ok) vis++;
          });
          m.hidden = vis === 0;
        } else {
          // clean/error module: match its own text
          m.hidden = !(q === '' || (m.getAttribute('data-search') || '').indexOf(q) !== -1);
        }
        if (!m.hidden) shown++;
      });
      depRows.forEach(function (r) {
        var ok = q === '' || (r.getAttribute('data-search') || '').indexOf(q) !== -1;
        r.hidden = !ok;
      });
      empty.hidden = shown !== 0;
    }

    search.addEventListener('input', apply);
    btns.forEach(function (b) {
      b.addEventListener('click', function () {
        btns.forEach(function (x) { x.classList.remove('active'); });
        b.classList.add('active');
        status = b.getAttribute('data-filter');
        apply();
      });
    });
  })();
  </script>
</body>
</html>
`
