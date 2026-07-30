package server

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/swarmviz/swarmviz/pkg/hub"
)

type FileTreeProvider interface {
	GetFileTree() interface{}
}

type LOCDeltaProvider interface {
	GetLOCDelta() (added int64, removed int64)
}

// LOCHistoryProvider is optional: when the LOCDeltaProvider also implements it,
// INIT_STATE carries the chart series so a reconnecting client redraws the
// whole curve instead of restarting from a single point.
type LOCHistoryProvider interface {
	GetLOCHistory() interface{}
}

type ServerOption func(*Server)

func WithStaticFS(staticFS fs.FS) ServerOption {
	return func(s *Server) {
		s.staticFS = staticFS
	}
}

// WithAPIToken requires callers of the mutating /api/ endpoints to present
// "Authorization: Bearer <token>". Empty leaves the ingest API open, which is
// the default because the server listens on loopback.
func WithAPIToken(token string) ServerOption {
	return func(s *Server) {
		s.apiToken = token
	}
}

type Server struct {
	mu               sync.RWMutex
	eventHub         hub.AgentEventHub
	ringBuffer       *RingBuffer
	clients          map[*Client]bool
	fileTreeProvider FileTreeProvider
	locDeltaProvider LOCDeltaProvider
	unsubscribeHub   func()
	staticFS         fs.FS
	apiToken         string
}

func NewServer(eventHub hub.AgentEventHub, bufferSize int, ftProvider FileTreeProvider, locProvider LOCDeltaProvider, opts ...ServerOption) *Server {
	rb := NewRingBuffer(bufferSize)
	s := &Server{
		eventHub:         eventHub,
		ringBuffer:       rb,
		clients:          make(map[*Client]bool),
		fileTreeProvider: ftProvider,
		locDeltaProvider: locProvider,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if eventHub != nil {
		s.unsubscribeHub = eventHub.Subscribe(s.handleHubEvent)
	}

	return s
}

func (s *Server) SetStaticFS(staticFS fs.FS) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticFS = staticFS
}

func (s *Server) Close() {
	if s.unsubscribeHub != nil {
		s.unsubscribeHub()
		s.unsubscribeHub = nil
	}

	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		delete(s.clients, c)
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		c.close()
	}
}

func (s *Server) handleHubEvent(event hub.HubEvent) {
	env := s.ringBuffer.Push(event.Type, event.Data, event.Timestamp)
	s.BroadcastEnvelope(env)
}

func (s *Server) BroadcastEvent(eventType string, data interface{}) WSEnvelope {
	env := s.ringBuffer.Push(eventType, data, time.Now().UTC())
	s.BroadcastEnvelope(env)
	return env
}

func (s *Server) BroadcastEnvelope(env WSEnvelope) {
	data, err := json.Marshal(env)
	if err != nil {
		return
	}

	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()

	for _, c := range clients {
		if !c.safeSend(data) {
			go s.unregisterClient(c)
		}
	}
}

func (s *Server) registerClient(c *Client) {
	s.mu.Lock()
	s.clients[c] = true
	s.mu.Unlock()
}

func (s *Server) unregisterClient(c *Client) {
	s.mu.Lock()
	_, exists := s.clients[c]
	if exists {
		delete(s.clients, c)
	}
	s.mu.Unlock()

	if exists {
		c.close()
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" || strings.HasPrefix(r.URL.Path, "/ws/") {
		s.handleWebSocket(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.handleAPI(w, r)
		return
	}

	s.mu.RLock()
	staticFS := s.staticFS
	s.mu.RUnlock()

	if staticFS == nil {
		http.NotFound(w, r)
		return
	}

	s.serveStaticOrSPA(w, r, staticFS)
}

func (s *Server) serveStaticOrSPA(w http.ResponseWriter, r *http.Request, staticFS fs.FS) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	cleanPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "." || cleanPath == "" {
		cleanPath = "index.html"
	}

	// 1. Try serving exact static file
	if s.tryServeFSFile(w, r, staticFS, cleanPath) {
		return
	}

	// 2. SPA routing fallback to index.html
	if !s.tryServeFSFile(w, r, staticFS, "index.html") {
		http.NotFound(w, r)
	}
}

func (s *Server) tryServeFSFile(w http.ResponseWriter, r *http.Request, staticFS fs.FS, filePath string) bool {
	f, err := staticFS.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	var seeker io.ReadSeeker
	if rs, ok := f.(io.ReadSeeker); ok {
		seeker = rs
	} else {
		content, readErr := io.ReadAll(f)
		if readErr != nil {
			return false
		}
		seeker = bytes.NewReader(content)
	}

	mimeType := getMimeType(filePath)
	w.Header().Set("Content-Type", mimeType)

	http.ServeContent(w, r, stat.Name(), stat.ModTime(), seeker)
	return true
}

func getMimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttf":
		return "font/ttf"
	case ".wasm":
		return "application/wasm"
	default:
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			return mimeType
		}
		return "application/octet-stream"
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &Client{
		server: s,
		conn:   conn,
		send:   make(chan []byte, 256),
	}

	sinceStr := r.URL.Query().Get("since")
	var sinceSeq uint64
	if sinceStr != "" {
		sinceSeq, _ = strconv.ParseUint(sinceStr, 10, 64)
	}

	var initEnvelopes []WSEnvelope
	useReplay := false

	if sinceSeq > 0 {
		replayed, ok := s.ringBuffer.GetSince(sinceSeq)
		if ok {
			useReplay = true
			initEnvelopes = replayed
		}
	}

	if !useReplay {
		initState := s.buildInitState()
		initEnv := WSEnvelope{
			Version:   1,
			Seq:       0,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Type:      "INIT_STATE",
			Data:      initState,
		}
		initEnvelopes = []WSEnvelope{initEnv}
	}

	s.registerClient(client)

	for _, env := range initEnvelopes {
		data, err := json.Marshal(env)
		if err == nil {
			client.safeSend(data)
		}
	}

	go client.writePump()
	go client.readPump()
}

func (s *Server) buildInitState() map[string]interface{} {
	var agents map[string]*hub.AgentNode
	var edges []hub.AgentEdge
	var activeClaims map[string]*hub.ActiveClaimInfo
	var conflicts map[string][]string

	if s.eventHub != nil {
		agents, edges, activeClaims, conflicts = s.eventHub.GetInitSnapshot()
	}

	var fileTree interface{}
	if s.fileTreeProvider != nil {
		fileTree = s.fileTreeProvider.GetFileTree()
	}

	var added, removed int64
	var locHistory interface{}
	if s.locDeltaProvider != nil {
		added, removed = s.locDeltaProvider.GetLOCDelta()
		if hp, ok := s.locDeltaProvider.(LOCHistoryProvider); ok {
			locHistory = hp.GetLOCHistory()
		}
	}

	recentEvents := s.ringBuffer.GetRecentEvents(50)

	return map[string]interface{}{
		"agents":        agents,
		"edges":         edges,
		"active_claims": activeClaims,
		"conflicts":     conflicts,
		"file_tree":     fileTree,
		"total_loc": map[string]interface{}{
			"total_added":   added,
			"total_removed": removed,
		},
		"loc_history":   locHistory,
		"recent_events": recentEvents,
		"last_seq":      s.ringBuffer.GetNewestSeq(),
	}
}
