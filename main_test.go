package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func newServer(t *testing.T, key string) (*httptest.Server, shelf) {
	t.Helper()
	s := shelf{dir: t.TempDir()}
	srv := httptest.NewServer(router(s, key))
	t.Cleanup(srv.Close)
	return srv, s
}

func upload(t *testing.T, srv *httptest.Server, key string, e Entry, cbz []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	// metadata first, exactly like Karasu builds it
	meta, _ := mw.CreateFormField("metadata")
	if err := json.NewEncoder(meta).Encode(e); err != nil {
		t.Fatal(err)
	}
	file, _ := mw.CreateFormFile("file", "chapter.cbz")
	file.Write(cbz)
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/shelf", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func list(t *testing.T, srv *httptest.Server) []Entry {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/shelf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Entries []Entry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Entries
}

// The contract's enforcement. Karasu refuses to mark a chapter read unless the
// url comes back byte for byte, so anything that "tidies" this string turns the
// whole feature into a silent no-op. Never weaken this test.
func TestChapterURLSurvivesRoundTrip(t *testing.T) {
	srv, _ := newServer(t, "")
	urls := []string{
		"/manga/one-piece/chapter-1090",
		"/manga/one-piece/chapter-1090/", // trailing slash
		"/manga/Vinland%20Saga/ch-1",     // pre-encoded space
		"/manga/vinland saga/ch-1",       // literal space
		"https://ex.com/read?id=5&p=1",   // absolute + query
		"/マンガ/第1話",                       // non-ASCII
		"//double//slashes//../ch-1",     // path traversal shapes stay untouched
		"/manga/a<b>&c/ch-1",             // html-ish characters
	}
	for i, u := range urls {
		id := int64(i + 1)
		resp := upload(t, srv, "", Entry{ChapterID: id, ChapterURL: u}, []byte("cbz"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("upload %q: status %d", u, resp.StatusCode)
		}
	}
	got := list(t, srv)
	if len(got) != len(urls) {
		t.Fatalf("want %d entries, got %d", len(urls), len(got))
	}
	for i, u := range urls {
		if got[i].ChapterURL != u {
			t.Errorf("mangled\n want %q\n  got %q", u, got[i].ChapterURL)
		}
	}
}

func TestUploadIsUpsertAndKeepsReadState(t *testing.T) {
	srv, _ := newServer(t, "")
	e := Entry{ChapterID: 42, ChapterURL: "/x/1", MangaTitle: "One Piece"}
	upload(t, srv, "", e, []byte("first")).Body.Close()

	resp, _ := http.Post(srv.URL+"/api/shelf/42/read", "", nil)
	resp.Body.Close()

	// same chapter re-uploaded: read state survives
	upload(t, srv, "", e, []byte("second")).Body.Close()
	got := list(t, srv)
	if len(got) != 1 || !got[0].Read || got[0].ReadAt == nil {
		t.Fatalf("read state lost on re-upload: %+v", got)
	}
	if got[0].Size != int64(len("second")) {
		t.Errorf("file not replaced, size %d", got[0].Size)
	}

	// different url under the same id: a restored backup renumbered things
	upload(t, srv, "", Entry{ChapterID: 42, ChapterURL: "/other/9"}, []byte("third")).Body.Close()
	got = list(t, srv)
	if got[0].Read {
		t.Error("read state carried over to a different chapter under the same id")
	}
}

func TestDeleteAbsentSucceeds(t *testing.T) {
	srv, _ := newServer(t, "")
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/shelf/999", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204 for an absent id, got %d", resp.StatusCode)
	}
}

func TestDeleteRemovesBothFiles(t *testing.T) {
	srv, s := newServer(t, "")
	upload(t, srv, "", Entry{ChapterID: 7, ChapterURL: "/x/7"}, []byte("cbz")).Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/shelf/7", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	for _, p := range []string{s.cbz(7), s.meta(7)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
}

func TestBlankKeyAcceptsUnauthenticated(t *testing.T) {
	srv, _ := newServer(t, "")
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blank key must accept unauthenticated requests, got %d", resp.StatusCode)
	}
}

func TestKeyRejectsAndAccepts(t *testing.T) {
	srv, _ := newServer(t, "s3cret")

	resp, _ := http.Get(srv.URL + "/api/health")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 without a key, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 with the key, got %d", resp.StatusCode)
	}

	// browsers cannot set headers on a plain link: ?key= is how /plugin.zip works
	resp, _ = http.Get(srv.URL + "/plugin.zip?key=s3cret")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for ?key=, got %d", resp.StatusCode)
	}

	// a browser hitting / must get the key form, not a dead end
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `name="key"`) {
		t.Fatalf("want the key form for a browser, got: %s", b)
	}
}

func TestEmptyShelfListsArrayNotNull(t *testing.T) {
	srv, _ := newServer(t, "")
	resp, err := http.Get(srv.URL + "/api/shelf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"entries":[]`) {
		t.Fatalf("entries must be [] and never null: %s", b)
	}
}

