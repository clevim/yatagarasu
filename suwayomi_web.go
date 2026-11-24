package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- Image proxy ------------------------------------------------------------

func (s shelf) handleSuwayomiImage(w http.ResponseWriter, r *http.Request) {
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusNotFound)
		return
	}
	imgPath := r.URL.Query().Get("path")
	if imgPath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	if err := suw.proxyImage(r.Context(), imgPath, w); err != nil {
		http.Error(w, "image proxy failed", http.StatusBadGateway)
	}
}

// ---- Browse & Extensions ----------------------------------------------------

type globalSearchGroup struct {
	Source SuwayomiSource
	Mangas []SuwayomiMangaCard
}

func (s shelf) handleBrowsePage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	nav := s.buildNav(r, "browse", len(entries))

	tab := r.URL.Query().Get("tab")
	if tab != "extensions" {
		tab = "sources"
	}
	selectedSourceID := strings.TrimSpace(r.URL.Query().Get("source"))
	browseType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))
	if browseType != "LATEST" && browseType != "SEARCH" {
		browseType = "POPULAR"
	}
	searchQ := strings.TrimSpace(r.URL.Query().Get("q"))
	if searchQ != "" && r.URL.Query().Get("type") == "" {
		browseType = "SEARCH"
	}
	extQ := strings.TrimSpace(r.URL.Query().Get("eq"))
	extLang := strings.TrimSpace(r.URL.Query().Get("lang"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	var (
		sources        []SuwayomiSource
		selectedSource *SuwayomiSource
		browseResult   *SuwayomiBrowseResult
		globalMode     bool
		globalResults  []globalSearchGroup
		extRepos       []string
		extLangs       []string
		installedExts  []SuwayomiExtension
		availableExts  []SuwayomiExtension
		problem        = r.URL.Query().Get("problem")
		note           = r.URL.Query().Get("note")
		suwConfigured  bool
	)

	if suw := s.suwayomi(); suw != nil {
		suwConfigured = true
		if tab == "extensions" {
			if repos, err := suw.getExtensionRepos(r.Context()); err == nil {
				extRepos = repos
			}
			exts, err := suw.listExtensions(r.Context())
			if err != nil {
				problem = "Could not load extensions from Suwayomi: " + err.Error()
			} else {
				langSet := map[string]struct{}{}
				needle := strings.ToLower(extQ)
				for _, e := range exts {
					if e.Lang != "" {
						langSet[e.Lang] = struct{}{}
					}
					if extLang != "" && !strings.EqualFold(e.Lang, extLang) {
						continue
					}
					if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) && !strings.Contains(strings.ToLower(e.Lang), needle) && !strings.Contains(strings.ToLower(e.PkgName), needle) {
						continue
					}
					if e.IsInstalled {
						installedExts = append(installedExts, e)
					} else {
						availableExts = append(availableExts, e)
					}
				}
				for l := range langSet {
					extLangs = append(extLangs, l)
				}
				sort.Strings(extLangs)
			}
		} else {
			var err error
			sources, err = suw.listSources(r.Context())
			if err != nil {
				problem = "Could not load sources from Suwayomi: " + err.Error()
			} else if len(sources) > 0 {
				if selectedSourceID == "all" {
					globalMode = true
					browseType = "SEARCH"
					if searchQ != "" {
						globalResults = runGlobalSearch(r.Context(), suw, sources, searchQ)
					}
				} else {
					if selectedSourceID == "" {
						selectedSourceID = sources[0].ID
					}
					for i := range sources {
						if sources[i].ID == selectedSourceID {
							selectedSource = &sources[i]
							break
						}
					}
					if selectedSource == nil {
						selectedSource = &sources[0]
						selectedSourceID = sources[0].ID
					}
					if browseType == "LATEST" && !selectedSource.SupportsLatest {
						browseType = "POPULAR"
					}
					if browseType != "SEARCH" || searchQ != "" {
						res, err := suw.browseSource(r.Context(), selectedSourceID, browseType, searchQ, page)
						if err != nil {
							problem = "Browse failed: " + err.Error()
						} else {
							browseResult = res
						}
					}
				}
			}
		}
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := browseTmpl.Execute(w, map[string]any{
		"Nonce":          nonce,
		"Nav":            nav,
		"Configured":     suwConfigured,
		"Tab":            tab,
		"Sources":        sources,
		"SelectedSource": selectedSource,
		"SourceID":       selectedSourceID,
		"GlobalMode":     globalMode,
		"GlobalResults":  globalResults,
		"BrowseType":     browseType,
		"Query":          searchQ,
		"ExtQuery":       extQ,
		"ExtLang":        extLang,
		"ExtLangs":       extLangs,
		"ExtRepos":       extRepos,
		"Page":           page,
		"PrevPage":       page - 1,
		"NextPage":       page + 1,
		"BrowseResult":   browseResult,
		"InstalledExts":  installedExts,
		"AvailableExts":  availableExts,
		"Note":           note,
		"Problem":        problem,
	}); err != nil {
		log.Printf("yata: rendering browse page failed: %v", err)
	}
}

func runGlobalSearch(ctx context.Context, suw *suwayomiClient, sources []SuwayomiSource, query string) []globalSearchGroup {
	limit := len(sources)
	if limit > 25 {
		limit = 25
	}
	results := make([]globalSearchGroup, limit)
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(idx int, src SuwayomiSource) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			res, err := suw.browseSource(sctx, src.ID, "SEARCH", query, 1)
			if err == nil && res != nil && len(res.Mangas) > 0 {
				results[idx] = globalSearchGroup{Source: src, Mangas: res.Mangas}
			}
		}(i, sources[i])
	}
	wg.Wait()
	var out []globalSearchGroup
	for _, g := range results {
		if len(g.Mangas) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func (s shelf) handleExtensionAction(w http.ResponseWriter, r *http.Request) {
	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, "/browse?tab=extensions&problem=Suwayomi+not+configured.", http.StatusSeeOther)
		return
	}
	action := strings.TrimSpace(r.PostFormValue("action"))
	pkgName := strings.TrimSpace(r.PostFormValue("pkgName"))

	var err error
	var msg string
	switch action {
	case "refresh":
		err = suw.fetchExtensions(r.Context())
		msg = "Extension list refreshed."
	case "install":
		err = suw.updateExtension(r.Context(), pkgName, true, false, false)
		msg = "Extension installed."
	case "update":
		err = suw.updateExtension(r.Context(), pkgName, false, true, false)
		msg = "Extension updated."
	case "uninstall":
		err = suw.updateExtension(r.Context(), pkgName, false, false, true)
		msg = "Extension uninstalled."
	case "add-repo":
		repoURL := strings.TrimSpace(r.PostFormValue("repoUrl"))
		if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") {
			http.Redirect(w, r, "/browse?tab=extensions&problem=Repository+URL+must+start+with+http%3A%2F%2F+or+https%3A%2F%2F.", http.StatusSeeOther)
			return
		}
		repos, rerr := suw.getExtensionRepos(r.Context())
		if rerr != nil {
			err = rerr
			break
		}
		exists := false
		for _, existing := range repos {
			if existing == repoURL {
				exists = true
				break
			}
		}
		if !exists {
			repos = append(repos, repoURL)
			_, err = suw.setExtensionRepos(r.Context(), repos)
			if err == nil {
				_ = suw.fetchExtensions(r.Context())
			}
		}
		msg = "Extension repository added."
	case "remove-repo":
		repoURL := strings.TrimSpace(r.PostFormValue("repoUrl"))
		repos, rerr := suw.getExtensionRepos(r.Context())
		if rerr != nil {
			err = rerr
			break
		}
		filtered := make([]string, 0, len(repos))
		for _, existing := range repos {
			if existing != repoURL {
				filtered = append(filtered, existing)
			}
		}
		_, err = suw.setExtensionRepos(r.Context(), filtered)
		msg = "Extension repository removed."
	default:
		http.Redirect(w, r, "/browse?tab=extensions&problem=Unknown+action.", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/browse?tab=extensions&problem="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/browse?tab=extensions&note="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ---- Manga Details ----------------------------------------------------------

type mangaChapterView struct {
	ID           int64
	Number       string
	Name         string
	Scanlator    string
	Uploaded     string
	IsRead       bool
	IsDownloaded bool
	PageCount    int
}

func (s shelf) handleMangaPage(w http.ResponseWriter, r *http.Request) {
	mangaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || mangaID <= 0 {
		http.Error(w, "invalid manga id", http.StatusBadRequest)
		return
	}
	entries, _ := s.list()
	nav := s.buildNav(r, "browse", len(entries))

	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, "/browse?problem=Configure+Suwayomi+in+Settings+first.", http.StatusSeeOther)
		return
	}

	detail, err := suw.getManga(r.Context(), mangaID)
	if err != nil {
		http.Error(w, "manga not found: "+err.Error(), http.StatusNotFound)
		return
	}

	var readCount, dlCount int
	chapters := make([]mangaChapterView, 0, len(detail.Chapters))
	for _, ch := range detail.Chapters {
		if ch.IsRead {
			readCount++
		}
		if ch.IsDownloaded {
			dlCount++
		}
		var upUnix int64
		if u, err := strconv.ParseInt(ch.UploadDate, 10, 64); err == nil {
			if u > 1e12 {
				u /= 1000
			}
			upUnix = u
		}
		chapters = append(chapters, mangaChapterView{
			ID:           ch.ID,
			Number:       chapterNumber(ch.ChapterNumber),
			Name:         ch.Name,
			Scanlator:    ch.Scanlator,
			Uploaded:     humanTime(upUnix),
			IsRead:       ch.IsRead,
			IsDownloaded: ch.IsDownloaded,
			PageCount:    ch.PageCount,
		})
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := mangaTmpl.Execute(w, map[string]any{
		"Nonce":      nonce,
		"Nav":        nav,
		"Manga":      detail,
		"Chapters":   chapters,
		"ReadCount":  readCount,
		"DlCount":    dlCount,
		"TotalCount": len(chapters),
		"Note":       r.URL.Query().Get("note"),
		"Problem":    r.URL.Query().Get("problem"),
	}); err != nil {
		log.Printf("yata: rendering manga page failed: %v", err)
	}
}

