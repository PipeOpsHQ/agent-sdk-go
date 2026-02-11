package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/agent"
	"github.com/PipeOpsHQ/agent-sdk-go/llm"
	"github.com/PipeOpsHQ/agent-sdk-go/observe"
	"github.com/PipeOpsHQ/agent-sdk-go/state"
	"github.com/PipeOpsHQ/agent-sdk-go/tools"
	"github.com/google/uuid"
)

// ExecutionPattern defines how multiple agents coordinate.
type ExecutionPattern string

const (
	// PatternSequential runs agents one after another, passing output as input.
	PatternSequential ExecutionPattern = "sequential"
	// PatternParallel runs agents concurrently, aggregating results.
	PatternParallel ExecutionPattern = "parallel"
	// PatternSupervisor has a supervisor agent delegate to worker agents.
	PatternSupervisor ExecutionPattern = "supervisor"
	// PatternRouter routes input to the most appropriate agent.
	PatternRouter ExecutionPattern = "router"
	// PatternDebate has agents argue different perspectives.
	PatternDebate ExecutionPattern = "debate"
	// PatternConsensus requires agents to reach agreement.
	PatternConsensus ExecutionPattern = "consensus"
)

// AgentConfig defines configuration for an agent in the multi-agent system.
type AgentConfig struct {
	ID           string
	Name         string
	Description  string
	Provider     llm.Provider
	SystemPrompt string
	Tools        []tools.Tool
	Role         AgentRole
	Options      []agent.Option
}

// AgentRole defines the role of an agent in the system.
type AgentRole string

const (
	RoleWorker     AgentRole = "worker"
	RoleSupervisor AgentRole = "supervisor"
	RoleRouter     AgentRole = "router"
	RoleSpecialist AgentRole = "specialist"
)

// OrchestratorConfig configures the multi-agent orchestrator.
type OrchestratorConfig struct {
	Pattern         ExecutionPattern
	MaxRounds       int // For debate/consensus patterns
	TimeoutPerAgent time.Duration
	SharedMemory    bool
	Observer        observe.Sink
	Store           state.Store
}

// Orchestrator manages multiple agents working together.
type Orchestrator struct {
	config   OrchestratorConfig
	registry *Registry
	memory   *SharedMemory
	observer observe.Sink
	store    state.Store

	mu     sync.RWMutex
	agents map[string]*ManagedAgent
}

// ManagedAgent wraps an agent with metadata.
type ManagedAgent struct {
	ID          string
	Name        string
	Description string
	Role        AgentRole
	Agent       *agent.Agent
	Provider    llm.Provider
	CreatedAt   time.Time
}

// MultiAgentResult contains the result from a multi-agent run.
type MultiAgentResult struct {
	RunID          string                    `json:"runId"`
	Pattern        ExecutionPattern          `json:"pattern"`
	FinalOutput    string                    `json:"finalOutput"`
	AgentResults   map[string]AgentRunResult `json:"agentResults"`
	TotalDuration  time.Duration             `json:"totalDuration"`
	TotalTokens    int                       `json:"totalTokens"`
	SelectedAgent  string                    `json:"selectedAgent,omitempty"`    // For router pattern
	ConsensusReach bool                      `json:"consensusReached,omitempty"` // For consensus pattern
	Rounds         int                       `json:"rounds,omitempty"`           // For debate/consensus
}

// AgentRunResult contains results from a single agent's run.
type AgentRunResult struct {
	AgentID   string        `json:"agentId"`
	AgentName string        `json:"agentName"`
	Output    string        `json:"output"`
	Duration  time.Duration `json:"duration"`
	Tokens    int           `json:"tokens"`
	Error     string        `json:"error,omitempty"`
}

// DelegationRequest is sent from one agent to another.
type DelegationRequest struct {
	FromAgentID string         `json:"fromAgentId"`
	ToAgentID   string         `json:"toAgentId"`
	Task        string         `json:"task"`
	Context     map[string]any `json:"context,omitempty"`
	Priority    int            `json:"priority"`
}

