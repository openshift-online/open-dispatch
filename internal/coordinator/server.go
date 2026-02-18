package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPort           = ":8899"
	DefaultSpaceName      = "default"
	DefaultStaleThreshold = 10 * time.Minute
)

const protocolFileName = "protocol.md"

type sseClient struct {
	ch    chan []byte
	space string
}

type Server struct {
	port             string
	dataDir          string
	spaces           map[string]*KnowledgeSpace
	mu               sync.RWMutex
	httpServer       *http.Server
	running          bool
	runMu            sync.Mutex
	EventLog         []string
	eventMu          sync.Mutex
	protocolTemplate string
	staleThreshold   time.Duration
	sseClients       map[*sseClient]struct{}
	sseMu            sync.Mutex
}

func NewServer(port, dataDir string) *Server {
	if port == "" {
		port = DefaultPort
	}
	return &Server{
		port:           port,
		dataDir:        dataDir,
		spaces:         make(map[string]*KnowledgeSpace),
		staleThreshold: DefaultStaleThreshold,
		sseClients:     make(map[*sseClient]struct{}),
	}
}

func (s *Server) SetStaleThreshold(d time.Duration) {
	s.staleThreshold = d
}

func (s *Server) Running() bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.running
}

func (s *Server) Port() string {
	return s.port
}

func (s *Server) logEvent(msg string) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	s.EventLog = append(s.EventLog, entry)
	if len(s.EventLog) > 200 {
		s.EventLog = s.EventLog[len(s.EventLog)-200:]
	}
}

func (s *Server) RecentEvents(n int) []string {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if n > len(s.EventLog) {
		n = len(s.EventLog)
	}
	out := make([]string, n)
	copy(out, s.EventLog[len(s.EventLog)-n:])
	return out
}

func (s *Server) Start() error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running {
		return fmt.Errorf("already running")
	}

	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	s.protocolTemplate = s.loadProtocolTemplate()

	if err := s.loadAllSpaces(); err != nil {
		return fmt.Errorf("load spaces: %w", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/spaces", s.handleListSpaces)
	mux.HandleFunc("/spaces/", s.handleSpaceRoute)
	mux.HandleFunc("/events", s.handleSSE)
	mux.HandleFunc("/raw", func(w http.ResponseWriter, r *http.Request) {
		s.handleSpaceRaw(w, r, DefaultSpaceName)
	})
	mux.HandleFunc("/agent/", func(w http.ResponseWriter, r *http.Request) {
		agentName := strings.TrimPrefix(r.URL.Path, "/agent/")
		agentName = strings.TrimRight(agentName, "/")
		s.handleSpaceAgent(w, r, DefaultSpaceName, agentName)
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		s.handleSpaceAgentsJSON(w, r, DefaultSpaceName)
	})

	listener, err := net.Listen("tcp", s.port)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.port, err)
	}
	s.port = ":" + strings.Split(listener.Addr().String(), ":")[len(strings.Split(listener.Addr().String(), ":"))-1]

	s.httpServer = &http.Server{Handler: mux}
	s.running = true

	go func() {
		s.logEvent(fmt.Sprintf("coordinator started on %s (data: %s)", s.port, s.dataDir))
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logEvent(fmt.Sprintf("server error: %v", err))
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if !s.running {
		return fmt.Errorf("not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.httpServer.Shutdown(ctx)
	s.running = false
	s.logEvent("coordinator stopped")
	return err
}

func (s *Server) spacePath(name string) string {
	return filepath.Join(s.dataDir, name+".json")
}

func (s *Server) spaceMarkdownPath(name string) string {
	return filepath.Join(s.dataDir, name+".md")
}

func (s *Server) loadAllSpaces() error {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		ks, err := s.loadSpace(name)
		if err != nil {
			s.logEvent(fmt.Sprintf("failed to load space %q: %v", name, err))
			continue
		}
		s.spaces[name] = ks
		s.logEvent(fmt.Sprintf("loaded space %q (%d agents)", name, len(ks.Agents)))
	}
	return nil
}

func (s *Server) loadSpace(name string) (*KnowledgeSpace, error) {
	data, err := os.ReadFile(s.spacePath(name))
	if err != nil {
		return nil, err
	}
	var ks KnowledgeSpace
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", name, err)
	}
	if ks.Agents == nil {
		ks.Agents = make(map[string]*AgentUpdate)
	}
	return &ks, nil
}