func (s shelf) handleMangaLibraryToggle(w http.ResponseWriter, r *http.Request) {
	mangaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || mangaID <= 0 {
		http.Error(w, "invalid manga id", http.StatusBadRequest)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusBadRequest)
		return
	}
	inLib := r.PostFormValue("inLibrary") == "true"
	if err := suw.toggleLibrary(r.Context(), mangaID, inLib); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	msg := "Removed from library."
	if inLib {
		msg = "Added to library."
	}
	http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=%s", mangaID, url.QueryEscape(msg)), http.StatusSeeOther)
}

func (s shelf) handleMangaRefreshChapters(w http.ResponseWriter, r *http.Request) {
	mangaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || mangaID <= 0 {
		http.Error(w, "invalid manga id", http.StatusBadRequest)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusBadRequest)
		return
	}
	if err := suw.fetchChapters(r.Context(), mangaID); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=Chapters+refreshed.", mangaID), http.StatusSeeOther)
}

func (s shelf) handleMangaChapterAction(w http.ResponseWriter, r *http.Request) {
	mangaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	chID, err2 := strconv.ParseInt(r.PathValue("chapterId"), 10, 64)
	if err != nil || err2 != nil || mangaID <= 0 || chID <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusBadRequest)
		return
	}
	action := strings.TrimSpace(r.PostFormValue("action"))
	var actErr error
	var msg string
	switch action {
	case "download":
		actErr = suw.enqueueDownload(r.Context(), chID)
		msg = "Chapter queued for download."
	case "delete-download":
		actErr = suw.deleteDownload(r.Context(), chID)
		msg = "Downloaded chapter deleted."
	case "read":
		actErr = suw.markChapterRead(r.Context(), chID, true, -1)
		msg = "Chapter marked read."
	case "unread":
		actErr = suw.markChapterRead(r.Context(), chID, false, 0)
		msg = "Chapter marked unread."
	default:
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=Unknown+action.", mangaID), http.StatusSeeOther)
		return
	}
	if actErr != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s", mangaID, url.QueryEscape(actErr.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=%s", mangaID, url.QueryEscape(msg)), http.StatusSeeOther)
}

