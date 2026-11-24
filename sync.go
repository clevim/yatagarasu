package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type SyncCategory struct {
	Name  string `json:"name"`
	Order int    `json:"order"`
	Flags int64  `json:"flags"`
}

type SyncMergedSource struct {
	SourceID       int64  `json:"sourceId"`
	URL            string `json:"url"`
	Priority       int    `json:"priority"`
	UpdatesEnabled bool   `json:"updatesEnabled"`
}

type SyncChapter struct {
	URL           string  `json:"url"`
	Name          string  `json:"name"`
	ChapterNumber float64 `json:"chapterNumber"`
	Scanlator     string  `json:"scanlator,omitempty"`
	Read          bool    `json:"read"`
	Bookmark      bool    `json:"bookmark"`
	LastPageRead  int     `json:"lastPageRead"`
	UpdatedAt     int64   `json:"updatedAt"`
}

type SyncMangaKey struct {
	SourceID int64  `json:"sourceId"`
	URL      string `json:"url"`
}

func (k SyncMangaKey) str() string {
	return fmt.Sprintf("%d|%s", k.SourceID, k.URL)
}

type SyncManga struct {
	SourceID      int64              `json:"sourceId"`
	URL           string             `json:"url"`
	Title         string             `json:"title"`
	Favorite      bool               `json:"favorite"`
	Categories    []string           `json:"categories,omitempty"`
	UpdatedAt     int64              `json:"updatedAt"`
	MergedSources []SyncMergedSource `json:"mergedSources,omitempty"`
	Chapters      []SyncChapter      `json:"chapters,omitempty"`
}

type SyncExtension struct {
	PkgName     string `json:"pkgName"`
	Name        string `json:"name"`
	Lang        string `json:"lang"`
	VersionCode int64  `json:"versionCode"`
}

type SyncPayload struct {
	DeviceID            string          `json:"deviceId,omitempty"`
	LastSyncAt          int64           `json:"lastSyncAt,omitempty"`
	ServerTime          int64           `json:"serverTime,omitempty"`
	Categories          []SyncCategory  `json:"categories"`
	Mangas              []SyncManga     `json:"mangas"`
	DeletedMangaKeys    []SyncMangaKey  `json:"deletedMangaKeys,omitempty"`
	ExtensionRepos      []string        `json:"extensionRepos"`
	InstalledExtensions []SyncExtension `json:"installedExtensions,omitempty"`
	MissingExtensions   []SyncExtension `json:"missingExtensions,omitempty"`
}

type persistedSyncState struct {
	Categories map[string]SyncCategory  `json:"categories"`
	Mangas     map[string]SyncManga     `json:"mangas"`
	DeletedAt  map[string]int64         `json:"deletedAt"`
	Repos      map[string]bool          `json:"repos"`
	Extensions map[string]SyncExtension `json:"extensions"`
	UpdatedAt  int64                    `json:"updatedAt"`
}

var syncMu sync.Mutex

func (s shelf) syncStatePath() string {
	return filepath.Join(s.dir, "sync", "state.json")
}

func (s shelf) loadSyncState() persistedSyncState {
	st := persistedSyncState{
		Categories: map[string]SyncCategory{},
		Mangas:     map[string]SyncManga{},
		DeletedAt:  map[string]int64{},
		Repos:      map[string]bool{},
		Extensions: map[string]SyncExtension{},
	}
	b, err := os.ReadFile(s.syncStatePath())
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.Categories == nil {
		st.Categories = map[string]SyncCategory{}
	}
	if st.Mangas == nil {
		st.Mangas = map[string]SyncManga{}
	}
	if st.DeletedAt == nil {
		st.DeletedAt = map[string]int64{}
	}
	if st.Repos == nil {
		st.Repos = map[string]bool{}
	}
	if st.Extensions == nil {
		st.Extensions = map[string]SyncExtension{}
	}
	return st
}

func (s shelf) saveSyncState(st persistedSyncState) error {
	dir := filepath.Dir(s.syncStatePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := s.syncStatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.syncStatePath())
}

func mergeSyncIntoState(st *persistedSyncState, in SyncPayload, serverNowMs int64) {
	// 1. Explicit deletions first so deleted items are not resurrected.
	for _, del := range in.DeletedMangaKeys {
		key := del.str()
		if existing, ok := st.Mangas[key]; ok {
			if existing.UpdatedAt <= in.LastSyncAt || in.LastSyncAt == 0 {
				delete(st.Mangas, key)
				st.DeletedAt[key] = serverNowMs
			}
		} else {
			st.DeletedAt[key] = serverNowMs
		}
	}

	// 2. Categories (union by name).
	for _, cat := range in.Categories {
		name := strings.TrimSpace(cat.Name)
		if name == "" {
			continue
		}
		cat.Name = name
		st.Categories[name] = cat
	}

	// 3. Extension repos (union).
	for _, repo := range in.ExtensionRepos {
		r := strings.TrimSpace(repo)
		if r != "" {
			st.Repos[r] = true
		}
	}

	// 4. Known extensions across devices.
	for _, ext := range in.InstalledExtensions {
		if ext.PkgName == "" {
			continue
		}
		if prev, ok := st.Extensions[ext.PkgName]; !ok || ext.VersionCode >= prev.VersionCode {
			st.Extensions[ext.PkgName] = ext
		}
	}

	// 5. Mangas + chapters + merged sources.
	for _, m := range in.Mangas {
		key := SyncMangaKey{SourceID: m.SourceID, URL: m.URL}.str()
		if delAt, wasDeleted := st.DeletedAt[key]; wasDeleted && m.UpdatedAt > 0 && m.UpdatedAt < delAt {
			continue
		}
		delete(st.DeletedAt, key)

		existing, exists := st.Mangas[key]
		if !exists {
			if m.UpdatedAt == 0 {
				m.UpdatedAt = serverNowMs
			}
			st.Mangas[key] = m
			continue
		}

		// Merge manga metadata: newer updatedAt wins; favorite=true wins on tie.
		merged := existing
		if m.Title != "" && (m.UpdatedAt >= existing.UpdatedAt || merged.Title == "") {
			merged.Title = m.Title
		}
		if m.UpdatedAt > existing.UpdatedAt {
			merged.Favorite = m.Favorite
			if len(m.Categories) > 0 {
				merged.Categories = m.Categories
			}
			if m.MergedSources != nil {
				merged.MergedSources = m.MergedSources
			}
			merged.UpdatedAt = m.UpdatedAt
		} else if m.UpdatedAt == existing.UpdatedAt {
			merged.Favorite = existing.Favorite || m.Favorite
			if len(m.Categories) > 0 && len(existing.Categories) == 0 {
				merged.Categories = m.Categories
			}
			if len(m.MergedSources) > 0 && len(existing.MergedSources) == 0 {
				merged.MergedSources = m.MergedSources
			}
		}

		merged.Chapters = mergeChapters(existing.Chapters, m.Chapters)
		st.Mangas[key] = merged
	}
	st.UpdatedAt = serverNowMs
}

