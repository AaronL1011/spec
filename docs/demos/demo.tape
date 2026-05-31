# spec TUI demo — high-value developer workflow
# Shows: dashboard → pipeline overview → open a spec → create a new spec → triage intake
# Render: vhs docs/demos/demo.tape

Output docs/demos/demo.gif

# ── Terminal settings ──────────────────────────────────────────────────────────
Set Shell      "fish"
Set FontSize   15
Set Width      1200
Set Height     700
Set Theme      "TokyoNight"

Set TypingSpeed 60ms
Set WaitTimeout 10s

# ── Open the TUI — lands on Dashboard ─────────────────────────────────────────
Sleep 1.5s
Type "spec"
Sleep 1.5s
Enter

# Let the dashboard settle, show the DO / REVIEW / INCOMING sections
Sleep 4s

# Navigate through the pipeline specs
Type "2"
Sleep 3s
Down
Sleep 400ms
Down
Sleep 1.5s

# ── Drill into a spec from the pipeline ──────────────────────────────────────
# Press Enter to open spec detail
Enter
Sleep 2s

Type "o"
Sleep 2s
Type "3"
Sleep 2s
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 4s

Type "n"
Sleep 300ms
Type "n"
Sleep 3s

Type "a"
Sleep 1.5s
Type "Have we considered how certain statuses might clobber others? What should take priority, etc."
Sleep 2s
Enter

Sleep 4s
Ctrl+C