// ---- Downloads & Updates ----------------------------------------------------

type downloadItemView struct {
	State         string
	ProgressPct   int
	ChapterID     int64
	ChapterName   string
	ChapterNumber string
	MangaID       int64
	MangaTitle    string
	ThumbnailURL  string
}

func (s shelf) handleDownloadsPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	nav := s.buildNav(r, "downloads", len(entries))

	var (
		configured bool
		state      = "STOPPED"
		items      []downloadItemView
		problem    = r.URL.Query().Get("problem")
		note       = r.URL.Query().Get("note")
	)

	if suw := s.suwayomi(); suw != nil {
		configured = true
		ds, err := suw.downloadStatus(r.Context())
		if err != nil {
			problem = "Could not load download queue: " + err.Error()
		} else {
			if ds.State != "" {
				state = ds.State
			}
			for _, it := range ds.Queue {
				pct := int(it.Progress * 100)
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}
				items = append(items, downloadItemView{
					State:         it.State,
					ProgressPct:   pct,
					ChapterID:     it.ChapterID,
					ChapterName:   it.ChapterName,
					ChapterNumber: chapterNumber(it.ChapterNumber),
					MangaID:       it.MangaID,
					MangaTitle:    it.MangaTitle,
					ThumbnailURL:  it.ThumbnailURL,
				})
			}
		}
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := downloadsTmpl.Execute(w, map[string]any{
		"Nonce":      nonce,
		"Nav":        nav,
		"Configured": configured,
		"State":      state,
		"Items":      items,
		"Note":       note,
		"Problem":    problem,
	}); err != nil {
		log.Printf("yata: rendering downloads page failed: %v", err)
	}
}