func (s *Server) saveSpace(ks *KnowledgeSpace) error {
	s.refreshProtocol(ks)
	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", ks.Name, err)
	}
	if err := os.WriteFile(s.spacePath(ks.Name), data, 0644); err != nil {
		return err
	}
	md := ks.RenderMarkdown()
	if err := os.WriteFile(s.spaceMarkdownPath(ks.Name), []byte(md), 0644); err != nil {
		s.logEvent(fmt.Sprintf("warning: failed to write markdown for %q: %v", ks.Name, err))
	}
	return nil
}

func (s *Server) refreshProtocol(ks *KnowledgeSpace) {
	template := s.loadProtocolTemplate()
	if template == "" {
		return
	}
	s.protocolTemplate = template
	ks.SharedContracts = strings.ReplaceAll(template, "{SPACE}", ks.Name)
}

func (s *Server) getOrCreateSpace(name string) *KnowledgeSpace {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ks, ok := s.spaces[name]; ok {
		return ks
	}
	ks := NewKnowledgeSpace(name)
	s.spaces[name] = ks
	s.logEvent(fmt.Sprintf("created space %q", name))
	return ks
}

func (s *Server) loadProtocolTemplate() string {
	path := filepath.Join(s.dataDir, protocolFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *Server) getSpace(name string) (*KnowledgeSpace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ks, ok := s.spaces[name]
	return ks, ok
}

func (s *Server) listSpaceNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.spaces))
	for name := range s.spaces {
		names = append(names, name)
	}
	return names
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, missionControlHTML)
}

func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type spaceSummary struct {
		Name       string    `json:"name"`
		AgentCount int       `json:"agent_count"`
		CreatedAt  time.Time `json:"created_at"`
		UpdatedAt  time.Time `json:"updated_at"`
	}

	s.mu.RLock()
	summaries := make([]spaceSummary, 0, len(s.spaces))
	for _, ks := range s.spaces {
		summaries = append(summaries, spaceSummary{
			Name:       ks.Name,
			AgentCount: len(ks.Agents),
			CreatedAt:  ks.CreatedAt,
			UpdatedAt:  ks.UpdatedAt,
		})
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summaries)
}

func (s *Server) handleSpaceRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/spaces/")
	parts := strings.SplitN(path, "/", 3)

	spaceName := parts[0]
	if spaceName == "" {
		s.handleListSpaces(w, r)
		return
	}

	if len(parts) == 1 || (len(parts) == 2 && parts[1] == "") {
		if r.Method == http.MethodDelete {
			s.handleDeleteSpace(w, r, spaceName)
			return
		}
		s.handleSpaceView(w, r, spaceName)
		return
	}

	subRoute := parts[1]

	switch subRoute {
	case "events":
		s.handleSpaceSSE(w, r, spaceName)
	case "raw":
		s.handleSpaceRaw(w, r, spaceName)
	case "contracts":
		s.handleSpaceContracts(w, r, spaceName)
	case "archive":
		s.handleSpaceArchive(w, r, spaceName)
	case "agent":
		agentName := ""
		if len(parts) == 3 {
			agentName = strings.TrimRight(parts[2], "/")
		}
		s.handleSpaceAgent(w, r, spaceName, agentName)
	case "api":
		if len(parts) == 3 && strings.TrimRight(parts[2], "/") == "agents" {
			s.handleSpaceAgentsJSON(w, r, spaceName)
		} else {
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSpaceView(w http.ResponseWriter, r *http.Request, spaceName string) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		s.handleSpaceJSON(w, r, spaceName)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, missionControlHTML)
}

