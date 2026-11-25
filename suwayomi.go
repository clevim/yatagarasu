package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// suwayomiClient talks to an official Suwayomi-Server instance over its native
// GraphQL (/api/graphql) and REST (/api/v1/chapter/{id}/download) endpoints.
// No fork or custom endpoints on Suwayomi are required.
type suwayomiClient struct {
	baseURL string
	user    string
	pass    string
	http    *http.Client
}

func (s shelf) suwayomi() *suwayomiClient {
	cur := s.cfg.get()
	if cur.SuwayomiURL == "" {
		return nil
	}
	return &suwayomiClient{
		baseURL: trimSlash(cur.SuwayomiURL),
		user:    cur.SuwayomiUser,
		pass:    cur.SuwayomiPass,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type suwayomiHealth struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	Version    string `json:"version,omitempty"`
}

func (c *suwayomiClient) addAuth(req *http.Request) {
	if c.user != "" || c.pass != "" {
		token := base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.pass))
		req.Header.Set("Authorization", "Basic "+token)
	}
}

func (c *suwayomiClient) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/graphql", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("suwayomi graphql http %d", resp.StatusCode)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return errors.New(cleanGraphQLError(envelope.Errors[0].Message))
	}
	if out != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func cleanGraphQLError(msg string) string {
	for _, line := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 240 {
				return line[:240]
			}
			return line
		}
	}
	return strings.TrimSpace(msg)
}

// graphqlUpload implements Suwayomi's JavalinGraphQLRequestParser multipart upload
// protocol ("operations", "map", and file part "0").
func (c *suwayomiClient) graphqlUpload(ctx context.Context, query string, variables map[string]any, varName, filename string, fileContent io.Reader, out any) error {
	vars := make(map[string]any, len(variables)+1)
	for k, v := range variables {
		vars[k] = v
	}
	vars[varName] = nil

	operationsBytes, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": vars,
	})
	if err != nil {
		return err
	}
	mapBytes, err := json.Marshal(map[string]any{
		"0": []string{"variables." + varName},
	})
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		if err := mw.WriteField("operations", string(operationsBytes)); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := mw.WriteField("map", string(mapBytes)); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		part, err := mw.CreateFormFile("0", filename)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, fileContent); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/graphql", pr)
	if err != nil {
		_ = pr.Close()
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	c.addAuth(req)

	uploadHTTP := &http.Client{Timeout: 2 * time.Minute}
	resp, err := uploadHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("suwayomi graphql upload http %d", resp.StatusCode)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return errors.New(envelope.Errors[0].Message)
	}
	if out != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func isUnsupportedGraphQLField(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fieldundefined") ||
		strings.Contains(msg, "validation error") ||
		strings.Contains(msg, "unknown field") ||
		strings.Contains(msg, "cannot query field") ||
		strings.Contains(msg, "undefined")
}