func (s shelf) handleDownloadsControl(w http.ResponseWriter, r *http.Request) {
	redir := "/downloads"
	if target := strings.TrimSpace(r.PostFormValue("redirect")); target == "/library" || target == "/downloads" {
		redir = target
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, redir+"?problem=Suwayomi+not+configured.", http.StatusSeeOther)
		return
	}
	action := strings.TrimSpace(r.PostFormValue("action"))
	var err error
	var msg string
	switch action {
	case "start":
		err = suw.controlDownloader(r.Context(), "start")
		msg = "Downloader resumed."
	case "stop":
		err = suw.controlDownloader(r.Context(), "stop")
		msg = "Downloader paused."
	case "clear":
		err = suw.controlDownloader(r.Context(), "clear")
		msg = "Download queue cleared."
	case "update-library":
		err = suw.startLibraryUpdate(r.Context())
		msg = "Library update started."
	case "dequeue":
		chID, parseErr := strconv.ParseInt(r.PostFormValue("chapterId"), 10, 64)
		if parseErr != nil || chID <= 0 {
			http.Redirect(w, r, redir+"?problem=Invalid+chapter+ID.", http.StatusSeeOther)
			return
		}
		err = suw.dequeueDownload(r.Context(), chID)
		msg = "Removed from queue."
	default:
		http.Redirect(w, r, redir+"?problem=Unknown+action.", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, redir+"?problem="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redir+"?note="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ---- Built-in Web Manga Reader (Suwayomi + Local Shelf CBZ) -----------------

func (s shelf) handleReadSuwayomiChapter(w http.ResponseWriter, r *http.Request) {
	chID, err := strconv.ParseInt(r.PathValue("chapterId"), 10, 64)
	if err != nil || chID <= 0 {
		http.Error(w, "invalid chapter id", http.StatusBadRequest)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, "/browse?problem=Configure+Suwayomi+in+Settings+first.", http.StatusSeeOther)
		return
	}
	info, err := suw.getChapterInfo(r.Context(), chID)
	if err != nil {
		http.Error(w, "chapter not found: "+err.Error(), http.StatusNotFound)
		return
	}
	pages, err := suw.getChapterPages(r.Context(), chID)
	if err != nil {
		http.Error(w, "could not load chapter pages: "+err.Error(), http.StatusBadGateway)
		return
	}

	backURL := "/browse"
	backLabel := "Browse"
	if info.MangaID > 0 {
		backURL = fmt.Sprintf("/manga/%d", info.MangaID)
		backLabel = "Manga"
	}
	var prevURL, nextURL string
	if info.PrevChapterID > 0 {
		prevURL = fmt.Sprintf("/read/%d", info.PrevChapterID)
	}
	if info.NextChapterID > 0 {
		nextURL = fmt.Sprintf("/read/%d", info.NextChapterID)
	}

	s.renderReader(w, readerViewData{
		MangaTitle:   info.MangaTitle,
		ChapterTitle: info.Name,
		BackURL:      backURL,
		BackLabel:    backLabel,
		PrevURL:      prevURL,
		NextURL:      nextURL,
		MarkReadURL:  fmt.Sprintf("/read/%d/read", chID),
		IsRead:       info.IsRead,
		Pages:        pages,
	})
}

func (s shelf) handleReadSuwayomiMarkRead(w http.ResponseWriter, r *http.Request) {
	chID, err := strconv.ParseInt(r.PathValue("chapterId"), 10, 64)
	if err != nil || chID <= 0 {
		http.Error(w, "invalid chapter id", http.StatusBadRequest)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusBadRequest)
		return
	}
	wantRead := true
	lastPage := -1
	if isForm(r) {
		wantRead = r.PostFormValue("read") != "false"
		if lp, err := strconv.Atoi(r.PostFormValue("lastPageRead")); err == nil {
			lastPage = lp
		}
	} else {
		var body struct {
			Read         *bool `json:"read"`
			LastPageRead *int  `json:"lastPageRead"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body)
		if body.Read != nil {
			wantRead = *body.Read
		}
		if body.LastPageRead != nil {
			lastPage = *body.LastPageRead
		}
	}
	if err := suw.markChapterRead(r.Context(), chID, wantRead, lastPage); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if isForm(r) {
		http.Redirect(w, r, fmt.Sprintf("/read/%d", chID), http.StatusSeeOther)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "read": wantRead})
}

// ---- Local Shelf CBZ Reader -------------------------------------------------

func isImageName(name string) bool {
	if strings.HasPrefix(name, "__MACOSX/") || strings.HasSuffix(name, "/") {
		return false
	}
	base := path.Base(name)
	if strings.HasPrefix(base, ".") {
		return false
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif", ".bmp":
		return true
	}
	return false
}

// naturalLess sorts filenames with embedded numbers in human reading order
// so "page2.jpg" comes before "page10.jpg".
func naturalLess(a, b string) bool {
	al, bl := strings.ToLower(a), strings.ToLower(b)
	ia, ib := 0, 0
	for ia < len(al) && ib < len(bl) {
		ca, cb := al[ia], bl[ib]
		if ca >= '0' && ca <= '9' && cb >= '0' && cb <= '9' {
			ja, jb := ia, ib
			for ja < len(al) && al[ja] >= '0' && al[ja] <= '9' {
				ja++
			}
			for jb < len(bl) && bl[jb] >= '0' && bl[jb] <= '9' {
				jb++
			}
			na, errA := strconv.Atoi(al[ia:ja])
			nb, errB := strconv.Atoi(bl[ib:jb])
			if errA == nil && errB == nil && na != nb {
				return na < nb
			}
			ia, ib = ja, jb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		ia++
		ib++
	}
	return len(al) < len(bl)
}

func listCBZImages(cbzPath string) ([]string, error) {
	zr, err := zip.OpenReader(cbzPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && isImageName(f.Name) {
			names = append(names, f.Name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	return names, nil
}

func (s shelf) handleReadShelfChapter(w http.ResponseWriter, r *http.Request) {
	id, err := chapterID(r)
	if err != nil {
		http.Error(w, "bad chapterId", http.StatusBadRequest)
		return
	}
	entry, err := s.load(id)
	if err != nil {
		http.Error(w, "no such shelf chapter", http.StatusNotFound)
		return
	}
	names, err := listCBZImages(s.cbz(id))
	if err != nil {
		http.Error(w, "could not read CBZ archive", http.StatusInternalServerError)
		return
	}

	pages := make([]string, len(names))
	for i := range names {
		pages[i] = fmt.Sprintf("/api/shelf/%d/page/%d", id, i)
	}

	// Find prev/next chapters on the shelf for the same manga.
	var prevURL, nextURL string
	if all, err := s.list(); err == nil {
		var same []Entry
		for _, e := range all {
			if e.MangaTitle == entry.MangaTitle {
				same = append(same, e)
			}
		}
		sort.SliceStable(same, func(i, j int) bool {
			if same[i].ChapterNumber != same[j].ChapterNumber {
				return same[i].ChapterNumber < same[j].ChapterNumber
			}
			return same[i].ChapterID < same[j].ChapterID
		})
		for i, e := range same {
			if e.ChapterID == id {
				if i > 0 {
					prevURL = fmt.Sprintf("/read/shelf/%d", same[i-1].ChapterID)
				}
				if i+1 < len(same) {
					nextURL = fmt.Sprintf("/read/shelf/%d", same[i+1].ChapterID)
				}
				break
			}
		}
	}

	s.renderReader(w, readerViewData{
		MangaTitle:   entry.MangaTitle,
		ChapterTitle: entry.ChapterName,
		BackURL:      "/",
		BackLabel:    "Shelf",
		PrevURL:      prevURL,
		NextURL:      nextURL,
		MarkReadURL:  fmt.Sprintf("/api/shelf/%d/read", id),
		IsRead:       entry.Read,
		Pages:        pages,
	})
}

func (s shelf) handleShelfChapterPage(w http.ResponseWriter, r *http.Request) {
	id, err := chapterID(r)
	if err != nil {
		http.Error(w, "bad chapterId", http.StatusBadRequest)
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || idx < 0 {
		http.Error(w, "bad page index", http.StatusBadRequest)
		return
	}
	zr, err := zip.OpenReader(s.cbz(id))
	if err != nil {
		http.Error(w, "chapter CBZ not found", http.StatusNotFound)
		return
	}
	defer zr.Close()

	var imgs []*zip.File
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && isImageName(f.Name) {
			imgs = append(imgs, f)
		}
	}
	sort.Slice(imgs, func(i, j int) bool { return naturalLess(imgs[i].Name, imgs[j].Name) })
	if idx >= len(imgs) {
		http.Error(w, "page out of range", http.StatusNotFound)
		return
	}
	f := imgs[idx]
	rc, err := f.Open()
	if err != nil {
		http.Error(w, "cannot open page", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	ext := strings.ToLower(path.Ext(f.Name))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		switch ext {
		case ".webp":
			ct = "image/webp"
		case ".avif":
			ct = "image/avif"
		default:
			ct = "image/jpeg"
		}
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (s shelf) readShelfChapterPageBytes(id int64, idx int) ([]byte, error) {
	if idx < 0 {
		return nil, errors.New("bad page index")
	}
	zr, err := zip.OpenReader(s.cbz(id))
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var imgs []*zip.File
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && isImageName(f.Name) {
			imgs = append(imgs, f)
		}
	}
	sort.Slice(imgs, func(i, j int) bool { return naturalLess(imgs[i].Name, imgs[j].Name) })
	if idx >= len(imgs) {
		return nil, errors.New("page out of range")
	}
	rc, err := imgs[idx].Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 32<<20))
}

type readerViewData struct {
	MangaTitle   string
	ChapterTitle string
	BackURL      string
	BackLabel    string
	PrevURL      string
	NextURL      string
	MarkReadURL  string
	IsRead       bool
	Pages        []string
}

func (s shelf) renderReader(w http.ResponseWriter, data readerViewData) {
	it := s.imageTranslator()
	hasTranslator := it != nil
	translatorLang := ""
	if it != nil {
		translatorLang = it.lang
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := readerTmpl.Execute(w, map[string]any{
		"Nonce":          nonce,
		"MangaTitle":     data.MangaTitle,
		"ChapterTitle":   data.ChapterTitle,
		"BackURL":        data.BackURL,
		"BackLabel":      data.BackLabel,
		"PrevURL":        data.PrevURL,
		"NextURL":        data.NextURL,
		"MarkReadURL":    data.MarkReadURL,
		"IsRead":         data.IsRead,
		"Pages":          data.Pages,
		"PageCount":      len(data.Pages),
		"HasTranslator":  hasTranslator,
		"TranslatorLang": translatorLang,
	}); err != nil {
		log.Printf("yata: rendering reader page failed: %v", err)
	}
}

