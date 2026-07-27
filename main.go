// Yatagarasu — KOReader shelf for Karasu.
//
// Karasu (Android) pushes chapters here as CBZ; the KOReader plugin downloads
// them and reports back which ones were finished.
//
// The contract with Karasu is docs/koreader-sync.md and is frozen.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the scratch image has no /usr/share/zoneinfo
)

// Entry is the on-disk and on-the-wire shape of one shelf slot.
//
// ChapterURL is opaque: never url.Parse, path.Clean, filepath.Join, TrimSuffix
// or lowercase it. Karasu only marks a chapter read when the url it gets back
// equals the one it stored, which is what stops a restored backup (chapter ids
// get renumbered) from marking unrelated chapters as read.
type Entry struct {
	ChapterID     int64   `json:"chapterId"`
	ChapterURL    string  `json:"chapterUrl"`
	MangaTitle    string  `json:"mangaTitle"`
	ChapterName   string  `json:"chapterName"`
	ChapterNumber float64 `json:"chapterNumber"`
	SourceID      int64   `json:"sourceId"`
	Read          bool    `json:"read"`
	UploadedAt    int64   `json:"uploadedAt"`
	ReadAt        *int64  `json:"readAt"`

	// Derived at listing time for the plugin. Karasu ignores unknown keys.
	Size int64  `json:"size"`
	File string `json:"file"`
}

type shelf struct {
	dir string
	cfg *config
}

func (s shelf) cbz(id int64) string {
	return filepath.Join(s.dir, strconv.FormatInt(id, 10)+".cbz")
}

func (s shelf) meta(id int64) string {
	return filepath.Join(s.dir, strconv.FormatInt(id, 10)+".json")
}

func (s shelf) load(id int64) (Entry, error) {
	var e Entry
	b, err := os.ReadFile(s.meta(id))
	if err != nil {
		return e, err
	}
	return e, json.Unmarshal(b, &e)
}

func (s shelf) save(e Entry) error {
	e.Size, e.File = 0, "" // derived, not stored
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp := s.meta(e.ChapterID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.meta(e.ChapterID))
}

func (s shelf) list() ([]Entry, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "*.json"))
	if err != nil {
		return nil, err
	}
	entries := []Entry{} // never nil: the plugin's Lua will not default it
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			log.Printf("yata: skipping unreadable %s: %v", p, err)
			continue
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil {
			log.Printf("yata: skipping malformed %s: %v", p, err)
			continue
		}
		if st, err := os.Stat(s.cbz(e.ChapterID)); err == nil {
			e.Size = st.Size()
		}
		e.File = "/api/shelf/" + strconv.FormatInt(e.ChapterID, 10) + "/file"
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ChapterID < entries[j].ChapterID })
	return entries, nil
}

func (s shelf) handleList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.list()
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"entries": entries})
}

// handleUpload streams the CBZ straight to disk. Chapters are routinely 60 MB,
// so no ParseMultipartForm. Karasu sends metadata before file, but the temp
// name does not depend on that ordering.
func (s shelf) handleUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart/form-data", http.StatusBadRequest)
		return
	}
	var (
		meta    Entry
		hasMeta bool
		tmpName string
	)
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName) // no-op once renamed
		}
	}()

	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "bad multipart body", http.StatusBadRequest)
			return
		}
		switch p.FormName() {
		case "metadata":
			if err := json.NewDecoder(p).Decode(&meta); err != nil {
				p.Close()
				http.Error(w, "bad metadata json", http.StatusBadRequest)
				return
			}
			hasMeta = true
		case "file":
			f, err := os.CreateTemp(s.dir, "upload-*.cbz")
			if err != nil {
				p.Close()
				http.Error(w, "cannot write to shelf", http.StatusInternalServerError)
				return
			}
			tmpName = f.Name()
			_, err = io.Copy(f, p) // never buffered
			f.Close()
			if err != nil {
				p.Close()
				http.Error(w, "upload interrupted", http.StatusBadRequest)
				return
			}
		}
		p.Close()
	}

	if !hasMeta || meta.ChapterID == 0 {
		http.Error(w, "missing metadata", http.StatusBadRequest)
		return
	}
	if tmpName == "" {
		http.Error(w, "missing file part", http.StatusBadRequest)
		return
	}
	if err := os.Rename(tmpName, s.cbz(meta.ChapterID)); err != nil {
		http.Error(w, "cannot store file", http.StatusInternalServerError)
		return
	}
	tmpName = ""

	// Re-upload of the same chapter keeps its read state. A different url under
	// the same id means a restored backup renumbered things: start clean.
	if old, err := s.load(meta.ChapterID); err == nil && old.ChapterURL == meta.ChapterURL {
		meta.Read, meta.ReadAt, meta.UploadedAt = old.Read, old.ReadAt, old.UploadedAt
	} else {
		meta.UploadedAt = now()
	}
	if err := s.save(meta); err != nil {
		http.Error(w, "cannot store metadata", http.StatusInternalServerError)
		return
	}
	writeJSON(w, meta)
}

