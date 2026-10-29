package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/run"
	"github.com/mann1x/automaticum/internal/store"
)

// CellSpec is a mode C cell's endpoint (065 §2, §3.1): a server the caller
// serves and owns, the model alias it serves, the arm values and basis
// facts only the caller knows, and how many lanes run the cell at once.
// automaticum probes the endpoint and gates on it, and never loads or
// unloads anything there.
type CellSpec struct {
	EndpointURL  string               `json:"endpoint_url"`
	Kind         string               `json:"endpoint_kind,omitempty"`
	ServedModel  string               `json:"served_model"`
	CallerValues *config.CallerValues `json:"caller_values,omitempty"`
	BasisFacts   *config.BasisFacts   `json:"basis_facts,omitempty"`
	// NLanes is how many lanes the caller asks for; never derived from the
	// server's slots. It is part of the basis.
	NLanes int `json:"lanes,omitempty"`
	// Lanes are the lanes the daemon chose, when the cell started.
	Lanes []string `json:"chosen_lanes,omitempty"`
}

// CellRecord is what a mode C run records of its cell, in its provenance
// as "cell".
type CellRecord struct {
	Kind         string               `json:"endpoint_kind"`
	ServedModel  string               `json:"served_model"`
	Lanes        int                  `json:"lanes"`
	CallerValues *config.CallerValues `json:"caller_values,omitempty"`
	BasisFacts   *config.BasisFacts   `json:"basis_facts,omitempty"`
	// Facts says how the basis facts were checked: "checked against
	// /props", or why they are the caller's word.
	Facts string     `json:"facts"`
	Props *run.Props `json:"props,omitempty"`
}

// The ways a cell's facts can stand.
const (
	factsChecked  = "checked against /props"
	factsAsserted = "caller-asserted"
)

// normalise fills a spec's defaults and says what is wrong with it.
func (c *CellSpec) normalise() error {
	if c.Kind == "" {
		c.Kind = "openai-compatible"
	}
	switch c.Kind {
	case "openai-compatible", "llamacpp", "ollama", "xollama":
	default:
		return fmt.Errorf("the endpoint kind is openai-compatible, llamacpp, ollama or xollama, not %q", c.Kind)
	}
	u, err := url.Parse(c.EndpointURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("the endpoint URL must be an http or https address")
	}
	if c.ServedModel == "" {
		return fmt.Errorf("mode C needs the model the endpoint serves")
	}
	if c.NLanes == 0 {
		c.NLanes = 1
	}
	if c.NLanes < 1 {
		return fmt.Errorf("lanes must be 1 or more")
	}
	return nil
}

