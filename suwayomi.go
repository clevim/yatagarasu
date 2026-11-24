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
