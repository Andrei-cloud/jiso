// keys.go is the key vocabulary an operator reads. The four names here are the
// strings bubbletea matches a physical key against AND the strings the hint
// tables print under that key, so a binding and its own label are the same name
// by construction: "etnr" in a WithKeys call is a hotkey that silently does
// nothing, and the table beside it would still say "enter".
//
// The capitalised forms the footer prints ("Enter", "Esc") are display casing of
// the same key and stay at the call sites, which is where the rest of the
// frame's display text lives; only the matching vocabulary is named here.
package theme

const (
	// KeyEnter is the Return/Enter key: submits a prompt, drills into a row,
	// advances a wizard step.
	KeyEnter = "enter"
	// KeyEsc cancels the current step, closes an overlay, and pops the page
	// stack -- the one key that means "back out" everywhere in the app.
	KeyEsc = "esc"
	// KeyTab moves focus between panes of a split page.
	KeyTab = "tab"
	// KeyNavJK is the two-key navigation pair, labelled as the pair rather than
	// as either key because a hint that says only "j" reads like a typo.
	KeyNavJK = "j/k"
)
