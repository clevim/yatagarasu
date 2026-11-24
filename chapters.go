package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func chapterHash(sourceID int64, mangaURL, chapterURL string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", sourceID, mangaURL, chapterURL)))
	return hex.EncodeToString(h[:16])
}

func (s shelf) localDownloadCBZ(sourceID int64, mangaURL, chapterURL string) string {
	return filepath.Join(s.dir, "downloads", chapterHash(sourceID, mangaURL, chapterURL)+".cbz")
}

func (s shelf) handleLocateChapter(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.URL.Query().Get("sourceId"), 10, 64)
	mangaURL := r.URL.Query().Get("mangaUrl")
	chapterURL := r.URL.Query().Get("chapterUrl")
	if err != nil || mangaURL == "" || chapterURL == "" {
		http.Error(w, "sourceId, mangaUrl and chapterUrl are required", http.StatusBadRequest)
		return
	}

	if st, err := os.Stat(s.localDownloadCBZ(sourceID, mangaURL, chapterURL)); err == nil && !st.IsDir() {
		writeJSON(w, map[string]any{
			"available": true,
			"source":    "local",
			"size":      st.Size(),
		})
		return
	}

	if suw := s.suwayomi(); suw != nil {
		if loc, err := suw.locateChapter(r.Context(), sourceID, mangaURL, chapterURL); err == nil && loc != nil {
			writeJSON(w, map[string]any{
				"available":         loc.IsDownloaded,
				"source":            "suwayomi",
				"suwayomiChapterId": loc.ChapterID,
				"canAcquire":        true,
			})
			return
		}
	}

	writeJSON(w, map[string]any{
		"available": false,
		"source":    "none",
	})
}

func (s shelf) handleChapterCBZ(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.URL.Query().Get("sourceId"), 10, 64)
	mangaURL := r.URL.Query().Get("mangaUrl")
	chapterURL := r.URL.Query().Get("chapterUrl")
	acquire := r.URL.Query().Get("acquire") == "true"
	if err != nil || mangaURL == "" || chapterURL == "" {
		http.Error(w, "sourceId, mangaUrl and chapterUrl are required", http.StatusBadRequest)
		return
	}

	localPath := s.localDownloadCBZ(sourceID, mangaURL, chapterURL)
	if st, err := os.Stat(localPath); err == nil && !st.IsDir() {
		w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
		http.ServeFile(w, r, localPath)
		return
	}

	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "chapter not available on server", http.StatusNotFound)
		return
	}

	loc, err := suw.locateChapter(r.Context(), sourceID, mangaURL, chapterURL)
	if err != nil || loc == nil {
		http.Error(w, "chapter not found on suwayomi", http.StatusNotFound)
		return
	}

	if !loc.IsDownloaded {
		if !acquire {
			http.Error(w, "chapter not downloaded on server", http.StatusNotFound)
			return
		}
		if err := suw.enqueueAndWaitDownload(r.Context(), loc.ChapterID, 90*time.Second); err != nil {
			http.Error(w, "server acquisition failed: "+err.Error(), http.StatusBadGateway)
			return
		}
	}

	if err := suw.streamChapterCBZ(r.Context(), loc.ChapterID, w); err != nil {
		// Headers may already be written if streaming failed mid-way.
		return
	}
}
