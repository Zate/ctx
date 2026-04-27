package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zate/ctx/internal/auth"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/query"
	ctxsync "github.com/zate/ctx/internal/sync"
	"github.com/zate/ctx/internal/view"
)

// Server is the ctx HTTP API server.
type Server struct {
	store  db.Store
	mux    *http.ServeMux
	config Config
	flows  *auth.DeviceFlowStore
}

// New creates a new Server with the given store and config.
func New(store db.Store, cfg Config) *Server {
	s := &Server{
		store:  store,
		mux:    http.NewServeMux(),
		config: cfg,
		flows:  auth.NewDeviceFlowStore(),
	}
	s.registerRoutes()
	s.registerAuthRoutes()
	s.registerWebUIRoutes()
	return s
}

// Handler returns the http.Handler with middleware applied.
func (s *Server) Handler() http.Handler {
	var handler http.Handler = s.mux
	if s.config.AdminPassword != "" {
		handler = s.authMiddleware(handler)
	}
	return loggingMiddleware(handler)
}

// ListenAndServe starts the server. Uses TLS if configured.
func (s *Server) ListenAndServe() error {
	addr := s.config.Addr()
	handler := s.Handler()

	if s.config.HasTLS() {
		log.Printf("ctx server listening on https://%s", addr)
		return http.ListenAndServeTLS(addr, s.config.TLSCert, s.config.TLSKey, handler)
	}

	log.Printf("ctx server listening on http://%s", addr)
	return http.ListenAndServe(addr, handler)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)

	// Node CRUD
	s.mux.HandleFunc("POST /api/nodes", s.handleCreateNode)
	s.mux.HandleFunc("GET /api/nodes/{id}", s.handleGetNode)
	s.mux.HandleFunc("PATCH /api/nodes/{id}", s.handleUpdateNode)
	s.mux.HandleFunc("DELETE /api/nodes/{id}", s.handleDeleteNode)

	// Edges
	s.mux.HandleFunc("GET /api/edges/{id}", s.handleGetEdges)
	s.mux.HandleFunc("POST /api/edges", s.handleCreateEdge)
	s.mux.HandleFunc("DELETE /api/edges", s.handleDeleteEdge)

	// Tags
	s.mux.HandleFunc("POST /api/nodes/{id}/tags", s.handleAddTags)
	s.mux.HandleFunc("DELETE /api/nodes/{id}/tags", s.handleRemoveTags)

	// Query and compose
	s.mux.HandleFunc("POST /api/query", s.handleQuery)
	s.mux.HandleFunc("POST /api/compose", s.handleCompose)

	// Sync
	s.mux.HandleFunc("POST /api/sync/push", s.handleSyncPush)
	s.mux.HandleFunc("POST /api/sync/pull", s.handleSyncPull)

	// Repo mappings
	s.mux.HandleFunc("POST /api/repo-mappings", s.handleCreateRepoMapping)
}

// --- Health ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Status ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var totalNodes, totalTokens, edgeCount, tagCount int
	_ = s.store.QueryRow("SELECT COUNT(*) FROM nodes WHERE superseded_by IS NULL").Scan(&totalNodes)
	_ = s.store.QueryRow("SELECT COALESCE(SUM(token_estimate), 0) FROM nodes WHERE superseded_by IS NULL").Scan(&totalTokens)
	_ = s.store.QueryRow("SELECT COUNT(*) FROM edges").Scan(&edgeCount)
	_ = s.store.QueryRow("SELECT COUNT(DISTINCT tag) FROM tags").Scan(&tagCount)

	writeJSON(w, http.StatusOK, map[string]any{
		"total_nodes":  totalNodes,
		"total_tokens": totalTokens,
		"total_edges":  edgeCount,
		"unique_tags":  tagCount,
	})
}

// --- Node CRUD ---

