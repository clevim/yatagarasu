package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthReportsCompanionV2Capabilities(t *testing.T) {
	dir := t.TempDir()
	s := shelf{dir: dir, cfg: loadConfig(dir)}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "yatagarasu" || body.Version != "2" {
		t.Fatalf("unexpected health: %+v", body)
	}
	joined := strings.Join(body.Capabilities, ",")
	for _, want := range []string{"shelf", "sync", "backups", "translate"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing capability %q in %v", want, body.Capabilities)
		}
	}
}

func TestSyncReconcileMultiDeviceAndDeletions(t *testing.T) {
	dir := t.TempDir()
	s := shelf{dir: dir, cfg: loadConfig(dir)}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	postSync := func(payload SyncPayload) SyncPayload {
		b, _ := json.Marshal(payload)
		resp, err := http.Post(srv.URL+"/api/sync/reconcile", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sync status %d", resp.StatusCode)
		}
		var out SyncPayload
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Device A pushes One Piece (ch 1 read) + extension repo + merged source.
	resA := postSync(SyncPayload{
		DeviceID: "phone-a",
		Categories: []SyncCategory{
			{Name: "Reading", Order: 1},
		},
		ExtensionRepos: []string{"https://example.org/index.min.json"},
		InstalledExtensions: []SyncExtension{
			{PkgName: "eu.kanade.tachiyomi.extension.en.test", Name: "Test", Lang: "en", VersionCode: 10},
		},
		Mangas: []SyncManga{
			{
				SourceID:   42,
				URL:        "/manga/one-piece",
				Title:      "One Piece",
				Favorite:   true,
				Categories: []string{"Reading"},
				UpdatedAt:  1000,
				MergedSources: []SyncMergedSource{
					{SourceID: 99, URL: "/alt/op", Priority: 0, UpdatesEnabled: true},
				},
				Chapters: []SyncChapter{
					{URL: "/ch/1", Name: "Ch 1", ChapterNumber: 1, Read: true, LastPageRead: 20, UpdatedAt: 1000},
					{URL: "/ch/2", Name: "Ch 2", ChapterNumber: 2, Read: false, LastPageRead: 5, UpdatedAt: 1000},
				},
			},
		},
	})
	if len(resA.Mangas) != 1 || len(resA.ExtensionRepos) != 1 {
		t.Fatalf("unexpected resA: %+v", resA)
	}

	// Device B has ch 2 read with newer timestamp and no extensions installed.
	resB := postSync(SyncPayload{
		DeviceID: "tablet-b",
		Mangas: []SyncManga{
			{
				SourceID:  42,
				URL:       "/manga/one-piece",
				Title:     "One Piece",
				Favorite:  true,
				UpdatedAt: 1000,
				Chapters: []SyncChapter{
					{URL: "/ch/1", Name: "Ch 1", ChapterNumber: 1, Read: false, LastPageRead: 0, UpdatedAt: 900},
					{URL: "/ch/2", Name: "Ch 2", ChapterNumber: 2, Read: true, LastPageRead: 18, UpdatedAt: 2000},
				},
			},
		},
	})
	if len(resB.Mangas) != 1 || len(resB.Mangas[0].Chapters) != 2 {
		t.Fatalf("unexpected resB mangas: %+v", resB.Mangas)
	}
	if !resB.Mangas[0].Chapters[0].Read || !resB.Mangas[0].Chapters[1].Read {
		t.Fatalf("expected both chapters read after merge, got %+v", resB.Mangas[0].Chapters)
	}
	if len(resB.MissingExtensions) != 1 || resB.MissingExtensions[0].PkgName != "eu.kanade.tachiyomi.extension.en.test" {
		t.Fatalf("expected missing extension reported to tablet-b, got %+v", resB.MissingExtensions)
	}

	// Device A deletes One Piece -> should not be resurrected.
	resDel := postSync(SyncPayload{
		DeviceID:         "phone-a",
		LastSyncAt:       resB.ServerTime,
		DeletedMangaKeys: []SyncMangaKey{{SourceID: 42, URL: "/manga/one-piece"}},
	})
	if len(resDel.Mangas) != 0 {
		t.Fatalf("expected deleted manga removed, got %+v", resDel.Mangas)
	}
}

func TestBackupsUploadListDownloadDelete(t *testing.T) {
	dir := t.TempDir()
	s := shelf{dir: dir, cfg: loadConfig(dir)}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/backups", strings.NewReader("fake-tachibk-payload"))
	req.Header.Set("X-Backup-Filename", "karasu_test.tachibk")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded BackupInfo
	json.NewDecoder(resp.Body).Decode(&uploaded)
	resp.Body.Close()
	if uploaded.ID == "" || uploaded.Size != int64(len("fake-tachibk-payload")) {
		t.Fatalf("unexpected upload response: %+v", uploaded)
	}

	// Download back
	dl, err := http.Get(srv.URL + "/api/backups/" + uploaded.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(dl.Body)
	dl.Body.Close()
	if string(got) != "fake-tachibk-payload" {
		t.Fatalf("downloaded backup mismatch: %q", string(got))
	}

	// Delete
	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/backups/"+uploaded.ID, nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil || delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete failed: %v %v", err, delResp)
	}
}

func TestTranslateOffloadAndCacheHit(t *testing.T) {
	var llmCalls atomic.Int32
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		writeJSON(w, map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": `{"translations":["Olá mundo"],"glossary":{"Nakama":"Companheiros"}}`,
					},
				},
			},
		})
	}))
	defer mockLLM.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.TranslateProvider = "openai"
	st.TranslateEndpoint = mockLLM.URL
	st.TranslateModel = "test-model"
	if err := cfg.save(st); err != nil {
		t.Fatal(err)
	}

	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	reqBody := TranslateRequest{
		FromLang: "en",
		ToLang:   "pt-BR",
		Pages: map[string]PageTranslation{
			"001.jpg": {
				ImgWidth:  1000,
				ImgHeight: 1500,
				Blocks: []TranslationBlock{
					{Text: "Hello world", Width: 100, Height: 40, X: 10, Y: 10},
				},
			},
		},
	}

	call := func() TranslateResponse {
		b, _ := json.Marshal(reqBody)
		resp, err := http.Post(srv.URL+"/api/translations/translate", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("translate status %d", resp.StatusCode)
		}
		var out TranslateResponse
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	first := call()
	if first.Pages["001.jpg"].Blocks[0].Translation != "Olá mundo" {
		t.Fatalf("unexpected translation: %+v", first)
	}
	if first.Learned["Nakama"] != "Companheiros" {
		t.Fatalf("expected glossary learned, got %+v", first.Learned)
	}

	// Second call must hit the shared cache without calling the LLM again.
	second := call()
	if second.Pages["001.jpg"].Blocks[0].Translation != "Olá mundo" {
		t.Fatalf("unexpected cached translation: %+v", second)
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call due to cache hit, got %d", llmCalls.Load())
	}
}

