package builtin

import (
	"testing"

	"pitago/src/app"
	"pitago/src/pirpc"
)

func hubModel() *app.Model {
	m := app.New(nil, "")
	m.Cmds = []pirpc.RepoCommand{
		{Name: "skill:archify", Description: "arch diagrams", Source: "skill"},
	}
	m.Plugins = []app.Plugin{{Spec: "npm:pi-lens", Name: "pi-lens"}}
	m.OpenPconfig()
	return &m
}

// selectPsec moves the hub to a section with focus on the right pane.
func selectPsec(m *app.Model, d *app.Dialog, id string) {
	for i, sid := range d.PsecIDs {
		if sid == id {
			d.ProvCursor = i
		}
	}
	d.ProvFocus = false
	m.LoadPsecRows(d)
}

func TestConfirmPconfigRunsSkill(t *testing.T) {
	m := hubModel()
	d := m.Dialogs[0]
	selectPsec(m, d, app.PsecSkill)
	if len(d.FIdx) == 0 {
		t.Fatal("skill section should have a row")
	}
	mm, _ := confirmPconfig(m, d, d.FIdx[d.Cursor])
	if len(mm.(*app.Model).Dialogs) != 0 {
		t.Error("FillCommand should close all dialogs")
	}
}

func TestConfirmPconfigAgentPopsHub(t *testing.T) {
	m := hubModel()
	d := m.Dialogs[0] // Agent section, action row
	d.ProvFocus = false
	mm, cmd := confirmPconfig(m, d, d.FIdx[d.Cursor])
	m2 := mm.(*app.Model)
	if len(m2.Dialogs) != 0 {
		t.Fatalf("agent reuses the classic dialog: hub must pop, got %d", len(m2.Dialogs))
	}
	if cmd == nil {
		t.Error("agent should return the loadSettings cmd")
	}
}

func TestConfirmPconfigInfoRowStays(t *testing.T) {
	m := hubModel()
	d := m.Dialogs[0]
	selectPsec(m, d, app.PsecPlugin)
	mm, _ := confirmPconfig(m, d, d.FIdx[d.Cursor])
	if len(mm.(*app.Model).Dialogs) != 1 {
		t.Error("info row should keep the hub open")
	}
}