type createNodeRequest struct {
	Type     string   `json:"type"`
	Content  string   `json:"content"`
	Summary  *string  `json:"summary,omitempty"`
	Metadata string   `json:"metadata,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req createNodeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	input := db.CreateNodeInput{
		Type:     req.Type,
		Content:  req.Content,
		Summary:  req.Summary,
		Metadata: req.Metadata,
		Tags:     req.Tags,
	}

	node, err := s.store.CreateNode(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, node)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolvePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	node, err := s.store.GetNode(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, node)
}

type updateNodeRequest struct {
	Content  *string `json:"content,omitempty"`
	Type     *string `json:"type,omitempty"`
	Summary  *string `json:"summary,omitempty"`
	Metadata *string `json:"metadata,omitempty"`
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolvePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var req updateNodeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	node, err := s.store.UpdateNode(id, db.UpdateNodeInput{
		Content:  req.Content,
		Type:     req.Type,
		Summary:  req.Summary,
		Metadata: req.Metadata,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolvePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := s.store.DeleteNode(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// --- Edges ---

func (s *Server) handleGetEdges(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolvePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "both"
	}

	edges, err := s.store.GetEdges(id, direction)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, edges)
}

type createEdgeRequest struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Type   string `json:"type"`
}

func (s *Server) handleCreateEdge(w http.ResponseWriter, r *http.Request) {
	var req createEdgeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fromID, err := s.resolvePathID(req.FromID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve from_id: %v", err))
		return
	}
	toID, err := s.resolvePathID(req.ToID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve to_id: %v", err))
		return
	}

	edge, err := s.store.CreateEdge(fromID, toID, req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, edge)
}

type deleteEdgeRequest struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Type   string `json:"type,omitempty"`
}

func (s *Server) handleDeleteEdge(w http.ResponseWriter, r *http.Request) {
	var req deleteEdgeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fromID, err := s.resolvePathID(req.FromID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve from_id: %v", err))
		return
	}
	toID, err := s.resolvePathID(req.ToID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve to_id: %v", err))
		return
	}

	if err := s.store.DeleteEdge(fromID, toID, req.Type); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Tags ---

type tagsRequest struct {
	Tags []string `json:"tags"`
}

func (s *Server) handleAddTags(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolvePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var req tagsRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	for _, tag := range req.Tags {
		if err := s.store.AddTag(id, tag); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	tags, _ := s.store.GetTags(id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "tags": tags})
}

func (s *Server) handleRemoveTags(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolvePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var req tagsRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	for _, tag := range req.Tags {
		if err := s.store.RemoveTag(id, tag); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	tags, _ := s.store.GetTags(id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "tags": tags})
}

// --- Query ---

type queryRequest struct {
	Query             string `json:"query"`
	IncludeSuperseded bool   `json:"include_superseded"`
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	nodes, err := query.ExecuteQuery(s.store, req.Query, req.IncludeSuperseded)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(nodes),
		"nodes": nodes,
	})
}

// --- Compose ---

type composeRequest struct {
	Query    string   `json:"query,omitempty"`
	IDs      []string `json:"ids,omitempty"`
	SeedID   string   `json:"seed,omitempty"`
	Depth    int      `json:"depth,omitempty"`
	Budget   int      `json:"budget,omitempty"`
	Template string   `json:"template,omitempty"`
	Edges    bool     `json:"edges,omitempty"`
}

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var req composeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	budget := req.Budget
	if budget <= 0 {
		budget = 50000
	}
	depth := req.Depth
	if depth <= 0 {
		depth = 1
	}

	opts := view.ComposeOptions{
		Query:        req.Query,
		IDs:          req.IDs,
		SeedID:       req.SeedID,
		Depth:        depth,
		Budget:       budget,
		IncludeEdges: req.Edges,
	}

	result, err := view.Compose(s.store, opts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Template != "" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, view.RenderTemplate(result, req.Template))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"node_count":   result.NodeCount,
		"total_tokens": result.TotalTokens,
		"rendered_at":  result.RenderedAt,
		"nodes":        result.Nodes,
		"edges":        result.Edges,
	})
}

// --- Sync ---

func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request) {
	var req ctxsync.PushRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var accepted, conflicts int
	var serverVersion int64

	for _, change := range req.Changes {
		if change.Node == nil {
			continue
		}

		if change.Deleted {
			_ = s.store.DeleteNode(change.Node.ID)
			accepted++
			continue
		}

		// Check if node exists on server
		existing, err := s.store.GetNode(change.Node.ID)
		if err != nil {
			// Node doesn't exist on server — create it
			node, createErr := s.store.CreateNode(db.CreateNodeInput{
				Type:     change.Node.Type,
				Content:  change.Node.Content,
				Summary:  change.Node.Summary,
				Metadata: change.Node.Metadata,
				Tags:     change.Node.Tags,
			})
			if createErr != nil {
				conflicts++
				continue
			}
			// Update sync_version on the newly created node
			_, _ = s.store.Exec("UPDATE nodes SET sync_version = sync_version + 1 WHERE id = $1", node.ID)
			accepted++
			continue
		}

		// Node exists — last-write-wins
		if existing.UpdatedAt.After(change.Node.UpdatedAt) {
			conflicts++
			continue
		}

		content := change.Node.Content
		nodeType := change.Node.Type
		_, _ = s.store.UpdateNode(change.Node.ID, db.UpdateNodeInput{
			Content: &content,
			Type:    &nodeType,
			Summary: change.Node.Summary,
		})
		_, _ = s.store.Exec("UPDATE nodes SET sync_version = sync_version + 1 WHERE id = $1", change.Node.ID)
		accepted++
	}

	// Get current max sync version
	_ = s.store.QueryRow("SELECT COALESCE(MAX(sync_version), 0) FROM nodes").Scan(&serverVersion)

	writeJSON(w, http.StatusOK, ctxsync.PushResponse{
		Accepted:    accepted,
		Conflicts:   conflicts,
		SyncVersion: serverVersion,
	})
}

func (s *Server) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	var req ctxsync.PullRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	changes, maxVersion, err := ctxsync.GetLocalChanges(s.store, req.SyncVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ctxsync.PullResponse{
		Changes:     changes,
		SyncVersion: maxVersion,
	})
}

func (s *Server) handleCreateRepoMapping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NormalizedURL string `json:"normalized_url"`
		ProjectTag    string `json:"project_tag"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id := db.NewID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.Exec(
		`INSERT INTO repo_mappings (id, normalized_url, project_tag, created_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (normalized_url) DO UPDATE SET project_tag = $3`,
		id, req.NormalizedURL, req.ProjectTag, now,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"normalized_url": req.NormalizedURL,
		"project_tag":    req.ProjectTag,
	})
}

// --- Helpers ---

func (s *Server) resolvePathID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing id")
	}
	return s.store.ResolveID(raw)
}

func readJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}
	if len(body) == 0 {
		return fmt.Errorf("empty request body")
	}
	return json.Unmarshal(body, v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