// same says whether two specs ask for the same cell; the lanes the daemon
// chose are not compared.
func (c *CellSpec) same(o *CellSpec) bool {
	a, b := *c, *o
	a.Lanes, b.Lanes = nil, nil
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// cellEndpointName names a cell's endpoint after its batch.
func cellEndpointName(batch string) string { return "cell-" + strings.TrimPrefix(batch, "k-") }

// endpoint is the cell's endpoint as the lanes and gates see it.
func (c *CellSpec) endpoint(batch string) *config.Endpoint {
	return &config.Endpoint{Name: cellEndpointName(batch), Kind: c.Kind, BaseURL: c.EndpointURL, Capacity: c.NLanes}
}

// addCell registers a cell's endpoint for its lanes. It is called with
// d.mu held.
func (d *Daemon) addCell(batch string, c *CellSpec) error {
	return d.Local.AddCellEndpoint(c.endpoint(batch), c.Lanes)
}

// cellLanes chooses n lanes for a cell: the one asked for first, then the
// others by name. Fewer lanes than asked is a refusal: the lane count is
// part of the basis and is never lowered quietly.
func (d *Daemon) cellLanes(n int, asked string) ([]string, error) {
	var names []string
	for name := range d.Local.Lanes {
		if name != asked {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if asked != "" {
		if d.Local.Lanes[asked] == nil {
			return nil, fmt.Errorf("this installation has no lane %q", asked)
		}
		names = append([]string{asked}, names...)
	}
	if len(names) < n {
		return nil, fmt.Errorf("the cell asks for %d lanes and this installation has %d", n, len(names))
	}
	return names[:n], nil
}

// armFor loads an arm as the runs of batch use it: for a mode C cell, with
// the cell's endpoint, model and values.
func (d *Daemon) armFor(batch, name string) (*config.Arm, *CellSpec, error) {
	arm, err := config.LoadArm(d.Local.ArmsDir(), name)
	if err != nil {
		return nil, nil, err
	}
	d.mu.Lock()
	cell := d.ctl.Cells[batch]
	d.mu.Unlock()
	if cell == nil {
		return arm, nil, nil
	}
	if err := arm.ForCell(cellEndpointName(batch), cell.Kind, cell.ServedModel, cell.CallerValues); err != nil {
		return nil, nil, err
	}
	return arm, cell, nil
}

// checkCell reads what the cell's server says of itself and compares it
// with the caller's facts. It returns the record for the run, and why the
// run must not start when the server contradicts the caller.
func (d *Daemon) checkCell(ctx context.Context, batch string, cell *CellSpec, arm *config.Arm) (*CellRecord, string) {
	rec := &CellRecord{Kind: cell.Kind, ServedModel: cell.ServedModel, Lanes: cell.NLanes,
		CallerValues: cell.CallerValues, BasisFacts: cell.BasisFacts}
	ep := d.Local.Endpoint(cellEndpointName(batch))
	if ep == nil {
		return rec, "the cell's endpoint is not registered"
	}
	if run.Ollama(ep) {
		rec.Facts = factsAsserted + ": an " + ep.Kind + " endpoint has no /props"
		return rec, ""
	}
	hc := d.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	props, err := run.ReadProps(ctx, hc, ep, "")
	if err != nil {
		return rec, err.Error()
	}
	if props == nil {
		rec.Facts = factsAsserted + ": the server has no /props"
		return rec, ""
	}
	rec.Props, rec.Facts = props, factsChecked
	window := 0
	if arm.Model.ContextWindow != nil {
		window = *arm.Model.ContextWindow
	}
	if bad := run.Contradictions(props, cell.BasisFacts, window); len(bad) > 0 {
		return rec, "the server's /props contradicts the caller: " + strings.Join(bad, "; ")
	}
	return rec, ""
}

// startCell checks a mode C start request and chooses its lanes. It
// returns the lanes; the spec is normalised in place.
func (d *Daemon) startCell(ctx context.Context, req *StartRequest, names []string) ([]string, int, error) {
	c := req.Cell
	if req.Key == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("a mode C cell needs an idempotency key")
	}
	if len(names) != 1 {
		return nil, http.StatusBadRequest, fmt.Errorf("a mode C cell runs one arm")
	}
	if err := c.normalise(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	batch := "k-" + req.Key
	d.mu.Lock()
	old := d.ctl.Cells[batch]
	d.mu.Unlock()
	if old != nil {
		if !old.same(c) {
			return nil, http.StatusConflict, fmt.Errorf("the key names a cell with another endpoint, model, values, facts or lane count")
		}
		c.Lanes = slices.Clone(old.Lanes)
		return c.Lanes, 0, nil
	}
	// A key that already names a batch on an arm's own endpoint cannot
	// become a cell on another.
	if runs, err := d.State.ListRuns(ctx, store.RunFilter{Batch: batch}); err != nil {
		return nil, http.StatusInternalServerError, err
	} else if len(runs) > 0 {
		return nil, http.StatusConflict, fmt.Errorf("the key names a batch that is not a mode C cell")
	}
	queue, err := d.State.Queue(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if slices.ContainsFunc(queue, func(it store.QueueItem) bool { return it.Batch == batch }) {
		return nil, http.StatusConflict, fmt.Errorf("the key names a batch that is not a mode C cell")
	}
	lanes, err := d.cellLanes(c.NLanes, req.Lane)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}
	c.Lanes = lanes
	return lanes, 0, nil
}
