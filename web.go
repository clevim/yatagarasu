package main

import (
	"encoding/json"
	"fmt"
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
)

// ---- view model -------------------------------------------------------------

type chapterRow struct {
	ID       int64
	Number   string
	Name     string
	Size     string
	Read     bool
	When     string // uploaded, or read
	WhenFull string
	num      float64
}

type mangaGroup struct {
	Title    string
	Chapters []chapterRow
	Size     string
	ReadN    int
	TotalN   int
	bytes    int64
}

// dayBar carries SVG coordinates rather than percentages: the chart is drawn
// in a viewBox, so nothing depends on how a flex or grid container decides to
// share out its width.
type dayBar struct {
	Count int
	X     int // left edge in viewBox units
	Y     int // top of the bar
	H     int // bar height
	Title string
	Today bool
}

// Chart geometry, in viewBox units. The width follows the configured window,
// so a 7-day and a 30-day chart draw bars of the same thickness.
const (
	chartStep = 20
	chartBarW = 12
	chartH    = 60
	chartMinH = 3 // a day with activity must never render as empty
)

type recentRow struct {
	Manga   string
	Chapter string
	When    string
}

type navState struct {
	Active         string // "shelf" | "library" | "translate" | "backups" | "settings"
	ShelfCount     int
	LibraryCount   int
	BackupsCount   int
	CacheCount     int
	Suwayomi       suwayomiHealth
	Translate      translateHealth
	Version        string
}

type libraryMangaRow struct {
	SuwayomiID    int64
	SourceID      int64
	URL           string
	Href          string
	Title         string
	Initial       string
	ThumbnailURL  string
	Categories    []string
	UnreadCount   int
	DownloadCount int
	ReadCount     int
	TotalCount    int
	ProgressPct   int
	MergedCount   int
	Updated       string
}

type backupRow struct {
	ID        string
	Name      string
	Size      string
	Created   string
	CreatedAt int64
}

// ---- helpers ----------------------------------------------------------------

func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b > 0:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return "—"
}

func humanTime(unix int64) string {
	if unix <= 0 {
		return "—"
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return time.Unix(unix, 0).Format("2 Jan")
}

// dayStart is midnight where the reader lives. time.Truncate rounds in UTC, so
// it puts the day boundary at 21:00 in Brazil and labels every bar a day early.
func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// daysBetween counts calendar days. The half-day offset absorbs the 23- and
// 25-hour days a DST change produces.
func daysBetween(from, to time.Time) int {
	return int((to.Sub(from) + 12*time.Hour) / (24 * time.Hour))
}

// chapterNumber prints 1090 rather than 1090.0, but keeps 1090.5.
func chapterNumber(n float64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func countFilesIn(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".tmp") {
			n++
		}
	}
	return n
}

func (s shelf) translationStats() (cachedLines, chapterFiles, contextFiles int) {
	transCacheMu.Lock()
	cachedLines = len(s.loadTransCache())
	transCacheMu.Unlock()
	chapterFiles = countFilesIn(filepath.Join(s.translationsDir(), "chapters"))
	contextFiles = countFilesIn(filepath.Join(s.translationsDir(), "context"))
	return
}

func (s shelf) buildNav(r *http.Request, active string, shelfN int) navState {
	st := s.loadSyncState()
	backups, _ := s.listBackups()
	cachedLines, _, _ := s.translationStats()
	suwHealth := suwayomiHealth{Configured: false}
	if suw := s.suwayomi(); suw != nil {
		suwHealth = suw.checkHealth(r.Context())
	}
	return navState{
		Active:       active,
		ShelfCount:   shelfN,
		LibraryCount: len(st.Mangas),
		BackupsCount: len(backups),
		CacheCount:   cachedLines,
		Suwayomi:     suwHealth,
		Translate:    s.translateStatus(),
		Version:      version,
	}
}

// ---- handlers ---------------------------------------------------------------

