package task

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// A scripted user states its defaults, and each rule's event, response and
// fields must go together; what is not built yet is refused by name.
func TestUserScript(t *testing.T) {
	parse := func(doc string) []string {
		t.Helper()
		var v struct {
			User User `toml:"user"`
		}
		md, err := toml.Decode(doc, &v)
		if err != nil {
			t.Fatal(err)
		}
		if u := md.Undecoded(); len(u) > 0 {
			t.Fatalf("undecoded: %v", u)
		}
		return v.User.validate()
	}
	good := `
[user.default]
approval = "approve"
answer = "Go on."

[[user.rule]]
on = "approval"
tool = "run_commands"
match = "rm -rf"
do = "deny"
text = "Do not delete anything."
times = 0

[[user.rule]]
on = "question"
match = "which file"
do = "option"
option = 2

[[user.rule]]
on = "turn"
turn = 3
do = "steer"
text = "Run the checker before you edit again."

[[user.rule]]
on = "elapsed"
after_s = 600
do = "restore"
restore = "workspace"
checkpoint = 1

[[user.rule]]
on = "completion"
do = "queue"
text = "Now also add a test."
`
	if p := parse(good); len(p) != 0 {
		t.Fatalf("a good script: %q", p)
	}

	cases := []struct{ doc, want string }{
		{`[user.default]
answer = "x"`, `approval must be "approve" or "deny"`},
		{`[user.default]
approval = "approve"`, "answer must be stated"},
		{`[user.default]
approval = "approve"
feedback = "no"
answer = "x"`, "feedback goes with a default denial"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "approval"
do = "steer"
text = "x"`, "do must be approve, deny"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "turn"
turn = 2
do = "mode"`, "mode switch is not built yet"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "turn"
turn = 2
do = "restore"
restore = "messages"
checkpoint = 1`, "restore of the messages is not built yet"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "question"
match = "(?i)file"
do = "answer"
text = "a"`, "no inline flags"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "turn"
do = "queue"
text = "x"`, "turn, from 1, goes with"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "elapsed"
after_s = 60
do = "cancel"`, `do = "cancel" needs text`},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "completion"
do = "steer"
text = "x"`, "do must be queue"},
		{`[user.default]
approval = "approve"
answer = "x"
[[user.rule]]
on = "tool_call"
do = "approve"`, "on must be approval, question"},
	}
	for _, c := range cases {
		p := parse(c.doc)
		if !strings.Contains(strings.Join(p, "; "), c.want) {
			t.Errorf("want %q in %q\n%s", c.want, p, c.doc)
		}
	}

	var v struct {
		User User `toml:"user"`
	}
	toml.Decode(good, &v)
	v.User.validate()
	if *v.User.Rules[0].Times != 0 || *v.User.Rules[2].Times != 1 {
		t.Errorf("times: %d %d", *v.User.Rules[0].Times, *v.User.Rules[2].Times)
	}
}
