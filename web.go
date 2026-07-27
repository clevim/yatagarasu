package main

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
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

	keyQ := ""
	if k := r.URL.Query().Get("key"); k != "" {
		keyQ = "?key=" + url.QueryEscape(k)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := indexTmpl.Execute(w, map[string]any{
		"KeyQuery":  template.URL(keyQ),
		"Key":       r.URL.Query().Get("key"),
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

// askForKey turns a 401 in a browser into something usable: submitting the
// form is a plain GET with ?key=, which is what the middleware also accepts.
func askForKey(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	io.WriteString(w, `<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yatagarasu</title>`+baseCSS+`
<style>
  .gate{max-width:22rem;margin:18vh auto;padding:0 1.5rem}
  .gate h1{margin-bottom:.25rem}
  .gate p{color:var(--muted);margin-bottom:1.5rem}
</style>
<body><main class="gate">
  <h1>Yatagarasu</h1>
  <p>This shelf is protected. Enter its API key to continue.</p>
  <form method="get" action="/">
    <label class="field"><span>API key</span>
      <input name="key" type="password" autofocus autocomplete="current-password">
    </label>
    <button class="btn primary" type="submit">Open shelf</button>
  </form>
</main>
`)
}
