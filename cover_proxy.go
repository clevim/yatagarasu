package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCoverBytes = 10 << 20 // 10 MiB

// coverProxyURL rewrites external http(s) cover URLs to Yatagarasu's local
// disk-cached proxy (/api/cover?url=...) so MangaDex and scanlator hotlink
// protections (Referer checks) never break library thumbnails.
func coverProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return "/api/cover?url=" + url.QueryEscape(raw)
	}
	return ""
}

func isBlockedCoverIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// Carrier-grade NAT 100.64.0.0/10
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}

// coverHTTPClient is a hardened HTTP client for external cover fetching. Tests
// may override coverAllowPrivateIPs to point at httptest servers on 127.0.0.1.
var coverAllowPrivateIPs bool

func newCoverHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, errors.New("no ip addresses for host")
			}
			var chosen net.IP
			for _, ipAddr := range ips {
				if !coverAllowPrivateIPs && isBlockedCoverIP(ipAddr.IP) {
					return nil, fmt.Errorf("blocked private cover host %s", host)
				}
				if chosen == nil {
					chosen = ipAddr.IP
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
		},
		TLSHandshakeTimeout: 8 * time.Second,
		MaxIdleConns:        32,
		IdleConnTimeout:     60 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("disallowed redirect scheme")
			}
			return nil
		},
	}
}

func (s shelf) coversDir() string {
	return filepath.Join(s.dir, "covers")
}

func (s shelf) coverCachePath(rawURL string) string {
	h := sha256.Sum256([]byte(rawURL))
	return filepath.Join(s.coversDir(), hex.EncodeToString(h[:16]))
}

