package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
	"github.com/swarmviz/swarmviz/pkg/hub"
)

type mockFileTreeProvider struct{}

func (m *mockFileTreeProvider) GetFileTree() interface{} {
	return map[string]string{"main.go": "file"}
}

type mockLOCProvider struct{}

func (m *mockLOCProvider) GetLOCDelta() (int64, int64) {
	return 100, 20
}

type pipeListener struct {
	connChan  chan net.Conn
	closeOnce sync.Once
	closed    chan struct{}
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		connChan: make(chan net.Conn, 64),
		closed:   make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn, ok := <-l.connChan:
		if !ok {
			return nil, net.ErrClosed
		}
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

type testWSServer struct {
	ts      *httptest.Server
	inMem   *http.Server
	pipeL   *pipeListener
	isPipe  bool
	cleanup func()
}

func createOrPipeTestServer(t *testing.T, handler http.Handler) *testWSServer {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err == nil {
		ts := httptest.NewUnstartedServer(handler)
		ts.Listener = l
		ts.Start()
		return &testWSServer{
			ts: ts,
			cleanup: func() {
				ts.Close()
			},
		}
	}

	pl := newPipeListener()
	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(pl)
	}()

	return &testWSServer{
		inMem:  srv,
		pipeL:  pl,
		isPipe: true,
		cleanup: func() {
			_ = srv.Close()
			_ = pl.Close()
		},
	}
}

func (s *testWSServer) Dial(wsPath string) (*websocket.Conn, error) {
	if !s.isPipe {
		wsURL := "ws" + strings.TrimPrefix(s.ts.URL, "http") + wsPath
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		return ws, err
	}

	serverConn, clientConn := net.Pipe()
	s.pipeL.connChan <- serverConn

	u, err := url.Parse("ws://127.0.0.1:8080" + wsPath)
	if err != nil {
		serverConn.Close()
		clientConn.Close()
		return nil, err
	}
	reqHeader := make(http.Header)
	reqHeader.Set("Host", u.Host)

	ws, _, err := websocket.NewClient(clientConn, u, reqHeader, 4096, 4096)
	return ws, err
}

func TestServer_BuildInitState(t *testing.T) {
	eventHub := hub.NewEventHub()
	eventHub.AgentCreated("agent-1", hub.Worker, "", "Worker 1")
	_ = eventHub.ClaimFile("agent-1", "main.go")

	srv := NewServer(eventHub, 100, &mockFileTreeProvider{}, &mockLOCProvider{})
	defer srv.Close()

	initState := srv.buildInitState()

	if initState["agents"] == nil {
		t.Errorf("expected agents in init state")
	}
	if initState["active_claims"] == nil {
		t.Errorf("expected active_claims in init state")
	}
	if initState["file_tree"] == nil {
		t.Errorf("expected file_tree in init state")
	}
	if loc, ok := initState["total_loc"].(map[string]interface{}); !ok || loc["total_added"] != int64(100) {
		t.Errorf("expected total_added=100 in total_loc, got %+v", loc)
	}
}

type mockAgentEventHub struct {
	hub.AgentEventHub
	calledSnapshot bool
}

func (m *mockAgentEventHub) Subscribe(listener hub.EventListener) func() {
	return func() {}
}

func (m *mockAgentEventHub) GetInitSnapshot() (map[string]*hub.AgentNode, []hub.AgentEdge, map[string]*hub.ActiveClaimInfo, map[string][]string) {
	m.calledSnapshot = true
	agents := map[string]*hub.AgentNode{
		"mock-1": {ID: "mock-1", Label: "Mock Agent"},
	}
	return agents, nil, nil, nil
}

func TestServer_BuildInitState_CustomAgentEventHub(t *testing.T) {
	mockHub := &mockAgentEventHub{}
	srv := NewServer(mockHub, 100, nil, nil)
	defer srv.Close()

	initState := srv.buildInitState()

	if !mockHub.calledSnapshot {
		t.Fatalf("expected GetInitSnapshot to be called on interface")
	}
	agents, ok := initState["agents"].(map[string]*hub.AgentNode)
	if !ok || agents["mock-1"] == nil {
		t.Fatalf("expected mock-1 agent in initState, got %v", initState["agents"])
	}
}

func TestServer_BroadcastAndUnregister(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 100, nil, nil)
	defer srv.Close()

	client := &Client{
		server: srv,
		conn:   nil,
		send:   make(chan []byte, 10),
	}

	srv.registerClient(client)

	env := srv.BroadcastEvent("TEST_BROADCAST", map[string]string{"key": "value"})

	select {
	case data := <-client.send:
		var rEnv WSEnvelope
		if err := json.Unmarshal(data, &rEnv); err != nil {
			t.Fatalf("failed unmarshaling broadcast payload: %v", err)
		}
		if rEnv.Type != "TEST_BROADCAST" || rEnv.Seq != env.Seq {
			t.Errorf("expected type TEST_BROADCAST and seq=%d, got type=%s seq=%d", env.Seq, rEnv.Type, rEnv.Seq)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for client broadcast")
	}

	srv.unregisterClient(client)

	srv.mu.RLock()
	clientCount := len(srv.clients)
	srv.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", clientCount)
	}
}