// handleDelete: absent is success. A sync is a reconcile, so Karasu deleting
// something that is not here is normal traffic, not an error.
func (s shelf) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := chapterID(r)
	if err != nil {
		http.Error(w, "bad chapterId", http.StatusBadRequest)
		return
	}
	os.Remove(s.cbz(id))
	os.Remove(s.meta(id))
	w.WriteHeader(http.StatusNoContent)
}

// handleFile serves the CBZ. ServeFile gives Content-Length and Range support;
// e-readers drop Wi-Fi mid-download often enough that resume matters.
func (s shelf) handleFile(w http.ResponseWriter, r *http.Request) {
	id, err := chapterID(r)
	if err != nil {
		http.Error(w, "bad chapterId", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
	http.ServeFile(w, r, s.cbz(id))
}

// handleRead flips the read flag. Idempotent: the plugin re-reports on every
// sweep, and an empty body means "read".
func (s shelf) handleRead(w http.ResponseWriter, r *http.Request) {
	id, err := chapterID(r)
	if err != nil {
		http.Error(w, "bad chapterId", http.StatusBadRequest)
		return
	}
	e, err := s.load(id)
	if err != nil {
		http.Error(w, "no such entry", http.StatusNotFound)
		return
	}

	want := true
	if isForm(r) {
		// The web UI un-marks a chapter someone reported by mistake.
		want = r.PostFormValue("read") == "true"
	} else {
		body := struct {
			Read *bool `json:"read"`
		}{}
		json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body) // empty body is fine
		want = body.Read == nil || *body.Read
	}

	wasRead := e.Read
	e.Read = want
	if e.Read {
		if e.ReadAt == nil {
			t := now()
			e.ReadAt = &t
		}
	} else {
		e.ReadAt = nil
	}
	if err := s.save(e); err != nil {
		http.Error(w, "cannot store metadata", http.StatusInternalServerError)
		return
	}
	// Only the transition is history. The plugin re-reports on every sweep.
	if e.Read && !wasRead {
		s.appendHistory(e, *e.ReadAt)
	}

	if isForm(r) {
		http.Redirect(w, r, "/"+keyQuery(r), http.StatusSeeOther)
		return
	}
	writeJSON(w, e)
}

func (s shelf) handleHealth(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	writeJSON(w, map[string]any{
		"name":    "yatagarasu",
		"version": version,
		"entries": len(entries),
	})
}

// auth: a blank key is an intentionally open shelf (LAN behind a firewall),
// not a misconfiguration. The ?key= form exists so a browser can fetch
// /plugin.zip without a way to set headers.
//
// The key is read per request, not closed over: the settings page can change it
// and the next request has to use the new one without a restart.
func auth(cfg *config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := cfg.apiKey()
		want := []byte("Bearer " + key)
		if key != "" {
			got := r.Header.Get("Authorization")
			if got == "" && r.URL.Query().Get("key") != "" {
				got = "Bearer " + r.URL.Query().Get("key")
			}
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				// A browser cannot set a header by following a link, and the
				// browser is how the plugin gets downloaded: give it a form
				// instead of a dead end.
				if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
					askForKey(w)
					return
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func chapterID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("chapterId"), 10, 64)
}

// isForm marks a request coming from the web UI rather than from the plugin.
func isForm(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
}

// keyQuery carries the browser's ?key= across a redirect, so acting on the
// page does not bounce the user back to the key form.
func keyQuery(r *http.Request) string {
	k := r.URL.Query().Get("key")
	if k == "" {
		k = r.PostFormValue("key")
	}
	if k == "" {
		return ""
	}
	return "?key=" + url.QueryEscape(k)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

const version = "1"

// now is a var so tests can freeze it.
var now = func() int64 { return time.Now().Unix() }

func router(s shelf) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/shelf", s.handleList)
	mux.HandleFunc("POST /api/shelf", s.handleUpload)
	mux.HandleFunc("DELETE /api/shelf/{chapterId}", s.handleDelete)
	mux.HandleFunc("GET /api/shelf/{chapterId}/file", s.handleFile)
	mux.HandleFunc("POST /api/shelf/{chapterId}/read", s.handleRead)
	// The browser's delete: an HTML form cannot send DELETE.
	mux.HandleFunc("POST /api/shelf/{chapterId}/delete", s.handleDeleteForm)
	mux.HandleFunc("GET /plugin.zip", pluginZipHandler(s.cfg))
	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("POST /settings", s.handleSaveSettings)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	return auth(s.cfg, mux)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	dir := env("YATA_DATA", "/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("yata: cannot use data dir %s: %v", dir, err)
	}
	s := shelf{dir: dir, cfg: loadConfig(dir)}
	if s.cfg.apiKey() == "" {
		log.Print("yata: no API key set, accepting unauthenticated requests")
	}
	addr := env("YATA_ADDR", ":8080")
	log.Printf("yata: listening on %s, data in %s", addr, s.dir)
	log.Fatal(http.ListenAndServe(addr, router(s)))
}
