package run

import (
	"fmt"
	"strings"

	"github.com/mann1x/automaticum/internal/store"
)

// Facts is everything a verdict is decided from. Collecting the facts and
// deciding are kept apart so the rules of spec 020 §3 can be read, and
// tested, in one place.
type Facts struct {
	// Skipped is why the run never started; empty if it did.
	Skipped string
	// Invalid lists the integrity checks that failed.
	Invalid []string
	// Aborted is why no verdict could be produced; empty otherwise.
	Aborted string
	// Limit is the limit that ended the run ("wall" or "idle"), LimitOwner
	// who set it (arm, task) and LimitBy who enforced it (runner, harness).
	Limit, LimitOwner, LimitBy string
	// OracleRan is true when the primary oracle gave an answer, and
	// OraclePassed is that answer.
	OracleRan    bool
	OraclePassed bool
	// GuardStop is the guard trip that ended the run, for an arm that stops
	// at the first one (D99).
	GuardStop string
}

// Decide applies the verdict rules and returns the verdict with its reason.
//
// The order is the point. A run that never started is SKIPPED whatever else
// is known. A failed integrity check makes the result uncountable, so INVALID
// comes before anything the oracle says. A run that produced no verdict is
// ABORTED. After that the oracle speaks, and a pass outranks the clock: a
// limit only makes a TIMEOUT of a run the oracle did not pass.
func Decide(f Facts) (store.Verdict, string) {
	switch {
	case f.Skipped != "":
		return store.Skipped, f.Skipped
	case len(f.Invalid) > 0:
		return store.Invalid, strings.Join(f.Invalid, "; ")
	case f.Aborted != "":
		return store.Aborted, f.Aborted
	case !f.OracleRan:
		return store.Aborted, "the oracle gave no answer"
	case f.GuardStop != "":
		// The arm stops a run at its first guard trip and counts it as
		// failed (D99); what the oracle said is kept beside it.
		said := "no"
		if f.OraclePassed {
			said = "pass"
		}
		return store.Fail, fmt.Sprintf("stopped at %s; the oracle said %s", f.GuardStop, said)
	case f.OraclePassed:
		if f.Limit != "" {
			return store.Pass, fmt.Sprintf("oracle passed; the %s limit had fired (%s)", f.Limit, limitWho(f))
		}
		return store.Pass, "oracle passed"
	case f.Limit != "":
		return store.Timeout, fmt.Sprintf("%s limit (%s)", f.Limit, limitWho(f))
	}
	return store.Fail, "oracle ran and said no"
}

func limitWho(f Facts) string {
	return fmt.Sprintf("set by the %s, enforced by the %s", f.LimitOwner, f.LimitBy)
}