// DelegationResponse is the response from a delegated task.
type DelegationResponse struct {
	FromAgentID string `json:"fromAgentId"`
	ToAgentID   string `json:"toAgentId"`
	Result      string `json:"result"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

// NewOrchestrator creates a new multi-agent orchestrator.
func NewOrchestrator(config OrchestratorConfig) (*Orchestrator, error) {
	if config.Pattern == "" {
		config.Pattern = PatternSequential
	}
	if config.MaxRounds <= 0 {
		config.MaxRounds = 3
	}
	if config.TimeoutPerAgent <= 0 {
		config.TimeoutPerAgent = 5 * time.Minute
	}

	o := &Orchestrator{
		config:   config,
		registry: NewRegistry(),
		agents:   make(map[string]*ManagedAgent),
		observer: config.Observer,
		store:    config.Store,
	}

	if config.SharedMemory {
		o.memory = NewSharedMemory()
	}

	return o, nil
}

// RegisterAgent adds an agent to the orchestrator.
func (o *Orchestrator) RegisterAgent(cfg AgentConfig) error {
	if cfg.ID == "" {
		cfg.ID = uuid.NewString()
	}
	if cfg.Provider == nil {
		return errors.New("provider is required")
	}
	if cfg.Role == "" {
		cfg.Role = RoleWorker
	}

	// Build agent options
	opts := append([]agent.Option{}, cfg.Options...)
	if cfg.SystemPrompt != "" {
		opts = append(opts, agent.WithSystemPrompt(cfg.SystemPrompt))
	}
	for _, tool := range cfg.Tools {
		opts = append(opts, agent.WithTool(tool))
	}
	if o.store != nil {
		opts = append(opts, agent.WithStore(o.store))
	}
	if o.observer != nil {
		opts = append(opts, agent.WithObserver(o.observer))
	}
	if o.memory != nil {
		opts = append(opts, agent.WithTool(o.createMemoryReadTool()))
		opts = append(opts, agent.WithTool(o.createMemoryWriteTool()))
	}
	if o.config.Pattern == PatternSupervisor && cfg.Role == RoleSupervisor {
		opts = append(opts, agent.WithTool(o.createDelegationTool()))
	}

	// Create the agent
	ag, err := agent.New(cfg.Provider, opts...)
	if err != nil {
		return fmt.Errorf("failed to create agent %q: %w", cfg.Name, err)
	}

	managed := &ManagedAgent{
		ID:          cfg.ID,
		Name:        cfg.Name,
		Description: cfg.Description,
		Role:        cfg.Role,
		Agent:       ag,
		Provider:    cfg.Provider,
		CreatedAt:   time.Now().UTC(),
	}

	o.mu.Lock()
	o.agents[cfg.ID] = managed
	o.mu.Unlock()

	// Register in the registry
	o.registry.Register(AgentInfo{
		ID:           cfg.ID,
		Name:         cfg.Name,
		Description:  cfg.Description,
		Role:         cfg.Role,
		Capabilities: extractCapabilities(cfg.Tools),
	})

	return nil
}

// Run executes the multi-agent system with the given input.
func (o *Orchestrator) Run(ctx context.Context, input string) (*MultiAgentResult, error) {
	if len(o.agents) == 0 {
		return nil, errors.New("no agents registered")
	}

	runID := uuid.NewString()
	startTime := time.Now()

	o.emit(ctx, observe.Event{
		Kind:   observe.KindCustom,
		Status: observe.StatusStarted,
		Name:   "multiagent.run",
		Attributes: map[string]any{
			"runId":   runID,
			"pattern": string(o.config.Pattern),
			"agents":  len(o.agents),
		},
	})

	var result *MultiAgentResult
	var err error

	switch o.config.Pattern {
	case PatternSequential:
		result, err = o.runSequential(ctx, runID, input)
	case PatternParallel:
		result, err = o.runParallel(ctx, runID, input)
	case PatternSupervisor:
		result, err = o.runSupervisor(ctx, runID, input)
	case PatternRouter:
		result, err = o.runRouter(ctx, runID, input)
	case PatternDebate:
		result, err = o.runDebate(ctx, runID, input)
	case PatternConsensus:
		result, err = o.runConsensus(ctx, runID, input)
	default:
		err = fmt.Errorf("unknown execution pattern: %s", o.config.Pattern)
	}

	if err != nil {
		o.emit(ctx, observe.Event{
			Kind:   observe.KindCustom,
			Status: observe.StatusFailed,
			Name:   "multiagent.run",
			Attributes: map[string]any{
				"runId": runID,
				"error": err.Error(),
			},
		})
		return nil, err
	}

	result.RunID = runID
	result.Pattern = o.config.Pattern
	result.TotalDuration = time.Since(startTime)

	o.emit(ctx, observe.Event{
		Kind:   observe.KindCustom,
		Status: observe.StatusCompleted,
		Name:   "multiagent.run",
		Attributes: map[string]any{
			"runId":    runID,
			"duration": result.TotalDuration.String(),
			"tokens":   result.TotalTokens,
		},
	})

	return result, nil
}

// GetAgent returns a managed agent by ID.
func (o *Orchestrator) GetAgent(id string) (*ManagedAgent, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	ag, ok := o.agents[id]
	return ag, ok
}

// ListAgents returns all registered agents.
func (o *Orchestrator) ListAgents() []*ManagedAgent {
	o.mu.RLock()
	defer o.mu.RUnlock()
	agents := make([]*ManagedAgent, 0, len(o.agents))
	for _, ag := range o.agents {
		agents = append(agents, ag)
	}
	return agents
}

// Memory returns the shared memory (nil if not enabled).
func (o *Orchestrator) Memory() *SharedMemory {
	return o.memory
}

func (o *Orchestrator) emit(ctx context.Context, evt observe.Event) {
	if o.observer != nil {
		_ = o.observer.Emit(ctx, evt)
	}
}

func extractCapabilities(toolList []tools.Tool) []string {
	caps := make([]string, 0, len(toolList))
	for _, t := range toolList {
		caps = append(caps, t.Definition().Name)
	}
	return caps
}