func (c *suwayomiClient) checkHealth(ctx context.Context) suwayomiHealth {
	h := suwayomiHealth{Configured: true}
	var data struct {
		AboutServer struct {
			Version string `json:"version"`
		} `json:"aboutServer"`
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.graphql(ctx, `query { aboutServer { version } }`, nil, &data); err == nil {
		h.Reachable = true
		h.Version = data.AboutServer.Version
	}
	return h
}

type suwayomiLocatedChapter struct {
	MangaID      int64
	ChapterID    int64
	IsDownloaded bool
}

func (c *suwayomiClient) locateChapter(ctx context.Context, sourceID int64, mangaURL, chapterURL string) (*suwayomiLocatedChapter, error) {
	var data struct {
		Mangas struct {
			Nodes []struct {
				ID       int64  `json:"id"`
				SourceID string `json:"sourceId"`
				URL      string `json:"url"`
				Chapters struct {
					Nodes []struct {
						ID           int64  `json:"id"`
						URL          string `json:"url"`
						IsDownloaded bool   `json:"isDownloaded"`
					} `json:"nodes"`
				} `json:"chapters"`
			} `json:"nodes"`
		} `json:"mangas"`
	}
	query := `query LocateChapter($sourceId: LongString!, $url: String!) {
		mangas(condition: { sourceId: $sourceId, url: $url }) {
			nodes {
				id
				sourceId
				url
				chapters {
					nodes { id url isDownloaded }
				}
			}
		}
	}`
	vars := map[string]any{
		"sourceId": strconv.FormatInt(sourceID, 10),
		"url":      mangaURL,
	}
	if err := c.graphql(ctx, query, vars, &data); err != nil {
		return nil, err
	}
	for _, m := range data.Mangas.Nodes {
		for _, ch := range m.Chapters.Nodes {
			if ch.URL == chapterURL {
				return &suwayomiLocatedChapter{
					MangaID:      m.ID,
					ChapterID:    ch.ID,
					IsDownloaded: ch.IsDownloaded,
				}, nil
			}
		}
	}
	return nil, nil
}

func (c *suwayomiClient) enqueueAndWaitDownload(ctx context.Context, chapterID int64, timeout time.Duration) error {
	enqueue := `mutation Enqueue($id: Int!) {
		enqueueChapterDownload(input: { id: $id }) {
			downloadStatus { state }
		}
	}`
	if err := c.graphql(ctx, enqueue, map[string]any{"id": chapterID}, nil); err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	checkQuery := `query CheckDownloaded($id: Int!) {
		chapter(id: $id) { id isDownloaded }
	}`
	for time.Now().Before(deadline) {
		var res struct {
			Chapter struct {
				ID           int64 `json:"id"`
				IsDownloaded bool  `json:"isDownloaded"`
			} `json:"chapter"`
		}
		if err := c.graphql(ctx, checkQuery, map[string]any{"id": chapterID}, &res); err == nil && res.Chapter.IsDownloaded {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return errors.New("timed out waiting for suwayomi chapter download")
}

func (c *suwayomiClient) streamChapterCBZ(ctx context.Context, chapterID int64, w http.ResponseWriter) error {
	url := fmt.Sprintf("%s/api/v1/chapter/%d/download", c.baseURL, chapterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.addAuth(req)

	// Dedicated client without the 30s timeout so large CBZs can stream cleanly.
	streamHTTP := &http.Client{}
	resp, err := streamHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("suwayomi chapter download http %d", resp.StatusCode)
	}
	w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, resp.Body)
	return err
}

// pullLibrary reads Suwayomi's current library, categories, chapter read states,
// and extension repos so Yatagarasu can merge them during /api/sync/reconcile.
func (c *suwayomiClient) pullLibrary(ctx context.Context) (*SyncPayload, error) {
	var data struct {
		Categories struct {
			Nodes []struct {
				ID    int    `json:"id"`
				Name  string `json:"name"`
				Order int    `json:"order"`
			} `json:"nodes"`
		} `json:"categories"`
		Mangas struct {
			Nodes []struct {
				ID           int64  `json:"id"`
				SourceID     string `json:"sourceId"`
				URL          string `json:"url"`
				Title        string `json:"title"`
				ThumbnailURL string `json:"thumbnailUrl"`
				InLibrary    bool   `json:"inLibrary"`
				Categories   struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"categories"`
				Chapters struct {
					Nodes []struct {
						ID            int64   `json:"id"`
						URL           string  `json:"url"`
						Name          string  `json:"name"`
						ChapterNumber float64 `json:"chapterNumber"`
						Scanlator     string  `json:"scanlator"`
						IsRead        bool    `json:"isRead"`
						IsBookmarked  bool    `json:"isBookmarked"`
						LastPageRead  int     `json:"lastPageRead"`
						LastReadAt    string  `json:"lastReadAt"`
					} `json:"nodes"`
				} `json:"chapters"`
				TrackRecords struct {
					Nodes []struct {
						TrackerID       int       `json:"trackerId"`
						RemoteID        flexInt64 `json:"remoteId"`
						LibraryID       flexInt64 `json:"libraryId"`
						Title           string    `json:"title"`
						Status          int       `json:"status"`
						Score           float64   `json:"score"`
						LastChapterRead float64   `json:"lastChapterRead"`
						TotalChapters   int       `json:"totalChapters"`
						RemoteURL       string    `json:"remoteUrl"`
						StartDate       flexInt64 `json:"startDate"`
						FinishDate      flexInt64 `json:"finishDate"`
					} `json:"nodes"`
				} `json:"trackRecords"`
			} `json:"nodes"`
		} `json:"mangas"`
		Settings struct {
			ExtensionRepos []string `json:"extensionRepos"`
		} `json:"settings"`
	}

	query := `query PullLibrary {
		categories { nodes { id name order } }
		mangas(condition: { inLibrary: true }) {
			nodes {
				id sourceId url title thumbnailUrl inLibrary
				categories { nodes { name } }
				chapters {
					nodes { id url name chapterNumber scanlator isRead isBookmarked lastPageRead lastReadAt }
				}
				trackRecords {
					nodes { trackerId remoteId libraryId title status score lastChapterRead totalChapters remoteUrl startDate finishDate }
				}
			}
		}
		settings { extensionRepos }
	}`
	if err := c.graphql(ctx, query, nil, &data); err != nil {
		return nil, err
	}

	repos := append([]string(nil), data.Settings.ExtensionRepos...)
	if storeRepos, err := c.getExtensionRepos(ctx); err == nil && len(storeRepos) > 0 {
		repos = mergeStringSet(repos, storeRepos)
	}

	out := &SyncPayload{
		ExtensionRepos: repos,
	}
	for _, cat := range data.Categories.Nodes {
		if strings.EqualFold(cat.Name, "Default") {
			continue
		}
		out.Categories = append(out.Categories, SyncCategory{
			Name:  cat.Name,
			Order: cat.Order,
		})
	}
	for _, m := range data.Mangas.Nodes {
		sid, err := strconv.ParseInt(m.SourceID, 10, 64)
		if err != nil {
			continue
		}
		sm := SyncManga{
			SourceID:     sid,
			URL:          m.URL,
			Title:        m.Title,
			ThumbnailURL: strings.TrimSpace(m.ThumbnailURL),
			Favorite:     m.InLibrary,
		}
		for _, cat := range m.Categories.Nodes {
			if !strings.EqualFold(cat.Name, "Default") {
				sm.Categories = append(sm.Categories, cat.Name)
			}
		}
		for _, ch := range m.Chapters.Nodes {
			lastRead, _ := strconv.ParseInt(ch.LastReadAt, 10, 64)
			if lastRead > 0 && lastRead < 1e12 {
				lastRead *= 1000 // Suwayomi returns seconds; normalize to ms
			}
			sm.Chapters = append(sm.Chapters, SyncChapter{
				URL:           ch.URL,
				Name:          ch.Name,
				ChapterNumber: ch.ChapterNumber,
				Scanlator:     ch.Scanlator,
				Read:          ch.IsRead,
				Bookmark:      ch.IsBookmarked,
				LastPageRead:  ch.LastPageRead,
				UpdatedAt:     lastRead,
			})
		}
		for _, tr := range m.TrackRecords.Nodes {
			if tr.TrackerID <= 0 || int64(tr.RemoteID) == 0 {
				continue
			}
			sm.Tracks = append(sm.Tracks, SyncTrack{
				TrackerID:           tr.TrackerID,
				RemoteID:            int64(tr.RemoteID),
				LibraryID:           int64(tr.LibraryID),
				Title:               tr.Title,
				Status:              tr.Status,
				Score:               tr.Score,
				LastChapterRead:     tr.LastChapterRead,
				TotalChapters:       tr.TotalChapters,
				TrackingURL:         tr.RemoteURL,
				StartedReadingDate:  int64(tr.StartDate),
				FinishedReadingDate: int64(tr.FinishDate),
			})
		}
		out.Mangas = append(out.Mangas, sm)
	}
	return out, nil
}

// pushChapterReadStates pushes updated read/bookmark/page state back to Suwayomi
// for chapters that exist on Suwayomi. Best-effort: never fails the reconcile.
func (c *suwayomiClient) pushChapterReadStates(ctx context.Context, mangas []SyncManga) {
	for _, m := range mangas {
		var data struct {
			Mangas struct {
				Nodes []struct {
					ID       int64 `json:"id"`
					Chapters struct {
						Nodes []struct {
							ID           int64  `json:"id"`
							URL          string `json:"url"`
							IsRead       bool   `json:"isRead"`
							IsBookmarked bool   `json:"isBookmarked"`
							LastPageRead int    `json:"lastPageRead"`
						} `json:"nodes"`
					} `json:"chapters"`
				} `json:"nodes"`
			} `json:"mangas"`
		}
		q := `query FindManga($sourceId: LongString!, $url: String!) {
			mangas(condition: { sourceId: $sourceId, url: $url }) {
				nodes {
					id
					chapters { nodes { id url isRead isBookmarked lastPageRead } }
				}
			}
		}`
		if err := c.graphql(ctx, q, map[string]any{
			"sourceId": strconv.FormatInt(m.SourceID, 10),
			"url":      m.URL,
		}, &data); err != nil || len(data.Mangas.Nodes) == 0 {
			continue
		}

		byURL := make(map[string]SyncChapter, len(m.Chapters))
		for _, ch := range m.Chapters {
			byURL[ch.URL] = ch
		}
		mut := `mutation UpdateChapter($id: Int!, $isRead: Boolean, $isBookmarked: Boolean, $lastPageRead: Int) {
			updateChapter(input: { id: $id, patch: { isRead: $isRead, isBookmarked: $isBookmarked, lastPageRead: $lastPageRead } }) {
				chapter { id }
			}
		}`
		for _, remoteCh := range data.Mangas.Nodes[0].Chapters.Nodes {
			localCh, ok := byURL[remoteCh.URL]
			if !ok {
				continue
			}
			if localCh.Read == remoteCh.IsRead && localCh.Bookmark == remoteCh.IsBookmarked && localCh.LastPageRead == remoteCh.LastPageRead {
				continue
			}
			_ = c.graphql(ctx, mut, map[string]any{
				"id":           remoteCh.ID,
				"isRead":       localCh.Read,
				"isBookmarked": localCh.Bookmark,
				"lastPageRead": localCh.LastPageRead,
			}, nil)
		}
	}
}

// ---- Suwayomi-WebUI native API methods --------------------------------------

// suwayomiImageURL rewrites a Suwayomi-relative asset path into a Yatagarasu
// image proxy URL so browsers never need direct network access or separate auth
// to Suwayomi.
func suwayomiImageURL(rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return ""
	}
	return "/api/suwayomi/image?path=" + url.QueryEscape(rawPath)
}

type SuwayomiSource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Lang           string `json:"lang"`
	IconURL        string `json:"iconUrl"`
	SupportsLatest bool   `json:"supportsLatest"`
	IsNsfw         bool   `json:"isNsfw"`
}

func (c *suwayomiClient) listSources(ctx context.Context) ([]SuwayomiSource, error) {
	var data struct {
		Sources struct {
			Nodes []SuwayomiSource `json:"nodes"`
		} `json:"sources"`
	}
	q := `query ListSources {
		sources {
			nodes {
				id
				name
				lang
				iconUrl
				supportsLatest
				isNsfw
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := data.Sources.Nodes
	for i := range out {
		out[i].IconURL = suwayomiImageURL(out[i].IconURL)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lang != out[j].Lang {
			return out[i].Lang < out[j].Lang
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

type SuwayomiMangaCard struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnailUrl"`
	InLibrary    bool   `json:"inLibrary"`
	SourceID     string `json:"sourceId"`
}

type SuwayomiBrowseResult struct {
	HasNextPage bool                `json:"hasNextPage"`
	Mangas      []SuwayomiMangaCard `json:"mangas"`
}

type SuwayomiSortSelection struct {
	Index     int  `json:"index"`
	Ascending bool `json:"ascending"`
}

type SuwayomiSourceFilter struct {
	Position        int                    `json:"position"`
	Type            string                 `json:"type"`
	Name            string                 `json:"name"`
	CheckBoxDefault bool                   `json:"checkBoxDefault,omitempty"`
	SelectValues    []string               `json:"selectValues,omitempty"`
	SelectDefault   int                    `json:"selectDefault,omitempty"`
	TextDefault     string                 `json:"textDefault,omitempty"`
	TriStateDefault string                 `json:"triStateDefault,omitempty"`
	SortValues      []string               `json:"sortValues,omitempty"`
	SortDefault     *SuwayomiSortSelection `json:"sortDefault,omitempty"`
	Filters         []SuwayomiSourceFilter `json:"filters,omitempty"`
}

type SuwayomiFilterChange struct {
	Position      int                    `json:"position"`
	CheckBoxState *bool                  `json:"checkBoxState,omitempty"`
	SelectState   *int                   `json:"selectState,omitempty"`
	TextState     *string                `json:"textState,omitempty"`
	TriState      *string                `json:"triState,omitempty"`
	SortState     *SuwayomiSortSelection `json:"sortState,omitempty"`
	GroupChange   *SuwayomiFilterChange  `json:"groupChange,omitempty"`
}

func (c *suwayomiClient) browseSource(ctx context.Context, sourceID, browseType, searchQuery string, page int, filters ...SuwayomiFilterChange) (*SuwayomiBrowseResult, error) {
	if page < 1 {
		page = 1
	}
	t := strings.ToUpper(strings.TrimSpace(browseType))
	switch t {
	case "POPULAR", "LATEST", "SEARCH":
	default:
		t = "POPULAR"
	}
	vars := map[string]any{
		"source": sourceID,
		"type":   t,
		"page":   page,
	}
	if t == "SEARCH" {
		vars["query"] = searchQuery
	}
	if len(filters) > 0 {
		vars["filters"] = filters
	}
	mut := `mutation FetchSourceManga($source: LongString!, $type: FetchSourceMangaType!, $query: String, $page: Int!, $filters: [FilterChangeInput!]) {
		fetchSourceManga(input: { source: $source, type: $type, query: $query, page: $page, filters: $filters }) {
			hasNextPage
			mangas {
				id
				title
				thumbnailUrl
				inLibrary
				sourceId
			}
		}
	}`
	var data struct {
		FetchSourceManga SuwayomiBrowseResult `json:"fetchSourceManga"`
	}
	if err := c.graphql(ctx, mut, vars, &data); err != nil {
		return nil, err
	}
	res := &data.FetchSourceManga
	for i := range res.Mangas {
		res.Mangas[i].ThumbnailURL = suwayomiImageURL(res.Mangas[i].ThumbnailURL)
	}
	return res, nil
}

type SuwayomiExtension struct {
	PkgName     string `json:"pkgName"`
	Name        string `json:"name"`
	Initial     string `json:"-"`
	Lang        string `json:"lang"`
	VersionName string `json:"versionName"`
	VersionCode int    `json:"versionCode"`
	IconURL     string `json:"iconUrl"`
	IsInstalled bool   `json:"isInstalled"`
	HasUpdate   bool   `json:"hasUpdate"`
	IsObsolete  bool   `json:"isObsolete"`
	IsNsfw      bool   `json:"isNsfw"`
}

func (c *suwayomiClient) listExtensions(ctx context.Context) ([]SuwayomiExtension, error) {
	var data struct {
		Extensions struct {
			Nodes []SuwayomiExtension `json:"nodes"`
		} `json:"extensions"`
	}
	q := `query ListExtensions {
		extensions {
			nodes {
				pkgName
				name
				lang
				versionName
				versionCode
				iconUrl
				isInstalled
				hasUpdate
				isObsolete
				isNsfw
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := data.Extensions.Nodes
	for i := range out {
		out[i].IconURL = suwayomiImageURL(out[i].IconURL)
		out[i].Initial = titleInitial(out[i].Name)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HasUpdate != out[j].HasUpdate {
			return out[i].HasUpdate
		}
		if out[i].Lang != out[j].Lang {
			return out[i].Lang < out[j].Lang
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (c *suwayomiClient) fetchExtensions(ctx context.Context) error {
	mut := `mutation FetchExtensions {
		fetchExtensions(input: {}) {
			extensions { pkgName }
		}
	}`
	return c.graphql(ctx, mut, nil, nil)
}

func (c *suwayomiClient) updateExtension(ctx context.Context, pkgName string, install, update, uninstall bool) error {
	mut := `mutation UpdateExtension($pkgName: String!, $install: Boolean, $update: Boolean, $uninstall: Boolean) {
		updateExtension(input: { id: $pkgName, patch: { install: $install, update: $update, uninstall: $uninstall } }) {
			extension {
				pkgName
				isInstalled
				hasUpdate
			}
		}
	}`
	patch := map[string]any{
		"pkgName": pkgName,
	}
	if install {
		patch["install"] = true
	}
	if update {
		patch["update"] = true
	}
	if uninstall {
		patch["uninstall"] = true
	}
	return c.graphql(ctx, mut, patch, nil)
}

type SuwayomiChapter struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	ChapterNumber float64 `json:"chapterNumber"`
	Scanlator     string  `json:"scanlator"`
	UploadDate    string  `json:"uploadDate"`
	IsRead        bool    `json:"isRead"`
	IsBookmarked  bool    `json:"isBookmarked"`
	IsDownloaded  bool    `json:"isDownloaded"`
	LastPageRead  int     `json:"lastPageRead"`
	PageCount     int     `json:"pageCount"`
	SourceOrder   int     `json:"sourceOrder"`
}

type SuwayomiMangaDetail struct {
	ID           int64                 `json:"id"`
	Title        string                `json:"title"`
	Author       string                `json:"author"`
	Artist       string                `json:"artist"`
	Description  string                `json:"description"`
	Genres       []string              `json:"genre"`
	Status       string                `json:"status"`
	ThumbnailURL string                `json:"thumbnailUrl"`
	InLibrary    bool                  `json:"inLibrary"`
	SourceID     string                `json:"sourceId"`
	SourceName   string                `json:"sourceName"`
	SourceLang   string                `json:"sourceLang"`
	Categories   []string              `json:"categories"`
	CategoryIDs  []int                 `json:"categoryIds"`
	Chapters     []SuwayomiChapter     `json:"chapters"`
	TrackRecords []SuwayomiTrackRecord `json:"trackRecords,omitempty"`
}

func (c *suwayomiClient) getManga(ctx context.Context, mangaID int64) (*SuwayomiMangaDetail, error) {
	var data struct {
		Manga struct {
			ID           int64    `json:"id"`
			Title        string   `json:"title"`
			Author       string   `json:"author"`
			Artist       string   `json:"artist"`
			Description  string   `json:"description"`
			Genres       []string `json:"genre"`
			Status       string   `json:"status"`
			ThumbnailURL string   `json:"thumbnailUrl"`
			InLibrary    bool     `json:"inLibrary"`
			SourceID     string   `json:"sourceId"`
			Source       *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Lang string `json:"lang"`
			} `json:"source"`
			Categories struct {
				Nodes []struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"categories"`
			Chapters struct {
				Nodes []SuwayomiChapter `json:"nodes"`
			} `json:"chapters"`
			TrackRecords struct {
				Nodes []SuwayomiTrackRecord `json:"nodes"`
			} `json:"trackRecords"`
		} `json:"manga"`
	}
	q := `query GetManga($id: Int!) {
		manga(id: $id) {
			id
			title
			author
			artist
			description
			genre
			status
			thumbnailUrl
			inLibrary
			sourceId
			source { id name lang }
			categories { nodes { id name } }
			chapters {
				nodes {
					id
					name
					chapterNumber
					scanlator
					uploadDate
					isRead
					isBookmarked
					isDownloaded
					lastPageRead
					pageCount
					sourceOrder
				}
			}
			trackRecords {
				nodes {
					id
					trackerId
					remoteId
					title
					status
					displayScore
					score
					lastChapterRead
					totalChapters
					remoteUrl
				}
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"id": mangaID}, &data); err != nil {
		return nil, err
	}
	m := data.Manga
	if m.ID == 0 {
		return nil, errors.New("manga not found")
	}
	detail := &SuwayomiMangaDetail{
		ID:           m.ID,
		Title:        m.Title,
		Author:       m.Author,
		Artist:       m.Artist,
		Description:  m.Description,
		Genres:       m.Genres,
		Status:       m.Status,
		ThumbnailURL: suwayomiImageURL(m.ThumbnailURL),
		InLibrary:    m.InLibrary,
		SourceID:     m.SourceID,
		Chapters:     m.Chapters.Nodes,
		TrackRecords: m.TrackRecords.Nodes,
	}
	if m.Source != nil {
		detail.SourceName = m.Source.Name
		detail.SourceLang = m.Source.Lang
	}
	for _, cat := range m.Categories.Nodes {
		if !strings.EqualFold(cat.Name, "Default") {
			detail.Categories = append(detail.Categories, cat.Name)
			if cat.ID > 0 {
				detail.CategoryIDs = append(detail.CategoryIDs, cat.ID)
			}
		}
	}
	// Newest / highest chapter number first on the details page.
	sort.SliceStable(detail.Chapters, func(i, j int) bool {
		if detail.Chapters[i].SourceOrder != detail.Chapters[j].SourceOrder {
			return detail.Chapters[i].SourceOrder > detail.Chapters[j].SourceOrder
		}
		return detail.Chapters[i].ChapterNumber > detail.Chapters[j].ChapterNumber
	})
	return detail, nil
}

func (c *suwayomiClient) toggleLibrary(ctx context.Context, mangaID int64, inLibrary bool) error {
	mut := `mutation UpdateManga($id: Int!, $inLibrary: Boolean!) {
		updateManga(input: { id: $id, patch: { inLibrary: $inLibrary } }) {
			manga { id inLibrary }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{
		"id":        mangaID,
		"inLibrary": inLibrary,
	}, nil)
}

func (c *suwayomiClient) fetchManga(ctx context.Context, mangaID int64) error {
	mut := `mutation FetchManga($id: Int!) {
		fetchManga(input: { id: $id }) {
			manga { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"id": mangaID}, nil)
}

func (c *suwayomiClient) fetchChapters(ctx context.Context, mangaID int64) error {
	mut := `mutation FetchChapters($mangaId: Int!) {
		fetchChapters(input: { mangaId: $mangaId }) {
			chapters { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"mangaId": mangaID}, nil)
}

func (c *suwayomiClient) enqueueDownload(ctx context.Context, chapterID int64) error {
	mut := `mutation EnqueueDownload($id: Int!) {
		enqueueChapterDownload(input: { id: $id }) {
			downloadStatus { state }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"id": chapterID}, nil)
}

func (c *suwayomiClient) dequeueDownload(ctx context.Context, chapterID int64) error {
	mut := `mutation DequeueDownload($id: Int!) {
		dequeueChapterDownload(input: { id: $id }) {
			downloadStatus { state }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"id": chapterID}, nil)
}

func (c *suwayomiClient) deleteDownload(ctx context.Context, chapterID int64) error {
	mut := `mutation DeleteDownload($id: Int!) {
		deleteDownloadedChapter(input: { id: $id }) {
			chapters { id isDownloaded }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"id": chapterID}, nil)
}

type SuwayomiDownloadItem struct {
	State         string  `json:"state"`
	Progress      float64 `json:"progress"`
	Tries         int     `json:"tries"`
	ChapterID     int64   `json:"chapterId"`
	ChapterName   string  `json:"chapterName"`
	ChapterNumber float64 `json:"chapterNumber"`
	MangaID       int64   `json:"mangaId"`
	MangaTitle    string  `json:"mangaTitle"`
	ThumbnailURL  string  `json:"thumbnailUrl"`
}

type SuwayomiDownloadStatus struct {
	State string                 `json:"state"`
	Queue []SuwayomiDownloadItem `json:"queue"`
}

func (c *suwayomiClient) downloadStatus(ctx context.Context) (*SuwayomiDownloadStatus, error) {
	var data struct {
		DownloadStatus struct {
			State string `json:"state"`
			Queue []struct {
				State    string  `json:"state"`
				Progress float64 `json:"progress"`
				Tries    int     `json:"tries"`
				Chapter  struct {
					ID            int64   `json:"id"`
					Name          string  `json:"name"`
					ChapterNumber float64 `json:"chapterNumber"`
				} `json:"chapter"`
				Manga struct {
					ID           int64  `json:"id"`
					Title        string `json:"title"`
					ThumbnailURL string `json:"thumbnailUrl"`
				} `json:"manga"`
			} `json:"queue"`
		} `json:"downloadStatus"`
	}
	q := `query DownloadStatus {
		downloadStatus {
			state
			queue {
				state
				progress
				tries
				chapter { id name chapterNumber }
				manga { id title thumbnailUrl }
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := &SuwayomiDownloadStatus{
		State: data.DownloadStatus.State,
		Queue: make([]SuwayomiDownloadItem, 0, len(data.DownloadStatus.Queue)),
	}
	for _, it := range data.DownloadStatus.Queue {
		out.Queue = append(out.Queue, SuwayomiDownloadItem{
			State:         it.State,
			Progress:      it.Progress,
			Tries:         it.Tries,
			ChapterID:     it.Chapter.ID,
			ChapterName:   it.Chapter.Name,
			ChapterNumber: it.Chapter.ChapterNumber,
			MangaID:       it.Manga.ID,
			MangaTitle:    it.Manga.Title,
			ThumbnailURL:  suwayomiImageURL(it.Manga.ThumbnailURL),
		})
	}
	return out, nil
}

func (c *suwayomiClient) controlDownloader(ctx context.Context, action string) error {
	var mut string
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start":
		mut = `mutation StartDownloader { startDownloader(input: {}) { downloadStatus { state } } }`
	case "stop":
		mut = `mutation StopDownloader { stopDownloader(input: {}) { downloadStatus { state } } }`
	case "clear":
		mut = `mutation ClearDownloader { clearDownloader(input: {}) { downloadStatus { state } } }`
	default:
		return fmt.Errorf("unknown downloader action %q", action)
	}
	return c.graphql(ctx, mut, nil, nil)
}

type SuwayomiUpdateStatus struct {
	IsRunning              bool     `json:"isRunning"`
	TotalJobs              int      `json:"totalJobs"`
	FinishedJobs           int      `json:"finishedJobs"`
	SkippedMangasCount     int      `json:"skippedMangasCount"`
	SkippedCategoriesCount int      `json:"skippedCategoriesCount"`
	RunningMangas          []string `json:"runningMangas,omitempty"`
	PendingMangas          int      `json:"pendingMangas"`
}

type rawSuwayomiUpdateStatus struct {
	JobsInfo struct {
		IsRunning              bool `json:"isRunning"`
		TotalJobs              int  `json:"totalJobs"`
		FinishedJobs           int  `json:"finishedJobs"`
		SkippedMangasCount     int  `json:"skippedMangasCount"`
		SkippedCategoriesCount int  `json:"skippedCategoriesCount"`
	} `json:"jobsInfo"`
	MangaUpdates []struct {
		Status string `json:"status"`
		Manga  struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"manga"`
	} `json:"mangaUpdates"`
}

func parseRawUpdateStatus(raw rawSuwayomiUpdateStatus) *SuwayomiUpdateStatus {
	st := &SuwayomiUpdateStatus{
		IsRunning:              raw.JobsInfo.IsRunning,
		TotalJobs:              raw.JobsInfo.TotalJobs,
		FinishedJobs:           raw.JobsInfo.FinishedJobs,
		SkippedMangasCount:     raw.JobsInfo.SkippedMangasCount,
		SkippedCategoriesCount: raw.JobsInfo.SkippedCategoriesCount,
	}
	for _, mu := range raw.MangaUpdates {
		switch strings.ToUpper(strings.TrimSpace(mu.Status)) {
		case "RUNNING":
			if title := strings.TrimSpace(mu.Manga.Title); title != "" {
				st.RunningMangas = append(st.RunningMangas, title)
			}
		case "PENDING":
			st.PendingMangas++
		}
	}
	if st.TotalJobs == 0 && (len(st.RunningMangas) > 0 || st.PendingMangas > 0) {
		st.TotalJobs = st.FinishedJobs + len(st.RunningMangas) + st.PendingMangas
	}
	return st
}

// ensureLibraryUpdaterDefaults disables Suwayomi's excludeUnreadChapters and
// excludeNotStarted defaults (which otherwise skip almost every library manga).
func (c *suwayomiClient) ensureLibraryUpdaterDefaults(ctx context.Context) {
	mut := `mutation EnsureUpdaterSettings {
		setSettings(input: { settings: { excludeUnreadChapters: false, excludeNotStarted: false } }) {
			settings { downloadAsCbz }
		}
	}`
	if err := c.graphql(ctx, mut, nil, nil); err != nil {
		userMut := `mutation EnsureUpdaterUserSettings {
			setUserSettings(input: { userSettings: { excludeUnreadChapters: false, excludeNotStarted: false } }) {
				userSettings { excludeNotStarted }
			}
		}`
		_ = c.graphql(ctx, userMut, nil, nil)
	}
}

func (c *suwayomiClient) startLibraryUpdate(ctx context.Context) (*SuwayomiUpdateStatus, error) {
	c.ensureLibraryUpdaterDefaults(ctx)
	var data struct {
		UpdateLibrary struct {
			UpdateStatus rawSuwayomiUpdateStatus `json:"updateStatus"`
		} `json:"updateLibrary"`
	}
	mut := `mutation UpdateLibrary {
		updateLibrary(input: {}) {
			updateStatus {
				jobsInfo {
					isRunning
					totalJobs
					finishedJobs
					skippedMangasCount
					skippedCategoriesCount
				}
				mangaUpdates {
					status
					manga { id title }
				}
			}
		}
	}`
	if err := c.graphql(ctx, mut, nil, &data); err != nil {
		return nil, err
	}
	st := parseRawUpdateStatus(data.UpdateLibrary.UpdateStatus)
	if st.TotalJobs == 0 && st.SkippedMangasCount == 0 {
		select {
		case <-ctx.Done():
			return st, nil
		case <-time.After(60 * time.Millisecond):
		}
		if latest, err := c.getLibraryUpdateStatus(ctx); err == nil && latest != nil {
			if latest.TotalJobs > 0 || latest.SkippedMangasCount > 0 || latest.IsRunning {
				return latest, nil
			}
		}
	}
	return st, nil
}

func (c *suwayomiClient) getLibraryUpdateStatus(ctx context.Context) (*SuwayomiUpdateStatus, error) {
	var data struct {
		LibraryUpdateStatus rawSuwayomiUpdateStatus `json:"libraryUpdateStatus"`
	}
	q := `query LibraryUpdateStatus {
		libraryUpdateStatus {
			jobsInfo {
				isRunning
				totalJobs
				finishedJobs
				skippedMangasCount
				skippedCategoriesCount
			}
			mangaUpdates {
				status
				manga { id title }
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	return parseRawUpdateStatus(data.LibraryUpdateStatus), nil
}

func (c *suwayomiClient) stopLibraryUpdate(ctx context.Context) error {
	mut := `mutation StopUpdate {
		updateStop(input: {}) {
			clientMutationId
		}
	}`
	return c.graphql(ctx, mut, nil, nil)
}

type SuwayomiChapterReaderInfo struct {
	ID            int64
	Name          string
	ChapterNumber float64
	IsRead        bool
	LastPageRead  int
	MangaID       int64
	MangaTitle    string
	PrevChapterID int64
	NextChapterID int64
}

func (c *suwayomiClient) getChapterInfo(ctx context.Context, chapterID int64) (*SuwayomiChapterReaderInfo, error) {
	var data struct {
		Chapter struct {
			ID            int64   `json:"id"`
			Name          string  `json:"name"`
			ChapterNumber float64 `json:"chapterNumber"`
			IsRead        bool    `json:"isRead"`
			LastPageRead  int     `json:"lastPageRead"`
			MangaID       int64   `json:"mangaId"`
			Manga         *struct {
				ID       int64  `json:"id"`
				Title    string `json:"title"`
				Chapters struct {
					Nodes []struct {
						ID            int64   `json:"id"`
						ChapterNumber float64 `json:"chapterNumber"`
						SourceOrder   int     `json:"sourceOrder"`
					} `json:"nodes"`
				} `json:"chapters"`
			} `json:"manga"`
		} `json:"chapter"`
	}
	q := `query GetChapter($id: Int!) {
		chapter(id: $id) {
			id
			name
			chapterNumber
			isRead
			lastPageRead
			mangaId
			manga {
				id
				title
				chapters {
					nodes { id chapterNumber sourceOrder }
				}
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"id": chapterID}, &data); err != nil {
		return nil, err
	}
	ch := data.Chapter
	if ch.ID == 0 {
		return nil, errors.New("chapter not found")
	}
	info := &SuwayomiChapterReaderInfo{
		ID:            ch.ID,
		Name:          ch.Name,
		ChapterNumber: ch.ChapterNumber,
		IsRead:        ch.IsRead,
		LastPageRead:  ch.LastPageRead,
		MangaID:       ch.MangaID,
	}
	if ch.Manga != nil {
		info.MangaTitle = ch.Manga.Title
		if info.MangaID == 0 {
			info.MangaID = ch.Manga.ID
		}
		list := ch.Manga.Chapters.Nodes
		// Reading order: ascending sourceOrder / chapterNumber
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].SourceOrder != list[j].SourceOrder {
				return list[i].SourceOrder < list[j].SourceOrder
			}
			return list[i].ChapterNumber < list[j].ChapterNumber
		})
		for i, item := range list {
			if item.ID == ch.ID {
				if i > 0 {
					info.PrevChapterID = list[i-1].ID
				}
				if i+1 < len(list) {
					info.NextChapterID = list[i+1].ID
				}
				break
			}
		}
	}
	return info, nil
}

func (c *suwayomiClient) getChapterPages(ctx context.Context, chapterID int64) ([]string, error) {
	mut := `mutation FetchChapterPages($chapterId: Int!) {
		fetchChapterPages(input: { chapterId: $chapterId }) {
			pages
		}
	}`
	var data struct {
		FetchChapterPages struct {
			Pages []string `json:"pages"`
		} `json:"fetchChapterPages"`
	}
	longSuw := *c
	longSuw.http = &http.Client{Timeout: 60 * time.Second}
	if err := longSuw.graphql(ctx, mut, map[string]any{"chapterId": chapterID}, &data); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(data.FetchChapterPages.Pages))
	for _, p := range data.FetchChapterPages.Pages {
		out = append(out, suwayomiImageURL(p))
	}
	return out, nil
}

func (c *suwayomiClient) markChapterRead(ctx context.Context, chapterID int64, isRead bool, lastPageRead int) error {
	mut := `mutation MarkChapterRead($id: Int!, $isRead: Boolean!, $lastPageRead: Int) {
		updateChapter(input: { id: $id, patch: { isRead: $isRead, lastPageRead: $lastPageRead } }) {
			chapter { id isRead lastPageRead }
		}
	}`
	vars := map[string]any{
		"id":     chapterID,
		"isRead": isRead,
	}
	if lastPageRead >= 0 {
		vars["lastPageRead"] = lastPageRead
	}
	return c.graphql(ctx, mut, vars, nil)
}

func (c *suwayomiClient) proxyImage(ctx context.Context, imgPath string, w http.ResponseWriter) error {
	imgPath = strings.TrimSpace(imgPath)
	if !strings.HasPrefix(imgPath, "/") || strings.HasPrefix(imgPath, "//") || strings.Contains(imgPath, "..") {
		return errors.New("invalid suwayomi image path")
	}
	target := c.baseURL + imgPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	c.addAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("suwayomi image http %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	if !strings.HasPrefix(strings.ToLower(ct), "image/") {
		return fmt.Errorf("unexpected non-image content-type %q", ct)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *suwayomiClient) fetchImageBytes(ctx context.Context, imgPath string) ([]byte, error) {
	imgPath = strings.TrimSpace(imgPath)
	if !strings.HasPrefix(imgPath, "/") || strings.HasPrefix(imgPath, "//") || strings.Contains(imgPath, "..") {
		return nil, errors.New("invalid suwayomi image path")
	}
	target := c.baseURL + imgPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("suwayomi image http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

type SuwayomiLibraryManga struct {
	ID            int64
	SourceID      int64
	URL           string
	Title         string
	ThumbnailURL  string
	UnreadCount   int
	DownloadCount int
	TotalChapters int
	Categories    []string
}

type SuwayomiLibraryData struct {
	Categories []SyncCategory
	Mangas     []SuwayomiLibraryManga
}

func (c *suwayomiClient) listLibrary(ctx context.Context) (*SuwayomiLibraryData, error) {
	var data struct {
		Categories struct {
			Nodes []struct {
				ID    int    `json:"id"`
				Name  string `json:"name"`
				Order int    `json:"order"`
			} `json:"nodes"`
		} `json:"categories"`
		Mangas struct {
			Nodes []struct {
				ID            int64  `json:"id"`
				SourceID      string `json:"sourceId"`
				URL           string `json:"url"`
				Title         string `json:"title"`
				ThumbnailURL  string `json:"thumbnailUrl"`
				UnreadCount   int    `json:"unreadCount"`
				DownloadCount int    `json:"downloadCount"`
				Chapters      struct {
					TotalCount int `json:"totalCount"`
				} `json:"chapters"`
				Categories struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"categories"`
			} `json:"nodes"`
		} `json:"mangas"`
	}
	q := `query ListLibrary {
		categories { nodes { id name order } }
		mangas(condition: { inLibrary: true }) {
			nodes {
				id
				sourceId
				url
				title
				thumbnailUrl
				unreadCount
				downloadCount
				chapters { totalCount }
				categories { nodes { name } }
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := &SuwayomiLibraryData{}
	for _, cat := range data.Categories.Nodes {
		if strings.EqualFold(cat.Name, "Default") {
			continue
		}
		out.Categories = append(out.Categories, SyncCategory{
			Name:  cat.Name,
			Order: cat.Order,
		})
	}
	for _, m := range data.Mangas.Nodes {
		sid, _ := strconv.ParseInt(m.SourceID, 10, 64)
		var cats []string
		for _, cat := range m.Categories.Nodes {
			if !strings.EqualFold(cat.Name, "Default") {
				cats = append(cats, cat.Name)
			}
		}
		out.Mangas = append(out.Mangas, SuwayomiLibraryManga{
			ID:            m.ID,
			SourceID:      sid,
			URL:           m.URL,
			Title:         m.Title,
			ThumbnailURL:  suwayomiImageURL(m.ThumbnailURL),
			UnreadCount:   m.UnreadCount,
			DownloadCount: m.DownloadCount,
			TotalChapters: m.Chapters.TotalCount,
			Categories:    cats,
		})
	}
	return out, nil
}

func normalizeSuwayomiRepoURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if idx := strings.IndexAny(u, "?#"); idx >= 0 {
		u = strings.TrimSpace(u[:idx])
	}
	if u == "" {
		return ""
	}
	lower := strings.ToLower(u)
	if strings.HasSuffix(lower, ".pb") || strings.HasSuffix(lower, ".json") {
		return u
	}
	u = strings.TrimRight(u, "/")
	if u == "" || u == "http:" || u == "https:" {
		return ""
	}
	return u + "/index.min.json"
}

func (c *suwayomiClient) getExtensionRepos(ctx context.Context) ([]string, error) {
	var data struct {
		ExtensionStores struct {
			Nodes []struct {
				IndexURL string `json:"indexUrl"`
			} `json:"nodes"`
		} `json:"extensionStores"`
		Settings struct {
			ExtensionRepos []string `json:"extensionRepos"`
		} `json:"settings"`
	}
	q := `query GetExtensionRepos {
		extensionStores { nodes { indexUrl } }
		settings { extensionRepos }
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		if !isUnsupportedGraphQLField(err) {
			return nil, err
		}
		// Fallback for older Suwayomi (< v2.4) without extensionStores, or future
		// versions without settings.extensionRepos.
		var storesOnly struct {
			ExtensionStores struct {
				Nodes []struct {
					IndexURL string `json:"indexUrl"`
				} `json:"nodes"`
			} `json:"extensionStores"`
		}
		if errStores := c.graphql(ctx, `query GetExtensionStores { extensionStores { nodes { indexUrl } } }`, nil, &storesOnly); errStores == nil {
			out := make([]string, 0, len(storesOnly.ExtensionStores.Nodes))
			seen := map[string]bool{}
			for _, n := range storesOnly.ExtensionStores.Nodes {
				u := strings.TrimSpace(n.IndexURL)
				if u != "" && !seen[u] {
					seen[u] = true
					out = append(out, u)
				}
			}
			return out, nil
		}

		var legacy struct {
			Settings struct {
				ExtensionRepos []string `json:"extensionRepos"`
			} `json:"settings"`
		}
		if errLegacy := c.graphql(ctx, `query GetExtensionReposLegacy { settings { extensionRepos } }`, nil, &legacy); errLegacy != nil {
			return nil, errLegacy
		}
		return legacy.Settings.ExtensionRepos, nil
	}

	out := make([]string, 0, len(data.ExtensionStores.Nodes)+len(data.Settings.ExtensionRepos))
	seen := map[string]bool{}
	for _, n := range data.ExtensionStores.Nodes {
		u := strings.TrimSpace(n.IndexURL)
		if u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	for _, r := range data.Settings.ExtensionRepos {
		u := strings.TrimSpace(r)
		if u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out, nil
}

// migrateLegacyExtensionRepos ensures every repo stored in legacy
// settings.extensionRepos is registered in Suwayomi v2.4's ExtensionStoreTable
// (which ExtensionsList.fetchExtensions actually reads from).
func (c *suwayomiClient) migrateLegacyExtensionRepos(ctx context.Context) bool {
	var data struct {
		ExtensionStores struct {
			Nodes []struct {
				IndexURL string `json:"indexUrl"`
			} `json:"nodes"`
		} `json:"extensionStores"`
		Settings struct {
			ExtensionRepos []string `json:"extensionRepos"`
		} `json:"settings"`
	}
	q := `query GetExtensionRepos {
		extensionStores { nodes { indexUrl } }
		settings { extensionRepos }
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return false
	}
	storeSet := make(map[string]bool, len(data.ExtensionStores.Nodes))
	for _, n := range data.ExtensionStores.Nodes {
		if u := strings.TrimSpace(n.IndexURL); u != "" {
			storeSet[u] = true
		}
	}
	migrated := false
	for _, n := range data.ExtensionStores.Nodes {
		raw := strings.TrimSpace(n.IndexURL)
		norm := normalizeSuwayomiRepoURL(raw)
		if norm == "" || norm == raw || storeSet[norm] {
			continue
		}
		if err := c.addExtensionRepo(ctx, norm); err == nil {
			storeSet[norm] = true
			migrated = true
		}
	}
	for _, r := range data.Settings.ExtensionRepos {
		raw := strings.TrimSpace(r)
		norm := normalizeSuwayomiRepoURL(raw)
		if norm == "" || storeSet[norm] || (norm == raw && storeSet[raw]) {
			continue
		}
		if err := c.addExtensionRepo(ctx, norm); err == nil {
			storeSet[norm] = true
			migrated = true
		}
	}
	return migrated
}

func (c *suwayomiClient) findMangaID(ctx context.Context, sourceID int64, mangaURL string) (int64, error) {
	if sourceID == 0 || strings.TrimSpace(mangaURL) == "" {
		return 0, nil
	}
	var data struct {
		Mangas struct {
			Nodes []struct {
				ID int64 `json:"id"`
			} `json:"nodes"`
		} `json:"mangas"`
	}
	q := `query FindMangaID($sourceId: LongString!, $url: String!) {
		mangas(condition: { sourceId: $sourceId, url: $url }) {
			nodes { id }
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{
		"sourceId": strconv.FormatInt(sourceID, 10),
		"url":      mangaURL,
	}, &data); err != nil {
		return 0, err
	}
	if len(data.Mangas.Nodes) == 0 {
		return 0, nil
	}
	return data.Mangas.Nodes[0].ID, nil
}

func (c *suwayomiClient) setExtensionRepos(ctx context.Context, repos []string) ([]string, error) {
	if repos == nil {
		repos = []string{}
	}
	var data struct {
		SetSettings struct {
			Settings struct {
				ExtensionRepos []string `json:"extensionRepos"`
			} `json:"settings"`
		} `json:"setSettings"`
	}
	mut := `mutation SetExtensionRepos($repos: [String!]!) {
		setSettings(input: { settings: { extensionRepos: $repos } }) {
			settings { extensionRepos }
		}
	}`
	if err := c.graphql(ctx, mut, map[string]any{"repos": repos}, &data); err != nil {
		return nil, err
	}
	return data.SetSettings.Settings.ExtensionRepos, nil
}

func (c *suwayomiClient) addExtensionRepo(ctx context.Context, repoURL string) error {
	norm := normalizeSuwayomiRepoURL(repoURL)
	if norm == "" {
		return errors.New("invalid repository URL")
	}
	mut := `mutation AddExtensionStore($url: String!) {
		addExtensionStore(input: { indexUrl: $url }) {
			extensionStore { indexUrl }
		}
	}`
	err := c.graphql(ctx, mut, map[string]any{"url": norm}, nil)
	if err == nil {
		return nil
	}
	if !isUnsupportedGraphQLField(err) {
		return err
	}

	existing, rerr := c.getExtensionRepos(ctx)
	if rerr != nil {
		return rerr
	}
	for _, r := range existing {
		if strings.TrimSpace(r) == norm {
			return nil
		}
	}
	existing = append(existing, norm)
	_, err = c.setExtensionRepos(ctx, existing)
	return err
}

func (c *suwayomiClient) removeExtensionRepo(ctx context.Context, repoURL string) error {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return nil
	}
	norm := normalizeSuwayomiRepoURL(raw)

	mut := `mutation RemoveExtensionStore($url: String!) {
		removeExtensionStore(input: { indexUrl: $url }) {
			clientMutationId
		}
	}`
	err := c.graphql(ctx, mut, map[string]any{"url": raw}, nil)
	if err == nil {
		if norm != "" && norm != raw {
			_ = c.graphql(ctx, mut, map[string]any{"url": norm}, nil)
		}
		return nil
	}
	if !isUnsupportedGraphQLField(err) && norm != "" && norm != raw {
		if errNorm := c.graphql(ctx, mut, map[string]any{"url": norm}, nil); errNorm == nil {
			return nil
		}
	}

	// Fallback to legacy setExtensionRepos (also cleans up un-normalized entries stored in settings.extensionRepos).
	existing, rerr := c.getExtensionRepos(ctx)
	if rerr != nil {
		if !isUnsupportedGraphQLField(err) {
			return err
		}
		return rerr
	}
	filtered := make([]string, 0, len(existing))
	for _, r := range existing {
		trimmed := strings.TrimSpace(r)
		if trimmed != raw && (norm == "" || trimmed != norm) {
			filtered = append(filtered, r)
		}
	}
	_, serr := c.setExtensionRepos(ctx, filtered)
	if serr != nil && !isUnsupportedGraphQLField(err) {
		return err
	}
	return serr
}

// installExternalExtension uploads an external .apk via Suwayomi's GraphQL
// installExternalExtension mutation first, falling back to the legacy REST
// endpoint POST /api/v1/extension/install for backwards compatibility.
func (c *suwayomiClient) installExternalExtension(ctx context.Context, filename string, r io.Reader) error {
	if strings.TrimSpace(filename) == "" {
		filename = "extension.apk"
	}
	apkBytes, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	gqlMut := `mutation InstallExternalExtension($file: Upload!) {
		installExternalExtension(input: { extensionFile: $file }) {
			extension { pkgName }
		}
	}`
	if err := c.graphqlUpload(ctx, gqlMut, nil, "file", filename, bytes.NewReader(apkBytes), nil); err == nil {
		return nil
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := part.Write(apkBytes); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/extension/install", pr)
	if err != nil {
		_ = pr.Close()
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	c.addAuth(req)

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if trimmed := strings.TrimSpace(string(msg)); trimmed != "" {
			return fmt.Errorf("suwayomi apk install http %d: %s", resp.StatusCode, trimmed)
		}
		return fmt.Errorf("suwayomi apk install http %d", resp.StatusCode)
	}
	return nil
}

type SuwayomiRestoreStatus struct {
	State         string `json:"state"`
	MangaProgress int    `json:"mangaProgress"`
	TotalManga    int    `json:"totalManga"`
}

func (c *suwayomiClient) restoreBackup(ctx context.Context, filename string, r io.Reader) (string, *SuwayomiRestoreStatus, error) {
	if strings.TrimSpace(filename) == "" {
		filename = "backup.tachibk"
	}
	var data struct {
		RestoreBackup struct {
			ID     string                 `json:"id"`
			Status *SuwayomiRestoreStatus `json:"status"`
		} `json:"restoreBackup"`
	}
	mut := `mutation RestoreBackup($backup: Upload!) {
		restoreBackup(input: { backup: $backup }) {
			id
			status { state mangaProgress totalManga }
		}
	}`
	if err := c.graphqlUpload(ctx, mut, nil, "backup", filename, r, &data); err != nil {
		return "", nil, err
	}
	return data.RestoreBackup.ID, data.RestoreBackup.Status, nil
}

func (c *suwayomiClient) getRestoreStatus(ctx context.Context, id string) (*SuwayomiRestoreStatus, error) {
	var data struct {
		RestoreStatus *SuwayomiRestoreStatus `json:"restoreStatus"`
	}
	q := `query RestoreStatus($id: String!) {
		restoreStatus(id: $id) {
			state
			mangaProgress
			totalManga
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"id": id}, &data); err != nil {
		return nil, err
	}
	return data.RestoreStatus, nil
}

func (c *suwayomiClient) waitRestoreBackup(ctx context.Context, id string, initial *SuwayomiRestoreStatus, maxWait time.Duration) *SuwayomiRestoreStatus {
	cur := initial
	if cur != nil {
		st := strings.ToUpper(strings.TrimSpace(cur.State))
		if st == "SUCCESS" || st == "FAILURE" {
			return cur
		}
	}
	if strings.TrimSpace(id) == "" || maxWait <= 0 {
		return cur
	}
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return cur
		case <-time.After(200 * time.Millisecond):
		}
		next, err := c.getRestoreStatus(ctx, id)
		if err != nil {
			continue
		}
		if next != nil {
			cur = next
			st := strings.ToUpper(strings.TrimSpace(cur.State))
			if st == "SUCCESS" || st == "FAILURE" {
				return cur
			}
		}
	}
	return cur
}

// materializeMissingMangas checks Suwayomi's in-library manga keys and imports
// any favorite SyncManga entries missing from Suwayomi via a synthesized .tachibk backup.
func (c *suwayomiClient) materializeMissingMangas(ctx context.Context, cats []SyncCategory, mangas []SyncManga) (int, error) {
	var favs []SyncManga
	for _, m := range mangas {
		if m.Favorite && m.SourceID != 0 && m.URL != "" {
			favs = append(favs, m)
		}
	}
	if len(favs) == 0 {
		return 0, nil
	}

	var data struct {
		Mangas struct {
			Nodes []struct {
				SourceID string `json:"sourceId"`
				URL      string `json:"url"`
			} `json:"nodes"`
		} `json:"mangas"`
	}
	q := `query ExistingLibraryKeys {
		mangas(condition: { inLibrary: true }) {
			nodes { sourceId url }
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return 0, err
	}

	have := make(map[string]bool, len(data.Mangas.Nodes))
	for _, n := range data.Mangas.Nodes {
		if sid, err := strconv.ParseInt(n.SourceID, 10, 64); err == nil {
			have[SyncMangaKey{SourceID: sid, URL: n.URL}.str()] = true
		}
	}

	var missing []SyncManga
	seenMissing := map[string]bool{}
	for _, m := range favs {
		key := SyncMangaKey{SourceID: m.SourceID, URL: m.URL}.str()
		if !have[key] && !seenMissing[key] {
			seenMissing[key] = true
			missing = append(missing, m)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	tachibkBytes, err := encodeSyncPayloadToTachibk(cats, missing)
	if err != nil {
		return 0, err
	}
	restoreID, initStatus, err := c.restoreBackup(ctx, "yata-sync.tachibk", bytes.NewReader(tachibkBytes))
	if err != nil {
		return 0, err
	}
	finalStatus := c.waitRestoreBackup(ctx, restoreID, initStatus, 15*time.Second)
	if finalStatus != nil && strings.EqualFold(finalStatus.State, "FAILURE") {
		return 0, errors.New("suwayomi failed to materialize library backup")
	}
	return len(missing), nil
}

// ---- Source Preferences -----------------------------------------------------

type SuwayomiSourcePreference struct {
	Position     int
	Type         string
	Title        string
	Summary      string
	CurrentBool  bool
	CurrentStr   string
	CurrentMulti []string
	Entries      []string
	EntryValues  []string
}

type SuwayomiSourcePreferences struct {
	SourceID    string
	DisplayName string
	Preferences []SuwayomiSourcePreference
}

func (c *suwayomiClient) getSourcePreferences(ctx context.Context, sourceID string) (*SuwayomiSourcePreferences, error) {
	var data struct {
		Source struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Preferences []struct {
				TypeName                             string   `json:"__typename"`
				Title                                string   `json:"title"`
				Summary                              string   `json:"summary"`
				CheckBoxPreferenceTitle              string   `json:"CheckBoxPreferenceTitle"`
				CheckBoxPreferenceCurrentValue       bool     `json:"CheckBoxPreferenceCurrentValue"`
				SwitchPreferenceTitle                string   `json:"SwitchPreferenceTitle"`
				SwitchPreferenceCurrentValue         bool     `json:"SwitchPreferenceCurrentValue"`
				ListPreferenceTitle                  string   `json:"ListPreferenceTitle"`
				ListPreferenceCurrentValue           string   `json:"ListPreferenceCurrentValue"`
				MultiSelectListPreferenceTitle       string   `json:"MultiSelectListPreferenceTitle"`
				MultiSelectListPreferenceCurrentValue []string `json:"MultiSelectListPreferenceCurrentValue"`
				EditTextPreferenceTitle              string   `json:"EditTextPreferenceTitle"`
				EditTextPreferenceCurrentValue       string   `json:"EditTextPreferenceCurrentValue"`
				Entries                              []string `json:"entries"`
				EntryValues                          []string `json:"entryValues"`
			} `json:"preferences"`
		} `json:"source"`
	}
	q := `query GetSourcePreferences($id: LongString!) {
		source(id: $id) {
			id
			displayName
			preferences {
				__typename
				... on CheckBoxPreference { title summary CheckBoxPreferenceCurrentValue: currentValue }
				... on SwitchPreference { title summary SwitchPreferenceCurrentValue: currentValue }
				... on ListPreference { title summary entries entryValues ListPreferenceCurrentValue: currentValue }
				... on MultiSelectListPreference { title summary entries entryValues MultiSelectListPreferenceCurrentValue: currentValue }
				... on EditTextPreference { title summary EditTextPreferenceCurrentValue: currentValue }
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"id": sourceID}, &data); err != nil {
		return nil, err
	}
	if data.Source.ID == "" {
		return nil, errors.New("source not found")
	}
	out := &SuwayomiSourcePreferences{
		SourceID:    data.Source.ID,
		DisplayName: data.Source.DisplayName,
		Preferences: make([]SuwayomiSourcePreference, 0, len(data.Source.Preferences)),
	}
	for i, p := range data.Source.Preferences {
		pref := SuwayomiSourcePreference{
			Position:    i,
			Type:        p.TypeName,
			Title:       p.Title,
			Summary:     p.Summary,
			Entries:     p.Entries,
			EntryValues: p.EntryValues,
		}
		switch p.TypeName {
		case "CheckBoxPreference":
			if p.CheckBoxPreferenceTitle != "" {
				pref.Title = p.CheckBoxPreferenceTitle
			}
			pref.CurrentBool = p.CheckBoxPreferenceCurrentValue
		case "SwitchPreference":
			if p.SwitchPreferenceTitle != "" {
				pref.Title = p.SwitchPreferenceTitle
			}
			pref.CurrentBool = p.SwitchPreferenceCurrentValue
		case "ListPreference":
			if p.ListPreferenceTitle != "" {
				pref.Title = p.ListPreferenceTitle
			}
			pref.CurrentStr = p.ListPreferenceCurrentValue
		case "MultiSelectListPreference":
			if p.MultiSelectListPreferenceTitle != "" {
				pref.Title = p.MultiSelectListPreferenceTitle
			}
			pref.CurrentMulti = p.MultiSelectListPreferenceCurrentValue
		case "EditTextPreference":
			if p.EditTextPreferenceTitle != "" {
				pref.Title = p.EditTextPreferenceTitle
			}
			pref.CurrentStr = p.EditTextPreferenceCurrentValue
		}
		out.Preferences = append(out.Preferences, pref)
	}
	return out, nil
}

func (c *suwayomiClient) updateSourcePreference(ctx context.Context, sourceID string, position int, change map[string]any) error {
	payload := make(map[string]any, len(change)+1)
	for k, v := range change {
		payload[k] = v
	}
	payload["position"] = position
	mut := `mutation UpdateSourcePreference($source: LongString!, $change: SourcePreferenceChangeInput!) {
		updateSourcePreference(input: { source: $source, change: $change }) {
			source { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{
		"source": sourceID,
		"change": payload,
	}, nil)
}

// ---- Categories CRUD --------------------------------------------------------

type SuwayomiCategory struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Order      int    `json:"order"`
	Default    bool   `json:"default"`
	MangaCount int    `json:"mangaCount"`
}

func (c *suwayomiClient) listCategories(ctx context.Context) ([]SuwayomiCategory, error) {
	var data struct {
		Categories struct {
			Nodes []struct {
				ID      int    `json:"id"`
				Name    string `json:"name"`
				Order   int    `json:"order"`
				Default bool   `json:"default"`
				Mangas  struct {
					TotalCount int `json:"totalCount"`
				} `json:"mangas"`
			} `json:"nodes"`
		} `json:"categories"`
	}
	q := `query ListCategories {
		categories {
			nodes {
				id
				name
				order
				default
				mangas { totalCount }
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := make([]SuwayomiCategory, 0, len(data.Categories.Nodes))
	for _, n := range data.Categories.Nodes {
		out = append(out, SuwayomiCategory{
			ID:         n.ID,
			Name:       n.Name,
			Order:      n.Order,
			Default:    n.Default,
			MangaCount: n.Mangas.TotalCount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (c *suwayomiClient) createCategory(ctx context.Context, name string) (*SuwayomiCategory, error) {
	var data struct {
		CreateCategory struct {
			Category SuwayomiCategory `json:"category"`
		} `json:"createCategory"`
	}
	mut := `mutation CreateCategory($name: String!) {
		createCategory(input: { name: $name }) {
			category { id name order default }
		}
	}`
	if err := c.graphql(ctx, mut, map[string]any{"name": name}, &data); err != nil {
		return nil, err
	}
	return &data.CreateCategory.Category, nil
}

func (c *suwayomiClient) updateCategory(ctx context.Context, id int, name string) error {
	mut := `mutation UpdateCategory($id: Int!, $name: String!) {
		updateCategory(input: { id: $id, patch: { name: $name } }) {
			category { id name }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"id": id, "name": name}, nil)
}

func (c *suwayomiClient) deleteCategory(ctx context.Context, id int) error {
	mut := `mutation DeleteCategory($id: Int!) {
		deleteCategory(input: { categoryId: $id }) {
			category { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"id": id}, nil)
}

func (c *suwayomiClient) updateMangaCategories(ctx context.Context, mangaID int64, addTo, removeFrom []int) error {
	patch := map[string]any{}
	if len(addTo) > 0 {
		patch["addToCategories"] = addTo
	}
	if len(removeFrom) > 0 {
		patch["removeFromCategories"] = removeFrom
	}
	if len(patch) == 0 {
		return nil
	}
	mut := `mutation UpdateMangaCategories($id: Int!, $patch: UpdateMangaCategoriesPatchInput!) {
		updateMangaCategories(input: { id: $id, patch: $patch }) {
			manga { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{
		"id":    mangaID,
		"patch": patch,
	}, nil)
}

// ---- Recent Updates Feed ----------------------------------------------------

type SuwayomiUpdateChapter struct {
	ID            int64
	Name          string
	ChapterNumber float64
	Scanlator     string
	FetchedAt     int64
	UploadDate    int64
	IsRead        bool
	IsDownloaded  bool
	MangaID       int64
	MangaTitle    string
	ThumbnailURL  string
}

func parseSuwayomiUnix(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	if v > 1e12 {
		v /= 1000
	}
	return v
}

func (c *suwayomiClient) listRecentUpdates(ctx context.Context, limit int) ([]SuwayomiUpdateChapter, error) {
	if limit <= 0 {
		limit = 50
	}
	var data struct {
		Chapters struct {
			Nodes []struct {
				ID            int64   `json:"id"`
				Name          string  `json:"name"`
				ChapterNumber float64 `json:"chapterNumber"`
				Scanlator     string  `json:"scanlator"`
				FetchedAt     string  `json:"fetchedAt"`
				UploadDate    string  `json:"uploadDate"`
				IsRead        bool    `json:"isRead"`
				IsDownloaded  bool    `json:"isDownloaded"`
				Manga         *struct {
					ID           int64  `json:"id"`
					Title        string `json:"title"`
					ThumbnailURL string `json:"thumbnailUrl"`
					InLibrary    bool   `json:"inLibrary"`
				} `json:"manga"`
			} `json:"nodes"`
		} `json:"chapters"`
	}
	q := `query RecentUpdates($first: Int!) {
		chapters(
			filter: { inLibrary: { equalTo: true } }
			order: [{ by: FETCHED_AT, byType: DESC }]
			first: $first
		) {
			nodes {
				id
				name
				chapterNumber
				scanlator
				fetchedAt
				uploadDate
				isRead
				isDownloaded
				manga {
					id
					title
					thumbnailUrl
					inLibrary
				}
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"first": limit}, &data); err != nil {
		return nil, err
	}
	out := make([]SuwayomiUpdateChapter, 0, len(data.Chapters.Nodes))
	for _, n := range data.Chapters.Nodes {
		item := SuwayomiUpdateChapter{
			ID:            n.ID,
			Name:          n.Name,
			ChapterNumber: n.ChapterNumber,
			Scanlator:     n.Scanlator,
			FetchedAt:     parseSuwayomiUnix(n.FetchedAt),
			UploadDate:    parseSuwayomiUnix(n.UploadDate),
			IsRead:        n.IsRead,
			IsDownloaded:  n.IsDownloaded,
		}
		if n.Manga != nil {
			item.MangaID = n.Manga.ID
			item.MangaTitle = n.Manga.Title
			item.ThumbnailURL = suwayomiImageURL(n.Manga.ThumbnailURL)
		}
		out = append(out, item)
	}
	return out, nil
}

// pushRemovals unfavorites manga deleted in Karasu from Suwayomi's library.
func (c *suwayomiClient) pushRemovals(ctx context.Context, keys []SyncMangaKey) {
	q := `query FindForRemoval($sourceId: LongString!, $url: String!) {
		mangas(condition: { sourceId: $sourceId, url: $url, inLibrary: true }) {
			nodes { id }
		}
	}`
	for _, k := range keys {
		var data struct {
			Mangas struct {
				Nodes []struct {
					ID int64 `json:"id"`
				} `json:"nodes"`
			} `json:"mangas"`
		}
		if err := c.graphql(ctx, q, map[string]any{
			"sourceId": strconv.FormatInt(k.SourceID, 10),
			"url":      k.URL,
		}, &data); err != nil {
			continue
		}
		for _, m := range data.Mangas.Nodes {
			_ = c.toggleLibrary(ctx, m.ID, false)
		}
	}
}

// pushCategoriesAndRepos creates missing categories and merges extension repos
// into Suwayomi so changes made in Karasu appear immediately on the server.
func (c *suwayomiClient) pushCategoriesAndRepos(ctx context.Context, cats []SyncCategory, repos []string) {
	if len(cats) > 0 {
		if existing, err := c.listCategories(ctx); err == nil {
			have := make(map[string]bool, len(existing))
			for _, ec := range existing {
				have[strings.ToLower(strings.TrimSpace(ec.Name))] = true
			}
			for _, cat := range cats {
				name := strings.TrimSpace(cat.Name)
				if name == "" || strings.EqualFold(name, "Default") || have[strings.ToLower(name)] {
					continue
				}
				_, _ = c.createCategory(ctx, name)
				have[strings.ToLower(name)] = true
			}
		}
	}
	if len(repos) > 0 {
		added := c.migrateLegacyExtensionRepos(ctx)
		have := map[string]bool{}
		if existing, err := c.getExtensionRepos(ctx); err == nil {
			for _, r := range existing {
				trimmed := strings.TrimSpace(r)
				if trimmed != "" {
					have[trimmed] = true
				}
			}
		}
		for _, r := range repos {
			norm := normalizeSuwayomiRepoURL(r)
			if norm == "" || (!strings.HasPrefix(norm, "http://") && !strings.HasPrefix(norm, "https://")) {
				continue
			}
			if have[norm] {
				continue
			}
			if err := c.addExtensionRepo(ctx, norm); err == nil {
				have[norm] = true
				added = true
			}
		}
		if added {
			_ = c.fetchExtensions(ctx)
		}
	}
}

// ---- Dynamic Source Filters (B1) --------------------------------------------

type rawSourceFilter struct {
	TypeName        string                 `json:"__typename"`
	Name            string                 `json:"name"`
	CheckBoxDefault bool                   `json:"checkBoxDefault"`
	SelectValues    []string               `json:"selectValues"`
	SelectDefault   int                    `json:"selectDefault"`
	TextDefault     string                 `json:"textDefault"`
	TriStateDefault string                 `json:"triStateDefault"`
	SortValues      []string               `json:"sortValues"`
	SortDefault     *SuwayomiSortSelection `json:"sortDefault"`
	GroupFilters    []rawSourceFilter      `json:"groupFilters"`
}

func convertRawFilters(raw []rawSourceFilter) []SuwayomiSourceFilter {
	out := make([]SuwayomiSourceFilter, 0, len(raw))
	for i, rf := range raw {
		f := SuwayomiSourceFilter{
			Position:        i,
			Type:            rf.TypeName,
			Name:            rf.Name,
			CheckBoxDefault: rf.CheckBoxDefault,
			SelectValues:    rf.SelectValues,
			SelectDefault:   rf.SelectDefault,
			TextDefault:     rf.TextDefault,
			TriStateDefault: rf.TriStateDefault,
			SortValues:      rf.SortValues,
			SortDefault:     rf.SortDefault,
		}
		if f.Type == "TriStateFilter" && f.TriStateDefault == "" {
			f.TriStateDefault = "IGNORE"
		}
		if len(rf.GroupFilters) > 0 {
			f.Filters = convertRawFilters(rf.GroupFilters)
		}
		out = append(out, f)
	}
	return out
}

func (c *suwayomiClient) getSourceFilters(ctx context.Context, sourceID string) ([]SuwayomiSourceFilter, error) {
	var data struct {
		Source struct {
			ID      string            `json:"id"`
			Filters []rawSourceFilter `json:"filters"`
		} `json:"source"`
	}
	q := `query GetSourceFilters($id: LongString!) {
		source(id: $id) {
			id
			filters {
				__typename
				... on HeaderFilter { name }
				... on SeparatorFilter { name }
				... on CheckBoxFilter { name checkBoxDefault: default }
				... on SelectFilter { name selectValues: values selectDefault: default }
				... on TextFilter { name textDefault: default }
				... on TriStateFilter { name triStateDefault: default }
				... on SortFilter { name sortValues: values sortDefault: default { index ascending } }
				... on GroupFilter {
					name
					groupFilters: filters {
						__typename
						... on HeaderFilter { name }
						... on SeparatorFilter { name }
						... on CheckBoxFilter { name checkBoxDefault: default }
						... on SelectFilter { name selectValues: values selectDefault: default }
						... on TextFilter { name textDefault: default }
						... on TriStateFilter { name triStateDefault: default }
						... on SortFilter { name sortValues: values sortDefault: default { index ascending } }
					}
				}
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"id": sourceID}, &data); err != nil {
		return nil, err
	}
	return convertRawFilters(data.Source.Filters), nil
}

// ---- Global Reading History (B2) --------------------------------------------

type SuwayomiHistoryChapter struct {
	ID            int64
	Name          string
	ChapterNumber float64
	LastReadAt    int64
	LastPageRead  int
	PageCount     int
	IsRead        bool
	MangaID       int64
	MangaTitle    string
	ThumbnailURL  string
}

func (c *suwayomiClient) listReadHistory(ctx context.Context, limit int) ([]SuwayomiHistoryChapter, error) {
	if limit <= 0 {
		limit = 50
	}
	var data struct {
		Chapters struct {
			Nodes []struct {
				ID            int64   `json:"id"`
				Name          string  `json:"name"`
				ChapterNumber float64 `json:"chapterNumber"`
				LastReadAt    string  `json:"lastReadAt"`
				LastPageRead  int     `json:"lastPageRead"`
				PageCount     int     `json:"pageCount"`
				IsRead        bool    `json:"isRead"`
				Manga         *struct {
					ID           int64  `json:"id"`
					Title        string `json:"title"`
					ThumbnailURL string `json:"thumbnailUrl"`
				} `json:"manga"`
			} `json:"nodes"`
		} `json:"chapters"`
	}
	q := `query ReadHistory($first: Int!) {
		chapters(
			filter: { lastReadAt: { greaterThan: "0" } }
			order: [{ by: LAST_READ_AT, byType: DESC }]
			first: $first
		) {
			nodes {
				id
				name
				chapterNumber
				lastReadAt
				lastPageRead
				pageCount
				isRead
				manga {
					id
					title
					thumbnailUrl
				}
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{"first": limit}, &data); err != nil {
		return nil, err
	}
	out := make([]SuwayomiHistoryChapter, 0, len(data.Chapters.Nodes))
	for _, n := range data.Chapters.Nodes {
		ts := parseSuwayomiUnix(n.LastReadAt)
		if ts <= 0 {
			continue
		}
		item := SuwayomiHistoryChapter{
			ID:            n.ID,
			Name:          n.Name,
			ChapterNumber: n.ChapterNumber,
			LastReadAt:    ts,
			LastPageRead:  n.LastPageRead,
			PageCount:     n.PageCount,
			IsRead:        n.IsRead,
		}
		if n.Manga != nil {
			item.MangaID = n.Manga.ID
			item.MangaTitle = n.Manga.Title
			item.ThumbnailURL = suwayomiImageURL(n.Manga.ThumbnailURL)
		}
		out = append(out, item)
	}
	return out, nil
}

// ---- Trackers (AniList / MAL / MangaUpdates) (B3) ---------------------------

type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(n)
	return nil
}

type SuwayomiTracker struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Icon       string `json:"icon"`
	IsLoggedIn bool   `json:"isLoggedIn"`
	AuthURL    string `json:"authUrl"`
}

type SuwayomiTrackRecord struct {
	ID              int64     `json:"id"`
	TrackerID       int       `json:"trackerId"`
	TrackerName     string    `json:"-"`
	RemoteID        flexInt64 `json:"remoteId"`
	Title           string    `json:"title"`
	Status          int       `json:"status"`
	DisplayScore    string    `json:"displayScore"`
	Score           float64   `json:"score"`
	LastChapterRead float64   `json:"lastChapterRead"`
	TotalChapters   int       `json:"totalChapters"`
	RemoteURL       string    `json:"remoteUrl"`
}

type SuwayomiTrackSearchResult struct {
	TrackerID        int       `json:"trackerId"`
	RemoteID         flexInt64 `json:"remoteId"`
	Title            string    `json:"title"`
	CoverURL         string    `json:"coverUrl"`
	PublishingType   string    `json:"publishingType"`
	PublishingStatus string    `json:"publishingStatus"`
	StartDate        string    `json:"startDate"`
	TotalChapters    int       `json:"totalChapters"`
	Summary          string    `json:"summary"`
}

func (c *suwayomiClient) listTrackers(ctx context.Context) ([]SuwayomiTracker, error) {
	var data struct {
		Trackers struct {
			Nodes []SuwayomiTracker `json:"nodes"`
		} `json:"trackers"`
	}
	q := `query ListTrackers {
		trackers {
			nodes {
				id
				name
				icon
				isLoggedIn
				authUrl
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := data.Trackers.Nodes
	for i := range out {
		if strings.HasPrefix(out[i].Icon, "/") {
			out[i].Icon = suwayomiImageURL(out[i].Icon)
		}
	}
	return out, nil
}

func (c *suwayomiClient) searchTracker(ctx context.Context, trackerID int, query string) ([]SuwayomiTrackSearchResult, error) {
	var data struct {
		SearchTracker struct {
			TrackSearches []SuwayomiTrackSearchResult `json:"trackSearches"`
		} `json:"searchTracker"`
	}
	q := `query SearchTracker($trackerId: Int!, $query: String!) {
		searchTracker(input: { trackerId: $trackerId, query: $query }) {
			trackSearches {
				trackerId
				remoteId
				title
				coverUrl
				publishingType
				publishingStatus
				startDate
				totalChapters
				summary
			}
		}
	}`
	if err := c.graphql(ctx, q, map[string]any{
		"trackerId": trackerID,
		"query":     query,
	}, &data); err != nil {
		return nil, err
	}
	return data.SearchTracker.TrackSearches, nil
}

func (c *suwayomiClient) bindTrack(ctx context.Context, mangaID int64, trackerID int, remoteID int64) error {
	mut := `mutation BindTrack($mangaId: Int!, $trackerId: Int!, $remoteId: LongString!) {
		bindTrack(input: { mangaId: $mangaId, trackerId: $trackerId, remoteId: $remoteId }) {
			trackRecord { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{
		"mangaId":   mangaID,
		"trackerId": trackerID,
		"remoteId":  strconv.FormatInt(remoteID, 10),
	}, nil)
}

func (c *suwayomiClient) unbindTrack(ctx context.Context, recordID int64) error {
	mut := `mutation UnbindTrack($recordId: Int!) {
		unbindTrack(input: { recordId: $recordId }) {
			trackRecord { id }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"recordId": recordID}, nil)
}

func (c *suwayomiClient) loginTrackerOAuth(ctx context.Context, trackerID int, callbackURL string) error {
	mut := `mutation LoginTrackerOAuth($trackerId: Int!, $callbackUrl: String!) {
		loginTrackerOAuth(input: { trackerId: $trackerId, callbackUrl: $callbackUrl }) {
			isLoggedIn
		}
	}`
	return c.graphql(ctx, mut, map[string]any{
		"trackerId":   trackerID,
		"callbackUrl": strings.TrimSpace(callbackURL),
	}, nil)
}

func (c *suwayomiClient) loginTrackerCredentials(ctx context.Context, trackerID int, username, password string) error {
	mut := `mutation LoginTrackerCredentials($trackerId: Int!, $username: String!, $password: String!) {
		loginTrackerCredentials(input: { trackerId: $trackerId, username: $username, password: $password }) {
			isLoggedIn
		}
	}`
	return c.graphql(ctx, mut, map[string]any{
		"trackerId": trackerID,
		"username":  strings.TrimSpace(username),
		"password":  password,
	}, nil)
}

func (c *suwayomiClient) logoutTracker(ctx context.Context, trackerID int) error {
	mut := `mutation LogoutTracker($trackerId: Int!) {
		logoutTracker(input: { trackerId: $trackerId }) {
			isLoggedIn
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"trackerId": trackerID}, nil)
}

// syncTrackerAccounts automatically logs credential-based trackers (such as
// Kitsu or MangaUpdates) into Suwayomi when synced from Karasu and not yet
// logged in on the server.
func (c *suwayomiClient) syncTrackerAccounts(ctx context.Context, accounts []SyncTrackerAccount) {
	if len(accounts) == 0 {
		return
	}
	trackers, err := c.listTrackers(ctx)
	if err != nil {
		return
	}
	loggedByID := make(map[int]bool, len(trackers))
	for _, tr := range trackers {
		loggedByID[tr.ID] = tr.IsLoggedIn
	}
	for _, acc := range accounts {
		if acc.TrackerID <= 0 || acc.AuthExpired || loggedByID[acc.TrackerID] {
			continue
		}
		if strings.TrimSpace(acc.Username) != "" && acc.Password != "" {
			_ = c.loginTrackerCredentials(ctx, acc.TrackerID, acc.Username, acc.Password)
		}
	}
}

// ---- Downloaded Read Chapters Cleanup (C4) ----------------------------------

type SuwayomiDownloadedChapter struct {
	ID            int64   `json:"id"`
	MangaID       int64   `json:"mangaId"`
	ChapterNumber float64 `json:"chapterNumber"`
	SourceOrder   int     `json:"sourceOrder"`
	LastReadAt    int64   `json:"-"`
}

func (c *suwayomiClient) listDownloadedReadChapters(ctx context.Context) ([]SuwayomiDownloadedChapter, error) {
	var data struct {
		Chapters struct {
			Nodes []struct {
				ID            int64   `json:"id"`
				MangaID       int64   `json:"mangaId"`
				ChapterNumber float64 `json:"chapterNumber"`
				SourceOrder   int     `json:"sourceOrder"`
				LastReadAt    string  `json:"lastReadAt"`
			} `json:"nodes"`
		} `json:"chapters"`
	}
	q := `query DownloadedReadChapters {
		chapters(condition: { isDownloaded: true, isRead: true }) {
			nodes {
				id
				mangaId
				chapterNumber
				sourceOrder
				lastReadAt
			}
		}
	}`
	if err := c.graphql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	out := make([]SuwayomiDownloadedChapter, 0, len(data.Chapters.Nodes))
	for _, n := range data.Chapters.Nodes {
		out = append(out, SuwayomiDownloadedChapter{
			ID:            n.ID,
			MangaID:       n.MangaID,
			ChapterNumber: n.ChapterNumber,
			SourceOrder:   n.SourceOrder,
			LastReadAt:    parseSuwayomiUnix(n.LastReadAt),
		})
	}
	return out, nil
}

func (c *suwayomiClient) deleteDownloadedChapters(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	mut := `mutation DeleteDownloadedChapters($ids: [Int!]!) {
		deleteDownloadedChapters(input: { ids: $ids }) {
			chapters { id isDownloaded }
		}
	}`
	return c.graphql(ctx, mut, map[string]any{"ids": ids}, nil)
}

