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

func (s shelf) handleSuwayomiUpdateStatus(w http.ResponseWriter, r *http.Request) {
	suw := s.suwayomi()
	if suw == nil {
		writeJSON(w, SuwayomiUpdateStatus{IsRunning: false})
		return
	}
	st, err := suw.getLibraryUpdateStatus(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if st == nil {
		writeJSON(w, SuwayomiUpdateStatus{IsRunning: false})
		return
	}
	writeJSON(w, st)
}

// ---- Browse & Extensions ----------------------------------------------------

type globalSearchGroup struct {
	Source SuwayomiSource
	Mangas []SuwayomiMangaCard
}

type sourceFilterOptionView struct {
	Index    int
	Label    string
	Selected bool
}

type sourceFilterView struct {
	Position    int
	FieldName   string
	Type        string
	Name        string
	BoolVal     bool
	TextVal     string
	TriVal      string
	SelectVal   int
	SortIdx     int
	SortAsc     bool
	Options     []sourceFilterOptionView
	Children    []sourceFilterView
}

func parseOneFilter(f SuwayomiSourceFilter, prefix string, q url.Values, applied bool, qsOut url.Values) (sourceFilterView, *SuwayomiFilterChange) {
	v := sourceFilterView{
		Position:  f.Position,
		FieldName: prefix,
		Type:      f.Type,
		Name:      f.Name,
	}
	var ch *SuwayomiFilterChange

	switch f.Type {
	case "CheckBoxFilter":
		val := f.CheckBoxDefault
		if applied {
			raw := q.Get(prefix)
			val = raw == "1" || raw == "true" || raw == "on"
			if val {
				qsOut.Set(prefix, "1")
			}
			if val != f.CheckBoxDefault {
				b := val
				ch = &SuwayomiFilterChange{Position: f.Position, CheckBoxState: &b}
			}
		}
		v.BoolVal = val

	case "SelectFilter":
		val := f.SelectDefault
		if raw := q.Get(prefix); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n < len(f.SelectValues) {
				val = n
				qsOut.Set(prefix, strconv.Itoa(n))
				if val != f.SelectDefault {
					idx := val
					ch = &SuwayomiFilterChange{Position: f.Position, SelectState: &idx}
				}
			}
		}
		v.SelectVal = val
		for i, lbl := range f.SelectValues {
			v.Options = append(v.Options, sourceFilterOptionView{Index: i, Label: lbl, Selected: i == val})
		}

	case "TextFilter":
		val := f.TextDefault
		if q.Has(prefix) {
			val = q.Get(prefix)
			if val != "" {
				qsOut.Set(prefix, val)
			}
			if val != f.TextDefault {
				t := val
				ch = &SuwayomiFilterChange{Position: f.Position, TextState: &t}
			}
		}
		v.TextVal = val

	case "TriStateFilter":
		val := f.TriStateDefault
		if val == "" {
			val = "IGNORE"
		}
		if raw := strings.ToUpper(strings.TrimSpace(q.Get(prefix))); raw == "IGNORE" || raw == "INCLUDE" || raw == "EXCLUDE" {
			val = raw
			if val != "IGNORE" {
				qsOut.Set(prefix, val)
			}
			if val != f.TriStateDefault && !(f.TriStateDefault == "" && val == "IGNORE") {
				ts := val
				ch = &SuwayomiFilterChange{Position: f.Position, TriState: &ts}
			}
		}
		v.TriVal = val

	case "SortFilter":
		defIdx := 0
		defAsc := false
		if f.SortDefault != nil {
			defIdx = f.SortDefault.Index
			defAsc = f.SortDefault.Ascending
		}
		idx := defIdx
		asc := defAsc
		idxKey := prefix + "_idx"
		ascKey := prefix + "_asc"
		if raw := q.Get(idxKey); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n < len(f.SortValues) {
				idx = n
				qsOut.Set(idxKey, strconv.Itoa(n))
			}
		}
		if q.Has(ascKey) {
			asc = q.Get(ascKey) == "true" || q.Get(ascKey) == "1"
			if asc {
				qsOut.Set(ascKey, "true")
			} else {
				qsOut.Set(ascKey, "false")
			}
		}
		if idx != defIdx || asc != defAsc {
			ch = &SuwayomiFilterChange{
				Position:  f.Position,
				SortState: &SuwayomiSortSelection{Index: idx, Ascending: asc},
			}
		}
		v.SortIdx = idx
		v.SortAsc = asc
		for i, lbl := range f.SortValues {
			v.Options = append(v.Options, sourceFilterOptionView{Index: i, Label: lbl, Selected: i == idx})
		}
	}
	return v, ch
}