func TestFileDownloadSupportsRange(t *testing.T) {
	srv, _ := newServer(t, "")
	upload(t, srv, "", Entry{ChapterID: 5, ChapterURL: "/x/5"}, []byte("0123456789")).Body.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/shelf/5/file", nil)
	req.Header.Set("Range", "bytes=4-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("want 206 for a Range request, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "456789" {
		t.Fatalf("bad range body: %q", b)
	}
}

func TestMarkReadIsIdempotentAndUndoable(t *testing.T) {
	srv, _ := newServer(t, "")
	upload(t, srv, "", Entry{ChapterID: 3, ChapterURL: "/x/3"}, []byte("cbz")).Body.Close()

	for range 2 { // the plugin re-reports on every sweep
		resp, _ := http.Post(srv.URL+"/api/shelf/3/read", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("read report failed: %d", resp.StatusCode)
		}
	}
	if got := list(t, srv); !got[0].Read {
		t.Fatal("chapter not marked read")
	}

	resp, _ := http.Post(srv.URL+"/api/shelf/3/read", "application/json", strings.NewReader(`{"read":false}`))
	resp.Body.Close()
	if got := list(t, srv); got[0].Read || got[0].ReadAt != nil {
		t.Fatalf("un-read did not clear the flag: %+v", got[0])
	}
}

func TestPluginZipIsConfiguredForThisServer(t *testing.T) {
	t.Setenv("YATA_PUBLIC_URL", "https://yata.example.net/")
	srv, _ := newServer(t, "k3y")

	resp, err := http.Get(srv.URL + "/plugin.zip?key=k3y")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"yata.koplugin/_meta.lua":  false,
		"yata.koplugin/main.lua":   false,
		"yata.koplugin/api.lua":    false,
		"yata.koplugin/config.lua": false,
	}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; !ok {
			continue
		}
		want[f.Name] = true
		if f.Name != "yata.koplugin/config.lua" {
			continue
		}
		rc, _ := f.Open()
		cfg, _ := io.ReadAll(rc)
		rc.Close()
		if !strings.Contains(string(cfg), `base_url = "https://yata.example.net"`) {
			t.Errorf("config.lua has the wrong base_url:\n%s", cfg)
		}
		if !strings.Contains(string(cfg), `api_key = "k3y"`) {
			t.Errorf("config.lua has the wrong api_key:\n%s", cfg)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s missing from the zip", name)
		}
	}
}

// The plugin's download path (resume, ignored Range, short body) is checked by
// api_test.lua, which stubs KOReader's modules. Running it from here keeps it
// in `go test ./...` instead of in a README nobody re-reads.
func TestPluginDownloadPath(t *testing.T) {
	lua := ""
	for _, name := range []string{"lua5.1", "luajit", "lua"} {
		if p, err := exec.LookPath(name); err == nil {
			lua = p
			break
		}
	}
	if lua == "" {
		t.Skip("no lua interpreter: run api_test.lua by hand")
	}
	out, err := exec.Command(lua, "api_test.lua").CombinedOutput()
	if err != nil {
		t.Fatalf("api_test.lua failed: %v\n%s", err, out)
	}
}

// The chart is read by someone who knows what they finished yesterday, so a
// day boundary that lands at 21:00 local (what time.Truncate does) is visible.
func TestChartDaysAreLocalDays(t *testing.T) {
	loc := time.FixedZone("BRT", -3*60*60)
	now := time.Date(2026, 7, 26, 10, 52, 0, 0, loc)
	if got := dayStart(now).Format("2 Jan"); got != "26 Jul" {
		t.Fatalf("today labelled %s, want 26 Jul", got)
	}
	evening := time.Date(2026, 7, 25, 22, 0, 0, 0, loc)
	if got := daysBetween(dayStart(evening), dayStart(now)); got != 1 {
		t.Fatalf("last night is %d days ago, want 1", got)
	}
}

// A template.Execute error aborts the page half-written, so the failure looks
// like a truncated shelf rather than an error. Anything rendered after the
// chart is proof the whole template ran.
func TestIndexPageRendersWholePage(t *testing.T) {
	srv, _ := newServer(t, "")
	upload(t, srv, "", Entry{ChapterID: 8, ChapterURL: "/x/8", MangaTitle: "Berserk", ChapterName: "Chapter 1"}, []byte("cbz")).Body.Close()

	for range 2 { // once with no history, once with a charted read event
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		page := string(b)
		if !strings.Contains(page, "Berserk") {
			t.Fatalf("page stops before the shelf, template aborted:\n%s", page)
		}
		if !strings.HasSuffix(strings.TrimSpace(page), "</div>") {
			t.Fatalf("page is truncated, template aborted:\n%s", page)
		}

		// mark it read so the second pass has a history event to chart
		resp, _ = http.Post(srv.URL+"/api/shelf/8/read", "", nil)
		resp.Body.Close()
	}

	resp, _ := http.Get(srv.URL + "/")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `class="chart"`) {
		t.Fatalf("chart missing once there is history:\n%s", b)
	}
}

func TestWebUIUndoAndHistory(t *testing.T) {
	srv, s := newServer(t, "")
	upload(t, srv, "", Entry{ChapterID: 8, ChapterURL: "/x/8", MangaTitle: "Berserk", ChapterName: "Chapter 1"}, []byte("cbz")).Body.Close()

	// the plugin reports it read, twice (it re-reports on every sweep)
	for range 2 {
		resp, _ := http.Post(srv.URL+"/api/shelf/8/read", "", nil)
		resp.Body.Close()
	}
	if h := s.history(); len(h) != 1 || h[0].MangaTitle != "Berserk" {
		t.Fatalf("history should hold exactly one event, got %+v", h)
	}

	// the web UI undoes it: a form post, answered with a redirect back
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(srv.URL+"/api/shelf/8/read", url.Values{"read": {"false"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303 back to the page, got %d", resp.StatusCode)
	}
	if got := list(t, srv); got[0].Read {
		t.Fatal("undo did not clear the read flag")
	}
}
