package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Campaign is an ordered set of batches with stop conditions (030 §6, 050
// §1), read from campaigns/<name>.toml. It is data: what it says is fixed
// when the campaign starts, and an edit to the file affects the next start
// only.
type Campaign struct {
	Name        string          `toml:"-" json:"name"`
	Description string          `toml:"description" json:"description,omitempty"`
	Batches     []CampaignBatch `toml:"batch" json:"batches"`
	// Stop ends the whole campaign: the batch in flight is stopped and no
	// later batch starts.
	Stop CampaignStop `toml:"stop" json:"stop"`
	// SHA256 is the hash of the file as it was read.
	SHA256 string `toml:"-" json:"sha256"`
}

// CampaignBatch is one batch of a campaign: Runs runs of every arm on every
// task, queued in Order on Lane.
type CampaignBatch struct {
	Name  string   `toml:"name" json:"name"`
	Arms  []string `toml:"arms" json:"arms"`
	Tasks []string `toml:"tasks" json:"tasks"`
	// Suites adds every task of each suite to Tasks.
	Suites []string `toml:"suites" json:"suites,omitempty"`
	Runs   int      `toml:"runs" json:"runs"`
	// Order is "interleaved" (the default) or "blocks" (050 §1).
	Order string `toml:"order" json:"order,omitempty"`
	Lane  string `toml:"lane" json:"lane,omitempty"`
	// WallS and IdleS are the batch's own limits (D107), in place of the
	// arm's and the task's; IdleS 0 is no idle limit.
	WallS *int `toml:"wall_s" json:"wall_s,omitempty"`
	IdleS *int `toml:"idle_s" json:"idle_s,omitempty"`
	// CaptureRequests saves every model request and response of the
	// batch's runs (D104); off unless the file says so.
	CaptureRequests bool `toml:"capture_requests" json:"capture_requests,omitempty"`
	// Stop ends this batch; the campaign goes on with the next.
	Stop CampaignStop `toml:"stop" json:"stop"`
}

// CampaignStop are the conditions that stop a batch or a campaign. None is
// set unless the file sets it: a zero means "not a condition".
type CampaignStop struct {
	// AfterConsecutivePass stops once every arm's last N finished runs all
	// passed.
	AfterConsecutivePass int `toml:"after_consecutive_pass" json:"after_consecutive_pass,omitempty"`
	// OnInvalid stops at the first INVALID run.
	OnInvalid bool `toml:"on_invalid" json:"on_invalid,omitempty"`
	// At stops at this time: no run starts after it, the one in flight
	// finishes.
	At time.Time `toml:"at" json:"at,omitzero"`
}

// LoadCampaign reads campaigns/<name>.toml. Unknown fields are errors.
func LoadCampaign(dir, name string) (*Campaign, error) {
	if !armNameRE.MatchString(name) {
		return nil, fmt.Errorf("campaign %q: a name is lower-case letters, digits, dots, dashes and underscores, starting with a letter or digit", name)
	}
	path := filepath.Join(dir, name+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("campaign %s: %w", name, err)
	}
	var c Campaign
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return nil, fmt.Errorf("campaign %s: %w", name, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("campaign %s: unknown fields: %s", name, strings.Join(keys, ", "))
	}
	c.Name = name
	sum := sha256.Sum256(data)
	c.SHA256 = hex.EncodeToString(sum[:])
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate says what is wrong with a campaign, all of it at once. Whether
// its arms, tasks and lanes exist is the daemon's to check, when it starts.
func (c *Campaign) Validate() error {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if len(c.Batches) == 0 {
		bad("no [[batch]]")
	}
	seen := map[string]bool{}
	for i, b := range c.Batches {
		where := fmt.Sprintf("batch %d", i+1)
		if b.Name != "" {
			where = "batch " + b.Name
		}
		switch {
		case b.Name == "":
			bad("%s: no name", where)
		case !armNameRE.MatchString(b.Name):
			bad("%s: a name is lower-case letters, digits, dots, dashes and underscores, starting with a letter or digit", where)
		case seen[b.Name]:
			bad("%s: named twice", where)
		}
		seen[b.Name] = true
		if len(b.Arms) == 0 {
			bad("%s: no arms", where)
		}
		if len(b.Tasks) == 0 && len(b.Suites) == 0 {
			bad("%s: no tasks", where)
		}
		if b.WallS != nil && *b.WallS <= 0 {
			bad("%s: wall_s must be positive", where)
		}
		if b.IdleS != nil && *b.IdleS < 0 {
			bad("%s: idle_s must be 0 (none) or positive", where)
		}
		if b.Runs < 1 {
			bad("%s: runs must be 1 or more", where)
		}
		if b.Order != "" && b.Order != "interleaved" && b.Order != "blocks" {
			bad("%s: order is \"interleaved\" or \"blocks\", not %q", where, b.Order)
		}
		if b.Stop.AfterConsecutivePass < 0 {
			bad("%s: stop.after_consecutive_pass must not be negative", where)
		}
	}
	if c.Stop.AfterConsecutivePass < 0 {
		bad("stop.after_consecutive_pass must not be negative")
	}
	if len(problems) > 0 {
		return fmt.Errorf("campaign %s: %s", c.Name, strings.Join(problems, "; "))
	}
	return nil
}
