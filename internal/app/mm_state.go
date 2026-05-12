package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MMState struct {
	Accounts map[string]MMAccountState `json:"accounts"`
}

type MMAccountState struct {
	ActiveMarketID     string `json:"activeMarketId,omitempty"`
	ActiveMarketTitle  string `json:"activeMarketTitle,omitempty"`
	ActiveInstanceDate string `json:"activeInstanceDate,omitempty"`
}

func LoadMMState() (*MMState, error) {
	path, err := mmStatePath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return &MMState{Accounts: make(map[string]MMAccountState)}, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat mm state: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mm state: %w", err)
	}
	state := &MMState{Accounts: make(map[string]MMAccountState)}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("parse mm state: %w", err)
	}
	if state.Accounts == nil {
		state.Accounts = make(map[string]MMAccountState)
	}
	return state, nil
}

func SaveMMState(state *MMState) error {
	if state == nil {
		state = &MMState{Accounts: make(map[string]MMAccountState)}
	}
	if state.Accounts == nil {
		state.Accounts = make(map[string]MMAccountState)
	}
	path, err := mmStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create mm state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mm state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write mm state: %w", err)
	}
	return nil
}

func (s *MMState) Account(name string) MMAccountState {
	if s == nil || s.Accounts == nil {
		return MMAccountState{}
	}
	return s.Accounts[strings.TrimSpace(name)]
}

func (s *MMState) SetAccount(name string, account MMAccountState) {
	if s == nil {
		return
	}
	if s.Accounts == nil {
		s.Accounts = make(map[string]MMAccountState)
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return
	}
	if account.ActiveMarketID == "" && account.ActiveMarketTitle == "" && account.ActiveInstanceDate == "" {
		delete(s.Accounts, trimmed)
		return
	}
	account.ActiveMarketID = strings.TrimSpace(account.ActiveMarketID)
	account.ActiveMarketTitle = strings.TrimSpace(account.ActiveMarketTitle)
	account.ActiveInstanceDate = strings.TrimSpace(account.ActiveInstanceDate)
	s.Accounts[trimmed] = account
}

func mmStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "axiom-cli", "mm-state.json"), nil
}
