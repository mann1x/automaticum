// Package run executes one run: an arm on a task, once, on a lane. It
// prepares the workspace, probes the endpoint, starts the runner in the
// lane's container, takes in what it prints, enforces the limits, collects
// the artefacts, runs the oracle and decides the verdict.
package run

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"

	"github.com/mann1x/automaticum/internal/task"
)

// Protocol is the version of the contract between the harness and the
// runner: the job file in, JSON lines out. See runner/PROTOCOL.md.
const Protocol = 6

// Paths inside the lane's container.
const (
	containerWorkspace = "/workspace"
	containerJob       = "/job"
	containerOut       = "/out"
	jobFile            = "job.json"
)

// RunnerCommand starts the runner in a lane image.
var RunnerCommand = []string{"node", "/opt/automaticum/runner/runner.mjs", containerJob + "/" + jobFile}

// Job is what the runner is given.
type Job struct {
	Protocol  int             `json:"protocol"`
	RunID     string          `json:"run_id"`
	SessionID string          `json:"session_id"`
	Arm       json.RawMessage `json:"arm"` // the resolved arm
	Endpoint  JobEndpoint     `json:"endpoint"`
	Prompt    string          `json:"prompt"`
	// DefaultAnswer is what the scripted user says when the agent asks.
	DefaultAnswer string `json:"default_answer"`
	// User is the task's scripted user (040 §6.2, protocol 2); absent, the
	// runner approves every tool and answers DefaultAnswer.
	User *task.User `json:"user,omitempty"`
	// WorkspaceGit asks the runner to make the workspace a git repository,
	// the seed its first commit, before the session starts.
	WorkspaceGit bool   `json:"workspace_git,omitempty"`
	Workspace    string `json:"workspace"`
	Out          string `json:"out"`
	// WallS is the limit the runner enforces itself by stopping the agent.
	WallS int `json:"wall_s"`
	// SDKRelease is the version the installed SDK package must be: that of
	// the build the arm names.
	SDKRelease string `json:"sdk_release"`
}

// JobEndpoint is the model server as the runner reaches it.
type JobEndpoint struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
}

// End is the runner's last line.
type End struct {
	// Outcome is completed, refused (it never started the agent), invalid
	// (an integrity check failed), wall_limit, guard_stop or error.
	Outcome  string   `json:"outcome"`
	Reason   string   `json:"reason"`
	Problems []string `json:"problems"`
	// AgentEnd is how the agent ended, as the SDK says it.
	AgentEnd       string          `json:"agent_end"`
	ClaimedSuccess *bool           `json:"claimed_success"`
	Usage          json.RawMessage `json:"usage"`
	SDK            struct {
		Release       string `json:"release"`
		PackageSHA256 string `json:"package_sha256"`
	} `json:"sdk"`
	// ReadBack is "compared" when the resolved configuration was compared
	// with the arm, or says why it was not.
	ReadBack string `json:"read_back"`
	Requests int    `json:"requests"`
	// AgentRequestsAnswered is how many of the agent's requests (chat
	// requests offering tools) the server answered; nil from a runner
	// that did not count them. AgentError is the agent's last error when
	// it ended in error.
	AgentRequestsAnswered *int   `json:"agent_requests_answered"`
	AgentError            string `json:"agent_error"`
	// Iterations is how many turns the agent took; Questions how many of
	// its questions the scripted user answered.
	Iterations *int `json:"iterations"`
	Questions  int  `json:"questions_answered"`
	// GuardStop is the guard trip that ended the run, one the arm's
	// guards.stop_on names (protocol 4, D99; the list since protocol 5, D100).
	GuardStop json.RawMessage `json:"guard_stop,omitempty"`
}

// Runner outcomes.
const (
	OutcomeCompleted = "completed"
	OutcomeRefused   = "refused"
	OutcomeInvalid   = "invalid"
	OutcomeWallLimit = "wall_limit"
	OutcomeError     = "error"
	// OutcomeGuardStop: a guard tripped that the arm's guards.stop_on names
	// (protocol 4).
	OutcomeGuardStop = "guard_stop"
)

// stream takes in the runner's output as it arrives. Every line is one JSON
// object with a kind "k": "event" for an SDK event, "runner" for the
// runner's own notes, "end" for the last line.
type stream struct {
	mu      sync.Mutex
	partial bytes.Buffer
	last    time.Time
	lines   int
	events  int
	bad     int
	end     *End
	onLine  func(line []byte)
	// A run is held between two model requests when the operator asks
	// (050 §4): the runner says "held" and "released". held is the time
	// held so far, heldAt when the current hold began, holds each one.
	held   time.Duration
	heldAt time.Time
	holds  []Hold
}

// Hold is one interval in which a run was held between two requests.
type Hold struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func newStream(now time.Time) *stream { return &stream{last: now} }

func (s *stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = time.Now()
	n := len(p)
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.partial.Write(p)
			return n, nil
		}
		line := p[:i]
		if s.partial.Len() > 0 {
			s.partial.Write(line)
			line = s.partial.Bytes()
		}
		s.line(line)
		s.partial.Reset()
		p = p[i+1:]
	}
}

func (s *stream) line(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	s.lines++
	var head struct {
		K string `json:"k"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		s.bad++
		return
	}
	switch head.K {
	case "runner":
		var n struct {
			T    time.Time `json:"t"`
			Note string    `json:"note"`
		}
		if json.Unmarshal(line, &n) == nil {
			switch {
			case n.Note == "held" && s.heldAt.IsZero():
				s.heldAt = n.T
			case n.Note == "released" && !s.heldAt.IsZero():
				s.held += n.T.Sub(s.heldAt)
				s.holds = append(s.holds, Hold{s.heldAt, n.T})
				s.heldAt = time.Time{}
			}
		}
	case "event":
		s.events++
	case "end":
		var e End
		if json.Unmarshal(line, &e) == nil {
			s.end = &e
		}
	}
	if s.onLine != nil {
		s.onLine(append([]byte(nil), line...))
	}
}

// idle is how long nothing has arrived. A held run is not idle.
func (s *stream) idle(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.heldAt.IsZero() {
		return 0
	}
	return now.Sub(s.last)
}

// heldFor is how long the run has been held in all, the hold it is in
// included; holding says whether it is in one.
func (s *stream) heldFor(now time.Time) (total time.Duration, holding bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heldAt.IsZero() {
		return s.held, false
	}
	return s.held + now.Sub(s.heldAt), true
}

func (s *stream) heldIntervals() []Hold {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Hold(nil), s.holds...)
}

func (s *stream) snapshot() (lines, events, bad int, end *End) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lines, s.events, s.bad, s.end
}