func mergeChapters(a, b []SyncChapter) []SyncChapter {
	byURL := make(map[string]SyncChapter, len(a)+len(b))
	for _, ch := range a {
		byURL[ch.URL] = ch
	}
	for _, ch := range b {
		prev, exists := byURL[ch.URL]
		if !exists {
			byURL[ch.URL] = ch
			continue
		}
		res := prev
		if ch.Name != "" && res.Name == "" {
			res.Name = ch.Name
		}
		if ch.UpdatedAt > prev.UpdatedAt {
			res.Read = ch.Read
			res.Bookmark = ch.Bookmark
			res.LastPageRead = ch.LastPageRead
			res.UpdatedAt = ch.UpdatedAt
		} else if ch.UpdatedAt == prev.UpdatedAt {
			// Tie-breaker: highest progress wins.
			res.Read = prev.Read || ch.Read
			res.Bookmark = prev.Bookmark || ch.Bookmark
			if ch.LastPageRead > prev.LastPageRead {
				res.LastPageRead = ch.LastPageRead
			}
		}
		byURL[ch.URL] = res
	}
	out := make([]SyncChapter, 0, len(byURL))
	for _, ch := range byURL {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChapterNumber != out[j].ChapterNumber {
			return out[i].ChapterNumber < out[j].ChapterNumber
		}
		return out[i].URL < out[j].URL
	})
	return out
}

func (s shelf) handleSyncReconcile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20) // 32 MiB JSON payload cap
	var req SyncPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid sync payload", http.StatusBadRequest)
		return
	}

	serverNowMs := now() * 1000

	syncMu.Lock()
	defer syncMu.Unlock()

	st := s.loadSyncState()

	// If Suwayomi is configured, pull its current library state first so changes
	// made in Suwayomi WebUI merge cleanly with Karasu's payload.
	suw := s.suwayomi()
	if suw != nil {
		if remote, err := suw.pullLibrary(r.Context()); err == nil && remote != nil {
			mergeSyncIntoState(&st, *remote, serverNowMs)
		}
	}

	mergeSyncIntoState(&st, req, serverNowMs)
	if err := s.saveSyncState(st); err != nil {
		http.Error(w, "failed to persist sync state", http.StatusInternalServerError)
		return
	}

	resp := SyncPayload{
		ServerTime:     serverNowMs,
		Categories:     make([]SyncCategory, 0, len(st.Categories)),
		Mangas:         make([]SyncManga, 0, len(st.Mangas)),
		ExtensionRepos: make([]string, 0, len(st.Repos)),
	}
	for _, c := range st.Categories {
		resp.Categories = append(resp.Categories, c)
	}
	sort.Slice(resp.Categories, func(i, j int) bool {
		if resp.Categories[i].Order != resp.Categories[j].Order {
			return resp.Categories[i].Order < resp.Categories[j].Order
		}
		return resp.Categories[i].Name < resp.Categories[j].Name
	})
	for _, m := range st.Mangas {
		resp.Mangas = append(resp.Mangas, m)
	}
	sort.Slice(resp.Mangas, func(i, j int) bool {
		if resp.Mangas[i].SourceID != resp.Mangas[j].SourceID {
			return resp.Mangas[i].SourceID < resp.Mangas[j].SourceID
		}
		return resp.Mangas[i].URL < resp.Mangas[j].URL
	})
	for repo := range st.Repos {
		resp.ExtensionRepos = append(resp.ExtensionRepos, repo)
	}
	sort.Strings(resp.ExtensionRepos)

	// Compute extensions known to the hub that this device does not have installed.
	devicePkgs := make(map[string]bool, len(req.InstalledExtensions))
	for _, e := range req.InstalledExtensions {
		devicePkgs[e.PkgName] = true
	}
	for pkg, ext := range st.Extensions {
		if !devicePkgs[pkg] {
			resp.MissingExtensions = append(resp.MissingExtensions, ext)
		}
	}
	sort.Slice(resp.MissingExtensions, func(i, j int) bool {
		return resp.MissingExtensions[i].PkgName < resp.MissingExtensions[j].PkgName
	})

	// Best-effort push of merged chapter read states back to Suwayomi.
	if suw != nil && len(resp.Mangas) > 0 {
		suw.pushChapterReadStates(r.Context(), resp.Mangas)
	}

	writeJSON(w, resp)
}