func buildSourceFilters(raw []SuwayomiSourceFilter, q url.Values) ([]sourceFilterView, []SuwayomiFilterChange, string) {
	applied := q.Get("f_applied") == "1"
	qsOut := url.Values{}
	if applied {
		qsOut.Set("f_applied", "1")
	}
	views := make([]sourceFilterView, 0, len(raw))
	var changes []SuwayomiFilterChange

	for _, f := range raw {
		prefix := fmt.Sprintf("f_%d", f.Position)
		if f.Type == "GroupFilter" {
			gv := sourceFilterView{
				Position:  f.Position,
				FieldName: prefix,
				Type:      f.Type,
				Name:      f.Name,
			}
			for _, sub := range f.Filters {
				subPrefix := fmt.Sprintf("f_%d_%d", f.Position, sub.Position)
				sv, sc := parseOneFilter(sub, subPrefix, q, applied, qsOut)
				gv.Children = append(gv.Children, sv)
				if sc != nil {
					changes = append(changes, SuwayomiFilterChange{
						Position:    f.Position,
						GroupChange: sc,
					})
				}
			}
			views = append(views, gv)
			continue
		}
		fv, fc := parseOneFilter(f, prefix, q, applied, qsOut)
		views = append(views, fv)
		if fc != nil {
			changes = append(changes, *fc)
		}
	}

	filterQS := ""
	if len(changes) > 0 {
		filterQS = "&" + qsOut.Encode()
	}
	return views, changes, filterQS
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
		sources           []SuwayomiSource
		selectedSource    *SuwayomiSource
		sourceFilters     []sourceFilterView
		activeFilterCount int
		filterQS          string
		browseResult      *SuwayomiBrowseResult
		globalMode        bool
		globalResults     []globalSearchGroup
		extRepos          []string
		extLangs          []string
		updateExts        []SuwayomiExtension
		installedExts     []SuwayomiExtension
		availableExts     []SuwayomiExtension
		problem           = r.URL.Query().Get("problem")
		note              = r.URL.Query().Get("note")
		suwConfigured     bool
	)

	if suw := s.suwayomi(); suw != nil {
		suwConfigured = true
		if tab == "extensions" {
			if repos, err := suw.getExtensionRepos(r.Context()); err == nil {
				extRepos = repos
			}
			exts, err := suw.listExtensions(r.Context())
			if err == nil && len(extRepos) > 0 && len(exts) == 0 {
				if suw.migrateLegacyExtensionRepos(r.Context()) {
					if repos, rerr := suw.getExtensionRepos(r.Context()); rerr == nil {
						extRepos = repos
					}
				}
				_ = suw.fetchExtensions(r.Context())
				if refreshed, lerr := suw.listExtensions(r.Context()); lerr == nil {
					exts = refreshed
				}
			}
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
						if e.HasUpdate {
							updateExts = append(updateExts, e)
						} else {
							installedExts = append(installedExts, e)
						}
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
					var filterChanges []SuwayomiFilterChange
					if rawFilters, ferr := suw.getSourceFilters(r.Context(), selectedSourceID); ferr == nil && len(rawFilters) > 0 {
						sourceFilters, filterChanges, filterQS = buildSourceFilters(rawFilters, r.URL.Query())
						activeFilterCount = len(filterChanges)
					}
					if len(filterChanges) > 0 {
						browseType = "SEARCH"
					}
					if browseType == "LATEST" && !selectedSource.SupportsLatest {
						browseType = "POPULAR"
					}
					if browseType != "SEARCH" || searchQ != "" || len(filterChanges) > 0 {
						res, err := suw.browseSource(r.Context(), selectedSourceID, browseType, searchQ, page, filterChanges...)
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
		"Nonce":             nonce,
		"Nav":               nav,
		"Configured":        suwConfigured,
		"Tab":               tab,
		"Sources":           sources,
		"SelectedSource":    selectedSource,
		"SourceID":          selectedSourceID,
		"SourceFilters":     sourceFilters,
		"ActiveFilterCount": activeFilterCount,
		"FilterQS":          filterQS,
		"GlobalMode":        globalMode,
		"GlobalResults":     globalResults,
		"BrowseType":        browseType,
		"Query":             searchQ,
		"ExtQuery":          extQ,
		"ExtLang":           extLang,
		"ExtLangs":          extLangs,
		"ExtRepos":          extRepos,
		"Page":              page,
		"PrevPage":          page - 1,
		"NextPage":          page + 1,
		"BrowseResult":      browseResult,
		"UpdateExts":        updateExts,
		"InstalledExts":     installedExts,
		"AvailableExts":     availableExts,
		"Note":              note,
		"Problem":           problem,
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
		suw.migrateLegacyExtensionRepos(r.Context())
		err = suw.fetchExtensions(r.Context())
		msg = "Extension list refreshed."
	case "install":
		err = suw.updateExtension(r.Context(), pkgName, true, false, false)
		msg = "Extension installed."
	case "update":
		err = suw.updateExtension(r.Context(), pkgName, false, true, false)
		msg = "Extension updated."
	case "update-all":
		exts, lerr := suw.listExtensions(r.Context())
		if lerr != nil {
			err = lerr
			break
		}
		updated := 0
		for _, e := range exts {
			if e.IsInstalled && e.HasUpdate {
				if uerr := suw.updateExtension(r.Context(), e.PkgName, false, true, false); uerr != nil {
					err = uerr
					break
				}
				updated++
			}
		}
		if err == nil {
			if updated == 0 {
				msg = "No extensions needed updating."
			} else if updated == 1 {
				msg = "Updated 1 extension."
			} else {
				msg = fmt.Sprintf("Updated %d extensions.", updated)
			}
		}
	case "uninstall":
		err = suw.updateExtension(r.Context(), pkgName, false, false, true)
		msg = "Extension uninstalled."
	case "add-repo":
		repoURL := strings.TrimSpace(r.PostFormValue("repoUrl"))
		if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") {
			http.Redirect(w, r, "/browse?tab=extensions&problem=Repository+URL+must+start+with+http%3A%2F%2F+or+https%3A%2F%2F.", http.StatusSeeOther)
			return
		}
		normURL := normalizeSuwayomiRepoURL(repoURL)
		if normURL == "" {
			http.Redirect(w, r, "/browse?tab=extensions&problem=Invalid+repository+URL.", http.StatusSeeOther)
			return
		}
		err = suw.addExtensionRepo(r.Context(), normURL)
		if err == nil {
			_ = suw.fetchExtensions(r.Context())
		}
		msg = "Extension repository added."
	case "remove-repo":
		repoURL := strings.TrimSpace(r.PostFormValue("repoUrl"))
		err = suw.removeExtensionRepo(r.Context(), repoURL)
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

func (s shelf) handleExtensionInstallAPK(w http.ResponseWriter, r *http.Request) {
	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, "/browse?tab=extensions&problem=Suwayomi+not+configured.", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Redirect(w, r, "/browse?tab=extensions&problem=Expected+multipart+file+upload.", http.StatusSeeOther)
		return
	}
	var installed bool
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Redirect(w, r, "/browse?tab=extensions&problem="+url.QueryEscape("Upload failed: "+err.Error()), http.StatusSeeOther)
			return
		}
		if p.FormName() == "file" {
			fname := strings.TrimSpace(p.FileName())
			if fname == "" {
				p.Close()
				http.Redirect(w, r, "/browse?tab=extensions&problem=Please+choose+an+.apk+file.", http.StatusSeeOther)
				return
			}
			if !strings.HasSuffix(strings.ToLower(fname), ".apk") {
				p.Close()
				http.Redirect(w, r, "/browse?tab=extensions&problem=Only+.apk+extension+files+are+supported.", http.StatusSeeOther)
				return
			}
			ierr := suw.installExternalExtension(r.Context(), fname, p)
			p.Close()
			if ierr != nil {
				http.Redirect(w, r, "/browse?tab=extensions&problem="+url.QueryEscape(ierr.Error()), http.StatusSeeOther)
				return
			}
			installed = true
			break
		}
		p.Close()
	}
	if !installed {
		http.Redirect(w, r, "/browse?tab=extensions&problem=Missing+file+upload.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/browse?tab=extensions&note="+url.QueryEscape("External APK installed."), http.StatusSeeOther)
}