func TestSuwayomiBridgeAcquireAndStreamCBZ(t *testing.T) {
	var downloaded atomic.Bool
	mockSuwayomi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/chapter/777/download" {
			w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
			w.Write([]byte("PK\x03\x04mock-cbz-bytes"))
			return
		}
		var gql struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&gql)
		switch {
		case strings.Contains(gql.Query, "LocateChapter"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"mangas": map[string]any{
						"nodes": []map[string]any{
							{
								"id":       10,
								"sourceId": "42",
								"url":      "/manga/op",
								"chapters": map[string]any{
									"nodes": []map[string]any{
										{"id": 777, "url": "/ch/1", "isDownloaded": downloaded.Load()},
									},
								},
							},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "enqueueChapterDownload"):
			downloaded.Store(true)
			writeJSON(w, map[string]any{"data": map[string]any{"enqueueChapterDownload": map[string]any{}}})
		case strings.Contains(gql.Query, "CheckDownloaded"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"chapter": map[string]any{"id": 777, "isDownloaded": downloaded.Load()},
				},
			})
		}
	}))
	defer mockSuwayomi.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuwayomi.URL
	_ = cfg.save(st)

	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// Without acquire=true it returns 404.
	resp404, _ := http.Get(srv.URL + "/api/chapters/cbz?sourceId=42&mangaUrl=/manga/op&chapterUrl=/ch/1")
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 without acquire, got %d", resp404.StatusCode)
	}

	// With acquire=true it asks Suwayomi to download and streams the CBZ.
	resp200, err := http.Get(srv.URL + "/api/chapters/cbz?sourceId=42&mangaUrl=/manga/op&chapterUrl=/ch/1&acquire=true")
	if err != nil {
		t.Fatal(err)
	}
	cbz, _ := io.ReadAll(resp200.Body)
	resp200.Body.Close()
	if resp200.StatusCode != http.StatusOK || string(cbz) != "PK\x03\x04mock-cbz-bytes" {
		t.Fatalf("expected streamed CBZ, got %d %q", resp200.StatusCode, string(cbz))
	}
}

func newMockSuwayomiWebUI(t *testing.T) (*httptest.Server, *atomic.Bool, *atomic.Bool, *atomic.Int32) {
	t.Helper()
	var (
		inLibrary   atomic.Bool
		chapterRead atomic.Bool
		dlRunning   atomic.Bool
		extInstalled atomic.Bool
		libUpdates  atomic.Int32
	)
	dlRunning.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/manga/") && strings.HasSuffix(r.URL.Path, "/thumbnail") {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("PNG-COVER-BYTES"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/chapter/") {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("JPEG-PAGE-BYTES"))
			return
		}

		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&gql); err != nil {
			http.Error(w, "bad gql", http.StatusBadRequest)
			return
		}

		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})

		case strings.Contains(gql.Query, "ListSources"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"sources": map[string]any{
						"nodes": []map[string]any{
							{"id": "101", "name": "MangaDex", "lang": "en", "iconUrl": "/api/v1/source/icon/101", "supportsLatest": true},
						},
					},
				},
			})

		case strings.Contains(gql.Query, "FetchSourceManga"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"fetchSourceManga": map[string]any{
						"hasNextPage": true,
						"mangas": []map[string]any{
							{"id": 55, "title": "Vagabond", "thumbnailUrl": "/api/v1/manga/55/thumbnail", "inLibrary": true, "sourceId": "101"},
							{"id": 56, "title": "Vinland Saga", "thumbnailUrl": "/api/v1/manga/56/thumbnail", "inLibrary": false, "sourceId": "101"},
						},
					},
				},
			})

		case strings.Contains(gql.Query, "ListExtensions"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"extensions": map[string]any{
						"nodes": []map[string]any{
							{"pkgName": "eu.kanade.tachiyomi.extension.en.mangadex", "name": "MangaDex", "lang": "en", "versionName": "1.4.2", "isInstalled": true, "hasUpdate": true},
							{"pkgName": "eu.kanade.tachiyomi.extension.en.comick", "name": "Comick", "lang": "en", "versionName": "1.2.0", "isInstalled": extInstalled.Load(), "hasUpdate": false},
						},
					},
				},
			})

		case strings.Contains(gql.Query, "UpdateExtension"):
			if v, ok := gql.Variables["install"].(bool); ok && v {
				extInstalled.Store(true)
			}
			if v, ok := gql.Variables["uninstall"].(bool); ok && v {
				extInstalled.Store(false)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateExtension": map[string]any{"extension": map[string]any{"pkgName": gql.Variables["pkgName"], "isInstalled": extInstalled.Load()}}}})

		case strings.Contains(gql.Query, "GetManga"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"manga": map[string]any{
						"id":           55,
						"title":        "Vagabond",
						"author":       "Takehiko Inoue",
						"artist":       "Takehiko Inoue",
						"description":  "The story of Miyamoto Musashi.",
						"genre":        []string{"Action", "Historical"},
						"status":       "ONGOING",
						"thumbnailUrl": "/api/v1/manga/55/thumbnail",
						"inLibrary":    inLibrary.Load(),
						"sourceId":     "101",
						"source":       map[string]any{"id": "101", "name": "MangaDex", "lang": "en"},
						"categories":   map[string]any{"nodes": []map[string]any{{"name": "Seinen"}}},
						"chapters": map[string]any{
							"nodes": []map[string]any{
								{"id": 501, "name": "Chapter 1", "chapterNumber": 1, "isRead": chapterRead.Load(), "isDownloaded": true, "pageCount": 24, "sourceOrder": 1},
								{"id": 502, "name": "Chapter 2", "chapterNumber": 2, "isRead": false, "isDownloaded": false, "pageCount": 20, "sourceOrder": 2},
							},
						},
					},
				},
			})

		case strings.Contains(gql.Query, "UpdateManga"):
			if v, ok := gql.Variables["inLibrary"].(bool); ok {
				inLibrary.Store(v)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateManga": map[string]any{"manga": map[string]any{"id": 55, "inLibrary": inLibrary.Load()}}}})

		case strings.Contains(gql.Query, "FetchChapters"):
			writeJSON(w, map[string]any{"data": map[string]any{"fetchChapters": map[string]any{"chapters": []map[string]any{{"id": 501}, {"id": 502}}}}})

		case strings.Contains(gql.Query, "GetChapter"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"chapter": map[string]any{
						"id":            501,
						"name":          "Chapter 1",
						"chapterNumber": 1,
						"isRead":        chapterRead.Load(),
						"lastPageRead":  0,
						"mangaId":       55,
						"manga": map[string]any{
							"id":    55,
							"title": "Vagabond",
							"chapters": map[string]any{
								"nodes": []map[string]any{
									{"id": 501, "chapterNumber": 1, "sourceOrder": 1},
									{"id": 502, "chapterNumber": 2, "sourceOrder": 2},
								},
							},
						},
					},
				},
			})

		case strings.Contains(gql.Query, "FetchChapterPages"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"fetchChapterPages": map[string]any{
						"pages": []string{
							"/api/v1/chapter/501/page/0",
							"/api/v1/chapter/501/page/1",
						},
					},
				},
			})

		case strings.Contains(gql.Query, "MarkChapterRead"):
			if v, ok := gql.Variables["isRead"].(bool); ok {
				chapterRead.Store(v)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateChapter": map[string]any{"chapter": map[string]any{"id": 501, "isRead": chapterRead.Load()}}}})

		case strings.Contains(gql.Query, "DownloadStatus"):
			state := "STOPPED"
			if dlRunning.Load() {
				state = "STARTED"
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"downloadStatus": map[string]any{
						"state": state,
						"queue": []map[string]any{
							{
								"state":    "DOWNLOADING",
								"progress": 0.5,
								"tries":    0,
								"chapter":  map[string]any{"id": 502, "name": "Chapter 2", "chapterNumber": 2},
								"manga":    map[string]any{"id": 55, "title": "Vagabond", "thumbnailUrl": "/api/v1/manga/55/thumbnail"},
							},
						},
					},
				},
			})

		case strings.Contains(gql.Query, "StopDownloader"):
			dlRunning.Store(false)
			writeJSON(w, map[string]any{"data": map[string]any{"stopDownloader": map[string]any{"downloadStatus": map[string]any{"state": "STOPPED"}}}})

		case strings.Contains(gql.Query, "StartDownloader"):
			dlRunning.Store(true)
			writeJSON(w, map[string]any{"data": map[string]any{"startDownloader": map[string]any{"downloadStatus": map[string]any{"state": "STARTED"}}}})

		case strings.Contains(gql.Query, "ClearDownloader"):
			writeJSON(w, map[string]any{"data": map[string]any{"clearDownloader": map[string]any{"downloadStatus": map[string]any{"state": "STOPPED"}}}})

		case strings.Contains(gql.Query, "UpdateLibrary"):
			libUpdates.Add(1)
			writeJSON(w, map[string]any{"data": map[string]any{"updateLibrary": map[string]any{"updateStatus": map[string]any{"isRunning": true}}}})

		case strings.Contains(gql.Query, "EnqueueDownload"), strings.Contains(gql.Query, "DeleteDownload"), strings.Contains(gql.Query, "DequeueDownload"):
			writeJSON(w, map[string]any{"data": map[string]any{}})
		}
	}))
	return srv, &inLibrary, &chapterRead, &libUpdates
}

