package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
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

// ---- helpers ----------------------------------------------------------------

func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	case b > 0:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return "—"
}

func humanTime(unix int64) string {
	if unix <= 0 {
		return ""
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

	// Most recently updated manga first: the same ordering Karasu uses to pick
	// what goes on the shelf, so the page matches the app.
	sort.SliceStable(order, func(i, j int) bool { return newest[order[i]] > newest[order[j]] })
	groups := make([]*mangaGroup, 0, len(order))
	for _, title := range order {
		g := byManga[title]
		sort.SliceStable(g.Chapters, func(i, j int) bool { return g.Chapters[i].num < g.Chapters[j].num })
		g.Size = humanSize(g.bytes)
		groups = append(groups, g)
	}

	// Activity: finished chapters per day over the configured window.
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

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := indexTmpl.Execute(w, map[string]any{
		"Nonce":     nonce,
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
		// Chart geometry lives in one place; the template only draws it.
		"ChartDays": chartDays,
		"ChartW":    chartDays * chartStep,
		"ChartH":    chartH,
		"BarW":      chartBarW,
		"ChartFrom": today.AddDate(0, 0, -chartDays+1).Format("2 Jan"),
	})
	if err != nil {
		// A template error aborts mid-page, so the browser shows half a shelf.
		// Nothing useful can be sent at that point — but it must not be silent.
		log.Printf("yata: rendering the shelf page failed: %v", err)
	}
}

// askForKey turns a 401 in a browser into something usable. The form POSTs the
// key to /login, which trades it for a session cookie — so the key is typed
// once and never appears in a URL, a history entry or a tunnel's request log.
//
// The caller writes the status code: this is a 401 body from the gate and a
// plain 200 after a logout.
func askForKey(w http.ResponseWriter, problem string) {
	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := gateTmpl.Execute(w, map[string]any{"Problem": problem, "Nonce": nonce}); err != nil {
		log.Printf("yata: rendering the key form failed: %v", err)
	}
}