// ---- Source Preferences -----------------------------------------------------

type sourcePrefOptionView struct {
	Label    string
	Value    string
	Selected bool
}

type sourcePrefView struct {
	Position    int
	Type        string
	Title       string
	Summary     string
	CurrentBool bool
	CurrentStr  string
	Options     []sourcePrefOptionView
}

func (s shelf) handleSourceSettingsPage(w http.ResponseWriter, r *http.Request) {
	sourceID := strings.TrimSpace(r.PathValue("id"))
	if sourceID == "" {
		http.Error(w, "invalid source id", http.StatusBadRequest)
		return
	}
	entries, _ := s.list()
	nav := s.buildNav(r, "browse", len(entries))

	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, "/browse?problem=Configure+Suwayomi+in+Settings+first.", http.StatusSeeOther)
		return
	}

	prefs, err := suw.getSourcePreferences(r.Context(), sourceID)
	if err != nil {
		http.Redirect(w, r, "/browse?tab=sources&source="+url.QueryEscape(sourceID)+"&problem="+url.QueryEscape("Could not load source settings: "+err.Error()), http.StatusSeeOther)
		return
	}

	views := make([]sourcePrefView, 0, len(prefs.Preferences))
	for _, p := range prefs.Preferences {
		pv := sourcePrefView{
			Position:    p.Position,
			Type:        p.Type,
			Title:       p.Title,
			Summary:     p.Summary,
			CurrentBool: p.CurrentBool,
			CurrentStr:  p.CurrentStr,
		}
		if p.Type == "ListPreference" || p.Type == "MultiSelectListPreference" {
			selectedSet := map[string]bool{}
			if p.Type == "ListPreference" {
				selectedSet[p.CurrentStr] = true
			} else {
				for _, v := range p.CurrentMulti {
					selectedSet[v] = true
				}
			}
			for i, val := range p.EntryValues {
				label := val
				if i < len(p.Entries) && p.Entries[i] != "" {
					label = p.Entries[i]
				}
				pv.Options = append(pv.Options, sourcePrefOptionView{
					Label:    label,
					Value:    val,
					Selected: selectedSet[val],
				})
			}
		}
		views = append(views, pv)
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := sourceSettingsTmpl.Execute(w, map[string]any{
		"Nonce":       nonce,
		"Nav":         nav,
		"SourceID":    prefs.SourceID,
		"DisplayName": prefs.DisplayName,
		"Preferences": views,
		"Note":        r.URL.Query().Get("note"),
		"Problem":     r.URL.Query().Get("problem"),
	}); err != nil {
		log.Printf("yata: rendering source settings page failed: %v", err)
	}
}