// handleCoverProxy fetches and disk-caches external manga cover images with
// SSRF protection and target-origin Referer headers to bypass hotlink blocks.
func (s shelf) handleCoverProxy(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if rawURL == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "invalid cover url", http.StatusBadRequest)
		return
	}

	cachePath := s.coverCachePath(rawURL)
	if cached, err := os.ReadFile(cachePath); err == nil && len(cached) > 0 {
		ct := http.DetectContentType(cached)
		if strings.HasPrefix(ct, "image/") {
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", "private, max-age=604800")
			w.Header().Set("X-Yata-Cache", "hit")
			_, _ = w.Write(cached)
			return
		}
	}

	client := newCoverHTTPClient()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		http.Error(w, "invalid cover url", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "cover fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("cover upstream http %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxCoverBytes {
		http.Error(w, "invalid cover body", http.StatusBadGateway)
		return
	}
	ct := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.HasPrefix(ct, "image/") {
		detected := http.DetectContentType(body)
		if !strings.HasPrefix(detected, "image/") {
			http.Error(w, "upstream response is not an image", http.StatusBadGateway)
			return
		}
		ct = detected
	}

	if err := os.MkdirAll(s.coversDir(), 0o755); err == nil {
		tmp := cachePath + ".tmp"
		if os.WriteFile(tmp, body, 0o644) == nil {
			_ = os.Rename(tmp, cachePath)
		}
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("X-Yata-Cache", "miss")
	_, _ = w.Write(body)
}

// ---- Synced Manga Details & Instant Materialization -------------------------

type syncedChapterView struct {
	Name          string
	Number        string
	Scanlator     string
	Read          bool
	Bookmark      bool
	LastPageRead  int
	ShelfID       int64
}

func (s shelf) handleSyncedMangaPage(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("sourceId")), 10, 64)
	mangaURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if err != nil || sourceID == 0 || mangaURL == "" {
		http.Redirect(w, r, "/library?problem=Invalid+manga+link.", http.StatusSeeOther)
		return
	}

	// If Suwayomi already has this manga, jump straight to the full Suwayomi details page.
	suw := s.suwayomi()
	if suw != nil {
		if suwID, ferr := suw.findMangaID(r.Context(), sourceID, mangaURL); ferr == nil && suwID > 0 {
			http.Redirect(w, r, fmt.Sprintf("/manga/%d", suwID), http.StatusSeeOther)
			return
		}
	}

	st := s.loadSyncState()
	key := SyncMangaKey{SourceID: sourceID, URL: mangaURL}.str()
	m, ok := st.Mangas[key]
	if !ok {
		http.Redirect(w, r, "/library?problem=Manga+not+found+in+synced+library.", http.StatusSeeOther)
		return
	}

	// Match any chapters already uploaded to the KOReader shelf so they can be read immediately.
	shelfEntries, _ := s.list()
	shelfByURL := make(map[string]int64, len(shelfEntries))
	for _, e := range shelfEntries {
		if e.ChapterURL != "" {
			shelfByURL[e.ChapterURL] = e.ChapterID
		}
	}

	sortedCh := append([]SyncChapter(nil), m.Chapters...)
	sort.Slice(sortedCh, func(i, j int) bool {
		if sortedCh[i].ChapterNumber != sortedCh[j].ChapterNumber {
			return sortedCh[i].ChapterNumber > sortedCh[j].ChapterNumber
		}
		return sortedCh[i].Name > sortedCh[j].Name
	})

	chapters := make([]syncedChapterView, 0, len(sortedCh))
	readCount := 0
	for _, ch := range sortedCh {
		if ch.Read {
			readCount++
		}
		num := ""
		if ch.ChapterNumber > 0 {
			num = strconv.FormatFloat(ch.ChapterNumber, 'f', -1, 64)
		}
		chapters = append(chapters, syncedChapterView{
			Name:         ch.Name,
			Number:       num,
			Scanlator:    ch.Scanlator,
			Read:         ch.Read,
			Bookmark:     ch.Bookmark,
			LastPageRead: ch.LastPageRead,
			ShelfID:      shelfByURL[ch.URL],
		})
	}

	nav := s.buildNav(r, "library", len(shelfEntries))
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	data := map[string]any{
		"Nonce":             nonce,
		"Nav":               nav,
		"Manga":             m,
		"CoverURL":          coverProxyURL(m.ThumbnailURL),
		"Initial":           titleInitial(m.Title),
		"Chapters":          chapters,
		"ReadCount":         readCount,
		"TotalCount":        len(chapters),
		"UnreadCount":       max(0, len(chapters)-readCount),
		"SuwayomiConnected": suw != nil,
		"Problem":           r.URL.Query().Get("problem"),
		"Note":              r.URL.Query().Get("note"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := syncedMangaTmpl.Execute(w, data); err != nil {
		log.Printf("yata: rendering synced manga page failed: %v", err)
	}
}

func (s shelf) handleSyncedMangaMaterialize(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("sourceId")), 10, 64)
	mangaURL := strings.TrimSpace(r.PostFormValue("url"))
	back := fmt.Sprintf("/manga/sync?sourceId=%d&url=%s", sourceID, url.QueryEscape(mangaURL))
	if err != nil || sourceID == 0 || mangaURL == "" {
		http.Redirect(w, r, "/library?problem=Invalid+manga+request.", http.StatusSeeOther)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, back+"&problem=Suwayomi+is+not+configured.", http.StatusSeeOther)
		return
	}
	if suwID, ferr := suw.findMangaID(r.Context(), sourceID, mangaURL); ferr == nil && suwID > 0 {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d", suwID), http.StatusSeeOther)
		return
	}

	st := s.loadSyncState()
	key := SyncMangaKey{SourceID: sourceID, URL: mangaURL}.str()
	m, ok := st.Mangas[key]
	if !ok {
		http.Redirect(w, r, "/library?problem=Manga+not+found+in+synced+library.", http.StatusSeeOther)
		return
	}
	cats := make([]SyncCategory, 0, len(st.Categories))
	for _, c := range st.Categories {
		cats = append(cats, c)
	}
	if _, merr := suw.materializeMissingMangas(r.Context(), cats, []SyncManga{m}); merr != nil {
		http.Redirect(w, r, back+"&problem="+url.QueryEscape("Could not import into Suwayomi: "+merr.Error()), http.StatusSeeOther)
		return
	}
	if suwID, ferr := suw.findMangaID(r.Context(), sourceID, mangaURL); ferr == nil && suwID > 0 {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=Imported+into+Suwayomi.", suwID), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"&problem="+url.QueryEscape("Import queued in Suwayomi. Install the source extension under Browse → Extensions if chapters do not load yet."), http.StatusSeeOther)
}
