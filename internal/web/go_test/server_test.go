// The contract for the web front end's SECURITY BOUNDARY.
//
// WHY THIS FILE EXISTS, AND WHY IT IS FIRST: until this package, the only
// untrusted input this program accepted was a file the operator pointed at. A
// listening socket changes that. Anything running on this machine can send it
// requests, and so can any web page the operator happens to have open in another
// tab — a page cannot read our responses cross-origin, but it can absolutely make
// us WRITE, and this program's whole job is writing files.
//
// Three guards answer that, and none of them is sufficient alone:
//
//  1. the listener binds loopback, so nothing off this machine can reach it;
//  2. a token minted per run must accompany every request, and a foreign page
//     cannot read it;
//  3. an Origin that is not ours is refused even if a token somehow leaked.
//
// WHAT IS A RULE AND WHAT IS AN ACCIDENT: all three are rules. So is the root
// containment — the server is given ONE directory and no request may read, list
// or write outside it, whether it tries with "..", with an absolute path, or with
// a symlink pointing out. That the token is a query parameter on the page and a
// header on the API is an accident of convenience, not a rule.
package go_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/mateusands/imgconv/internal/web"
)

func newServer(t *testing.T, root string) *Server {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatalf("creating the server: %v", err)
	}
	return s
}

func TestServer_ShouldRefuseARequestWithoutTheRunToken(t *testing.T) {
	s := newServer(t, t.TempDir())

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/files", nil))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 — an untokened request reached a handler", rec.Code)
	}
}

func TestServer_ShouldAcceptARequestCarryingTheRunToken(t *testing.T) {
	s := newServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.Header.Set("X-Imgconv-Token", s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Errorf("the run's own token was refused; the guard rejects everything, which is not a guard")
	}
}

func TestServer_ShouldRefuseAWrongTokenEvenWhenItIsTheRightLength(t *testing.T) {
	s := newServer(t, t.TempDir())

	wrong := strings.Repeat("a", len(s.Token()))
	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.Header.Set("X-Imgconv-Token", wrong)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a wrong token of the right length", rec.Code)
	}
}

func TestServer_ShouldMintADifferentTokenForEveryRun(t *testing.T) {
	a, b := newServer(t, t.TempDir()), newServer(t, t.TempDir())

	if a.Token() == b.Token() {
		t.Fatal("two runs share a token; it is derived from something predictable")
	}
	if len(a.Token()) < 32 {
		t.Errorf("token is %d chars; too short to resist guessing from a local process", len(a.Token()))
	}
}

// The attack this exists for: a page the operator has open on some other site
// scripting requests at localhost. It cannot read our responses, but it can make
// us write, and writing is what this program does.
func TestServer_ShouldRefuseARequestWhoseOriginIsAnotherSite(t *testing.T) {
	s := newServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.Header.Set("X-Imgconv-Token", s.Token())
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 — a foreign Origin was served even with a token", rec.Code)
	}
}

func TestServer_ShouldListenOnLoopbackOnly(t *testing.T) {
	s := newServer(t, t.TempDir())
	if err := s.Listen(); err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer s.Close()

	addr := s.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("listening on %q — anything but 127.0.0.1 exposes this to the network", addr)
	}
}

func TestResolve_ShouldRefuseAPathThatEscapesTheRootWithDotDot(t *testing.T) {
	s := newServer(t, t.TempDir())

	for _, attempt := range []string{
		"../etc/passwd",
		"../../etc/passwd",
		"a/../../etc/passwd",
		"..",
	} {
		if got, err := s.Resolve(attempt); err == nil {
			t.Errorf("Resolve(%q) = %q, want a refusal", attempt, got)
		}
	}
}

func TestResolve_ShouldRefuseAnAbsolutePathOutsideTheRoot(t *testing.T) {
	s := newServer(t, t.TempDir())

	if got, err := s.Resolve("/etc/passwd"); err == nil {
		t.Errorf("Resolve(\"/etc/passwd\") = %q, want a refusal", got)
	}
}

func TestResolve_ShouldRefuseASymlinkThatPointsOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("not yours"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "innocent.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	s := newServer(t, root)
	// The NAME is inside the root; only following the link reveals it is not.
	// A containment check on the string alone passes this and is wrong.
	if got, err := s.Resolve("innocent.png"); err == nil {
		t.Errorf("Resolve followed a symlink out of the root to %q", got)
	}
}

// Updated when the folder browser was removed: "inside the root" stopped being
// the criterion, because there is no root. A file resolves when a human picked it
// in the system dialog and never otherwise.
//
// A guard that refuses everything passes every refusal test above and is useless,
// so this is the test that says the allowlist also LETS THINGS THROUGH.
func TestResolve_ShouldAllowAFileTheOperatorPicked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer(t, dir)

	// Before it is picked, its own name means nothing.
	if _, err := s.Resolve("photo.png"); err == nil {
		t.Error("a file resolved before anyone chose it")
	}

	entries := s.Add([]string{path})
	if len(entries) != 1 {
		t.Fatalf("Add returned %d entries, want 1", len(entries))
	}
	got, err := s.Resolve(entries[0].ID)
	if err != nil {
		t.Fatalf("a picked file must resolve: %v", err)
	}
	if got != path {
		t.Errorf("resolved to %q, want %q", got, path)
	}
}

// The sibling-directory case, which a plain string prefix gets wrong: a root of
// "/home/me/photos" does NOT contain "/home/me/photos-secret", but
// strings.HasPrefix says it does. The separator is what tells them apart.
//
// This test exists because deliberately breaking contains() into a plain prefix
// check produced zero failures — the rule was commented and not covered.
func TestResolve_ShouldRefuseASiblingDirectoryWhoseNameStartsWithTheRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "photos")
	sibling := filepath.Join(base, "photos-secret")
	for _, d := range []string{root, sibling} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(sibling, "private.png")
	if err := os.WriteFile(secret, []byte("not yours"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A link inside the root whose target is the SIBLING, not a child.
	if err := os.Symlink(secret, filepath.Join(root, "innocent.png")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	s := newServer(t, root)
	if got, err := s.Resolve("innocent.png"); err == nil {
		t.Errorf("resolved into the sibling directory: %q — a prefix test without the separator", got)
	}
}
