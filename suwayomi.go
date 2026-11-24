package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		return errors.New(envelope.Errors[0].Message)
	}
	if out != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
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
				ID         int64  `json:"id"`
				SourceID   string `json:"sourceId"`
				URL        string `json:"url"`
				Title      string `json:"title"`
				InLibrary  bool   `json:"inLibrary"`
				Categories struct {
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
				id sourceId url title inLibrary
				categories { nodes { name } }
				chapters {
					nodes { id url name chapterNumber scanlator isRead isBookmarked lastPageRead lastReadAt }
				}
			}
		}
		settings { extensionRepos }
	}`
	if err := c.graphql(ctx, query, nil, &data); err != nil {
		return nil, err
	}

	out := &SyncPayload{
		ExtensionRepos: data.Settings.ExtensionRepos,
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
			SourceID: sid,
			URL:      m.URL,
			Title:    m.Title,
			Favorite: m.InLibrary,
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

func (c *suwayomiClient) browseSource(ctx context.Context, sourceID, browseType, searchQuery string, page int) (*SuwayomiBrowseResult, error) {
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
	mut := `mutation FetchSourceManga($source: LongString!, $type: FetchSourceMangaType!, $query: String, $page: Int!) {
		fetchSourceManga(input: { source: $source, type: $type, query: $query, page: $page }) {
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
	ID           int64             `json:"id"`
	Title        string            `json:"title"`
	Author       string            `json:"author"`
	Artist       string            `json:"artist"`
	Description  string            `json:"description"`
	Genres       []string          `json:"genre"`
	Status       string            `json:"status"`
	ThumbnailURL string            `json:"thumbnailUrl"`
	InLibrary    bool              `json:"inLibrary"`
	SourceID     string            `json:"sourceId"`
	SourceName   string            `json:"sourceName"`
	SourceLang   string            `json:"sourceLang"`
	Categories   []string          `json:"categories"`
	Chapters     []SuwayomiChapter `json:"chapters"`
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
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"categories"`
			Chapters struct {
				Nodes []SuwayomiChapter `json:"nodes"`
			} `json:"chapters"`
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
			categories { nodes { name } }
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
	}
	if m.Source != nil {
		detail.SourceName = m.Source.Name
		detail.SourceLang = m.Source.Lang
	}
	for _, cat := range m.Categories.Nodes {
		if !strings.EqualFold(cat.Name, "Default") {
			detail.Categories = append(detail.Categories, cat.Name)
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

func (c *suwayomiClient) startLibraryUpdate(ctx context.Context) error {
	mut := `mutation UpdateLibrary {
		updateLibrary(input: {}) {
			updateStatus { isRunning }
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
	if err := c.graphql(ctx, mut, map[string]any{"chapterId": chapterID}, &data); err != nil {
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
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, resp.Body)
	return err
}

