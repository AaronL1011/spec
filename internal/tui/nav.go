package tui

// View identifies each top-level view in the TUI.
type View int

const (
	ViewDashboard View = iota
	ViewPipeline
	ViewSpecs
	ViewTriage
	ViewReviews
	ViewSecurity
	ViewSettings
)

// ViewCount is the total number of top-level views.
const ViewCount = 7

// viewMeta stores display metadata for each view.
type viewMeta struct {
	Label    string
	Shortcut string
}

var viewMetas = [ViewCount]viewMeta{
	{Label: "Dashboard", Shortcut: "1"},
	{Label: "Pipeline", Shortcut: "2"},
	{Label: "Specs", Shortcut: "3"},
	{Label: "Triage", Shortcut: "4"},
	{Label: "Reviews", Shortcut: "5"},
	{Label: "Security", Shortcut: "6"},
	{Label: "Settings", Shortcut: "7"},
}

// Label returns the display name for a view.
func (v View) Label() string { return viewMetas[v].Label }

// Shortcut returns the keyboard shortcut for a view.
func (v View) Shortcut() string { return viewMetas[v].Shortcut }

// Next returns the next view, wrapping around.
func (v View) Next() View { return (v + 1) % ViewCount }

// Prev returns the previous view, wrapping around.
func (v View) Prev() View { return (v - 1 + ViewCount) % ViewCount }