func TestServer_InitStateOnConnect(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 100, nil, nil)
	defer srv.Close()

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	ws, err := ts.Dial("/ws")
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	var env WSEnvelope
	err = ws.ReadJSON(&env)
	if err != nil {
		t.Fatalf("failed to read JSON init state: %v", err)
	}

	if env.Type != "INIT_STATE" {
		t.Fatalf("expected INIT_STATE envelope, got %s", env.Type)
	}
}

func TestServer_EventReplay(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 100, nil, nil)
	defer srv.Close()

	srv.BroadcastEvent("EVENT_1", map[string]string{"foo": "bar"})
	srv.BroadcastEvent("EVENT_2", map[string]string{"foo": "baz"})

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	ws, err := ts.Dial("/ws?since=1")
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	var env WSEnvelope
	err = ws.ReadJSON(&env)
	if err != nil {
		t.Fatalf("failed to read replayed event: %v", err)
	}

	if env.Seq != 2 || env.Type != "EVENT_2" {
		t.Fatalf("expected replayed event seq=2, got seq=%d type=%s", env.Seq, env.Type)
	}
}

func TestServer_ControlProtocol_NotImplemented(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 100, nil, nil)
	defer srv.Close()

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	ws, err := ts.Dial("/ws")
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	var initEnv WSEnvelope
	_ = ws.ReadJSON(&initEnv)

	ctrlMsg := map[string]interface{}{
		"type": "AGENT_CONTROL",
		"data": map[string]string{
			"agent_id": "test-agent-1",
			"action":   "pause",
		},
	}
	ctrlBytes, _ := json.Marshal(ctrlMsg)
	err = ws.WriteMessage(websocket.TextMessage, ctrlBytes)
	if err != nil {
		t.Fatalf("failed to send AGENT_CONTROL message: %v", err)
	}

	var errEnv WSEnvelope
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = ws.ReadJSON(&errEnv)
	if err != nil {
		t.Fatalf("failed to read error envelope: %v", err)
	}

	if errEnv.Type != "ERROR" {
		t.Fatalf("expected ERROR envelope, got %s", errEnv.Type)
	}
}

func TestServer_ControlProtocol_Success(t *testing.T) {
	eventHub := hub.NewEventHub()
	var executedAction int32

	eventHub.RegisterControlHandler(func(agentID string, action hub.ControlAction) error {
		if agentID == "test-agent-1" && action == hub.ActionPause {
			atomic.StoreInt32(&executedAction, 1)
		}
		return nil
	})

	srv := NewServer(eventHub, 100, nil, nil)
	defer srv.Close()

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	ws, err := ts.Dial("/ws")
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	var initEnv WSEnvelope
	_ = ws.ReadJSON(&initEnv)

	ctrlMsg := map[string]interface{}{
		"type": "AGENT_CONTROL",
		"data": map[string]string{
			"agent_id": "test-agent-1",
			"action":   "pause",
		},
	}
	ctrlBytes, _ := json.Marshal(ctrlMsg)
	_ = ws.WriteMessage(websocket.TextMessage, ctrlBytes)

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executedAction) != 1 {
		t.Fatalf("expected control handler to be executed")
	}
}

func TestServer_StaticFileServingAndSPAFallback(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<html><body>SwarmViz App</body></html>")},
		"assets/app.js":    &fstest.MapFile{Data: []byte("console.log('swarmviz');")},
		"assets/style.css": &fstest.MapFile{Data: []byte("body { background: black; }")},
	}

	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 100, nil, nil, WithStaticFS(mockFS))
	defer srv.Close()

	// 1. Root GET / -> index.html
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for GET /, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("expected text/html content type, got %s", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "<html><body>SwarmViz App</body></html>" {
		t.Errorf("unexpected body content: %s", rec.Body.String())
	}

	// 2. Asset GET /assets/app.js -> javascript
	req = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for GET /assets/app.js, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Errorf("expected javascript content type, got %s", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "console.log('swarmviz');" {
		t.Errorf("unexpected body content: %s", rec.Body.String())
	}

	// 3. SPA fallback GET /agents/active -> index.html
	req = httptest.NewRequest(http.MethodGet, "/agents/active", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for SPA fallback GET /agents/active, got %d", rec.Code)
	}
	if rec.Body.String() != "<html><body>SwarmViz App</body></html>" {
		t.Errorf("expected SPA fallback body index.html, got %s", rec.Body.String())
	}

	// 4. API path non-existent GET /api/missing -> 404
	req = httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for /api/missing, got %d", rec.Code)
	}
}

