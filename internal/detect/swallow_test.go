package detect

import "testing"

func TestSwallowedArgs(t *testing.T) {
	schemas := map[string][]string{"editor": {"end_line", "file_path", "new_text", "old_text", "start_line"}}
	calls := []Call{
		// The client's case: new_text swallowed start_line, which is missing.
		{Turn: 3, Tool: "editor", Input: `{"file_path":"a.js","new_text":"x = 1;\",\"start_line\": 12"}`},
		// The name in the text, but the argument was passed too: not swallowed.
		{Turn: 4, Tool: "editor", Input: `{"file_path":"a.js","start_line":3,"new_text":"\"start_line\": 1"}`},
		// Code that happens to say a word: no delimiter, or no colon.
		{Turn: 5, Tool: "editor", Input: `{"file_path":"a.js","new_text":"let start_line = 1; old_text"}`},
		// A tool with no schema learns its names from the run's own calls.
		{Turn: 6, Tool: "write_to_file", Input: `{"path":"b","content":"ok"}`},
		{Turn: 7, Tool: "write_to_file", Input: `{"content":"hello',  path : \"b\""}`},
		{Turn: 8, Tool: "read_files", Input: `not json`},
	}
	got := SwallowedArgs(calls, schemas)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if f := got[0]; f.Index != 0 || f.Turn != 3 || f.Tool != "editor" || f.In != "new_text" || f.Swallowed != "start_line" || f.Around == "" {
		t.Errorf("first %+v", f)
	}
	if f := got[1]; f.Tool != "write_to_file" || f.Swallowed != "path" || f.In != "content" {
		t.Errorf("second %+v", f)
	}
	if got := SwallowedArgs(nil, schemas); len(got) != 0 {
		t.Errorf("no calls %+v", got)
	}
}
