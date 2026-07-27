package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// settings is what the web UI can change without recreating the container.
//
// The environment provides the defaults; once /data/settings.json exists it
// wins field by field, including an empty APIKey — "no key" is a legitimate
// setting (an open shelf on a trusted LAN), not an absent one.
type settings struct {
	PublicURL string `json:"publicUrl"`
	APIKey    string `json:"apiKey"`
	Timezone  string `json:"timezone"`
	ChartDays int    `json:"chartDays"`
}

type config struct {
	mu   sync.RWMutex
	s    settings
	loc  *time.Location
	path string
}

func loadConfig(dir string) *config {
	c := &config{
		path: filepath.Join(dir, "settings.json"),
		s: settings{
			PublicURL: trimSlash(os.Getenv("YATA_PUBLIC_URL")),
			APIKey:    os.Getenv("YATA_API_KEY"),
			Timezone:  os.Getenv("TZ"),
			ChartDays: 14,
		},
		loc: time.Local,
	}
	if b, err := os.ReadFile(c.path); err == nil {
		if err := json.Unmarshal(b, &c.s); err != nil {
			log.Printf("yata: ignoring unreadable settings.json: %v", err)
		}
	}
	c.s.ChartDays = validDays(c.s.ChartDays)
	if loc, err := loadLocation(c.s.Timezone); err == nil {
		c.loc = loc
	} else {
		log.Printf("yata: unknown timezone %q, using the container's: %v", c.s.Timezone, err)
	}
	return c
}

// loadLocation keeps the empty case out of every caller: no timezone set means
// whatever the container runs on, which for a scratch image is UTC.
func loadLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.Local, nil
	}
	return time.LoadLocation(tz)
}

func validDays(d int) int {
	switch d {
	case 7, 14, 30:
		return d
	}
	return 14
}

func (c *config) get() settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.s
}

func (c *config) apiKey() string    { return c.get().APIKey }
func (c *config) chartDays() int    { return c.get().ChartDays }
func (c *config) publicURL() string { return c.get().PublicURL }

// trimSlash keeps the base URL joinable: the plugin builds "<base>/api/shelf",
// and a trailing slash there is a 404 nobody can see from the e-reader.
func trimSlash(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }

func (c *config) location() *time.Location {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loc
}

// save validates before it writes: a timezone the container cannot resolve
// would otherwise silently fall back and quietly report the wrong days.
func (c *config) save(s settings) error {
	loc, err := loadLocation(s.Timezone)
	if err != nil {
		return err
	}
	s.ChartDays = validDays(s.ChartDays)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	// 0600: this file holds the API key.
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}

	c.mu.Lock()
	c.s, c.loc = s, loc
	c.mu.Unlock()
	return nil
}

func (s shelf) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "")
}

func (s shelf) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	switch r.PostFormValue("action") {
	case "clear-history":
		if err := os.Remove(s.historyPath()); err != nil && !os.IsNotExist(err) {
			s.renderSettings(w, r, "Could not clear the history: "+err.Error())
			return
		}
		s.redirectToSettings(w, r, "History cleared.")
		return
	case "empty-shelf":
		entries, _ := s.list()
		for _, e := range entries {
			os.Remove(s.cbz(e.ChapterID))
			os.Remove(s.meta(e.ChapterID))
		}
		s.redirectToSettings(w, r, "Shelf emptied.")
		return
	}

	days, _ := strconv.Atoi(r.PostFormValue("chartDays"))
	next := settings{
		PublicURL: trimSlash(r.PostFormValue("publicUrl")),
		APIKey:    strings.TrimSpace(r.PostFormValue("apiKey")),
		Timezone:  strings.TrimSpace(r.PostFormValue("timezone")),
		ChartDays: days,
	}
	if err := s.cfg.save(next); err != nil {
		s.renderSettings(w, r, "Not saved: "+err.Error())
		return
	}
	s.redirectToSettings(w, r, "Saved.")
}

// redirectToSettings carries only the flash message. Changing the API key used
// to log the browser out of its own settings page; the session cookie is a
// separate credential, so it now survives the change.
func (s shelf) redirectToSettings(w http.ResponseWriter, r *http.Request, note string) {
	q := url.Values{}
	if note != "" {
		q.Set("note", note)
	}
	http.Redirect(w, r, "/settings?"+q.Encode(), http.StatusSeeOther)
}

// commonZones is a shortcut for the datalist, not a whitelist: the field takes
// any IANA name and time.LoadLocation is what actually decides.
var commonZones = []string{
	"America/Sao_Paulo", "America/New_York", "America/Los_Angeles", "America/Mexico_City",
	"Europe/London", "Europe/Lisbon", "Europe/Madrid", "Europe/Berlin",
	"Asia/Tokyo", "Asia/Singapore", "Australia/Sydney", "UTC",
}

func (s shelf) renderSettings(w http.ResponseWriter, r *http.Request, problem string) {
	cur := s.cfg.get()
	entries, _ := s.list()

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if problem != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	err := settingsTmpl.Execute(w, map[string]any{
		"Nonce":      nonce,
		"HasKey":     s.cfg.apiKey() != "",
		"Note":       r.URL.Query().Get("note"),
		"Problem":    problem,
		"PublicURL":  cur.PublicURL,
		"APIKey":     cur.APIKey,
		"Timezone":   cur.Timezone,
		"ChartDays":  cur.ChartDays,
		"DayChoices": []int{7, 14, 30},
		"Zones":      commonZones,
		"Entries":    len(entries),
		"Events":     len(s.history()),
		"DataDir":    s.dir,
		"Now":        time.Now().In(s.cfg.location()).Format("Mon 2 Jan 2006, 15:04 MST"),
		"Version":    version,
	})
	if err != nil {
		log.Printf("yata: rendering the settings page failed: %v", err)
	}
}

// handleDeleteForm is the browser's way to drop an entry: forms cannot send
// DELETE. Karasu re-uploads the chapter on its next sync if it still wants it
// there, which makes this a "free the disk now" button and not a veto.
func (s shelf) handleDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := chapterID(r)
	if err != nil {
		http.Error(w, "bad chapterId", http.StatusBadRequest)
		return
	}
	os.Remove(s.cbz(id))
	os.Remove(s.meta(id))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
