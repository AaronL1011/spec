package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aaronl1011/spec-cli/internal/store"
)

// SessionState persists the build session for `spec do` resume.
type SessionState struct {
	SpecID       string    `json:"spec_id"`
	CurrentStep  int       `json:"current_step"`
	Branch       string    `json:"branch"`
	Repo         string    `json:"repo"`
	WorkDir      string    `json:"work_dir"`
	LastActivity time.Time `json:"last_activity"`
	Steps        []PRStep  `json:"steps"`
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

	// Create session directory
	if err := os.MkdirAll(SessionDir(specID), 0o755); err != nil {
		return nil, fmt.Errorf("creating session directory: %w", err)
	}

	if err := SaveSession(db, session); err != nil {
		return nil, err
	}
	return session, nil
}

// AdvanceStep marks the current step as complete and moves to the next.
func AdvanceStep(db *store.DB, session *SessionState) error {
	if session.CurrentStep > 0 && session.CurrentStep <= len(session.Steps) {
		session.Steps[session.CurrentStep-1].Status = "complete"
	}

	session.CurrentStep++
	if session.CurrentStep <= len(session.Steps) {
		session.Steps[session.CurrentStep-1].Status = "in-progress"
		session.Repo = session.Steps[session.CurrentStep-1].Repo
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
