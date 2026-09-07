// Package web is the graphical front end: a page served by the binary itself to
// the operator's own browser.
//
// It adds no dependency — net/http, embed and crypto/rand are standard library —
// so the program stays one static file. What it does add is the project's first
// listening socket, and that is the whole reason this file is mostly guards.
//
// Like the CLI and the TUI, it decides nothing about images: every conversion
// goes through internal/pipeline, and the format list comes from the registry in
// internal/imageio. A front end with a conversion path of its own is a bug.
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// tokenHeader carries the run's token on API requests. The page itself receives
// the token as a query parameter, because a browser navigating to a URL cannot
// set a header; everything after that uses this.
const tokenHeader = "X-Imgconv-Token"

// Server is one run of the web front end: one directory, one token, one listener.
type Server struct {
	token string
	ln    net.Listener
	mux   *http.ServeMux

	// mu guards everything below it. http.Serve runs each handler in its own
	// goroutine, and all of this changes while it does; it is the reason this
	// package is race-tested.
	// done is closed when the page asks the server to stop. Serve waits on it, so
	// quitting works however the program was started — including with no terminal
	// to close, which is what a double-click from a file manager gives you.
	done     chan struct{}
	stopOnce sync.Once

	mu sync.RWMutex
	// startDir is only where the file dialog opens. It is NOT a boundary.
	startDir string
	// picked is the allowlist, and it is the whole security model: id -> absolute
	// path, and a path gets in only by a human choosing it in the system's own
	// dialog. There is no traversal to attempt against a set that contains exactly
	// what somebody clicked.
	picked map[string]string
	order  []string
	nextID int
	picker Picker
}

// Entry is one chosen file as the page sees it. The id is what travels, never the
// path: a page that never learns where a file lives cannot leak it, and an id
// that is not in the allowlist resolves to nothing.
type Entry struct {
	ID   string
	Name string
	Size int64
}

// New prepares a server rooted at dir.
//
// The root is resolved through symlinks once, here, so that every later
// containment check compares against a real path. Resolving it per request would
// let the answer change between the check and the open.
func New(dir string) (*Server, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	token, err := mintToken()
	if err != nil {
		return nil, err
	}

	s := &Server{
		startDir: real,
		token:    token,
		mux:      http.NewServeMux(),
		picker:   nativePicker,
		picked:   map[string]string{},
		done:     make(chan struct{}),
	}
	s.routes()
	return s, nil
}

// mintToken returns 32 bytes of CSPRNG output, hex encoded.
//
// crypto/rand and not math/rand: this value is the only thing standing between a
// local process and the ability to make this program write files, and math/rand
// is seeded from something an attacker on the same machine can often guess.
func mintToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// Token is the secret for this run. It lives in memory only and is never written
// to disk or logged.
func (s *Server) Token() string { return s.token }

// StartDir is where the file dialog opens. It is a convenience, not a boundary:
// the operator can choose anything from that dialog, and what bounds this server
// is the allowlist below.
func (s *Server) StartDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startDir
}

// Add puts chosen files into the allowlist and returns the whole selection.
//
// It ADDS rather than replaces because building a batch in two passes is normal,
// and the second pass often lands in another folder. A path already present keeps
// its id, so picking the same file twice does not list it twice.
func (s *Server) Add(paths []string) []Entry {
	s.mu.Lock()
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		if info, err := os.Stat(real); err != nil || info.IsDir() {
			continue
		}
		if s.idOf(real) != "" {
			continue
		}
		s.nextID++
		id := strconv.Itoa(s.nextID)
		s.picked[id] = real
		s.order = append(s.order, id)
		s.startDir = filepath.Dir(real)
	}
	s.mu.Unlock()
	return s.Files()
}

// idOf finds an already-picked path. The caller holds the lock.
func (s *Server) idOf(path string) string {
	for id, p := range s.picked {
		if p == path {
			return id
		}
	}
	return ""
}

// Remove forgets one file. Nothing on disk is touched: this is the operator
// taking it off the list, not deleting it.
func (s *Server) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.picked, id)
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// Files is the selection, in the order it was chosen.
func (s *Server) Files() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Entry, 0, len(s.order))
	for _, id := range s.order {
		path, ok := s.picked[id]
		if !ok {
			continue
		}
		e := Entry{ID: id, Name: filepath.Base(path)}
		// A file can be deleted or replaced between being picked and being listed.
		// Reporting it with a zero size is honest; refusing to list it would hide
		// a choice the operator made.
		if info, err := os.Stat(path); err == nil {
			e.Size = info.Size()
		}
		out = append(out, e)
	}
	return out
}

