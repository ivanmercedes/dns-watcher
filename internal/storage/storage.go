package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"dns-watcher/internal/doh"
)

type Snapshot struct {
	Records   []string `json:"records"`
	Status    int      `json:"status"`
	UpdatedAt string   `json:"updated_at"`
}

type Change struct {
	Domain     string   `json:"domain"`
	RecordType string   `json:"record_type"`
	OldRecords []string `json:"old_records"`
	NewRecords []string `json:"new_records"`
	DetectedAt string   `json:"detected_at"`
}

type State struct {
	Snapshots map[string]Snapshot `json:"snapshots"`
	History   []Change            `json:"history"`
	LastCheck string              `json:"last_check_utc"`
}

type Store struct {
	Path  string
	State State
}

func snapshotKey(domain, recordType string) string {
	return domain + "|" + recordType
}

func Open(path string) (*Store, error) {
	st := State{
		Snapshots: make(map[string]Snapshot),
		History:   []Change{},
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if uErr := json.Unmarshal(data, &st); uErr != nil {
			return nil, uErr
		}
		if st.Snapshots == nil {
			st.Snapshots = make(map[string]Snapshot)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return &Store{Path: path, State: st}, nil
}

func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.State, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func (s *Store) ApplyResults(results []doh.Result, recordChanges bool) ([]Change, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var changes []Change

	for _, res := range results {
		if !res.OK {
			continue
		}
		key := snapshotKey(res.Domain, res.RecordType)
		prev, exists := s.State.Snapshots[key]
		if exists && !slices.Equal(prev.Records, res.Records) && recordChanges {
			ch := Change{
				Domain:     res.Domain,
				RecordType: res.RecordType,
				OldRecords: append([]string{}, prev.Records...),
				NewRecords: append([]string{}, res.Records...),
				DetectedAt: now,
			}
			changes = append(changes, ch)
			s.State.History = append(s.State.History, ch)
		}
		s.State.Snapshots[key] = Snapshot{
			Records:   append([]string{}, res.Records...),
			Status:    res.Status,
			UpdatedAt: now,
		}
	}
	s.State.LastCheck = now
	return changes, s.Save()
}

func (s *Store) GetHistory(domain string, limit int) []Change {
	var out []Change
	target := strings.ToLower(strings.TrimSpace(domain))
	for i := len(s.State.History) - 1; i >= 0; i-- {
		ch := s.State.History[i]
		if target != "" && ch.Domain != target {
			continue
		}
		out = append(out, ch)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) UniqueDomainsCount() int {
	seen := make(map[string]bool)
	for k := range s.State.Snapshots {
		if dom, _, ok := strings.Cut(k, "|"); ok {
			seen[dom] = true
		}
	}
	return len(seen)
}
