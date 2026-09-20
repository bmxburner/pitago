package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Sidebar pet — Go port of sidebar-pet.ts (pi extension).
//
// Tracks the turn live: idle | thinking (reasoning streaming)
// | writing (text streaming) | working (tools/steps)
// | success (turn finished) | error.
//
// Busy states are TIMED per cycle: the row shows elapsed seconds for the
// current status period ("Thinking... 7s"). Every transition restarts the
// count. done(stop) gates the Done flash, so Esc-abort decays quietly.

type petStatus string

const (
	petIdle     petStatus = "idle"
	petThinking petStatus = "thinking"
	petWriting  petStatus = "writing"
	petWorking  petStatus = "working"
	petSuccess  petStatus = "success"
	petError    petStatus = "error"
)

const (
	petFlashDelay = 4 * time.Second
	petTickEvery  = 500 * time.Millisecond
)

// Faces are ≤8 cells wide so the sidebar row doesn't jitter while cycling.
var petFaces = map[petStatus][]string{
	petIdle:     {"(◉‿◉)", "(˘‿˘)", "(◉‿◉)", "(-‿-)"},
	petThinking: {"(◔_◔)", "(◉_◔)", "(◔_◔)", "(¬_¬)", "(ᵕ_ᵕ)"},
	petWriting:  {"(•‿•)✎", "(•o•)⋆", "(•‿•)✎", "(•o•)⋆", "(•ᴗ•)✎", "(•ᴗ•)⋆", "(ᵔᴗᵔ)✎"},
	petWorking:  {"(◉▿◉)⚙", "(◉▽◉)⋆", "(◉▿◉)⚙", "(●▿●)⋆", "(•ᴗ•)⚙", "(•_•)⋆", "(ᗒᴗᗕ)⚙", "(•̀ᴗ•́)⋆"},
	petSuccess:  {"(ᵔᴥᵔ)", "(ᵔᴥᵔ)", "(ᵔᴥᵔ)", "(^‿^)", "(ᵔᴗᵔ)♡", "(•ᴗ•)✦", "(^ᴗ^)", "(ᵔ‿ᵔ)"},
	petError:    {"(ಠ_ಠ)", "()ಠ_ಠ)", "(T_T)", "(T_T)"},
}

func (p petStatus) busy() bool     { return p == petThinking || p == petWriting || p == petWorking }
func (p petStatus) flashing() bool { return p == petSuccess || p == petError }

type petState struct {
	status  petStatus
	since   time.Time // current status period began (busy only)
	tick    int
	gen     int  // transition generation; stale flash timers check it
	ticking bool // a 500ms loop is already scheduled
	inTurn  bool // between agent_start and agent_settled
	sawStop bool // a done(stop) closed a round (gates the Done flash)
}

type petTickMsg struct{}
type petFlashMsg struct{ gen int }

func petTickCmd() tea.Cmd {
	return tea.Tick(petTickEvery, func(time.Time) tea.Msg { return petTickMsg{} })
}

// petSet transitions the pet. Same-status calls are no-ops so stream deltas
// never restart the elapsed timer.
func (m *Model) petSet(next petStatus) tea.Cmd {
	if m.pet.status == next {
		return nil
	}
	m.pet.status = next
	m.pet.gen++
	if next.busy() {
		m.pet.since = time.Now()
	}
	var cmds []tea.Cmd
	if next.flashing() {
		gen := m.pet.gen
		cmds = append(cmds, tea.Tick(petFlashDelay, func(time.Time) tea.Msg {
			return petFlashMsg{gen: gen}
		}))
	}
	if (next.busy() || next.flashing()) && !m.pet.ticking {
		m.pet.ticking = true
		cmds = append(cmds, petTickCmd())
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

// petAnchor runs on agent_start: a fresh run anchors as working, but
// re-issued starts between rounds must not override thinking/writing, and a
// queued follow-up must not wipe the Done flash (its stream events re-anchor
// below if the turn genuinely continues).
func (m *Model) petAnchor() tea.Cmd {
	m.pet.inTurn = true
	m.pet.sawStop = false
	if m.pet.status.flashing() || m.pet.status.busy() {
		return nil
	}
	return m.petSet(petWorking)
}

// petSettled runs on agent_settled: pi will not continue automatically.
// Without a preceding done(stop) (Esc-abort) it decays quietly to idle.
func (m *Model) petSettled() tea.Cmd {
	m.pet.inTurn = false
	if m.pet.status.flashing() {
		return nil
	}
	if m.pet.sawStop {
		return m.petSet(petSuccess)
	}
	return m.petSet(petIdle)
}

func (m Model) petFace() string {
	s := m.pet.status
	if s == "" {
		s = petIdle // zero value reads as idle
	}
	faces := petFaces[s]
	if len(faces) == 0 {
		return ""
	}
	return faces[m.pet.tick%len(faces)]
}

func (m Model) petLabel() string {
	var s string
	switch m.pet.status {
	case petThinking:
		s = "Thinking..."
	case petWriting:
		s = "Writing..."
	case petWorking:
		s = "Working..."
	case petSuccess:
		s = "Done!"
	case petError:
		s = "Error"
	default:
		s = "Ready"
	}
	if m.pet.status.busy() && !m.pet.since.IsZero() {
		s += " " + fmtDur(time.Since(m.pet.since))
	}
	return s
}

// renderPet draws the PET sidebar section (face + timed status line).
// Fixed height: petRows content rows (title + face + separator) —
// recentAt's click mapping depends on this, keep them in sync.
const petRows = 3
func (m Model) renderPet(inner int) string {
	face := m.petFace()
	fstyle := statusBarStyle
	switch m.pet.status {
	case petThinking, petWriting, petWorking:
		fstyle = fstyle.Copy().Foreground(cText)
	case petSuccess:
		fstyle = okStyle
	case petError:
		fstyle = errStyle
	}
	row := fstyle.Render(face) + " " + statusBarStyle.Render(Short(m.petLabel(), inner-10))
	return sideTitleStyle.Render("PET") + "\n" + row + "\n" + sep() + "\n"
}