func (s shelf) handleIndex(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	events := s.history()
	loc := s.cfg.location()
	chartDays := s.cfg.chartDays()

	var totalSize int64
	var readN int
	byManga := map[string]*mangaGroup{}
	order := []string{}
	newest := map[string]int64{}

	for _, e := range entries {
		totalSize += e.Size
		if e.Read {
			readN++
		}
		g, ok := byManga[e.MangaTitle]
		if !ok {
			g = &mangaGroup{Title: e.MangaTitle}
			byManga[e.MangaTitle] = g
			order = append(order, e.MangaTitle)
		}
		when, whenFull := e.UploadedAt, "Uploaded "
		if e.Read && e.ReadAt != nil {
			when, whenFull = *e.ReadAt, "Read "
		}
		g.Chapters = append(g.Chapters, chapterRow{
			ID:       e.ChapterID,
			Number:   chapterNumber(e.ChapterNumber),
			Name:     e.ChapterName,
			Size:     humanSize(e.Size),
			Read:     e.Read,
			When:     humanTime(when),
			WhenFull: whenFull + time.Unix(when, 0).In(loc).Format("2 Jan 2006, 15:04"),
			num:      e.ChapterNumber,
		})
		g.TotalN++
		g.bytes += e.Size
		if e.Read {
			g.ReadN++
		}
		if e.UploadedAt > newest[e.MangaTitle] {
			newest[e.MangaTitle] = e.UploadedAt
		}
	}

	sort.SliceStable(order, func(i, j int) bool { return newest[order[i]] > newest[order[j]] })
	groups := make([]*mangaGroup, 0, len(order))
	for _, title := range order {
		g := byManga[title]
		sort.SliceStable(g.Chapters, func(i, j int) bool { return g.Chapters[i].num < g.Chapters[j].num })
		g.Size = humanSize(g.bytes)
		groups = append(groups, g)
	}

	today := dayStart(time.Now().In(loc))
	counts := make([]int, chartDays)
	finished7 := 0
	for _, ev := range events {
		d := daysBetween(dayStart(time.Unix(ev.At, 0).In(loc)), today)
		if d >= 0 && d < chartDays {
			counts[chartDays-1-d]++
			if d < 7 {
				finished7++
			}
		}
	}
	max := 1
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	bars := make([]dayBar, chartDays)
	for i, c := range counts {
		day := today.AddDate(0, 0, i-chartDays+1)
		h := c * chartH / max
		if c > 0 && h < chartMinH {
			h = chartMinH
		}
		bars[i] = dayBar{
			Count: c,
			X:     i * chartStep,
			Y:     chartH - h,
			H:     h,
			Title: fmt.Sprintf("%s — %d finished", day.Format("Mon 2 Jan"), c),
			Today: i == chartDays-1,
		}
	}

	recent := make([]recentRow, 0, 8)
	for i := len(events) - 1; i >= 0 && len(recent) < 8; i-- {
		recent = append(recent, recentRow{
			Manga:   events[i].MangaTitle,
			Chapter: events[i].ChapterName,
			When:    humanTime(events[i].At),
		})
	}

	nav := s.buildNav(r, "shelf", len(entries))
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := indexTmpl.Execute(w, map[string]any{
		"Nonce":     nonce,
		"Nav":       nav,
		"Manga":     groups,
		"Chapters":  len(entries),
		"Unread":    len(entries) - readN,
		"ReadN":     readN,
		"MangaN":    len(groups),
		"Size":      humanSize(totalSize),
		"Finished7": finished7,
		"Bars":      bars,
		"Recent":    recent,
		"HasStats":  len(events) > 0,
		"ChartDays": chartDays,
		"ChartW":    chartDays * chartStep,
		"ChartH":    chartH,
		"BarW":      chartBarW,
		"ChartFrom": today.AddDate(0, 0, -chartDays+1).Format("2 Jan"),
	})
	if err != nil {
		log.Printf("yata: rendering the shelf page failed: %v", err)
	}
}

