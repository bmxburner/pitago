package app

import (
	"github.com/charmbracelet/lipgloss"
)

const sideW = 34 // sidebar width

const sideInnerW = sideW - 4 // sidebar content width (box minus border 2 + padding 2)

const maxRecent = 5 // recent models kept in the sidebar

// palette (opencode-like monochrome: subtle borders, dim text, no rainbow)
const (
	cAccent   = lipgloss.Color("15")  // selected / emphasis: white
	cBorder   = lipgloss.Color("240") // subtle borders (opencode BorderNormal)
	cMuted    = lipgloss.Color("243") // dim text
	cText     = lipgloss.Color("252") // normal text
	cCode     = lipgloss.Color("250") // code / output
	cGreen    = lipgloss.Color("114") // success dot
	cRed      = lipgloss.Color("203") // error
	cSide     = lipgloss.Color("240") // unused now, kept subtle
	cInput    = lipgloss.Color("252") // input focus: white, not cyan
	cInputDim = lipgloss.Color("240") // input idle: subtle gray
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(cMuted)
	badgeStyle = lipgloss.NewStyle().
			Foreground(cMuted)
	statusBarStyle = lipgloss.NewStyle().Foreground(cMuted)
	userStyle      = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1).
			Foreground(cText)
	toolStyle = lipgloss.NewStyle().Foreground(cMuted)
	codeStyle = lipgloss.NewStyle().Foreground(cCode)
	sideStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1)
	sideTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sepStyle       = lipgloss.NewStyle().Foreground(cBorder)
	inputStyle     = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cInputDim).
			Padding(0, 1)
	inputFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cInput).
			Padding(0, 1)
	inputRunStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cGreen).
			Padding(0, 1)
	cmdPopStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1)
	cmdHiStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("238")).
			Foreground(lipgloss.Color("15"))
	rowHiStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("238")).
			Foreground(lipgloss.Color("15"))
	dlgStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(1, 3)
	errStyle  = lipgloss.NewStyle().Foreground(cRed)
	okStyle   = lipgloss.NewStyle().Foreground(cGreen)
	warnStyle = lipgloss.NewStyle().Foreground(cMuted)
)

// messages ---------------------------------------------------------------
