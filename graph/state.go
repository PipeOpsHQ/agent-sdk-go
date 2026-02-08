package graph

import (
	"encoding/json"
	"fmt"
	"time"
)

type State struct {
	RunID      string         `json:"runId"`
	SessionID  string         `json:"sessionId"`
	Input      string         `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	LastNodeID string         `json:"lastNodeId,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	StartedAt  time.Time      `json:"startedAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type checkpointSnapshot struct {
	State      State  `json:"state"`
	NextNodeID string `json:"nextNodeId,omitempty"`
}

func newState(runID, sessionID, input string, now time.Time) State {
	return State{
		RunID:     runID,
		SessionID: sessionID,
		Input:     input,
		Data:      map[string]any{},
		StartedAt: now,
		UpdatedAt: now,
	}
}

func (s *State) ensureData() {
	if s.Data == nil {
		s.Data = map[string]any{}
	}
}

func (s *State) EnsureData() {
	s.ensureData()
}

func (s State) snapshot(nextNodeID string) (map[string]any, error) {
	payload := checkpointSnapshot{
		State:      s,
		NextNodeID: nextNodeID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal checkpoint snapshot: %w", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("failed to decode checkpoint snapshot map: %w", err)
	}
	return out, nil
}

func restoreStateFromCheckpoint(raw map[string]any) (State, string, error) {
	if len(raw) == 0 {
		return State{}, "", fmt.Errorf("checkpoint state is empty")
	}
	payloadRaw, err := json.Marshal(raw)
	if err != nil {
		return State{}, "", fmt.Errorf("failed to marshal checkpoint state: %w", err)
	}
	var snapshot checkpointSnapshot
	if err := json.Unmarshal(payloadRaw, &snapshot); err != nil {
		return State{}, "", fmt.Errorf("failed to decode checkpoint state: %w", err)
	}
	snapshot.State.ensureData()
	return snapshot.State, snapshot.NextNodeID, nil
}