func (s *Server) handleSpaceJSON(w http.ResponseWriter, r *http.Request, spaceName string) {
	if r.Method == http.MethodDelete {
		s.handleDeleteSpace(w, r, spaceName)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ks, ok := s.getSpace(spaceName)
	if !ok {
		http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ks)
}

func (s *Server) handleDeleteSpace(w http.ResponseWriter, _ *http.Request, spaceName string) {
	s.mu.Lock()
	_, ok := s.spaces[spaceName]
	if !ok {
		s.mu.Unlock()
		http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
		return
	}
	delete(s.spaces, spaceName)
	s.mu.Unlock()

	os.Remove(s.spacePath(spaceName))
	os.Remove(s.spaceMarkdownPath(spaceName))

	s.logEvent(fmt.Sprintf("space %q deleted", spaceName))
	s.broadcastSSE(spaceName, "space_deleted", spaceName)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "deleted space %q", spaceName)
}

func (s *Server) handleSpaceRaw(w http.ResponseWriter, r *http.Request, spaceName string) {
	switch r.Method {
	case http.MethodGet:
		ks, ok := s.getSpace(spaceName)
		if !ok {
			http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, ks.RenderMarkdownWithStaleness(s.staleThreshold))

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		ks := s.getOrCreateSpace(spaceName)
		s.mu.Lock()
		ks.SharedContracts = sanitizeInput(string(body))
		ks.UpdatedAt = time.Now().UTC()
		if err := s.saveSpace(ks); err != nil {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf("save: %v", err), http.StatusInternalServerError)
			return
		}
		s.mu.Unlock()
		s.logEvent(fmt.Sprintf("[%s] shared contracts updated (%d bytes)", spaceName, len(body)))
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSpaceContracts(w http.ResponseWriter, r *http.Request, spaceName string) {
	switch r.Method {
	case http.MethodGet:
		ks, ok := s.getSpace(spaceName)
		if !ok {
			http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, ks.SharedContracts)

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		ks := s.getOrCreateSpace(spaceName)
		s.mu.Lock()
		ks.SharedContracts = sanitizeInput(string(body))
		ks.UpdatedAt = time.Now().UTC()
		if err := s.saveSpace(ks); err != nil {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf("save: %v", err), http.StatusInternalServerError)
			return
		}
		s.mu.Unlock()
		s.logEvent(fmt.Sprintf("[%s] contracts updated (%d bytes)", spaceName, len(body)))
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSpaceArchive(w http.ResponseWriter, r *http.Request, spaceName string) {
	switch r.Method {
	case http.MethodGet:
		ks, ok := s.getSpace(spaceName)
		if !ok {
			http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, ks.Archive)

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		ks := s.getOrCreateSpace(spaceName)
		s.mu.Lock()
		ks.Archive = sanitizeInput(string(body))
		ks.UpdatedAt = time.Now().UTC()
		if err := s.saveSpace(ks); err != nil {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf("save: %v", err), http.StatusInternalServerError)
			return
		}
		s.mu.Unlock()
		s.logEvent(fmt.Sprintf("[%s] archive updated (%d bytes)", spaceName, len(body)))
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSpaceAgent(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	if agentName == "" {
		http.Error(w, "missing agent name", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		ks, ok := s.getSpace(spaceName)
		if !ok {
			http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
			return
		}
		canonical := resolveAgentName(ks, agentName)
		s.mu.RLock()
		agent, exists := ks.Agents[canonical]
		s.mu.RUnlock()
		if !exists {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "{}")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent)

	case http.MethodPost:
		ks := s.getOrCreateSpace(spaceName)
		canonical := resolveAgentName(ks, agentName)

		contentType := r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var update AgentUpdate

		if strings.Contains(contentType, "application/json") {
			if err := json.Unmarshal(body, &update); err != nil {
				http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
				return
			}
		} else {
			update = AgentUpdate{
				Status:   StatusActive,
				Summary:  truncateLine(string(body), 120),
				FreeText: string(body),
			}
		}

		sanitizeAgentUpdate(&update)

		if err := update.Validate(); err != nil {
			http.Error(w, fmt.Sprintf("validation: %v", err), http.StatusBadRequest)
			return
		}

		update.UpdatedAt = time.Now().UTC()

		s.mu.Lock()
		ks.Agents[canonical] = &update
		ks.UpdatedAt = time.Now().UTC()
		if err := s.saveSpace(ks); err != nil {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf("save: %v", err), http.StatusInternalServerError)
			return
		}
		s.mu.Unlock()

		s.logEvent(fmt.Sprintf("[%s/%s] %s: %s", spaceName, canonical, update.Status, update.Summary))
		sseData, _ := json.Marshal(map[string]string{"space": spaceName, "agent": canonical, "status": string(update.Status), "summary": update.Summary})
		s.broadcastSSE(spaceName, "agent_updated", string(sseData))
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "accepted for [%s] in space %q", canonical, spaceName)

	case http.MethodDelete:
		ks, ok := s.getSpace(spaceName)
		if !ok {
			http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
			return
		}
		canonical := resolveAgentName(ks, agentName)
		s.mu.Lock()
		delete(ks.Agents, canonical)
		ks.UpdatedAt = time.Now().UTC()
		if err := s.saveSpace(ks); err != nil {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf("save: %v", err), http.StatusInternalServerError)
			return
		}
		s.mu.Unlock()
		s.logEvent(fmt.Sprintf("[%s/%s] agent removed", spaceName, canonical))
		sseData, _ := json.Marshal(map[string]string{"space": spaceName, "agent": canonical})
		s.broadcastSSE(spaceName, "agent_removed", string(sseData))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "removed [%s] from space %q", canonical, spaceName)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSpaceAgentsJSON(w http.ResponseWriter, r *http.Request, spaceName string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ks, ok := s.getSpace(spaceName)
	if !ok {
		http.Error(w, fmt.Sprintf("space %q not found", spaceName), http.StatusNotFound)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ks.Agents)
}

func (s *Server) broadcastSSE(space, event, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	payload := []byte(msg)
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for c := range s.sseClients {
		if c.space == "" || c.space == space {
			select {
			case c.ch <- payload:
			default:
			}
		}
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, "")
}

func (s *Server) handleSpaceSSE(w http.ResponseWriter, r *http.Request, spaceName string) {
	s.serveSSE(w, r, spaceName)
}

func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, space string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	client := &sseClient{ch: make(chan []byte, 64), space: space}
	s.sseMu.Lock()
	s.sseClients[client] = struct{}{}
	s.sseMu.Unlock()

	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, client)
		s.sseMu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-client.ch:
			w.Write(msg)
			flusher.Flush()
		}
	}
}