func TestSuwayomiWebUIBrowseMangaReaderDownloads(t *testing.T) {
	mockSuw, inLib, chRead, libUpdates := newMockSuwayomiWebUI(t)
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	if err := cfg.save(st); err != nil {
		t.Fatal(err)
	}
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 1. /browse (Sources tab)
	resp, err := http.Get(srv.URL + "/browse")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/browse status %d", resp.StatusCode)
	}
	html := string(body)
	for _, want := range []string{"MangaDex", "Vagabond", "In Library", "/api/suwayomi/image?path=", "/manga/55"} {
		if !strings.Contains(html, want) {
			t.Errorf("/browse missing %q", want)
		}
	}

	// 2. /browse?tab=extensions + install action
	resp, err = http.Get(srv.URL + "/browse?tab=extensions")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Comick") || !strings.Contains(string(body), "Update") {
		t.Fatalf("/browse?tab=extensions unexpected: %d", resp.StatusCode)
	}
	postResp, err := client.PostForm(srv.URL+"/browse/extensions", url.Values{
		"action":  {"install"},
		"pkgName": {"eu.kanade.tachiyomi.extension.en.comick"},
	})
	if err != nil || postResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("install extension failed: %v %v", err, postResp)
	}
	postResp.Body.Close()

	// 3. /manga/55 + toggle library + refresh chapters
	resp, err = http.Get(srv.URL + "/manga/55")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/manga/55 status %d", resp.StatusCode)
	}
	for _, want := range []string{"Vagabond", "Takehiko Inoue", "Chapter 1", "/read/501", "Add to Library"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/manga/55 missing %q", want)
		}
	}

	postResp, err = client.PostForm(srv.URL+"/manga/55/library", url.Values{"inLibrary": {"true"}})
	if err != nil || postResp.StatusCode != http.StatusSeeOther || !inLib.Load() {
		t.Fatalf("toggle library failed: %v %v inLib=%v", err, postResp, inLib.Load())
	}
	postResp.Body.Close()

	// 4. /read/501 + image proxy + mark read
	resp, err = http.Get(srv.URL + "/read/501")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/read/501 status %d", resp.StatusCode)
	}
	for _, want := range []string{"Vagabond", "Chapter 1", "/manga/55", "/read/502", "/api/suwayomi/image?path=%2Fapi%2Fv1%2Fchapter%2F501%2Fpage%2F0"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/read/501 missing %q", want)
		}
	}

	// Image proxy streams Suwayomi page and rejects SSRF paths
	imgResp, err := http.Get(srv.URL + "/api/suwayomi/image?path=" + url.QueryEscape("/api/v1/chapter/501/page/0"))
	if err != nil {
		t.Fatal(err)
	}
	imgBytes, _ := io.ReadAll(imgResp.Body)
	imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK || string(imgBytes) != "JPEG-PAGE-BYTES" {
		t.Fatalf("image proxy failed: %d %q", imgResp.StatusCode, string(imgBytes))
	}
	badImg, _ := http.Get(srv.URL + "/api/suwayomi/image?path=" + url.QueryEscape("http://evil.example/x"))
	badImg.Body.Close()
	if badImg.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for invalid proxy path, got %d", badImg.StatusCode)
	}

	// Reader auto-mark-read JSON endpoint
	mrResp, err := http.Post(srv.URL+"/read/501/read", "application/json", strings.NewReader(`{"read":true,"lastPageRead":1}`))
	if err != nil || mrResp.StatusCode != http.StatusOK || !chRead.Load() {
		t.Fatalf("mark read failed: %v %v read=%v", err, mrResp, chRead.Load())
	}
	mrResp.Body.Close()

	// 5. /downloads + pause + update library
	resp, err = http.Get(srv.URL + "/downloads")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/downloads status %d", resp.StatusCode)
	}
	for _, want := range []string{"STARTED", "Vagabond", "Chapter 2", "50%", "Update Library"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/downloads missing %q", want)
		}
	}

	postResp, err = client.PostForm(srv.URL+"/downloads/control", url.Values{"action": {"stop"}})
	if err != nil || postResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("pause downloader failed: %v %v", err, postResp)
	}
	postResp.Body.Close()

	postResp, err = client.PostForm(srv.URL+"/downloads/control", url.Values{"action": {"update-library"}})
	if err != nil || postResp.StatusCode != http.StatusSeeOther || libUpdates.Load() != 1 {
		t.Fatalf("update library failed: %v %v updates=%d", err, postResp, libUpdates.Load())
	}
	postResp.Body.Close()
}

