// Package state persists the remaining server time across restarts.
package state

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type timeFileContent struct {
	TimeRemaining int `json:"time_remaining"`
}

// State holds the remaining server run time in seconds, persisted to a file.
type State struct {
	mu            sync.Mutex
	timeRemaining int
	filePath      string
}

// New loads the persisted time from filePath, defaulting to a 15-minute
// grace period on first launch.
func New(filePath string) *State {
	return &State{
		timeRemaining: load(filePath),
		filePath:      filePath,
	}
}

func (s *State) GetTimeRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.timeRemaining
}

func (s *State) UpdateTimeRemaining(mutateFn func(int) int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timeRemaining = mutateFn(s.timeRemaining)
	s.save()
	return s.timeRemaining
}

func (s *State) SetTimeRemaining(val int) {
	s.UpdateTimeRemaining(func(_ int) int {
		return val
	})
}

func load(filePath string) int {
	if _, err := os.Stat(filePath); err == nil {
		file, err := os.Open(filePath) // #nosec G304
		if err == nil {
			defer file.Close()
			var content timeFileContent
			if err := json.NewDecoder(file).Decode(&content); err == nil {
				if content.TimeRemaining < 0 {
					return 0
				}
				return content.TimeRemaining
			}
		}
	}
	// Try to ensure parent directory exists
	dir := filepath.Dir(filePath)
	_ = os.MkdirAll(dir, 0755)
	return 900 // Default 15 minutes grace on first launch
}

// save writes to a temp file and renames it into place, so a crash mid-write
// can never leave a corrupt file that would reset the timer on next start.
func (s *State) save() {
	content := timeFileContent{TimeRemaining: s.timeRemaining}
	dir := filepath.Dir(s.filePath)
	_ = os.MkdirAll(dir, 0755)

	tmpPath := s.filePath + ".tmp"
	file, err := os.Create(tmpPath) // #nosec G304 -- path comes from server config, not user input
	if err != nil {
		log.Printf("Failed to save time file: %v", err)
		return
	}
	if err := json.NewEncoder(file).Encode(content); err != nil {
		log.Printf("Failed to encode time file: %v", err)
		_ = file.Close()
		return
	}
	if err := file.Close(); err != nil {
		log.Printf("Failed to close time file: %v", err)
		return
	}
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		log.Printf("Failed to replace time file: %v", err)
	}
}
