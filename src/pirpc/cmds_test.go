package pirpc

import (
	"strings"
	"testing"
)

func TestRPCCommands(t *testing.T) {
	needPi(t)
	c, err := Spawn(Options{NoSession: true})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer c.Close()
	cmds, err := c.GetCommands()
	if err != nil && strings.Contains(err.Error(), "timed out") {
		cmds, err = c.GetCommands() // pi cold start under load: one retry
	}
	if err != nil {
		t.Fatalf("get_commands: %v", err)
	}
	var ext, prm, skl int
	for _, k := range cmds {
		switch k.Source {
		case "extension":
			ext++
		case "prompt":
			prm++
		case "skill":
			skl++
		}
		if len(k.Name) < 40 {
			t.Logf("/%-28s [%s] %s", k.Name, k.Source, k.Description)
		}
	}
	t.Logf("total=%d ext=%d prompt=%d skill=%d", len(cmds), ext, prm, skl)
}