func TestLocalShelfCBZReader(t *testing.T) {
	dir := t.TempDir()
	s := shelf{dir: dir, cfg: loadConfig(dir)}

	// Write a real CBZ with pages out of lexicographical order (page10.png before page2.png)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, data string }{
		{"page10.png", "PAGE-TEN"},
		{"page2.png", "PAGE-TWO"},
		{"ComicInfo.xml", "<ComicInfo/>"},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(f.data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(s.cbz(42), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.save(Entry{
		ChapterID:     42,
		ChapterURL:    "/ch/42",
		MangaTitle:    "Berserk",
		ChapterName:   "Chapter 1",
		ChapterNumber: 1,
		UploadedAt:    now(),
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// Reader page renders without Suwayomi configured
	resp, err := http.Get(srv.URL + "/read/shelf/42")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/read/shelf/42 status %d", resp.StatusCode)
	}
	for _, want := range []string{"Berserk", "Chapter 1", "/api/shelf/42/page/0", "/api/shelf/42/page/1", "/api/shelf/42/read"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/read/shelf/42 missing %q", want)
		}
	}

	// Natural sort: index 0 must be page2.png, index 1 must be page10.png
	p0, err := http.Get(srv.URL + "/api/shelf/42/page/0")
	if err != nil {
		t.Fatal(err)
	}
	b0, _ := io.ReadAll(p0.Body)
	p0.Body.Close()
	if p0.StatusCode != http.StatusOK || string(b0) != "PAGE-TWO" {
		t.Fatalf("expected page2.png at index 0, got %d %q", p0.StatusCode, string(b0))
	}

	p1, err := http.Get(srv.URL + "/api/shelf/42/page/1")
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(p1.Body)
	p1.Body.Close()
	if p1.StatusCode != http.StatusOK || string(b1) != "PAGE-TEN" {
		t.Fatalf("expected page10.png at index 1, got %d %q", p1.StatusCode, string(b1))
	}
}


func TestImageTranslatorSidecarAndCache(t *testing.T) {
	var calls atomic.Int32
	mockMIT := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/translate/with-form/image" {
			calls.Add(1)
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG-TRANSLATED-PAGE"))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockMIT.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.ImageTranslatorURL = mockMIT.URL
	st.ImageTranslatorLang = "PTB"
	_ = cfg.save(st)

	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// First call: miss -> calls sidecar.
	r1, err := http.Post(srv.URL+"/api/translations/page?lang=PTB", "image/png", strings.NewReader("raw-manga-page"))
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(r1.Body)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK || string(b1) != "\x89PNG-TRANSLATED-PAGE" || r1.Header.Get("X-Yata-Cache") != "miss" {
		t.Fatalf("unexpected first translate response: %d %q cache=%s", r1.StatusCode, string(b1), r1.Header.Get("X-Yata-Cache"))
	}

	// Second call with same bytes: cache hit -> sidecar not called again.
	r2, err := http.Post(srv.URL+"/api/translations/page?lang=PTB", "image/png", strings.NewReader("raw-manga-page"))
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK || string(b2) != "\x89PNG-TRANSLATED-PAGE" || r2.Header.Get("X-Yata-Cache") != "hit" {
		t.Fatalf("unexpected cached translate response: %d %q cache=%s", r2.StatusCode, string(b2), r2.Header.Get("X-Yata-Cache"))
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 sidecar call, got %d", calls.Load())
	}
}

func TestTranslateProxyPageAndSSRFRejection(t *testing.T) {
	mockSuw, _, _, _ := newMockSuwayomiWebUI(t)
	defer mockSuw.Close()

	var mitCalls atomic.Int32
	mockMIT := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/translate/with-form/image" {
			mitCalls.Add(1)
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG-PROXY-TRANSLATED"))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockMIT.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	st.ImageTranslatorURL = mockMIT.URL
	st.ImageTranslatorLang = "PTB"
	if err := cfg.save(st); err != nil {
		t.Fatal(err)
	}
	s := shelf{dir: dir, cfg: cfg}

	// Create a local shelf CBZ chapter so we can test /api/shelf/{id}/page/{idx} too.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.Create("01.jpg")
	_, _ = fw.Write([]byte("LOCAL-CBZ-PAGE-0"))
	_ = zw.Close()
	if err := os.WriteFile(s.cbz(42), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s.save(Entry{ChapterID: 42, ChapterURL: "/ch/42", MangaTitle: "Berserk", ChapterName: "Ch 1", ChapterNumber: 1, UploadedAt: now()})

	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. Reader HTML exposes Translate button, Settings drawer, Scrubber, and all 5 modes.
	readResp, err := http.Get(srv.URL + "/read/501")
	if err != nil {
		t.Fatal(err)
	}
	readBody, _ := io.ReadAll(readResp.Body)
	readResp.Body.Close()
	for _, want := range []string{
		`id="translateBtn"`, `id="pageSlider"`, `Reader Settings`,
		`Webtoon (Vertical)`, `Single Page (RTL)`, `Single Page (LTR)`,
		`Double Page (RTL)`, `Double Page (LTR)`, `Fit Screen`, `Warm Paper`,
	} {
		if !strings.Contains(string(readBody), want) {
			t.Errorf("/read/501 missing %q", want)
		}
	}

	// 2. Proxy-translate a Suwayomi page: first miss, second cache hit.
	suwSrc := "/api/suwayomi/image?path=" + url.QueryEscape("/api/v1/chapter/501/page/0")
	r1, err := http.Get(srv.URL + "/api/translations/proxy-page?src=" + url.QueryEscape(suwSrc) + "&lang=PTB")
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(r1.Body)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK || string(b1) != "\x89PNG-PROXY-TRANSLATED" || r1.Header.Get("X-Yata-Cache") != "miss" {
		t.Fatalf("proxy-page suwayomi miss unexpected: %d %q cache=%s", r1.StatusCode, string(b1), r1.Header.Get("X-Yata-Cache"))
	}

	r2, err := http.Get(srv.URL + "/api/translations/proxy-page?src=" + url.QueryEscape(suwSrc) + "&lang=PTB")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK || string(b2) != "\x89PNG-PROXY-TRANSLATED" || r2.Header.Get("X-Yata-Cache") != "hit" {
		t.Fatalf("proxy-page suwayomi hit unexpected: %d %q cache=%s", r2.StatusCode, string(b2), r2.Header.Get("X-Yata-Cache"))
	}
	if mitCalls.Load() != 1 {
		t.Fatalf("expected 1 sidecar call for cached suwayomi page, got %d", mitCalls.Load())
	}

	// 3. Proxy-translate a local shelf CBZ page.
	rShelf, err := http.Get(srv.URL + "/api/translations/proxy-page?src=" + url.QueryEscape("/api/shelf/42/page/0") + "&lang=PTB")
	if err != nil {
		t.Fatal(err)
	}
	bShelf, _ := io.ReadAll(rShelf.Body)
	rShelf.Body.Close()
	if rShelf.StatusCode != http.StatusOK || string(bShelf) != "\x89PNG-PROXY-TRANSLATED" {
		t.Fatalf("proxy-page shelf unexpected: %d %q", rShelf.StatusCode, string(bShelf))
	}

	// 4. SSRF rejection: external URLs, protocol-relative URLs, path traversal, and non-page local endpoints must return 400.
	badSrcs := []string{
		"",
		"http://169.254.169.254/latest/meta-data/",
		"https://evil.example/page.png",
		"//evil.example/page.png",
		"/api/health",
		"/api/suwayomi/image?path=http://evil.example/x",
		"/api/suwayomi/image?path=../../etc/passwd",
		"/api/shelf/42/page/../0",
		"/api/shelf/42/file",
	}
	for _, bad := range badSrcs {
		resp, err := http.Get(srv.URL + "/api/translations/proxy-page?src=" + url.QueryEscape(bad))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for SSRF src %q, got %d", bad, resp.StatusCode)
		}
	}
}

func TestExtensionRepoManagementAndGlobalSearchAndLibraryGrid(t *testing.T) {
	var (
		mu    sync.Mutex
		repos = []string{"https://example.org/initial/index.min.json"}
	)
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&gql); err != nil {
			http.Error(w, "bad gql", http.StatusBadRequest)
			return
		}
		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})
		case strings.Contains(gql.Query, "GetExtensionRepos"):
			mu.Lock()
			cp := append([]string(nil), repos...)
			mu.Unlock()
			writeJSON(w, map[string]any{"data": map[string]any{"settings": map[string]any{"extensionRepos": cp}}})
		case strings.Contains(gql.Query, "SetExtensionRepos"):
			mu.Lock()
			repos = nil
			if raw, ok := gql.Variables["repos"].([]any); ok {
				for _, item := range raw {
					if s, ok := item.(string); ok {
						repos = append(repos, s)
					}
				}
			}
			cp := append([]string(nil), repos...)
			mu.Unlock()
			writeJSON(w, map[string]any{"data": map[string]any{"setSettings": map[string]any{"settings": map[string]any{"extensionRepos": cp}}}})
		case strings.Contains(gql.Query, "FetchExtensions"):
			writeJSON(w, map[string]any{"data": map[string]any{"fetchExtensions": map[string]any{"extensions": []any{}}}})
		case strings.Contains(gql.Query, "ListExtensions"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"extensions": map[string]any{
						"nodes": []map[string]any{
							{"pkgName": "eu.kanade.tachiyomi.extension.en.mangadex", "name": "MangaDex", "lang": "en", "versionName": "1.4.2", "isInstalled": true},
							{"pkgName": "eu.kanade.tachiyomi.extension.pt.slimeread", "name": "SlimeRead", "lang": "pt-BR", "versionName": "1.0.1", "isInstalled": false},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "ListSources"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"sources": map[string]any{
						"nodes": []map[string]any{
							{"id": "101", "name": "MangaDex", "lang": "en", "supportsLatest": true},
							{"id": "202", "name": "Comick", "lang": "en", "supportsLatest": false},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "FetchSourceManga"):
			srcID, _ := gql.Variables["source"].(string)
			title := "Vagabond"
			id := 55
			if srcID == "202" {
				title = "Vagabond (Comick)"
				id = 88
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"fetchSourceManga": map[string]any{
						"hasNextPage": false,
						"mangas": []map[string]any{
							{"id": id, "title": title, "thumbnailUrl": "/api/v1/manga/55/thumbnail", "inLibrary": true, "sourceId": srcID},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "ListLibrary"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"categories": map[string]any{
						"nodes": []map[string]any{
							{"id": 1, "name": "Seinen", "order": 1},
						},
					},
					"mangas": map[string]any{
						"nodes": []map[string]any{
							{
								"id":            55,
								"sourceId":      "101",
								"url":           "/manga/vagabond",
								"title":         "Vagabond",
								"thumbnailUrl":  "/api/v1/manga/55/thumbnail",
								"unreadCount":   12,
								"downloadCount": 5,
								"chapters":      map[string]any{"totalCount": 30},
								"categories":    map[string]any{"nodes": []map[string]any{{"name": "Seinen"}}},
							},
						},
					},
				},
			})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 1. /browse?tab=extensions displays Extension Repositories panel + lang filter
	resp, err := http.Get(srv.URL + "/browse?tab=extensions")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{"Extension Repositories", "https://example.org/initial/index.min.json", "extLangFilter", "pt-BR"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/browse?tab=extensions missing %q", want)
		}
	}

	// 2. Add a Keiyoushi repo via POST /browse/extensions
	keiyoushi := "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json"
	addResp, err := client.PostForm(srv.URL+"/browse/extensions", url.Values{
		"action":  {"add-repo"},
		"repoUrl": {keiyoushi},
	})
	if err != nil || addResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("add-repo failed: %v %v", err, addResp)
	}
	addResp.Body.Close()

	mu.Lock()
	if len(repos) != 2 || repos[1] != keiyoushi {
		t.Fatalf("expected keiyoushi repo added, got %v", repos)
	}
	mu.Unlock()

	// Invalid repo scheme is rejected without modifying repos
	badResp, err := client.PostForm(srv.URL+"/browse/extensions", url.Values{
		"action":  {"add-repo"},
		"repoUrl": {"javascript:alert(1)"},
	})
	if err != nil || badResp.StatusCode != http.StatusSeeOther || !strings.Contains(badResp.Header.Get("Location"), "problem=") {
		t.Fatalf("expected invalid repo URL rejected, got %v %v", err, badResp)
	}
	badResp.Body.Close()

	// 3. Remove the initial repo via POST /browse/extensions
	remResp, err := client.PostForm(srv.URL+"/browse/extensions", url.Values{
		"action":  {"remove-repo"},
		"repoUrl": {"https://example.org/initial/index.min.json"},
	})
	if err != nil || remResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("remove-repo failed: %v %v", err, remResp)
	}
	remResp.Body.Close()

	mu.Lock()
	if len(repos) != 1 || repos[0] != keiyoushi {
		t.Fatalf("expected only keiyoushi repo remaining, got %v", repos)
	}
	mu.Unlock()

	// 4. Global Search (/browse?tab=sources&source=all&q=Vagabond)
	gResp, err := http.Get(srv.URL + "/browse?tab=sources&source=all&q=Vagabond")
	if err != nil {
		t.Fatal(err)
	}
	gBody, _ := io.ReadAll(gResp.Body)
	gResp.Body.Close()
	for _, want := range []string{"All sources (Global Search)", "MangaDex", "Comick", "Vagabond (Comick)", "/manga/55", "/manga/88"} {
		if !strings.Contains(string(gBody), want) {
			t.Errorf("global search missing %q", want)
		}
	}

	// 5. Integrated Cover-Grid Library (/library)
	libResp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatal(err)
	}
	libBody, _ := io.ReadAll(libResp.Body)
	libResp.Body.Close()
	for _, want := range []string{
		"Vagabond", "/manga/55", "/api/suwayomi/image?path=",
		"12 unread", "5 downloaded", "Seinen", "Search library...",
		"Update", "/categories", `id="btnCompact"`, `id="btnComfy"`, `id="btnList"`,
	} {
		if !strings.Contains(string(libBody), want) {
			t.Errorf("/library missing %q", want)
		}
	}
}