func (s *Server) IsStale(agent *AgentUpdate) bool {
	if agent.Status == StatusDone || agent.Status == StatusError {
		return false
	}
	return time.Since(agent.UpdatedAt) > s.staleThreshold
}

func (s *Server) StaleAgents(spaceName string) map[string]*AgentUpdate {
	ks, ok := s.getSpace(spaceName)
	if !ok {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	stale := make(map[string]*AgentUpdate)
	for name, agent := range ks.Agents {
		if s.IsStale(agent) {
			stale[name] = agent
		}
	}
	return stale
}

func resolveAgentName(ks *KnowledgeSpace, raw string) string {
	for existing := range ks.Agents {
		if strings.EqualFold(existing, raw) {
			return existing
		}
	}
	return strings.ToUpper(raw[:1]) + strings.ToLower(raw[1:])
}

var devNullPattern = regexp.MustCompile(`\s*<\s*/dev/null\s*`)

func sanitizeInput(s string) string {
	return devNullPattern.ReplaceAllString(s, "")
}

func sanitizeAgentUpdate(u *AgentUpdate) {
	u.Summary = sanitizeInput(u.Summary)
	u.Phase = sanitizeInput(u.Phase)
	u.FreeText = sanitizeInput(u.FreeText)
	u.NextSteps = sanitizeInput(u.NextSteps)
	for i, item := range u.Items {
		u.Items[i] = sanitizeInput(item)
	}
	for i, q := range u.Questions {
		u.Questions[i] = sanitizeInput(q)
	}
	for i, b := range u.Blockers {
		u.Blockers[i] = sanitizeInput(b)
	}
	for si := range u.Sections {
		u.Sections[si].Title = sanitizeInput(u.Sections[si].Title)
		for i, item := range u.Sections[si].Items {
			u.Sections[si].Items[i] = sanitizeInput(item)
		}
	}
}

func truncateLine(s string, maxLen int) string {
	line := strings.SplitN(s, "\n", 2)[0]
	line = strings.TrimSpace(line)
	if len(line) > maxLen {
		return line[:maxLen-3] + "..."
	}
	return line
}