func titleInitial(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "?"
	}
	for _, r := range t {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func mergeStringSet(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range b {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func (s shelf) handleLibraryPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	st := s.loadSyncState()
	selectedCat := strings.TrimSpace(r.URL.Query().Get("cat"))
	problem := r.URL.Query().Get("problem")
	note := r.URL.Query().Get("note")
	updatingParam := r.URL.Query().Get("updating") == "1"

	var (
		suwConnected bool
		suwLib       *SuwayomiLibraryData
		updateStatus = &SuwayomiUpdateStatus{}
	)
	if suw := s.suwayomi(); suw != nil {
		suwConnected = true
		var err error
		suwLib, err = suw.listLibrary(r.Context())
		if err != nil && problem == "" {
			problem = "Could not fetch live Suwayomi library: " + err.Error()
		}
		if ust, uerr := suw.getLibraryUpdateStatus(r.Context()); uerr == nil && ust != nil {
			updateStatus = ust
		}
	}
	updatePct := 0
	if updateStatus.TotalJobs > 0 {
		updatePct = updateStatus.FinishedJobs * 100 / updateStatus.TotalJobs
		if updatePct > 100 {
			updatePct = 100
		}
	}

	catMap := make(map[string]SyncCategory, len(st.Categories))
	for _, c := range st.Categories {
		catMap[c.Name] = c
	}
	if suwLib != nil {
		for _, c := range suwLib.Categories {
			if _, exists := catMap[c.Name]; !exists {
				catMap[c.Name] = c
			}
		}
	}
	cats := make([]SyncCategory, 0, len(catMap))
	for _, c := range catMap {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool {
		if cats[i].Order != cats[j].Order {
			return cats[i].Order < cats[j].Order
		}
		return cats[i].Name < cats[j].Name
	})

	// Build merged manga list keyed by sourceId|url, with title fallback.
	allRows := make([]*libraryMangaRow, 0, len(st.Mangas))
	byKey := make(map[string]*libraryMangaRow, len(st.Mangas))
	byTitle := make(map[string]*libraryMangaRow, len(st.Mangas))

	for _, m := range st.Mangas {
		rc := 0
		for _, ch := range m.Chapters {
			if ch.Read {
				rc++
			}
		}
		tot := len(m.Chapters)
		unread := tot - rc
		if unread < 0 {
			unread = 0
		}
		pct := 0
		if tot > 0 {
			pct = rc * 100 / tot
		}
		upd := m.UpdatedAt
		if upd > 1e12 {
			upd /= 1000
		}
		thumb := strings.TrimSpace(m.ThumbnailURL)
		if strings.HasPrefix(thumb, "/") && !strings.HasPrefix(thumb, "//") {
			thumb = suwayomiImageURL(thumb)
		} else {
			thumb = coverProxyURL(thumb)
		}
		row := &libraryMangaRow{
			SourceID:     m.SourceID,
			URL:          m.URL,
			Title:        m.Title,
			Initial:      titleInitial(m.Title),
			ThumbnailURL: thumb,
			Categories:   append([]string(nil), m.Categories...),
			UnreadCount:  unread,
			ReadCount:    rc,
			TotalCount:   tot,
			ProgressPct:  pct,
			MergedCount:  len(m.MergedSources),
			Updated:      humanTime(upd),
		}
		allRows = append(allRows, row)
		if m.SourceID != 0 && m.URL != "" {
			byKey[fmt.Sprintf("%d|%s", m.SourceID, m.URL)] = row
		}
		if norm := strings.ToLower(strings.TrimSpace(m.Title)); norm != "" {
			if _, exists := byTitle[norm]; !exists {
				byTitle[norm] = row
			}
		}
	}

	if suwLib != nil {
		for _, m := range suwLib.Mangas {
			var row *libraryMangaRow
			if m.SourceID != 0 && m.URL != "" {
				row = byKey[fmt.Sprintf("%d|%s", m.SourceID, m.URL)]
			}
			if row == nil {
				row = byTitle[strings.ToLower(strings.TrimSpace(m.Title))]
			}
			if row != nil {
				row.SuwayomiID = m.ID
				if m.ThumbnailURL != "" {
					row.ThumbnailURL = m.ThumbnailURL
				}
				row.DownloadCount = m.DownloadCount
				if m.TotalChapters > 0 {
					row.TotalCount = m.TotalChapters
					row.UnreadCount = m.UnreadCount
					rc := m.TotalChapters - m.UnreadCount
					if rc < 0 {
						rc = 0
					}
					row.ReadCount = rc
					row.ProgressPct = rc * 100 / m.TotalChapters
				} else if m.UnreadCount > 0 {
					row.UnreadCount = m.UnreadCount
				}
				row.Categories = mergeStringSet(row.Categories, m.Categories)
			} else {
				tot := m.TotalChapters
				rc := tot - m.UnreadCount
				if rc < 0 {
					rc = 0
				}
				pct := 0
				if tot > 0 {
					pct = rc * 100 / tot
				}
				row = &libraryMangaRow{
					SuwayomiID:    m.ID,
					SourceID:      m.SourceID,
					URL:           m.URL,
					Title:         m.Title,
					Initial:       titleInitial(m.Title),
					ThumbnailURL:  m.ThumbnailURL,
					Categories:    m.Categories,
					UnreadCount:   m.UnreadCount,
					DownloadCount: m.DownloadCount,
					ReadCount:     rc,
					TotalCount:    tot,
					ProgressPct:   pct,
					Updated:       "live",
				}
				allRows = append(allRows, row)
			}
		}
	}

	var totalChapters, readChapters, mergedTotal int
	rows := make([]libraryMangaRow, 0, len(allRows))
	for _, row := range allRows {
		if row.SuwayomiID > 0 {
			row.Href = fmt.Sprintf("/manga/%d", row.SuwayomiID)
		} else if row.SourceID != 0 && row.URL != "" {
			row.Href = fmt.Sprintf("/manga/sync?sourceId=%d&url=%s", row.SourceID, url.QueryEscape(row.URL))
		}
		if selectedCat != "" {
			matched := false
			for _, c := range row.Categories {
				if c == selectedCat {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		totalChapters += row.TotalCount
		readChapters += row.ReadCount
		mergedTotal += row.MergedCount
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Title) < strings.ToLower(rows[j].Title) })

	repos := make([]string, 0, len(st.Repos))
	for rURL := range st.Repos {
		repos = append(repos, rURL)
	}
	sort.Strings(repos)

	exts := make([]SyncExtension, 0, len(st.Extensions))
	for _, e := range st.Extensions {
		exts = append(exts, e)
	}
	sort.Slice(exts, func(i, j int) bool { return exts[i].Name < exts[j].Name })

	lastSync := "never"
	if st.UpdatedAt > 0 {
		ts := st.UpdatedAt
		if ts > 1e12 {
			ts /= 1000
		}
		lastSync = humanTime(ts)
	}

	nav := s.buildNav(r, "library", len(entries))
	if len(allRows) > nav.LibraryCount {
		nav.LibraryCount = len(allRows)
	}
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := libraryTmpl.Execute(w, map[string]any{
		"Nonce":             nonce,
		"Nav":               nav,
		"SuwayomiConnected": suwConnected,
		"UpdateStatus":      updateStatus,
		"UpdatingParam":     updatingParam,
		"UpdateProgressPct": updatePct,
		"SelectedCat":       selectedCat,
		"Categories":        cats,
		"Mangas":            rows,
		"TotalMangas":       len(allRows),
		"TotalChapters":     totalChapters,
		"ReadChapters":      readChapters,
		"MergedTotal":       mergedTotal,
		"Repos":             repos,
		"Extensions":        exts,
		"LastSync":          lastSync,
		"Note":              note,
		"Problem":           problem,
	}); err != nil {
		log.Printf("yata: rendering library page failed: %v", err)
	}
}

func (s shelf) handleTranslatePage(w http.ResponseWriter, r *http.Request) {
	s.renderTranslatePage(w, r, "", "", "", "", "")
}

func (s shelf) handleTranslateTestForm(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("action") == "clear-cache" {
		transCacheMu.Lock()
		os.Remove(s.transCachePath())
		transCacheMu.Unlock()
		http.Redirect(w, r, "/translate?note=Translation+cache+cleared.", http.StatusSeeOther)
		return
	}

	sample := strings.TrimSpace(r.PostFormValue("sample"))
	fromLang := strings.TrimSpace(r.PostFormValue("fromLang"))
	toLang := strings.TrimSpace(r.PostFormValue("toLang"))
	if fromLang == "" {
		fromLang = "en"
	}
	if toLang == "" {
		toLang = "pt-BR"
	}
	if sample == "" {
		s.renderTranslatePage(w, r, "", "Enter a line to translate.", sample, fromLang, toLang)
		return
	}
	cur := s.cfg.get()
	if cur.TranslateProvider == "" {
		s.renderTranslatePage(w, r, "", "Configure a translation provider in Settings first.", sample, fromLang, toLang)
		return
	}
	translated, _, err := callTranslateLLM(r.Context(), cur, fromLang, toLang, SeriesNotes{}, []string{sample})
	if err != nil {
		s.renderTranslatePage(w, r, "", "Translation failed: "+err.Error(), sample, fromLang, toLang)
		return
	}
	out := ""
	if len(translated) > 0 {
		out = translated[0]
	}
	s.renderTranslatePage(w, r, out, "", sample, fromLang, toLang)
}

func (s shelf) handlePreTranslateEnqueue(w http.ResponseWriter, r *http.Request) {
	var (
		source    string
		chapterID int64
		title     string
		redirect  string
		wantJSON  bool
	)

	if isForm(r) {
		source = strings.TrimSpace(r.PostFormValue("source"))
		chapterID, _ = strconv.ParseInt(strings.TrimSpace(r.PostFormValue("chapterId")), 10, 64)
		title = strings.TrimSpace(r.PostFormValue("title"))
		redirect = strings.TrimSpace(r.PostFormValue("redirect"))
	} else {
		wantJSON = true
		var body struct {
			Source    string `json:"source"`
			ChapterID int64  `json:"chapterId"`
			Title     string `json:"title"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
			http.Error(w, "invalid json payload", http.StatusBadRequest)
			return
		}
		source = strings.TrimSpace(body.Source)
		chapterID = body.ChapterID
		title = strings.TrimSpace(body.Title)
	}

	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") || strings.ContainsAny(redirect, "\\\r\n") {
		redirect = "/translate"
	}
	sep := "?"
	if strings.Contains(redirect, "?") {
		sep = "&"
	}

	if s.imageTranslator() == nil {
		if wantJSON {
			http.Error(w, "image translator sidecar is not configured", http.StatusServiceUnavailable)
			return
		}
		http.Redirect(w, r, redirect+sep+"problem="+url.QueryEscape("Configure manga-image-translator in Settings first."), http.StatusSeeOther)
		return
	}
	if (source != "suwayomi" && source != "shelf") || chapterID <= 0 {
		if wantJSON {
			http.Error(w, "invalid source or chapterId", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, redirect+sep+"problem="+url.QueryEscape("Invalid chapter for pre-translation."), http.StatusSeeOther)
		return
	}

	q := s.preTranslateQueue()
	queued := q.enqueue(source, chapterID, title)
	if wantJSON {
		writeJSON(w, map[string]any{
			"ok":     true,
			"queued": queued,
			"status": q.status(),
		})
		return
	}
	msg := "Chapter queued for pre-translation."
	if !queued {
		msg = "Chapter is already queued for pre-translation."
	}
	http.Redirect(w, r, redirect+sep+"note="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s shelf) renderTranslatePage(w http.ResponseWriter, r *http.Request, result, problem, sample, fromLang, toLang string) {
	entries, _ := s.list()
	cachedLines, chapterFiles, contextFiles := s.translationStats()
	cur := s.cfg.get()
	if fromLang == "" {
		fromLang = "en"
	}
	if toLang == "" {
		toLang = "pt-BR"
	}
	it := s.imageTranslator()
	hasImgTrans := it != nil
	imgTransLang := ""
	if it != nil {
		imgTransLang = it.lang
	}

	nav := s.buildNav(r, "translate", len(entries))
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := translateTmpl.Execute(w, map[string]any{
		"Nonce":               nonce,
		"Nav":                 nav,
		"Provider":            cur.TranslateProvider,
		"Endpoint":            cur.TranslateEndpoint,
		"Model":               cur.TranslateModel,
		"CachedLines":         cachedLines,
		"ChapterFiles":        chapterFiles,
		"ContextFiles":        contextFiles,
		"HasImageTranslator":  hasImgTrans,
		"ImageTranslatorLang": imgTransLang,
		"Queue":               s.preTranslateQueue().status(),
		"Note":                r.URL.Query().Get("note"),
		"Problem":             problem,
		"Sample":              sample,
		"FromLang":            fromLang,
		"ToLang":              toLang,
		"Result":              result,
	}); err != nil {
		log.Printf("yata: rendering translate page failed: %v", err)
	}
}

func (s shelf) handleBackupsPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	backups, _ := s.listBackups()
	loc := s.cfg.location()
	var totalBytes int64
	rows := make([]backupRow, 0, len(backups))
	for _, b := range backups {
		totalBytes += b.Size
		rows = append(rows, backupRow{
			ID:        b.ID,
			Name:      b.Name,
			Size:      humanSize(b.Size),
			Created:   time.Unix(b.CreatedAt, 0).In(loc).Format("2 Jan 2006, 15:04"),
			CreatedAt: b.CreatedAt,
		})
	}
	nav := s.buildNav(r, "backups", len(entries))
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := backupsTmpl.Execute(w, map[string]any{
		"Nonce":      nonce,
		"Nav":        nav,
		"Backups":    rows,
		"TotalSize":  humanSize(totalBytes),
		"MaxBackups": s.cfg.get().MaxBackups,
		"Note":       r.URL.Query().Get("note"),
		"Problem":    r.URL.Query().Get("problem"),
	}); err != nil {
		log.Printf("yata: rendering backups page failed: %v", err)
	}
}

func (s shelf) handleBackupDeleteForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if p, ok := s.validBackupPath(id); ok {
		os.Remove(p)
	}
	http.Redirect(w, r, "/backups?note=Backup+deleted.", http.StatusSeeOther)
}

func (s shelf) handleBackupUploadForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUpload)
	if err := os.MkdirAll(s.backupsDir(), 0o755); err != nil {
		http.Redirect(w, r, "/backups?problem=Cannot+create+backups+directory.", http.StatusSeeOther)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/backups?problem=Choose+a+.tachibk+file+first.", http.StatusSeeOther)
		return
	}
	defer file.Close()

	id := fmt.Sprintf("%d_%s", now(), sanitizeBackupID(header.Filename))
	dstPath, ok := s.validBackupPath(id)
	if !ok {
		http.Redirect(w, r, "/backups?problem=Invalid+filename.", http.StatusSeeOther)
		return
	}
	tmp, err := os.CreateTemp(s.backupsDir(), "backup-*.tmp")
	if err != nil {
		http.Redirect(w, r, "/backups?problem=Cannot+write+backup.", http.StatusSeeOther)
		return
	}
	tmpPath := tmp.Name()
	n, err := tmp.ReadFrom(file)
	tmp.Close()
	if err != nil || n == 0 {
		os.Remove(tmpPath)
		http.Redirect(w, r, "/backups?problem=Empty+or+failed+upload.", http.StatusSeeOther)
		return
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		http.Redirect(w, r, "/backups?problem=Cannot+finalize+backup.", http.StatusSeeOther)
		return
	}
	s.pruneBackups()
	http.Redirect(w, r, "/backups?note=Backup+uploaded.", http.StatusSeeOther)
}

// askForKey turns a 401 in a browser into something usable. The form POSTs the
// key to /login, which trades it for a session cookie — so the key is typed
// once and never appears in a URL, a history entry or a tunnel's request log.
func askForKey(w http.ResponseWriter, problem string) {
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	if err := gateTmpl.Execute(w, map[string]any{
		"Nonce":   nonce,
		"Problem": problem,
	}); err != nil {
		log.Printf("yata: rendering the key gate failed: %v", err)
	}
}
