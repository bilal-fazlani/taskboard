package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

const waitFor = 3 * time.Second

// sseStream reads an event stream and reports each complete message: the
// event name for an event, or ":" plus the text for a comment.
type sseStream struct {
	resp     *http.Response
	messages chan string
	ended    chan struct{}
}

func openStream(t *testing.T, ctx context.Context, url string) *sseStream {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening event stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	s := &sseStream{resp: resp, messages: make(chan string, 100), ended: make(chan struct{})}
	go func() {
		defer close(s.ended)
		sc := bufio.NewScanner(resp.Body)
		var event, data string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if event != "" {
					s.messages <- event + " " + data
				}
				event, data = "", ""
			case strings.HasPrefix(line, ":"):
				s.messages <- line
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	s.expect(t, ": connected")
	return s
}

// expect waits for the next message and fails unless it is want.
func (s *sseStream) expect(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-s.messages:
		if got != want {
			t.Fatalf("got message %q, want %q", got, want)
		}
	case <-time.After(waitFor):
		t.Fatalf("no %q message within %v", want, waitFor)
	}
}

// expectNone fails if a message arrives within d.
func (s *sseStream) expectNone(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case got := <-s.messages:
		t.Fatalf("unexpected message %q", got)
	case <-time.After(d):
	}
}

func (s *sseStream) expectEnded(t *testing.T) {
	t.Helper()
	select {
	case <-s.ended:
	case <-time.After(waitFor):
		t.Fatal("event stream did not end")
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type running struct {
	srv     *Server
	watcher *db.Watcher
	url     string
	path    string
	stop    context.CancelFunc
	done    chan error
}

// serve runs Serve, with its database watcher, on a temp database and a
// loopback port. The server is stopped at the end of the test.
func serve(t *testing.T, opts ...Option) *running {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	opts = append([]Option{WithWatchInterval(10 * time.Millisecond)}, opts...)
	srv := New(db.NewStore(database), nil, opts...)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{srv: srv, url: "http://" + ln.Addr().String(), path: path, stop: cancel, done: make(chan error, 1)}
	orig := openWatcher
	opened := make(chan struct{})
	openWatcher = func(p string, d time.Duration) (*db.Watcher, error) {
		w, err := orig(p, d)
		r.watcher = w
		close(opened)
		return w, err
	}
	defer func() {
		<-opened
		openWatcher = orig
	}()
	go func() { r.done <- srv.Serve(ctx, ln, path) }()
	t.Cleanup(func() {
		cancel()
		<-r.done
	})
	return r
}

func TestEventsWriteFromAnotherConnection(t *testing.T) {
	r := serve(t)
	stream := openStream(t, context.Background(), r.url)

	// A separate pool on the same file, the way the CLI and MCP processes
	// open it.
	other, err := db.OpenAt(r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := db.NewStore(other).CreateProject(models.CreateProjectRequest{Name: "CLI", Prefix: "CLI"}); err != nil {
		t.Fatal(err)
	}

	stream.expect(t, "changed {}")
	stream.expectNone(t, 100*time.Millisecond)
}

func TestEventsWriteThroughAPI(t *testing.T) {
	r := serve(t)
	a := openStream(t, context.Background(), r.url)
	b := openStream(t, context.Background(), r.url)

	resp, err := http.Post(r.url+"/api/projects", "application/json",
		strings.NewReader(`{"name":"Web","prefix":"WEB"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: status %d", resp.StatusCode)
	}

	a.expect(t, "changed {}")
	b.expect(t, "changed {}")

	// Reads are not changes.
	resp, err = http.Get(r.url + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	a.expectNone(t, 100*time.Millisecond)
}

func TestEventsKeepAlive(t *testing.T) {
	srv := New(nil, nil, WithKeepAlive(20*time.Millisecond))
	ts := httptest.NewServer(srv)
	defer ts.Close()
	defer srv.events.close()

	stream := openStream(t, context.Background(), ts.URL)
	stream.expect(t, ": keep-alive")
	stream.expect(t, ": keep-alive")
}

func TestEventsDisconnectUnsubscribes(t *testing.T) {
	srv := New(nil, nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	defer srv.events.close()

	ctx, cancel := context.WithCancel(context.Background())
	openStream(t, ctx, ts.URL)
	stays := openStream(t, context.Background(), ts.URL)
	if n := srv.events.count(); n != 2 {
		t.Fatalf("subscribers = %d, want 2", n)
	}

	cancel()
	waitUntil(t, "the disconnected client to unsubscribe", func() bool { return srv.events.count() == 1 })

	srv.events.publish()
	stays.expect(t, "changed {}")
}

func TestBroadcasterSlowSubscriberDoesNotBlock(t *testing.T) {
	b := newBroadcaster()
	slow, unsubSlow := b.subscribe()
	defer unsubSlow()
	fast, unsubFast := b.subscribe()
	defer unsubFast()

	// Nobody reads slow; publishing many times must neither block nor queue
	// more than one pending signal for it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			b.publish()
			select {
			case <-fast:
			case <-time.After(waitFor):
				t.Error("fast subscriber missed a signal")
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(waitFor):
		t.Fatal("publish blocked on a slow subscriber")
	}
	if len(slow) != 1 {
		t.Fatalf("slow subscriber has %d pending signals, want 1", len(slow))
	}
}

func TestShutdownStopsWatcherAndEndsStreams(t *testing.T) {
	r := serve(t, WithKeepAlive(time.Hour))
	a := openStream(t, context.Background(), r.url)
	b := openStream(t, context.Background(), r.url)

	start := time.Now()
	r.stop()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
		// Keep the cleanup's receive from blocking.
		r.done <- nil
	case <-time.After(waitFor):
		t.Fatal("Serve did not return after its context was cancelled")
	}
	if d := time.Since(start); d > shutdownTimeout/2 {
		t.Fatalf("shutdown took %v; open streams held it up", d)
	}
	a.expectEnded(t)
	b.expectEnded(t)
	if n := r.srv.events.count(); n != 0 {
		t.Fatalf("subscribers after shutdown = %d, want 0", n)
	}

	if !r.watcher.Closed() {
		t.Fatal("Serve returned before closing its watcher")
	}

	// The server no longer accepts connections.
	if _, err := http.Get(r.url + "/api/projects"); err == nil {
		t.Fatal("server still accepting requests after shutdown")
	}
}

// failingListener is a listener whose Accept fails at once.
type failingListener struct{ net.Listener }

func (failingListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }

func TestServeReturnsListenerErrorAndStopsWatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var watcher *db.Watcher
	orig := openWatcher
	openWatcher = func(p string, d time.Duration) (*db.Watcher, error) {
		w, err := orig(p, d)
		watcher = w
		return w, err
	}
	defer func() { openWatcher = orig }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := New(db.NewStore(database), nil, WithWatchInterval(10*time.Millisecond))
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background(), failingListener{ln}, path) }()

	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Serve returned %v, want net.ErrClosed", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Serve hung after its listener failed")
	}
	if watcher == nil || !watcher.Closed() {
		t.Fatal("Serve returned without closing its watcher")
	}
}
