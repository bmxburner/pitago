// Package pet is the sidebar-pet state core — a Go port of pi's
// sidebar-pet.ts extension.
//
// Tracks the turn live: idle | thinking (reasoning streaming)
// | writing (text streaming) | working (tools/steps)
// | success (turn finished) | error.
//
// Busy states are TIMED per cycle: the row shows elapsed seconds for the
// current status period ("Thinking... 7s"). Pure transition helpers live
// here; the Model wiring (timers, tea.Cmds) stays in src/app.
package pet

import (
	"time"

	"openpi/src/components/format"
)

// Status is the pet's display state.
type Status string

const (
	Idle     Status = "idle"
	Thinking Status = "thinking"
	Writing  Status = "writing"
	Working  Status = "working"
	Success  Status = "success"
	Error    Status = "error"
)

// Busy reports streaming/working states (timed per cycle).
func (p Status) Busy() bool { return p == Thinking || p == Writing || p == Working }

// Flashing reports end-of-turn states (auto-decay via flash timer).
func (p Status) Flashing() bool { return p == Success || p == Error }

// Faces are ≤8 cells wide so the sidebar row doesn't jitter while cycling.
var Faces = map[Status][]string{
	Idle:     {"(◉‿◉)", "(˘‿˘)", "(◉‿◉)", "(-‿-)"},
	Thinking: {"(◔_◔)", "(◉_◔)", "(◔_◔)", "(¬_¬)", "(ᵕ_ᵕ)"},
	Writing:  {"(•‿•)✎", "(•o•)⋆", "(•‿•)✎", "(•o•)⋆", "(•ᴗ•)✎", "(•ᴗ•)⋆", "(ᵔᴗᵔ)✎"},
	Working:  {"(◉▿◉)⚙", "(◉▽◉)⋆", "(◉▿◉)⚙", "(●▿●)⋆", "(•ᴗ•)⚙", "(•_•)⋆", "(ᗒᴗᗕ)⚙", "(•̀ᴗ•́)⋆"},
	Success:  {"(ᵔᴥᵔ)", "(ᵔᴥᵔ)", "(ᵔᴥᵔ)", "(^‿^)", "(ᵔᴗᵔ)♡", "(•ᴗ•)✦", "(^ᴗ^)", "(ᵔ‿ᵔ)"},
	Error:    {"(ಠ_ಠ)", "()ಠ_ಠ)", "(T_T)", "(T_T)"},
}

// Face picks the animation frame for tick ("" reads as idle).
func Face(s Status, tick int) string {
	if s == "" {
		s = Idle // zero value reads as idle
	}
	faces := Faces[s]
	if len(faces) == 0 {
		return ""
	}
	return faces[tick%len(faces)]
}

// Label renders the status line, appending elapsed time for busy states.
func Label(s Status, since time.Time) string {
	var out string
	switch s {
	case Thinking:
		out = "Thinking..."
	case Writing:
		out = "Writing..."
	case Working:
		out = "Working..."
	case Success:
		out = "Done!"
	case Error:
		out = "Error"
	default:
		out = "Ready"
	}
	if s.Busy() && !since.IsZero() {
		out += " " + format.FmtDur(time.Since(since))
	}
	return out
}
