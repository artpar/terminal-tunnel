package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SaveSessionState saves a session state to disk
func SaveSessionState(state *SessionState) error {
	if state.ShortCode == "" {
		return nil
	}

	sessionsDir := GetSessionsDir()
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		return err
	}

	filePath := filepath.Join(sessionsDir, state.ShortCode+".json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0600)
}

// LoadSessionState loads a session state from disk
func LoadSessionState(shortCode string) (*SessionState, error) {
	filePath := filepath.Join(GetSessionsDir(), shortCode+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// RemoveSessionState removes a session state file
func RemoveSessionState(shortCode string) error {
	if shortCode == "" {
		return nil
	}
	filePath := filepath.Join(GetSessionsDir(), shortCode+".json")
	return os.Remove(filePath)
}

// LoadAllSessionStates loads all session states from disk
func LoadAllSessionStates() ([]*SessionState, error) {
	sessionsDir := GetSessionsDir()

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var states []*SessionState
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		shortCode := strings.TrimSuffix(entry.Name(), ".json")
		state, err := LoadSessionState(shortCode)
		if err != nil {
			continue // Skip invalid files
		}
		states = append(states, state)
	}

	return states, nil
}