func (s shelf) handleSourceSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	sourceID := strings.TrimSpace(r.PathValue("id"))
	if sourceID == "" {
		http.Error(w, "invalid source id", http.StatusBadRequest)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/source/%s/settings?problem=Invalid+form.", url.PathEscape(sourceID)), http.StatusSeeOther)
		return
	}
	pos, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("position")))
	if err != nil || pos < 0 {
		http.Redirect(w, r, fmt.Sprintf("/source/%s/settings?problem=Invalid+preference+position.", url.PathEscape(sourceID)), http.StatusSeeOther)
		return
	}
	prefType := strings.TrimSpace(r.PostFormValue("type"))
	var change map[string]any
	switch prefType {
	case "CheckBoxPreference":
		change = map[string]any{"checkBoxState": r.PostFormValue("value") == "true"}
	case "SwitchPreference":
		change = map[string]any{"switchState": r.PostFormValue("value") == "true"}
	case "ListPreference":
		change = map[string]any{"listState": r.PostFormValue("value")}
	case "MultiSelectListPreference":
		vals := r.PostForm["values"]
		if vals == nil {
			vals = []string{}
		}
		change = map[string]any{"multiSelectState": vals}
	case "EditTextPreference":
		change = map[string]any{"editTextState": r.PostFormValue("value")}
	default:
		http.Redirect(w, r, fmt.Sprintf("/source/%s/settings?problem=Unsupported+preference+type.", url.PathEscape(sourceID)), http.StatusSeeOther)
		return
	}
	if err := suw.updateSourcePreference(r.Context(), sourceID, pos, change); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/source/%s/settings?problem=%s", url.PathEscape(sourceID), url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/source/%s/settings?note=%s", url.PathEscape(sourceID), url.QueryEscape("Preference saved.")), http.StatusSeeOther)
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

type mangaCategoryChoice struct {
	ID      int
	Name    string
	Checked bool
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

	var catChoices []mangaCategoryChoice
	if allCats, err := suw.listCategories(r.Context()); err == nil {
		assignedByID := make(map[int]bool, len(detail.CategoryIDs))
		for _, id := range detail.CategoryIDs {
			assignedByID[id] = true
		}
		assignedByName := make(map[string]bool, len(detail.Categories))
		for _, name := range detail.Categories {
			assignedByName[name] = true
		}
		for _, c := range allCats {
			if c.ID == 0 || strings.EqualFold(c.Name, "Default") {
				continue
			}
			catChoices = append(catChoices, mangaCategoryChoice{
				ID:      c.ID,
				Name:    c.Name,
				Checked: assignedByID[c.ID] || assignedByName[c.Name],
			})
		}
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

	var (
		loggedInTrackers []SuwayomiTracker
		trackResults     []SuwayomiTrackSearchResult
		trackSearchQ     = strings.TrimSpace(r.URL.Query().Get("trackSearch"))
		trackSearchID, _ = strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("trackId")))
		problem          = r.URL.Query().Get("problem")
	)
	if allTrackers, err := suw.listTrackers(r.Context()); err == nil {
		nameByID := make(map[int]string, len(allTrackers))
		for _, t := range allTrackers {
			nameByID[t.ID] = t.Name
			if t.IsLoggedIn {
				loggedInTrackers = append(loggedInTrackers, t)
			}
		}
		for i := range detail.TrackRecords {
			if name := nameByID[detail.TrackRecords[i].TrackerID]; name != "" {
				detail.TrackRecords[i].TrackerName = name
			} else {
				detail.TrackRecords[i].TrackerName = fmt.Sprintf("Tracker #%d", detail.TrackRecords[i].TrackerID)
			}
		}
	}
	if trackSearchID == 0 && len(loggedInTrackers) > 0 {
		trackSearchID = loggedInTrackers[0].ID
	}
	if trackSearchQ != "" && trackSearchID > 0 {
		res, serr := suw.searchTracker(r.Context(), trackSearchID, trackSearchQ)
		if serr != nil {
			if problem == "" {
				problem = "Tracker search failed: " + serr.Error()
			}
		} else {
			trackResults = res
		}
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := mangaTmpl.Execute(w, map[string]any{
		"Nonce":                nonce,
		"Nav":                  nav,
		"Manga":                detail,
		"Chapters":             chapters,
		"CategoryChoices":      catChoices,
		"TrackRecords":         detail.TrackRecords,
		"LoggedInTrackers":     loggedInTrackers,
		"TrackSearchResults":   trackResults,
		"TrackSearchQuery":     trackSearchQ,
		"TrackSearchTrackerID": trackSearchID,
		"HasImageTranslator":   s.imageTranslator() != nil,
		"ReadCount":            readCount,
		"DlCount":              dlCount,
		"TotalCount":           len(chapters),
		"Note":                 r.URL.Query().Get("note"),
		"Problem":              problem,
	}); err != nil {
		log.Printf("yata: rendering manga page failed: %v", err)
	}
}