// Handler is the guarded mux. Every route goes through guard; nothing is reachable
// around it.
func (s *Server) Handler() http.Handler { return s.guard(s.mux) }

// Listen binds the socket. 127.0.0.1 explicitly, never ":port": the second form
// binds every interface, which would put a file-writing service on the network.
// Port 0 asks the kernel for a free one, so two runs never collide.
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.ln = ln
	return nil
}

// Addr is the bound address, valid after Listen.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// URL is what the operator opens: the address plus the token, because the first
// request is a navigation and cannot carry a header.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s/?t=%s", s.Addr(), url.QueryEscape(s.token))
}

// Serve blocks until the page asks it to stop, or until the listener dies.
func (s *Server) Serve() error {
	if s.ln == nil {
		if err := s.Listen(); err != nil {
			return err
		}
	}
	failed := make(chan error, 1)
	go func() { failed <- http.Serve(s.ln, s.Handler()) }()

	select {
	case <-s.done:
		// Asked to stop. Closing the listener is what ends http.Serve, and its
		// resulting "use of closed network connection" is the expected shape of a
		// clean shutdown rather than a failure worth reporting.
		s.Close()
		return nil
	case err := <-failed:
		return err
	}
}

// Stop asks Serve to return. It is safe to call more than once: two clicks on the
// same button must not panic on a closed channel.
func (s *Server) Stop() {
	s.stopOnce.Do(func() { close(s.done) })
}

// Close stops the listener.
func (s *Server) Close() error {
	if s.ln == nil {
		return nil
	}
	return s.ln.Close()
}

// guard is the security boundary, and it is deliberately one function: a second
// place that decides who may call is a second place that can be wrong.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.originAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.tokenAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenAllowed accepts the token from the header or, for the page navigation,
// from the query.
//
// subtle.ConstantTimeCompare and not ==: string comparison returns as soon as two
// bytes differ, and the time it took is a measurement of how much of the token
// was right. That is a slow leak, but it is a leak, and the fix costs nothing.
func (s *Server) tokenAllowed(r *http.Request) bool {
	given := r.Header.Get(tokenHeader)
	if given == "" {
		given = r.URL.Query().Get("t")
	}
	return subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) == 1
}

// originAllowed refuses a request that announces it came from somewhere else.
//
// This is the second layer, and it exists because the first one can leak: a token
// in a URL can end up in a Referer, a shell history or a screenshot. A browser
// sends Origin on every cross-origin request, so a page on another site cannot
// reach a handler here even holding the token. An ABSENT Origin is normal for a
// same-origin GET and is allowed — the token still has to be right.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// Resolve turns an id from the page into a path, or refuses it.
//
// It is a lookup in the allowlist and nothing else. That is the point: while the
// server browsed directories this function had to out-argue "..", an absolute
// path and a symlink, and every one of those was a chance to be wrong. A set that
// contains exactly what a human clicked has no such argument to lose.
func (s *Server) Resolve(id string) (string, error) {
	s.mu.RLock()
	path, ok := s.picked[id]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%q was not chosen; only files picked in the file dialog can be used", id)
	}
	return path, nil
}

// Run is the whole front end from cmd's point of view: bind, tell the operator
// where it is, try to open the browser, and serve until interrupted.
//
// The URL is printed even when the browser opens on its own, because the operator
// needs it when the browser that opened is not the one they use — and because a
// program that opens a window and says nothing is a program you cannot reach
// again. Closing the terminal ends the process and with it the server.
func Run(startDir string, out io.Writer) error {
	s, err := New(startDir)
	if err != nil {
		return err
	}
	if err := s.Listen(); err != nil {
		return err
	}
	defer s.Close()

	fmt.Fprintf(out, "imgconv: the file dialog opens in %s\n", s.StartDir())
	fmt.Fprintf(out, "imgconv: open %s\n", s.URL())
	fmt.Fprintf(out, "imgconv: press ctrl+c to stop\n")
	openBrowser(s.URL())

	return s.Serve()
}

// openBrowser is best effort and never fatal. A machine with no xdg-open is one
// where the operator pastes the URL, which is why it was printed first.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
