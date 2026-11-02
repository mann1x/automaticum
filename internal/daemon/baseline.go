package daemon

import (
	"encoding/json"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/store"
)

// ArmBaseline is the arm a run's arm is set against in `run show` (070
// §1.1, D130): the campaign's baseline arm where the run's batch belongs
// to a campaign that names one (a campaign's cohort shares it), else the
// arm the run's arm extends; and the fields in which the run's arm, as the
// run recorded it, differs from it as its file is now.
type ArmBaseline struct {
	Arm     string   `json:"arm"`
	Source  string   `json:"source"`
	Differs []string `json:"differs"`
	// Note says why there is no baseline, or that the run is the
	// baseline's own.
	Note string `json:"note,omitempty"`
}

// armBaseline finds a run's baseline and what differs from it.
func (d *Daemon) armBaseline(rec *store.Run) *ArmBaseline {
	var a config.Arm
	if json.Unmarshal(rec.Arm, &a) != nil || a.Name == "" {
		return nil
	}
	dir := d.Local.ArmsDir()
	b := &ArmBaseline{Differs: []string{}}
	d.mu.Lock()
	for name, c := range d.ctl.Campaigns {
		for _, cb := range c.Batches {
			if cb.Batch == rec.Batch && c.Def != nil && c.Def.Baseline != "" {
				b.Arm, b.Source = c.Def.Baseline, "campaign "+name
			}
		}
	}
	d.mu.Unlock()
	if b.Arm == "" {
		parent := config.ArmParent(dir, a.Name)
		if parent == "" {
			b.Note = "the arm's file extends no other arm (or is gone) and no campaign names a baseline"
			return b
		}
		b.Arm, b.Source = parent, "parent"
	}
	if b.Arm == a.Name {
		b.Note = "this run is of the baseline arm"
		return b
	}
	base, err := config.LoadArm(dir, b.Arm)
	if err != nil {
		b.Note = "the baseline's file cannot be read now: " + err.Error()
		return b
	}
	b.Differs = config.Diff(base, &a)
	return b
}