func (s shelf) handleMangaTrackAction(w http.ResponseWriter, r *http.Request) {
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
	action := strings.TrimSpace(r.PostFormValue("action"))
	switch action {
	case "search":
		trackerID, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("trackerId")))
		query := strings.TrimSpace(r.PostFormValue("query"))
		if trackerID <= 0 || query == "" {
			http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=Select+a+tracker+and+enter+a+search+query.#trackers", mangaID), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?trackId=%d&trackSearch=%s#trackers", mangaID, trackerID, url.QueryEscape(query)), http.StatusSeeOther)
		return

	case "bind":
		trackerID, err1 := strconv.Atoi(strings.TrimSpace(r.PostFormValue("trackerId")))
		remoteID, err2 := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("remoteId")), 10, 64)
		if err1 != nil || err2 != nil || trackerID <= 0 || remoteID <= 0 {
			http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=Invalid+tracker+binding+parameters.#trackers", mangaID), http.StatusSeeOther)
			return
		}
		if err := suw.bindTrack(r.Context(), mangaID, trackerID, remoteID); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s#trackers", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=Tracker+bound.#trackers", mangaID), http.StatusSeeOther)
		return

	case "unbind":
		recordID, perr := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("recordId")), 10, 64)
		if perr != nil || recordID <= 0 {
			http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=Invalid+track+record+ID.#trackers", mangaID), http.StatusSeeOther)
			return
		}
		if err := suw.unbindTrack(r.Context(), recordID); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s#trackers", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=Tracker+unbound.#trackers", mangaID), http.StatusSeeOther)
		return

	default:
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=Unknown+tracker+action.#trackers", mangaID), http.StatusSeeOther)
	}
}

