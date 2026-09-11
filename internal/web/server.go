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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mateusands/imgconv/internal/imageio"
)

// tokenHeader carries the run's token on API requests. The page itself receives
// the token as a query parameter, because a browser navigating to a URL cannot
// set a header; everything after that uses this.
const tokenHeader = "X-Imgconv-Token"

// cookiePrefix names the cookie that makes reloading work. The page receives the
// token in the URL and strips it from the address bar, which left a plain F5 with
// nothing to present.
//
// The PORT is appended to the name, and that is not cosmetic: a cookie is scoped
// to a host and cannot be scoped to a port, so two runs on 127.0.0.1 would share
// one cookie and the second would silently log the first one out.
const cookiePrefix = "imgconv_token"

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
	picker Picker

	// picking is held for as long as a dialog is open. It is separate from mu
	// because a dialog can stay open for minutes and mu guards data nobody should
	// wait minutes for.
	picking atomic.Bool
}

// Entry is one chosen file as the page sees it. The id is what travels, never the
// path: a page that never learns where a file lives cannot leak it, and an id
// that is not in the allowlist resolves to nothing.
type Entry struct {
	ID   string
	Name string
	Size int64

	// What the file's header declares. Zero values mean it could not be read —
	// the file is listed anyway, because dropping something the operator chose
	// would hide their own choice, and the conversion reports the real reason.
	Format string
	Width  int
	Height int
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

// claimPicker reports whether this request may open a dialog, taking the claim if
// so. releasePicker gives it back.
func (s *Server) claimPicker() bool { return s.picking.CompareAndSwap(false, true) }
func (s *Server) releasePicker()    { s.picking.Store(false) }

// CookieName is the cookie this run uses, which carries the port so that two
// runs on the same host do not overwrite each other.
func (s *Server) CookieName() string {
	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil || port == "" {
		return cookiePrefix
	}
	return cookiePrefix + "_" + port
}

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
		// An unguessable id. Sequential ones made the allowlist enumerable: a
		// caller that got past the guards could ask for "1" without ever having
		// seen the list, which is the enumerable-key problem with a nicer name.
		id, err := mintToken()
		if err != nil {
			continue
		}
		id = id[:24]
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
	// The lock is held only long enough to copy the ids and paths. Describing a
	// file opens it, and opening a file is not bounded work: a picked FIFO blocks
	// os.Open forever, and doing that under the lock would take the whole server
	// down with it — every Add and Remove would wait behind a read that never
	// returns.
	type picked struct{ id, path string }
	s.mu.RLock()
	snapshot := make([]picked, 0, len(s.order))
	for _, id := range s.order {
		if path, ok := s.picked[id]; ok {
			snapshot = append(snapshot, picked{id: id, path: path})
		}
	}
	s.mu.RUnlock()

	out := make([]Entry, 0, len(snapshot))
	for _, p := range snapshot {
		e := Entry{ID: p.id, Name: filepath.Base(p.path)}
		// A file can be deleted or replaced between being picked and being listed.
		// Reporting it with a zero size is honest; refusing to list it would hide
		// a choice the operator made.
		if info, err := os.Stat(p.path); err == nil {
			e.Size = info.Size()
		}
		e.Format, e.Width, e.Height = describe(p.path)
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
	// http.Serve has no timeouts at all: one client that opens a connection and
	// never finishes a request would hold a goroutine for the life of the process.
	// The write timeout is generous because a thumbnail decodes a real image, and
	// there is no read timeout on the body for the same reason a pick can take
	// minutes — a human is choosing.
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	failed := make(chan error, 1)
	go func() { failed <- srv.Serve(s.ln) }()

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
		if !s.fetchSiteAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.originAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.tokenAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Cheap headers for a page that holds a token and lists someone's files.
		// frame-ancestors is the one that matters: it stops any other page from
		// putting this one in an iframe, which is the shape a click on a hostile
		// page would have to take to reach a control here.
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")

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
	if given == "" {
		if c, err := r.Cookie(s.CookieName()); err == nil {
			given = c.Value
		}
	}
	return subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) == 1
}

// setTokenCookie is called only after a request has already proved it holds the
// token. It hands back the same value so the next request — a reload, a
// thumbnail, anything the browser starts on its own — needs nothing in its URL.
//
// HttpOnly because nothing in the page has any reason to read it. Secure is
// deliberately absent: this is http on loopback, and setting it would stop the
// cookie from ever being sent.
func (s *Server) setTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName(),
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
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

// fetchSiteAllowed refuses a request the browser says came from somewhere else.
//
// It exists because the cookie cannot tell ports apart. A cookie belongs to a
// HOST, and SameSite counts every port on 127.0.0.1 as the same site, so once
// this page had a cookie any other local server's page could embed
// <img src="http://127.0.0.1:ours/api/thumb?f=…"> and the browser would attach
// it. An <img> sends no Origin, so the origin check saw nothing to refuse.
//
// Sec-Fetch-Site is sent by the BROWSER, not by the page, and it separates the
// two cases nothing else could: "same-origin" is our own page, "none" is a typed
// URL or a bookmark, and "same-site" is precisely the neighbouring port. An
// absent header means a client too old to send one — or curl — and falls through
// to the token and origin checks, which is where it was before this existed.
func (s *Server) fetchSiteAllowed(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	default:
		return false
	}
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
	fmt.Fprintf(out, "imgconv: close this terminal to stop, or press ctrl+c\n")
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

// describe reads what a file declares about itself, and says nothing when it
// cannot. It goes through imageio because that is the only package allowed to
// know what an image format is, and it reads the HEADER: decoding every file in
// the list to print its dimensions would allocate every pixel of every picture
// the operator selected.
func describe(path string) (format string, w, h int) {
	src, err := imageio.Open(path)
	if err != nil {
		return "", 0, 0
	}
	defer src.Close()

	head, err := src.Header()
	if err != nil {
		return "", 0, 0
	}
	return head.Format, head.Width, head.Height
}
