package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