func TestExtensionsIconsUpdatesAllAndExternalAPK(t *testing.T) {
	var (
		mu          sync.Mutex
		updatedPkgs []string
		apkFilename string
		apkBytes    []byte
	)
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/extension/install" && r.Method == http.MethodPost {
			mr, err := r.MultipartReader()
			if err != nil {
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					http.Error(w, "read err", http.StatusBadRequest)
					return
				}
				if p.FormName() == "file" {
					b, _ := io.ReadAll(p)
					mu.Lock()
					apkFilename = p.FileName()
					apkBytes = b
					mu.Unlock()
				}
				p.Close()
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&gql); err != nil {
			http.Error(w, "bad gql", http.StatusBadRequest)
			return
		}
		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})
		case strings.Contains(gql.Query, "GetExtensionRepos"):
			writeJSON(w, map[string]any{"data": map[string]any{"settings": map[string]any{"extensionRepos": []string{}}}})
		case strings.Contains(gql.Query, "ListExtensions"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"extensions": map[string]any{
						"nodes": []map[string]any{
							{
								"pkgName":     "eu.kanade.tachiyomi.extension.en.mangadex",
								"name":        "MangaDex",
								"lang":        "en",
								"versionName": "1.4.2",
								"iconUrl":     "/api/v1/extension/icon/eu.kanade.tachiyomi.extension.en.mangadex",
								"isInstalled": true,
								"hasUpdate":   true,
								"isObsolete":  false,
								"isNsfw":      false,
							},
							{
								"pkgName":     "eu.kanade.tachiyomi.extension.en.oldsrc",
								"name":        "OldSource",
								"lang":        "en",
								"versionName": "1.0.0",
								"iconUrl":     "",
								"isInstalled": true,
								"hasUpdate":   false,
								"isObsolete":  true,
								"isNsfw":      false,
							},
							{
								"pkgName":     "eu.kanade.tachiyomi.extension.en.adult",
								"name":        "AdultSrc",
								"lang":        "en",
								"versionName": "2.0.0",
								"iconUrl":     "",
								"isInstalled": false,
								"hasUpdate":   false,
								"isObsolete":  false,
								"isNsfw":      true,
							},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "UpdateExtension"):
			pkg, _ := gql.Variables["pkgName"].(string)
			if upd, _ := gql.Variables["update"].(bool); upd {
				mu.Lock()
				updatedPkgs = append(updatedPkgs, pkg)
				mu.Unlock()
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateExtension": map[string]any{"extension": map[string]any{"pkgName": pkg, "isInstalled": true, "hasUpdate": false}}}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. Check /browse?tab=extensions renders icons, letter fallback, Updates available + Update All, 18+ toggle, obsolete badge, and APK upload form.
	resp, err := http.Get(srv.URL + "/browse?tab=extensions")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(body)
	for _, want := range []string{
		"Updates available", "Update All", `value="update-all"`,
		"/api/suwayomi/image?path=%2Fapi%2Fv1%2Fextension%2Ficon%2Feu.kanade.tachiyomi.extension.en.mangadex",
		`class="ext-icon-fallback">O<`,
		`id="extNsfwToggle"`, "Show 18+", `data-nsfw="true"`, "18+", "obsolete",
		`action="/browse/extensions/install-apk"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("/browse?tab=extensions missing %q", want)
		}
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 2. POST action=update-all updates only installed extensions with hasUpdate=true.
	updResp, err := client.PostForm(srv.URL+"/browse/extensions", url.Values{"action": {"update-all"}})
	if err != nil || updResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update-all failed: %v %v", err, updResp)
	}
	updResp.Body.Close()
	mu.Lock()
	if len(updatedPkgs) != 1 || updatedPkgs[0] != "eu.kanade.tachiyomi.extension.en.mangadex" {
		t.Fatalf("expected only mangadex updated, got %v", updatedPkgs)
	}
	mu.Unlock()

	// 3. POST /browse/extensions/install-apk streams valid .apk to Suwayomi.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "custom-ext.apk")
	_, _ = fw.Write([]byte("PK-FAKE-APK-DATA"))
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/browse/extensions/install-apk", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	apkResp, err := client.Do(req)
	if err != nil || apkResp.StatusCode != http.StatusSeeOther || !strings.Contains(apkResp.Header.Get("Location"), "note=") {
		t.Fatalf("install-apk failed: %v %v loc=%q", err, apkResp, apkResp.Header.Get("Location"))
	}
	apkResp.Body.Close()
	mu.Lock()
	if apkFilename != "custom-ext.apk" || string(apkBytes) != "PK-FAKE-APK-DATA" {
		t.Fatalf("suwayomi did not receive expected apk: name=%q data=%q", apkFilename, string(apkBytes))
	}
	mu.Unlock()

	// Non-.apk upload is rejected.
	var badBuf bytes.Buffer
	bmw := multipart.NewWriter(&badBuf)
	bfw, _ := bmw.CreateFormFile("file", "not-an-ext.zip")
	_, _ = bfw.Write([]byte("zip"))
	_ = bmw.Close()
	badReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/browse/extensions/install-apk", &badBuf)
	badReq.Header.Set("Content-Type", bmw.FormDataContentType())
	badResp, err := client.Do(badReq)
	if err != nil || badResp.StatusCode != http.StatusSeeOther || !strings.Contains(badResp.Header.Get("Location"), "problem=") {
		t.Fatalf("expected non-apk rejected, got %v %v", err, badResp)
	}
	badResp.Body.Close()
}

func TestSourcePreferencesGetAndUpdate(t *testing.T) {
	var (
		mu          sync.Mutex
		lastPos     int = -1
		lastChange  map[string]any
		lastSource  string
	)
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&gql); err != nil {
			http.Error(w, "bad gql", http.StatusBadRequest)
			return
		}
		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})
		case strings.Contains(gql.Query, "ListSources"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"sources": map[string]any{
						"nodes": []map[string]any{
							{"id": "101", "name": "MangaDex", "lang": "en", "supportsLatest": true},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "FetchSourceManga"):
			writeJSON(w, map[string]any{"data": map[string]any{"fetchSourceManga": map[string]any{"hasNextPage": false, "mangas": []any{}}}})
		case strings.Contains(gql.Query, "GetSourcePreferences"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"id":          "101",
						"displayName": "MangaDex (en)",
						"preferences": []map[string]any{
							{
								"__typename":                     "CheckBoxPreference",
								"CheckBoxPreferenceTitle":        "Data Saver",
								"summary":                        "Use lower quality images",
								"CheckBoxPreferenceCurrentValue": true,
							},
							{
								"__typename":                 "ListPreference",
								"ListPreferenceTitle":        "Cover quality",
								"summary":                    "Thumbnail resolution",
								"entries":                    []string{"Original", "512px", "256px"},
								"entryValues":                []string{"orig", "512", "256"},
								"ListPreferenceCurrentValue": "512",
							},
							{
								"__typename":                             "MultiSelectListPreference",
								"MultiSelectListPreferenceTitle":         "Content ratings",
								"entries":                                []string{"Safe", "Suggestive"},
								"entryValues":                            []string{"safe", "suggestive"},
								"MultiSelectListPreferenceCurrentValue":  []string{"safe"},
							},
							{
								"__typename":                     "EditTextPreference",
								"EditTextPreferenceTitle":        "Custom mirror",
								"EditTextPreferenceCurrentValue": "https://api.mangadex.org",
							},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "UpdateSourcePreference"):
			mu.Lock()
			lastSource, _ = gql.Variables["source"].(string)
			if p, ok := gql.Variables["position"].(float64); ok {
				lastPos = int(p)
			}
			lastChange, _ = gql.Variables["change"].(map[string]any)
			mu.Unlock()
			writeJSON(w, map[string]any{"data": map[string]any{"updateSourcePreference": map[string]any{"source": map[string]any{"id": "101"}}}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. /browse exposes Source Settings link for selected source
	bResp, err := http.Get(srv.URL + "/browse?tab=sources&source=101")
	if err != nil {
		t.Fatal(err)
	}
	bBody, _ := io.ReadAll(bResp.Body)
	bResp.Body.Close()
	if !strings.Contains(string(bBody), `href="/source/101/settings"`) {
		t.Errorf("/browse missing Source Settings link")
	}

	// 2. GET /source/101/settings renders all preferences
	resp, err := http.Get(srv.URL + "/source/101/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{"MangaDex (en)", "Data Saver", "Cover quality", "Content ratings", "Custom mirror", "https://api.mangadex.org"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/source/101/settings missing %q", want)
		}
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 3. POST /source/101/settings updates ListPreference
	uResp, err := client.PostForm(srv.URL+"/source/101/settings", url.Values{
		"position": {"1"},
		"type":     {"ListPreference"},
		"value":    {"orig"},
	})
	if err != nil || uResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update list pref failed: %v %v", err, uResp)
	}
	uResp.Body.Close()
	mu.Lock()
	if lastSource != "101" || lastPos != 1 || lastChange["listState"] != "orig" {
		t.Fatalf("unexpected preference update: src=%q pos=%d change=%+v", lastSource, lastPos, lastChange)
	}
	mu.Unlock()

	// 4. POST /source/101/settings updates MultiSelectListPreference
	mResp, err := client.PostForm(srv.URL+"/source/101/settings", url.Values{
		"position": {"2"},
		"type":     {"MultiSelectListPreference"},
		"values":   {"safe", "suggestive"},
	})
	if err != nil || mResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update multi-select pref failed: %v %v", err, mResp)
	}
	mResp.Body.Close()
	mu.Lock()
	vals, _ := lastChange["multiSelectState"].([]any)
	if lastPos != 2 || len(vals) != 2 {
		t.Fatalf("unexpected multiSelectState: pos=%d change=%+v", lastPos, lastChange)
	}
	mu.Unlock()
}

func TestCategoriesCRUDAndMangaCategoryAssignment(t *testing.T) {
	type mockCat struct {
		ID   int
		Name string
	}
	var (
		mu             sync.Mutex
		nextID         = 3
		cats           = []mockCat{{ID: 1, Name: "Reading"}, {ID: 2, Name: "Completed"}}
		mangaCatIDs    = []int{1}
	)
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&gql); err != nil {
			http.Error(w, "bad gql", http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})
		case strings.Contains(gql.Query, "ListCategories"):
			nodes := []map[string]any{{"id": 0, "name": "Default", "order": 0, "default": true, "mangas": map[string]any{"totalCount": 0}}}
			for i, c := range cats {
				nodes = append(nodes, map[string]any{"id": c.ID, "name": c.Name, "order": i + 1, "default": false, "mangas": map[string]any{"totalCount": 1}})
			}
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": nodes}}})
		case strings.Contains(gql.Query, "CreateCategory"):
			name, _ := gql.Variables["name"].(string)
			c := mockCat{ID: nextID, Name: name}
			nextID++
			cats = append(cats, c)
			writeJSON(w, map[string]any{"data": map[string]any{"createCategory": map[string]any{"category": map[string]any{"id": c.ID, "name": c.Name, "order": len(cats), "default": false}}}})
		case strings.Contains(gql.Query, "UpdateCategory"):
			id := int(gql.Variables["id"].(float64))
			name, _ := gql.Variables["name"].(string)
			for i := range cats {
				if cats[i].ID == id {
					cats[i].Name = name
				}
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateCategory": map[string]any{"category": map[string]any{"id": id, "name": name}}}})
		case strings.Contains(gql.Query, "DeleteCategory"):
			id := int(gql.Variables["id"].(float64))
			var kept []mockCat
			for _, c := range cats {
				if c.ID != id {
					kept = append(kept, c)
				}
			}
			cats = kept
			writeJSON(w, map[string]any{"data": map[string]any{"deleteCategory": map[string]any{"category": map[string]any{"id": id}}}})
		case strings.Contains(gql.Query, "GetManga"):
			var catNodes []map[string]any
			for _, cid := range mangaCatIDs {
				for _, c := range cats {
					if c.ID == cid {
						catNodes = append(catNodes, map[string]any{"id": c.ID, "name": c.Name})
					}
				}
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"manga": map[string]any{
						"id":         55,
						"title":      "Vagabond",
						"inLibrary":  true,
						"sourceId":   "101",
						"categories": map[string]any{"nodes": catNodes},
						"chapters":   map[string]any{"nodes": []any{}},
					},
				},
			})
		case strings.Contains(gql.Query, "UpdateMangaCategories"):
			patch, _ := gql.Variables["patch"].(map[string]any)
			set := map[int]bool{}
			for _, id := range mangaCatIDs {
				set[id] = true
			}
			if adds, ok := patch["addToCategories"].([]any); ok {
				for _, v := range adds {
					set[int(v.(float64))] = true
				}
			}
			if rems, ok := patch["removeFromCategories"].([]any); ok {
				for _, v := range rems {
					delete(set, int(v.(float64)))
				}
			}
			mangaCatIDs = nil
			for id := range set {
				mangaCatIDs = append(mangaCatIDs, id)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateMangaCategories": map[string]any{"manga": map[string]any{"id": 55}}}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 1. GET /categories lists custom categories (not Default)
	resp, err := http.Get(srv.URL + "/categories")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Reading") || !strings.Contains(string(body), "Completed") {
		t.Fatalf("/categories missing expected categories")
	}

	// 2. Create, rename, and delete category
	rCreate, err := client.PostForm(srv.URL+"/categories", url.Values{"action": {"create"}, "name": {"Seinen"}})
	if err != nil || rCreate.StatusCode != http.StatusSeeOther {
		t.Fatalf("create category failed: %v %v", err, rCreate)
	}
	rCreate.Body.Close()

	rRename, err := client.PostForm(srv.URL+"/categories", url.Values{"action": {"rename"}, "id": {"3"}, "name": {"Top Seinen"}})
	if err != nil || rRename.StatusCode != http.StatusSeeOther {
		t.Fatalf("rename category failed: %v %v", err, rRename)
	}
	rRename.Body.Close()

	mu.Lock()
	if len(cats) != 3 || cats[2].Name != "Top Seinen" {
		t.Fatalf("expected renamed category Top Seinen, got %+v", cats)
	}
	mu.Unlock()

	rDel, err := client.PostForm(srv.URL+"/categories", url.Values{"action": {"delete"}, "id": {"2"}})
	if err != nil || rDel.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete category failed: %v %v", err, rDel)
	}
	rDel.Body.Close()

	mu.Lock()
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories after delete, got %+v", cats)
	}
	mu.Unlock()

	// 3. Manga page renders inline Categories picker & POST /manga/55/categories updates assignment
	mResp, err := http.Get(srv.URL + "/manga/55")
	if err != nil {
		t.Fatal(err)
	}
	mBody, _ := io.ReadAll(mResp.Body)
	mResp.Body.Close()
	if !strings.Contains(string(mBody), `action="/manga/55/categories"`) || !strings.Contains(string(mBody), "Top Seinen") {
		t.Fatalf("/manga/55 missing inline categories editor")
	}

	aResp, err := client.PostForm(srv.URL+"/manga/55/categories", url.Values{"categoryIds": {"3"}})
	if err != nil || aResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("assign manga categories failed: %v %v", err, aResp)
	}
	aResp.Body.Close()

	mu.Lock()
	if len(mangaCatIDs) != 1 || mangaCatIDs[0] != 3 {
		t.Fatalf("expected manga assigned only to category 3, got %v", mangaCatIDs)
	}
	mu.Unlock()
}

func TestUpdatesFeedAndDayGrouping(t *testing.T) {
	origNow := now
	fixedNow := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)
	now = func() int64 { return fixedNow.Unix() }
	defer func() { now = origNow }()

	todayUnix := strconv.FormatInt(fixedNow.Add(-2*time.Hour).Unix(), 10)
	yestUnix := strconv.FormatInt(fixedNow.Add(-26*time.Hour).Unix(), 10)
	olderUnix := strconv.FormatInt(fixedNow.Add(-5*24*time.Hour).Unix(), 10)

	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gql struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&gql)
		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})
		case strings.Contains(gql.Query, "RecentUpdates"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"chapters": map[string]any{
						"nodes": []map[string]any{
							{
								"id": 901, "name": "Ch. 1100", "chapterNumber": 1100, "fetchedAt": todayUnix,
								"isRead": false, "isDownloaded": false,
								"manga": map[string]any{"id": 10, "title": "One Piece", "thumbnailUrl": "/api/v1/manga/10/thumbnail", "inLibrary": true},
							},
							{
								"id": 902, "name": "Ch. 327", "chapterNumber": 327, "fetchedAt": yestUnix,
								"isRead": true, "isDownloaded": true,
								"manga": map[string]any{"id": 55, "title": "Vagabond", "thumbnailUrl": "/api/v1/manga/55/thumbnail", "inLibrary": true},
							},
							{
								"id": 903, "name": "Ch. 375", "chapterNumber": 375, "fetchedAt": olderUnix,
								"isRead": false, "isDownloaded": false,
								"manga": map[string]any{"id": 77, "title": "Berserk", "thumbnailUrl": "/api/v1/manga/77/thumbnail", "inLibrary": true},
							},
						},
					},
				},
			})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	st.Timezone = "UTC"
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/updates")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(body)

	for _, want := range []string{
		`href="/updates"`, "Recent Updates", "Update Library",
		"Today", "Yesterday", "Thu, 19 Nov 2026",
		"One Piece", "Ch. 1100", `/manga/10`, `/read/901`, `action="/manga/10/chapter/901"`,
		"Vagabond", "Ch. 327", "saved",
		"Berserk", "Ch. 375",
		"/api/suwayomi/image?path=%2Fapi%2Fv1%2Fmanga%2F10%2Fthumbnail",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("/updates missing %q", want)
		}
	}
}

func TestSourceMigrationCopiesReadChaptersAndRemovesOld(t *testing.T) {
	var (
		mu          sync.Mutex
		inLibrary   = map[int64]bool{55: true, 88: false}
		markedRead  = map[int64]bool{}
		addedCatIDs []int
	)
	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&gql)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.0.0"}}})
		case strings.Contains(gql.Query, "ListSources"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"sources": map[string]any{
						"nodes": []map[string]any{
							{"id": "101", "name": "MangaDex", "lang": "en", "supportsLatest": true},
							{"id": "202", "name": "Comick", "lang": "en", "supportsLatest": true},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "FetchSourceManga"):
			srcID, _ := gql.Variables["source"].(string)
			if srcID == "202" {
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"fetchSourceManga": map[string]any{
							"hasNextPage": false,
							"mangas": []map[string]any{
								{"id": 88, "title": "Vagabond", "thumbnailUrl": "/api/v1/manga/88/thumbnail", "inLibrary": inLibrary[88], "sourceId": "202"},
							},
						},
					},
				})
				return
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"fetchSourceManga": map[string]any{
						"hasNextPage": false,
						"mangas": []map[string]any{
							{"id": 55, "title": "Vagabond", "thumbnailUrl": "/api/v1/manga/55/thumbnail", "inLibrary": inLibrary[55], "sourceId": "101"},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "GetManga"):
			id := int64(gql.Variables["id"].(float64))
			if id == 55 {
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"manga": map[string]any{
							"id": 55, "title": "Vagabond", "inLibrary": inLibrary[55], "sourceId": "101",
							"source":     map[string]any{"id": "101", "name": "MangaDex", "lang": "en"},
							"categories": map[string]any{"nodes": []map[string]any{{"id": 4, "name": "Seinen"}}},
							"chapters": map[string]any{
								"nodes": []map[string]any{
									{"id": 501, "name": "Ch 1", "chapterNumber": 1, "isRead": true, "sourceOrder": 1},
									{"id": 502, "name": "Ch 2", "chapterNumber": 2, "isRead": true, "sourceOrder": 2},
									{"id": 503, "name": "Ch 3", "chapterNumber": 3, "isRead": false, "sourceOrder": 3},
								},
							},
						},
					},
				})
				return
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"manga": map[string]any{
						"id": 88, "title": "Vagabond", "inLibrary": inLibrary[88], "sourceId": "202",
						"source":     map[string]any{"id": "202", "name": "Comick", "lang": "en"},
						"categories": map[string]any{"nodes": []any{}},
						"chapters": map[string]any{
							"nodes": []map[string]any{
								{"id": 801, "name": "Chapter 1", "chapterNumber": 1, "isRead": false, "sourceOrder": 1},
								{"id": 802, "name": "Chapter 2", "chapterNumber": 2, "isRead": false, "sourceOrder": 2},
								{"id": 803, "name": "Chapter 3", "chapterNumber": 3, "isRead": false, "sourceOrder": 3},
							},
						},
					},
				},
			})
		case strings.Contains(gql.Query, "UpdateManga("):
			id := int64(gql.Variables["id"].(float64))
			inLib, _ := gql.Variables["inLibrary"].(bool)
			inLibrary[id] = inLib
			writeJSON(w, map[string]any{"data": map[string]any{"updateManga": map[string]any{"manga": map[string]any{"id": id, "inLibrary": inLib}}}})
		case strings.Contains(gql.Query, "UpdateMangaCategories"):
			if patch, ok := gql.Variables["patch"].(map[string]any); ok {
				if adds, ok := patch["addToCategories"].([]any); ok {
					for _, v := range adds {
						addedCatIDs = append(addedCatIDs, int(v.(float64)))
					}
				}
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateMangaCategories": map[string]any{"manga": map[string]any{"id": 88}}}})
		case strings.Contains(gql.Query, "FetchChapters"):
			writeJSON(w, map[string]any{"data": map[string]any{"fetchChapters": map[string]any{"chapters": []any{}}}})
		case strings.Contains(gql.Query, "MarkChapterRead"):
			id := int64(gql.Variables["id"].(float64))
			isRead, _ := gql.Variables["isRead"].(bool)
			markedRead[id] = isRead
			writeJSON(w, map[string]any{"data": map[string]any{"updateChapter": map[string]any{"chapter": map[string]any{"id": id, "isRead": isRead}}}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	st := cfg.get()
	st.SuwayomiURL = mockSuw.URL
	_ = cfg.save(st)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// 1. GET /manga/55/migrate lists Comick candidate (id 88) and excludes self (id 55)
	resp, err := http.Get(srv.URL + "/manga/55/migrate")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(body)
	if !strings.Contains(html, "Comick") || !strings.Contains(html, `value="88"`) || strings.Contains(html, `value="55"`) {
		t.Fatalf("/manga/55/migrate unexpected candidates: %s", html)
	}

	// 2. POST /manga/55/migrate migrates to 88, copies ch 1 & 2 read state, copies category 4, and removes 55 from library
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	mResp, err := client.PostForm(srv.URL+"/manga/55/migrate", url.Values{"targetMangaId": {"88"}})
	if err != nil || mResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("migrate POST failed: %v %v", err, mResp)
	}
	mResp.Body.Close()
	if loc := mResp.Header.Get("Location"); !strings.HasPrefix(loc, "/manga/88?note=") {
		t.Fatalf("expected redirect to /manga/88?note=..., got %q", loc)
	}

	mu.Lock()
	defer mu.Unlock()
	if !inLibrary[88] || inLibrary[55] {
		t.Fatalf("expected target 88 in library and old 55 removed, got %+v", inLibrary)
	}
	if !markedRead[801] || !markedRead[802] || markedRead[803] {
		t.Fatalf("expected chapters 801 & 802 marked read and 803 untouched, got %+v", markedRead)
	}
	if len(addedCatIDs) != 1 || addedCatIDs[0] != 4 {
		t.Fatalf("expected category 4 copied to migrated manga, got %v", addedCatIDs)
	}
}