func (s shelf) handleMangaCategoriesUpdate(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=Invalid+form.", mangaID), http.StatusSeeOther)
		return
	}
	wantSet := map[int]bool{}
	for _, raw := range r.PostForm["categoryIds"] {
		if id, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && id > 0 {
			wantSet[id] = true
		}
	}

	detail, err := suw.getManga(r.Context(), mangaID)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	curSet := map[int]bool{}
	for _, id := range detail.CategoryIDs {
		curSet[id] = true
	}

	var addTo, removeFrom []int
	for id := range wantSet {
		if !curSet[id] {
			addTo = append(addTo, id)
		}
	}
	for id := range curSet {
		if !wantSet[id] {
			removeFrom = append(removeFrom, id)
		}
	}
	sort.Ints(addTo)
	sort.Ints(removeFrom)

	if err := suw.updateMangaCategories(r.Context(), mangaID, addTo, removeFrom); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=Categories+updated.", mangaID), http.StatusSeeOther)
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
	redir := fmt.Sprintf("/manga/%d", mangaID)
	if target := strings.TrimSpace(r.PostFormValue("redirect")); target == "/updates" {
		redir = target
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
		http.Redirect(w, r, redir+"?problem=Unknown+action.", http.StatusSeeOther)
		return
	}
	if actErr != nil {
		http.Redirect(w, r, redir+"?problem="+url.QueryEscape(actErr.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redir+"?note="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ---- Categories CRUD --------------------------------------------------------

func (s shelf) handleCategoriesPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	nav := s.buildNav(r, "library", len(entries))

	var (
		configured bool
		categories []SuwayomiCategory
		problem    = r.URL.Query().Get("problem")
		note       = r.URL.Query().Get("note")
	)
	if suw := s.suwayomi(); suw != nil {
		configured = true
		cats, err := suw.listCategories(r.Context())
		if err != nil {
			problem = "Could not load categories: " + err.Error()
		} else {
			for _, c := range cats {
				if c.ID == 0 || strings.EqualFold(c.Name, "Default") {
					continue
				}
				categories = append(categories, c)
			}
		}
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := categoriesTmpl.Execute(w, map[string]any{
		"Nonce":      nonce,
		"Nav":        nav,
		"Configured": configured,
		"Categories": categories,
		"Note":       note,
		"Problem":    problem,
	}); err != nil {
		log.Printf("yata: rendering categories page failed: %v", err)
	}
}

func (s shelf) handleCategoriesAction(w http.ResponseWriter, r *http.Request) {
	suw := s.suwayomi()
	if suw == nil {
		http.Redirect(w, r, "/categories?problem=Suwayomi+not+configured.", http.StatusSeeOther)
		return
	}
	action := strings.TrimSpace(r.PostFormValue("action"))
	var err error
	var msg string
	switch action {
	case "create":
		name := strings.TrimSpace(r.PostFormValue("name"))
		if name == "" {
			http.Redirect(w, r, "/categories?problem=Category+name+is+required.", http.StatusSeeOther)
			return
		}
		_, err = suw.createCategory(r.Context(), name)
		msg = "Category created."
	case "rename":
		id, perr := strconv.Atoi(strings.TrimSpace(r.PostFormValue("id")))
		name := strings.TrimSpace(r.PostFormValue("name"))
		if perr != nil || id <= 0 || name == "" {
			http.Redirect(w, r, "/categories?problem=Valid+category+ID+and+name+are+required.", http.StatusSeeOther)
			return
		}
		err = suw.updateCategory(r.Context(), id, name)
		msg = "Category renamed."
	case "delete":
		id, perr := strconv.Atoi(strings.TrimSpace(r.PostFormValue("id")))
		if perr != nil || id <= 0 {
			http.Redirect(w, r, "/categories?problem=Invalid+category+ID.", http.StatusSeeOther)
			return
		}
		err = suw.deleteCategory(r.Context(), id)
		msg = "Category deleted."
	default:
		http.Redirect(w, r, "/categories?problem=Unknown+action.", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/categories?problem="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/categories?note="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ---- Updates Feed -----------------------------------------------------------

type updateItemView struct {
	ID            int64
	Name          string
	ChapterNumber string
	Scanlator     string
	When          string
	IsRead        bool
	IsDownloaded  bool
	MangaID       int64
	MangaTitle    string
	ThumbnailURL  string
}

type updateDayGroup struct {
	Label string
	Items []updateItemView
}

func (s shelf) handleUpdatesPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	nav := s.buildNav(r, "updates", len(entries))
	loc := s.cfg.location()
	today := dayStart(time.Unix(now(), 0).In(loc))

	var (
		configured    bool
		groups        []updateDayGroup
		total         int
		updateStatus  = &SuwayomiUpdateStatus{}
		updatingParam = r.URL.Query().Get("updating") == "1"
		problem       = r.URL.Query().Get("problem")
		note          = r.URL.Query().Get("note")
	)

	if suw := s.suwayomi(); suw != nil {
		configured = true
		if ust, uerr := suw.getLibraryUpdateStatus(r.Context()); uerr == nil && ust != nil {
			updateStatus = ust
		}
		updates, err := suw.listRecentUpdates(r.Context(), 100)
		if err != nil {
			problem = "Could not load recent updates: " + err.Error()
		} else {
			total = len(updates)
			idxByLabel := map[string]int{}
			for _, u := range updates {
				tsUnix := u.FetchedAt
				if tsUnix == 0 {
					tsUnix = u.UploadDate
				}
				label := "Earlier"
				if tsUnix > 0 {
					t := time.Unix(tsUnix, 0).In(loc)
					d := daysBetween(dayStart(t), today)
					switch {
					case d == 0:
						label = "Today"
					case d == 1:
						label = "Yesterday"
					default:
						label = t.Format("Mon, 2 Jan 2006")
					}
				}
				idx, ok := idxByLabel[label]
				if !ok {
					idx = len(groups)
					idxByLabel[label] = idx
					groups = append(groups, updateDayGroup{Label: label})
				}
				groups[idx].Items = append(groups[idx].Items, updateItemView{
					ID:            u.ID,
					Name:          u.Name,
					ChapterNumber: chapterNumber(u.ChapterNumber),
					Scanlator:     u.Scanlator,
					When:          humanTime(tsUnix),
					IsRead:        u.IsRead,
					IsDownloaded:  u.IsDownloaded,
					MangaID:       u.MangaID,
					MangaTitle:    u.MangaTitle,
					ThumbnailURL:  u.ThumbnailURL,
				})
			}
		}
	}

	updatePct := 0
	if updateStatus.TotalJobs > 0 {
		updatePct = updateStatus.FinishedJobs * 100 / updateStatus.TotalJobs
		if updatePct > 100 {
			updatePct = 100
		}
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := updatesTmpl.Execute(w, map[string]any{
		"Nonce":             nonce,
		"Nav":               nav,
		"Configured":        configured,
		"UpdateStatus":      updateStatus,
		"UpdatingParam":     updatingParam,
		"UpdateProgressPct": updatePct,
		"Groups":            groups,
		"Total":             total,
		"Note":              note,
		"Problem":           problem,
	}); err != nil {
		log.Printf("yata: rendering updates page failed: %v", err)
	}
}

// ---- Global Reading History (B2) --------------------------------------------

type historyItemView struct {
	Timestamp     int64
	Source        string // "suwayomi" | "shelf"
	MangaID       int64
	MangaTitle    string
	ThumbnailURL  string
	ChapterID     int64
	ChapterName   string
	ChapterNumber string
	ReadURL       string
	Progress      string
	IsRead        bool
	When          string
}

type historyDayGroup struct {
	Label string
	Items []historyItemView
}

func (s shelf) handleHistoryPage(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.list()
	nav := s.buildNav(r, "history", len(entries))
	loc := s.cfg.location()
	today := dayStart(time.Unix(now(), 0).In(loc))

	var (
		items   []historyItemView
		problem = r.URL.Query().Get("problem")
		note    = r.URL.Query().Get("note")
	)

	if suw := s.suwayomi(); suw != nil {
		suwHist, err := suw.listReadHistory(r.Context(), 100)
		if err != nil {
			problem = "Could not load Suwayomi reading history: " + err.Error()
		} else {
			for _, h := range suwHist {
				prog := "In progress"
				if h.IsRead {
					prog = "Finished"
				} else if h.LastPageRead > 0 && h.PageCount > 0 {
					prog = fmt.Sprintf("Page %d / %d", h.LastPageRead+1, h.PageCount)
				} else if h.LastPageRead > 0 {
					prog = fmt.Sprintf("Page %d", h.LastPageRead+1)
				}
				items = append(items, historyItemView{
					Timestamp:     h.LastReadAt,
					Source:        "suwayomi",
					MangaID:       h.MangaID,
					MangaTitle:    h.MangaTitle,
					ThumbnailURL:  h.ThumbnailURL,
					ChapterID:     h.ID,
					ChapterName:   h.Name,
					ChapterNumber: chapterNumber(h.ChapterNumber),
					ReadURL:       fmt.Sprintf("/read/%d", h.ID),
					Progress:      prog,
					IsRead:        h.IsRead,
					When:          humanTime(h.LastReadAt),
				})
			}
		}
	}

	for _, ev := range s.history() {
		ts := ev.At
		if ts > 1e12 {
			ts /= 1000
		}
		items = append(items, historyItemView{
			Timestamp:   ts,
			Source:      "shelf",
			MangaTitle:  ev.MangaTitle,
			ChapterID:   ev.ChapterID,
			ChapterName: ev.ChapterName,
			ReadURL:     fmt.Sprintf("/read/shelf/%d", ev.ChapterID),
			Progress:    "KOReader",
			IsRead:      true,
			When:        humanTime(ts),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Timestamp > items[j].Timestamp
	})

	var groups []historyDayGroup
	idxByLabel := map[string]int{}
	for _, it := range items {
		label := "Earlier"
		if it.Timestamp > 0 {
			t := time.Unix(it.Timestamp, 0).In(loc)
			d := daysBetween(dayStart(t), today)
			switch {
			case d == 0:
				label = "Today"
			case d == 1:
				label = "Yesterday"
			default:
				label = t.Format("Mon, 2 Jan 2006")
			}
		}
		idx, ok := idxByLabel[label]
		if !ok {
			idx = len(groups)
			idxByLabel[label] = idx
			groups = append(groups, historyDayGroup{Label: label})
		}
		groups[idx].Items = append(groups[idx].Items, it)
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := historyTmpl.Execute(w, map[string]any{
		"Nonce":   nonce,
		"Nav":     nav,
		"Groups":  groups,
		"Total":   len(items),
		"Note":    note,
		"Problem": problem,
	}); err != nil {
		log.Printf("yata: rendering history page failed: %v", err)
	}
}

// ---- Source Migration -------------------------------------------------------

func (s shelf) handleMangaMigratePage(w http.ResponseWriter, r *http.Request) {
	mangaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || mangaID <= 0 {
		http.Error(w, "invalid manga id", http.StatusBadRequest)
		return
	}
	entries, _ := s.list()
	nav := s.buildNav(r, "library", len(entries))

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

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = detail.Title
	}
	problem := r.URL.Query().Get("problem")

	var groups []globalSearchGroup
	sources, err := suw.listSources(r.Context())
	if err != nil {
		problem = "Could not load sources: " + err.Error()
	} else if query != "" {
		rawGroups := runGlobalSearch(r.Context(), suw, sources, query)
		for _, g := range rawGroups {
			var filtered []SuwayomiMangaCard
			for _, m := range g.Mangas {
				if m.ID != mangaID {
					filtered = append(filtered, m)
				}
			}
			if len(filtered) > 0 {
				groups = append(groups, globalSearchGroup{Source: g.Source, Mangas: filtered})
			}
		}
	}

	nonce := newNonce()
	contentSecurityPolicy(w, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := migrateTmpl.Execute(w, map[string]any{
		"Nonce":   nonce,
		"Nav":     nav,
		"Manga":   detail,
		"Query":   query,
		"Groups":  groups,
		"Problem": problem,
		"Note":    r.URL.Query().Get("note"),
	}); err != nil {
		log.Printf("yata: rendering migrate page failed: %v", err)
	}
}

func (s shelf) handleMangaMigratePost(w http.ResponseWriter, r *http.Request) {
	mangaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || mangaID <= 0 {
		http.Error(w, "invalid manga id", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("targetMangaId")), 10, 64)
	if err != nil || targetID <= 0 || targetID == mangaID {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d/migrate?problem=Invalid+target+manga.", mangaID), http.StatusSeeOther)
		return
	}
	suw := s.suwayomi()
	if suw == nil {
		http.Error(w, "suwayomi not configured", http.StatusBadRequest)
		return
	}

	oldManga, err := suw.getManga(r.Context(), mangaID)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d/migrate?problem=%s", mangaID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}

	if err := suw.toggleLibrary(r.Context(), targetID, true); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d/migrate?problem=%s", mangaID, url.QueryEscape("Could not add target manga to library: "+err.Error())), http.StatusSeeOther)
		return
	}
	if len(oldManga.CategoryIDs) > 0 {
		_ = suw.updateMangaCategories(r.Context(), targetID, oldManga.CategoryIDs, nil)
	}
	_ = suw.fetchChapters(r.Context(), targetID)

	readNums := map[float64]bool{}
	for _, ch := range oldManga.Chapters {
		if ch.IsRead && ch.ChapterNumber > 0 {
			readNums[ch.ChapterNumber] = true
		}
	}

	copied := 0
	if newManga, err := suw.getManga(r.Context(), targetID); err == nil {
		for _, ch := range newManga.Chapters {
			if !ch.IsRead && ch.ChapterNumber > 0 && readNums[ch.ChapterNumber] {
				if err := suw.markChapterRead(r.Context(), ch.ID, true, -1); err == nil {
					copied++
				}
			}
		}
	}

	if err := suw.toggleLibrary(r.Context(), mangaID, false); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/manga/%d?problem=%s", targetID, url.QueryEscape("Migrated target, but failed to remove old manga from library: "+err.Error())), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Migrated from %s (%d read chapters copied).", oldManga.Title, copied)
	http.Redirect(w, r, fmt.Sprintf("/manga/%d?note=%s", targetID, url.QueryEscape(msg)), http.StatusSeeOther)
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
	if target := strings.TrimSpace(r.PostFormValue("redirect")); target == "/library" || target == "/downloads" || target == "/updates" {
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
	case "stop-update":
		err = suw.stopLibraryUpdate(r.Context())
		msg = "Library update stopped."
	case "update-library":
		syncMu.Lock()
		st := s.loadSyncState()
		syncMu.Unlock()
		var syncCats []SyncCategory
		for _, c := range st.Categories {
			syncCats = append(syncCats, c)
		}
		var syncFavs []SyncManga
		for _, m := range st.Mangas {
			if m.Favorite && m.SourceID != 0 && m.URL != "" {
				syncFavs = append(syncFavs, m)
			}
		}
		if len(syncFavs) > 0 {
			var syncRepos []string
			for rURL := range st.Repos {
				syncRepos = append(syncRepos, rURL)
			}
			if len(syncCats) > 0 || len(syncRepos) > 0 {
				suw.pushCategoriesAndRepos(r.Context(), syncCats, syncRepos)
			}
			mctx, mcancel := context.WithTimeout(r.Context(), 15*time.Second)
			_, _ = suw.materializeMissingMangas(mctx, syncCats, syncFavs)
			mcancel()
		}

		if lib, lerr := suw.listLibrary(r.Context()); lerr == nil && lib != nil && len(lib.Mangas) == 0 {
			http.Redirect(w, r, redir+"?problem="+url.QueryEscape("Your library is empty. Sync from Karasu, add manga in Browse, or restore a backup in Backups."), http.StatusSeeOther)
			return
		}

		status, uerr := suw.startLibraryUpdate(r.Context())
		if uerr != nil {
			err = uerr
			break
		}
		if status != nil && (status.TotalJobs > 0 || status.IsRunning) {
			noteMsg := "Library update started."
			if status.TotalJobs > 0 {
				noteMsg = fmt.Sprintf("Library update started (%d manga queued).", status.TotalJobs)
			}
			http.Redirect(w, r, redir+"?note="+url.QueryEscape(noteMsg)+"&updating=1", http.StatusSeeOther)
			return
		}
		if status != nil && status.SkippedMangasCount > 0 {
			noteMsg := fmt.Sprintf("Library up to date (%d manga skipped by filters).", status.SkippedMangasCount)
			http.Redirect(w, r, redir+"?note="+url.QueryEscape(noteMsg), http.StatusSeeOther)
			return
		}
		msg = "Library up to date."
	case "cleanup-read":
		var deleted int
		deleted, err = s.cleanupReadDownloads(r.Context())
		if err == nil {
			if deleted == 0 {
				msg = "No read downloads to clean."
			} else if deleted == 1 {
				msg = "Cleaned 1 read downloaded chapter."
			} else {
				msg = fmt.Sprintf("Cleaned %d read downloaded chapters.", deleted)
			}
		}
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
		BackURL:      "/shelf",
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

