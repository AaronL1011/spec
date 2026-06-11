package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/store"
)

// Node status values for the per-node ledger.
const (
	NodePending    = "pending"
	NodeInProgress = "in-progress"
	NodeComplete   = "complete"
	NodeFailed     = "failed"
)

// NodeState is the durable per-node record in the DAG ledger. It carries
// everything needed to resume, diff, and stack a node: its status, the branch
// and worktree git placed it on, the base ref it was cut from, an optional
// failure reason, and any draft-PR coordinates recorded during finishing.
type NodeState struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Branch   string `json:"branch,omitempty"`
	BaseRef  string `json:"base_ref,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Reason   string `json:"reason,omitempty"`
	PRNumber int    `json:"pr_number,omitempty"`
	PRURL    string `json:"pr_url,omitempty"`
}

// SessionState persists the build session for `spec do` resume.
type SessionState struct {
	SpecID       string    `json:"spec_id"`
	CurrentStep  int       `json:"current_step"`
	Branch       string    `json:"branch"`
	Repo         string    `json:"repo"`
	WorkDir      string    `json:"work_dir"`
	LastActivity time.Time `json:"last_activity"`
	Steps        []PRStep  `json:"steps"`
	// Nodes is the per-node status ledger keyed by PRStep.ID. It is the DAG
	// source of truth for the orchestrated build; CurrentStep is retained only
	// for the legacy sequential walk and is removed once the DAG engine lands.
	Nodes map[string]*NodeState `json:"nodes,omitempty"`
}

// InitNodes populates the ledger from the session's steps when it is empty,
// giving every node a pending record. Existing records are preserved so a
// reload never clobbers progress.
func (s *SessionState) InitNodes() {
	if s.Nodes == nil {
		s.Nodes = make(map[string]*NodeState, len(s.Steps))
	}
	for _, step := range s.Steps {
		id := step.NodeID()
		if _, ok := s.Nodes[id]; !ok {
			s.Nodes[id] = &NodeState{ID: id, Status: NodePending, BaseRef: step.BaseRef, Branch: step.Branch}
		}
	}
}

// node returns the ledger entry for an id, creating a pending one on demand so
// callers never have to nil-check.
func (s *SessionState) node(id string) *NodeState {
	if s.Nodes == nil {
		s.Nodes = make(map[string]*NodeState)
	}
	n, ok := s.Nodes[id]
	if !ok {
		n = &NodeState{ID: id, Status: NodePending}
		s.Nodes[id] = n
	}
	return n
}

// NodeStatus returns the status of a node, or "pending" if unknown.
func (s *SessionState) NodeStatus(id string) string {
	if n, ok := s.Nodes[id]; ok {
		return n.Status
	}
	return NodePending
}

// SetNodeStatus updates a node's status in the ledger.
func (s *SessionState) SetNodeStatus(id, status string) {
	s.node(id).Status = status
}

// MarkNodeComplete records a node as complete and clears any failure reason.
// It is idempotent: completing an already-complete node is a no-op.
func (s *SessionState) MarkNodeComplete(id string) {
	n := s.node(id)
	n.Status = NodeComplete
	n.Reason = ""
}

// MarkNodeFailed records a node as failed with a reason for resume/reporting.
func (s *SessionState) MarkNodeFailed(id, reason string) {
	n := s.node(id)
	n.Status = NodeFailed
	n.Reason = reason
}

// DoneSet returns the set of completed node IDs, the input to Graph.ReadySet.
func (s *SessionState) DoneSet() map[string]bool {
	done := make(map[string]bool, len(s.Nodes))
	for id, n := range s.Nodes {
		if n.Status == NodeComplete {
			done[id] = true
		}
	}
	return done
}

// NodesComplete reports whether every node in the ledger is complete. It
// returns false for an empty ledger so an uninitialised session is never
// mistaken for a finished one.
func (s *SessionState) NodesComplete() bool {
	if len(s.Nodes) == 0 {
		return false
	}
	for _, n := range s.Nodes {
		if n.Status != NodeComplete {
			return false
		}
	}
	return true
}

// FailedNodes returns the IDs of nodes recorded as failed, sorted for stable
// reporting.
func (s *SessionState) FailedNodes() []string {
	var ids []string
	for id, n := range s.Nodes {
		if n.Status == NodeFailed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// SessionDir returns the path to the session directory.
func SessionDir(specID string) string {
	return filepath.Join(specHomeDir(), "sessions", specID)
}

func specHomeDir() string {
	if override := os.Getenv("SPEC_HOME"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".spec"
	}
	return filepath.Join(home, ".spec")
}

// LoadSession loads a session from the database.
func LoadSession(db *store.DB, specID string) (*SessionState, error) {
	data, err := db.SessionGet(specID)
	if err != nil {
		return nil, fmt.Errorf("loading session %s: %w", specID, err)
	}
	if data == "" {
		return nil, nil
	}

	var session SessionState
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, fmt.Errorf("parsing session %s: %w", specID, err)
	}
	return &session, nil
}

// SaveSession persists a session to the database.
func SaveSession(db *store.DB, session *SessionState) error {
	session.LastActivity = time.Now()
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	return db.SessionSet(session.SpecID, string(data))
}

// CreateSession creates a new build session.
func CreateSession(db *store.DB, specID string, steps []PRStep, workDir string) (*SessionState, error) {
	session := &SessionState{
		SpecID:       specID,
		CurrentStep:  1,
		WorkDir:      workDir,
		LastActivity: time.Now(),
		Steps:        steps,
	}

	if len(steps) > 0 {
		session.Repo = steps[0].Repo
		steps[0].Status = "in-progress"
	}
	session.InitNodes()

	// Create session directory
	if err := os.MkdirAll(SessionDir(specID), 0o755); err != nil {
		return nil, fmt.Errorf("creating session directory: %w", err)
	}

	if err := SaveSession(db, session); err != nil {
		return nil, err
	}
	return session, nil
}

// AdvanceStep marks the current step as complete and moves to the next. It also
// mirrors the change into the node ledger so the DAG source of truth stays in
// sync during the sequential-to-DAG transition.
func AdvanceStep(db *store.DB, session *SessionState) error {
	if session.CurrentStep > 0 && session.CurrentStep <= len(session.Steps) {
		session.Steps[session.CurrentStep-1].Status = "complete"
		session.MarkNodeComplete(session.Steps[session.CurrentStep-1].NodeID())
	}

	session.CurrentStep++
	if session.CurrentStep <= len(session.Steps) {
		session.Steps[session.CurrentStep-1].Status = "in-progress"
		session.Repo = session.Steps[session.CurrentStep-1].Repo
		session.SetNodeStatus(session.Steps[session.CurrentStep-1].NodeID(), NodeInProgress)
	}

	return SaveSession(db, session)
}

// IsComplete returns true if all steps are done.
func (s *SessionState) IsComplete() bool {
	return s.CurrentStep > len(s.Steps)
}

// CurrentPRStep returns the current step, or nil if complete.
func (s *SessionState) CurrentPRStep() *PRStep {
	if s.CurrentStep > 0 && s.CurrentStep <= len(s.Steps) {
		return &s.Steps[s.CurrentStep-1]
	}
	return nil
}

// activityDB is the shared database reference for activity logging.
// Set via SetActivityDB during engine initialization.
var activityDB *store.DB

// SetActivityDB sets the database used for activity logging.
func SetActivityDB(db *store.DB) {
	activityDB = db
}

// LogActivity appends an entry to both the SQLite activity log and the session file.
func LogActivity(specID, entry string) error {
	// Write to SQLite if available
	if activityDB != nil {
		_ = activityDB.ActivityLog(specID, "build", entry, "", "spec")
	}

	// Also write to session file for backwards compatibility
	logPath := filepath.Join(SessionDir(specID), "activity.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f, "[%s] %s\n", timestamp, entry)
	return err
}

// logInvokeResult records a build_result activity event carrying the
// session-level signal the agent reported (exit reason, error class, token
// usage). It is best-effort: a nil result (interactive run, or a harness that
// reports nothing) is skipped, and the structured detail rides in the event
// metadata so `spec fix --auto` runs stay debuggable from the activity log.
func logInvokeResult(specID string, result *adapter.InvokeResult) {
	if result == nil {
		return
	}

	summary := invokeResultSummary(result)
	if activityDB != nil {
		metadata := ""
		if b, err := json.Marshal(result); err == nil {
			metadata = string(b)
		}
		_ = activityDB.ActivityLog(specID, "build_result", summary, metadata, "spec")
	}
	_ = LogActivity(specID, summary)
}

// invokeResultSummary renders a one-line human summary of an InvokeResult for
// the activity log.
func invokeResultSummary(r *adapter.InvokeResult) string {
	reason := r.ExitReason
	if reason == "" {
		reason = "unknown"
	}
	summary := "Build session ended: " + reason
	if r.ErrorClass != "" {
		summary += " (" + r.ErrorClass + ")"
	}
	if r.Tokens.Total > 0 {
		summary += fmt.Sprintf(" — %d tokens (in %d / out %d)", r.Tokens.Total, r.Tokens.Input, r.Tokens.Output)
	}
	return summary
}
