package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func parseMultipartGraphQLUpload(r *http.Request) (query string, vars map[string]any, filename string, fileBytes []byte, ok bool) {
	ct := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return "", nil, "", nil, false
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	var opsRaw []byte
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, "", nil, false
		}
		switch p.FormName() {
		case "operations":
			opsRaw, _ = io.ReadAll(p)
		case "0":
			filename = p.FileName()
			fileBytes, _ = io.ReadAll(p)
		}
		p.Close()
	}
	if len(opsRaw) == 0 {
		return "", nil, "", nil, false
	}
	var ops struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(opsRaw, &ops); err != nil {
		return "", nil, "", nil, false
	}
	return ops.Query, ops.Variables, filename, fileBytes, true
}

func TestTachibkProtobufRoundTripAndPackedCategories(t *testing.T) {
	cats := []SyncCategory{
		{Name: "Reading", Order: 1, Flags: 64},
		{Name: "Seinen", Order: 4, Flags: 0},
	}
	mangas := []SyncManga{
		{
			SourceID:     2499283573021220255,
			URL:          "/manga/berserk-uuid",
			Title:        "Berserk",
			ThumbnailURL: "https://uploads.mangadex.org/covers/berserk.jpg",
			Favorite:     true,
			Categories:   []string{"Reading", "Seinen"},
			UpdatedAt:    1700000123456,
			Chapters: []SyncChapter{
				{
					URL:           "/chapter/1",
					Name:          "The Black Swordsman",
					ChapterNumber: 1,
					Scanlator:     "Band of the Hawk",
					Read:          true,
					Bookmark:      true,
					LastPageRead:  44,
					UpdatedAt:     1700000100000,
				},
				{
					URL:           "/chapter/12.5",
					Name:          "Bonus Chapter 12.5",
					ChapterNumber: 12.5,
					Read:          false,
					Bookmark:      false,
					LastPageRead:  7,
					UpdatedAt:     1700000200000,
				},
				{
					URL:           "/chapter/12.3",
					Name:          "Decimal Float32 Precision",
					ChapterNumber: 12.3,
					Read:          false,
					Bookmark:      true,
					LastPageRead:  0,
					UpdatedAt:     1700000300000,
				},
			},
		},
	}
	repos := []string{
		"https://raw.githubusercontent.com/keiyoushi/extensions/repo",
		"https://example.org/custom/index.min.json",
	}

	tachibk, err := encodeSyncPayloadToTachibk(cats, mangas, repos...)
	if err != nil {
		t.Fatalf("encodeSyncPayloadToTachibk failed: %v", err)
	}
	if len(tachibk) < 2 || tachibk[0] != 0x1f || tachibk[1] != 0x8b {
		t.Fatalf("expected gzipped .tachibk magic header 1f 8b")
	}

	decoded, err := decodeTachibkToSyncPayload(tachibk)
	if err != nil {
		t.Fatalf("decodeTachibkToSyncPayload failed: %v", err)
	}

	if len(decoded.Categories) != 2 || decoded.Categories[0].Name != "Reading" || decoded.Categories[0].Flags != 64 || decoded.Categories[1].Name != "Seinen" || decoded.Categories[1].Order != 4 {
		t.Fatalf("categories mismatch: %+v", decoded.Categories)
	}
	if len(decoded.ExtensionRepos) != 2 || decoded.ExtensionRepos[0] != repos[0] || decoded.ExtensionRepos[1] != repos[1] {
		t.Fatalf("extensionRepos mismatch: %+v", decoded.ExtensionRepos)
	}
	if len(decoded.Mangas) != 1 {
		t.Fatalf("expected 1 manga, got %d", len(decoded.Mangas))
	}
	gotM := decoded.Mangas[0]
	if gotM.SourceID != mangas[0].SourceID || gotM.URL != mangas[0].URL || gotM.Title != "Berserk" ||
		gotM.ThumbnailURL != mangas[0].ThumbnailURL || !gotM.Favorite || gotM.UpdatedAt != mangas[0].UpdatedAt {
		t.Fatalf("manga metadata mismatch: %+v", gotM)
	}
	if len(gotM.Categories) != 2 || gotM.Categories[0] != "Reading" || gotM.Categories[1] != "Seinen" {
		t.Fatalf("manga categories mismatch: %+v", gotM.Categories)
	}
	if len(gotM.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(gotM.Chapters))
	}
	if gotM.Chapters[0].ChapterNumber != 1 || !gotM.Chapters[0].Read || !gotM.Chapters[0].Bookmark || gotM.Chapters[0].LastPageRead != 44 || gotM.Chapters[0].Scanlator != "Band of the Hawk" {
		t.Fatalf("chapter 0 mismatch: %+v", gotM.Chapters[0])
	}
	if gotM.Chapters[1].ChapterNumber != 12.5 || gotM.Chapters[1].Read || gotM.Chapters[1].LastPageRead != 7 {
		t.Fatalf("chapter 1 mismatch: %+v", gotM.Chapters[1])
	}
	if gotM.Chapters[2].ChapterNumber != 12.3 || !gotM.Chapters[2].Bookmark {
		t.Fatalf("chapter 2 float32 mismatch: %+v", gotM.Chapters[2])
	}

	// Verify packed categories ((17<<3)|2) and omitted favorite (defaults to true in Karasu/Mihon) also decode cleanly.
	var packedCats []byte
	packedCats = append(packedCats, 1, 4) // packed varints [1, 4]
	var rawManga []byte
	rawManga = appendProtoVarint(rawManga, 1, 777)
	rawManga = appendProtoString(rawManga, 2, "/m/vinland")
	rawManga = appendProtoString(rawManga, 3, "Vinland Saga")
	rawManga = appendProtoBytes(rawManga, 17, packedCats) // packed field 17!
	// field 100 (favorite) intentionally omitted -> must default to true

	var rawCat1, rawCat2 []byte
	rawCat1 = appendProtoString(rawCat1, 1, "Action")
	rawCat1 = appendProtoVarint(rawCat1, 2, 1)
	rawCat2 = appendProtoString(rawCat2, 1, "Historical")
	rawCat2 = appendProtoVarint(rawCat2, 2, 4)

	var rawRoot []byte
	rawRoot = appendProtoBytes(rawRoot, 1, rawManga)
	rawRoot = appendProtoBytes(rawRoot, 2, rawCat1)
	rawRoot = appendProtoBytes(rawRoot, 2, rawCat2)

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(rawRoot)
	_ = gw.Close()

	packedDecoded, err := decodeTachibkToSyncPayload(gzBuf.Bytes())
	if err != nil {
		t.Fatalf("packed decode failed: %v", err)
	}
	if len(packedDecoded.Mangas) != 1 || !packedDecoded.Mangas[0].Favorite {
		t.Fatalf("expected omitted favorite to default to true, got %+v", packedDecoded.Mangas)
	}
	if strings.Join(packedDecoded.Mangas[0].Categories, ",") != "Action,Historical" {
		t.Fatalf("expected packed categories [Action,Historical], got %v", packedDecoded.Mangas[0].Categories)
	}
}

func TestNormalizeSuwayomiRepoURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://raw.githubusercontent.com/keiyoushi/extensions/repo", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json"},
		{"  https://raw.githubusercontent.com/keiyoushi/extensions/repo/  ", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json"},
		{"https://raw.githubusercontent.com/keiyoushi/extensions/repo?token=abc#frag", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json"},
		{"https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json"},
		{"https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.pb", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.pb"},
		{"https://raw.githubusercontent.com/keiyoushi/extensions/repo/repo.json?x=1", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/repo.json"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeSuwayomiRepoURL(tc.in); got != tc.want {
			t.Errorf("normalizeSuwayomiRepoURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBackupRestoreWebAndAPI(t *testing.T) {
	var (
		mu              sync.Mutex
		restoredFiles   []string
		restoredPayloads []*SyncPayload
		addedStores     []string
		fetchExtCalls   int
		statusChecks    int
	)

	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q, _, fname, fbytes, ok := parseMultipartGraphQLUpload(r); ok {
			if strings.Contains(q, "RestoreBackup") {
				decoded, err := decodeTachibkToSyncPayload(fbytes)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				mu.Lock()
				restoredFiles = append(restoredFiles, fname)
				restoredPayloads = append(restoredPayloads, decoded)
				mu.Unlock()
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"restoreBackup": map[string]any{
							"id": "restore-job-1",
							"status": map[string]any{
								"state":         "RESTORING_MANGA",
								"mangaProgress": 0,
								"totalManga":    len(decoded.Mangas),
							},
						},
					},
				})
				return
			}
		}

		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&gql)
		mu.Lock()
		defer mu.Unlock()

		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.4.2373"}}})
		case strings.Contains(gql.Query, "RestoreStatus"):
			statusChecks++
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"restoreStatus": map[string]any{
						"state":         "SUCCESS",
						"mangaProgress": 2,
						"totalManga":    2,
					},
				},
			})
		case strings.Contains(gql.Query, "ListCategories"):
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": []any{}}}})
		case strings.Contains(gql.Query, "CreateCategory"):
			writeJSON(w, map[string]any{"data": map[string]any{"createCategory": map[string]any{"category": map[string]any{"id": 1, "name": gql.Variables["name"]}}}})
		case strings.Contains(gql.Query, "GetExtensionRepos"):
			var storeNodes []map[string]any
			for _, u := range addedStores {
				storeNodes = append(storeNodes, map[string]any{"indexUrl": u})
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"extensionStores": map[string]any{"nodes": storeNodes},
					"settings":        map[string]any{"extensionRepos": []string{}},
				},
			})
		case strings.Contains(gql.Query, "AddExtensionStore"):
			if u, ok := gql.Variables["url"].(string); ok {
				addedStores = append(addedStores, u)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"addExtensionStore": map[string]any{"extensionStore": map[string]any{"indexUrl": gql.Variables["url"]}}}})
		case strings.Contains(gql.Query, "FetchExtensions"):
			fetchExtCalls++
			writeJSON(w, map[string]any{"data": map[string]any{"fetchExtensions": map[string]any{"extensions": []any{}}}})
		case strings.Contains(gql.Query, "ListLibrary"):
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": []any{}}, "mangas": map[string]any{"nodes": []any{}}}})
		case strings.Contains(gql.Query, "LibraryUpdateStatus"):
			writeJSON(w, map[string]any{"data": map[string]any{"libraryUpdateStatus": map[string]any{"jobsInfo": map[string]any{"isRunning": false}}}})
		default:
			writeJSON(w, map[string]any{"data": map[string]any{}})
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

	tachibk, err := encodeSyncPayloadToTachibk(
		[]SyncCategory{{Name: "Favorites", Order: 1}},
		[]SyncManga{
			{
				SourceID:     101,
				URL:          "/manga/dandadan",
				Title:        "Dandadan",
				ThumbnailURL: "https://cdn.example.org/covers/dandadan.jpg",
				Favorite:     true,
				Categories:   []string{"Favorites"},
				Chapters: []SyncChapter{
					{URL: "/ch/1", Name: "Ch. 1", ChapterNumber: 1, Read: true, LastPageRead: 50},
				},
			},
			{
				SourceID: 101,
				URL:      "/manga/sakamoto",
				Title:    "Sakamoto Days",
				Favorite: true,
			},
		},
		"https://raw.githubusercontent.com/keiyoushi/extensions/repo",
	)
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/backups", bytes.NewReader(tachibk))
	req.Header.Set("X-Backup-Filename", "full_library.tachibk")
	upResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var upInfo BackupInfo
	_ = json.NewDecoder(upResp.Body).Decode(&upInfo)
	upResp.Body.Close()
	if upInfo.ID == "" {
		t.Fatalf("expected backup id")
	}

	// 1. /backups page renders "Restore to Library" button.
	pageResp, err := http.Get(srv.URL + "/backups")
	if err != nil {
		t.Fatal(err)
	}
	pageHTML, _ := io.ReadAll(pageResp.Body)
	pageResp.Body.Close()
	if !strings.Contains(string(pageHTML), "Restore to Library") || !strings.Contains(string(pageHTML), "/backups/"+upInfo.ID+"/restore") {
		t.Fatalf("/backups missing Restore to Library button:\n%s", string(pageHTML))
	}

	// 2. Web form POST /backups/{id}/restore imports into sync/state.json AND Suwayomi restoreBackup.
	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	formResp, err := noRedir.PostForm(srv.URL+"/backups/"+upInfo.ID+"/restore", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	formResp.Body.Close()
	loc := formResp.Header.Get("Location")
	if formResp.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "2+manga+imported") {
		t.Fatalf("unexpected web restore redirect: status=%d loc=%q", formResp.StatusCode, loc)
	}

	// 3. JSON API POST /api/backups/{id}/restore also succeeds and returns structured metadata.
	apiResp, err := http.Post(srv.URL+"/api/backups/"+upInfo.ID+"/restore", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var apiBody struct {
		OK               bool   `json:"ok"`
		ImportedMangas   int    `json:"importedMangas"`
		SuwayomiRestored bool   `json:"suwayomiRestored"`
		SuwayomiState    string `json:"suwayomiState"`
	}
	_ = json.NewDecoder(apiResp.Body).Decode(&apiBody)
	apiResp.Body.Close()
	if !apiBody.OK || apiBody.ImportedMangas != 2 || !apiBody.SuwayomiRestored || apiBody.SuwayomiState != "SUCCESS" {
		t.Fatalf("unexpected API restore response: %+v", apiBody)
	}

	// Verify sync/state.json holds both mangas, category, chapter read state, ThumbnailURL, and repo.
	persisted := s.loadSyncState()
	if len(persisted.Mangas) != 2 || persisted.Categories["Favorites"].Name != "Favorites" {
		t.Fatalf("sync state did not receive restored backup: %+v", persisted)
	}
	dandadan := persisted.Mangas["101|/manga/dandadan"]
	if dandadan.ThumbnailURL != "https://cdn.example.org/covers/dandadan.jpg" || len(dandadan.Chapters) != 1 || !dandadan.Chapters[0].Read {
		t.Fatalf("restored dandadan state mismatch: %+v", dandadan)
	}

	// Verify Suwayomi received normalized repo URL, fetchExtensions, restoreBackup, and RestoreStatus polling.
	mu.Lock()
	if len(restoredPayloads) != 2 || statusChecks < 2 {
		t.Fatalf("expected 2 Suwayomi restoreBackup calls and status checks, got %d / %d", len(restoredPayloads), statusChecks)
	}
	if len(addedStores) != 1 || addedStores[0] != "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json" || fetchExtCalls != 1 {
		t.Fatalf("expected normalized extensionStore added once and fetched, got stores=%v fetches=%d", addedStores, fetchExtCalls)
	}
	mu.Unlock()

	// Verify /library renders external https:// cover URL and CSP allows https: in img-src.
	libResp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatal(err)
	}
	libBody, _ := io.ReadAll(libResp.Body)
	libResp.Body.Close()
	if !strings.Contains(libResp.Header.Get("Content-Security-Policy"), "img-src 'self' data: https:") {
		t.Fatalf("expected CSP img-src to allow https:, got %q", libResp.Header.Get("Content-Security-Policy"))
	}
	if !strings.Contains(string(libBody), "/api/cover?url=https%3A%2F%2Fcdn.example.org%2Fcovers%2Fdandadan.jpg") {
		t.Fatalf("expected proxied cover URL rendered on /library when Suwayomi cover is absent")
	}
}

func TestSyncReconcileMaterializerAndSuwayomiV24ExtensionStores(t *testing.T) {
	type suwManga struct {
		ID           int64
		SourceID     int64
		URL          string
		Title        string
		ChapterCount int
	}

	var (
		mu              sync.Mutex
		stores          = []string{"https://raw.githubusercontent.com/keiyoushi/extensions/repo"} // initially bare like yata.clevs.dev.br!
		availExts       []map[string]any
		installedPkgs   = map[string]bool{}
		suwLibrary      []suwManga
		fetchExtCount   int
		fetchMangaIDs   []int64
		fetchChapterIDs []int64
		uploadedAPKName string
		uploadedAPKData []byte
	)

	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q, _, fname, fbytes, ok := parseMultipartGraphQLUpload(r); ok {
			mu.Lock()
			defer mu.Unlock()
			switch {
			case strings.Contains(q, "RestoreBackup"):
				decoded, err := decodeTachibkToSyncPayload(fbytes)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				for i, m := range decoded.Mangas {
					suwLibrary = append(suwLibrary, suwManga{
						ID:           int64(100 + len(suwLibrary) + i),
						SourceID:     m.SourceID,
						URL:          m.URL,
						Title:        m.Title,
						ChapterCount: len(m.Chapters),
					})
				}
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"restoreBackup": map[string]any{
							"id": "sync-restore-1",
							"status": map[string]any{
								"state":         "SUCCESS",
								"mangaProgress": len(decoded.Mangas),
								"totalManga":    len(decoded.Mangas),
							},
						},
					},
				})
				return
			case strings.Contains(q, "InstallExternalExtension"):
				uploadedAPKName = fname
				uploadedAPKData = fbytes
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"installExternalExtension": map[string]any{
							"extension": map[string]any{"pkgName": "eu.kanade.tachiyomi.extension.en.custom"},
						},
					},
				})
				return
			}
		}

		var gql struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&gql)
		mu.Lock()
		defer mu.Unlock()

		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.4.2373"}}})
		case strings.Contains(gql.Query, "GetExtensionRepos"):
			var nodes []map[string]any
			for _, s := range stores {
				nodes = append(nodes, map[string]any{"indexUrl": s})
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"extensionStores": map[string]any{"nodes": nodes},
					"settings":        map[string]any{"extensionRepos": []string{}},
				},
			})
		case strings.Contains(gql.Query, "AddExtensionStore"):
			u, _ := gql.Variables["url"].(string)
			found := false
			for _, existing := range stores {
				if existing == u {
					found = true
					break
				}
			}
			if !found && u != "" {
				stores = append(stores, u)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"addExtensionStore": map[string]any{"extensionStore": map[string]any{"indexUrl": u}}}})
		case strings.Contains(gql.Query, "RemoveExtensionStore"):
			u, _ := gql.Variables["url"].(string)
			var kept []string
			for _, existing := range stores {
				if existing != u {
					kept = append(kept, existing)
				}
			}
			stores = kept
			writeJSON(w, map[string]any{"data": map[string]any{"removeExtensionStore": map[string]any{"clientMutationId": "1"}}})
		case strings.Contains(gql.Query, "FetchExtensions"):
			fetchExtCount++
			for _, s := range stores {
				if strings.HasSuffix(s, "/index.min.json") {
					availExts = []map[string]any{
						{
							"pkgName":     "eu.kanade.tachiyomi.extension.en.mangadex",
							"name":        "MangaDex",
							"lang":        "en",
							"versionName": "1.4.200",
							"versionCode": 200,
						},
					}
				}
			}
			writeJSON(w, map[string]any{"data": map[string]any{"fetchExtensions": map[string]any{"extensions": availExts}}})
		case strings.Contains(gql.Query, "ListExtensions"):
			var nodes []map[string]any
			for _, e := range availExts {
				pkg, _ := e["pkgName"].(string)
				cp := map[string]any{}
				for k, v := range e {
					cp[k] = v
				}
				cp["isInstalled"] = installedPkgs[pkg]
				nodes = append(nodes, cp)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"extensions": map[string]any{"nodes": nodes}}})
		case strings.Contains(gql.Query, "UpdateExtension"):
			pkg, _ := gql.Variables["pkgName"].(string)
			if inst, _ := gql.Variables["install"].(bool); inst && pkg != "" {
				installedPkgs[pkg] = true
			}
			writeJSON(w, map[string]any{"data": map[string]any{"updateExtension": map[string]any{"extension": map[string]any{"pkgName": pkg, "isInstalled": installedPkgs[pkg]}}}})
		case strings.Contains(gql.Query, "PullLibrary"):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"categories": map[string]any{"nodes": []any{}},
					"mangas":     map[string]any{"nodes": []any{}},
					"settings":   map[string]any{"extensionRepos": []string{}},
				},
			})
		case strings.Contains(gql.Query, "ListCategories"):
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": []any{}}}})
		case strings.Contains(gql.Query, "CreateCategory"):
			writeJSON(w, map[string]any{"data": map[string]any{"createCategory": map[string]any{"category": map[string]any{"id": 1, "name": gql.Variables["name"]}}}})
		case strings.Contains(gql.Query, "ExistingLibraryKeys"):
			var nodes []map[string]any
			for _, m := range suwLibrary {
				nodes = append(nodes, map[string]any{"sourceId": "2499", "url": m.URL})
			}
			writeJSON(w, map[string]any{"data": map[string]any{"mangas": map[string]any{"nodes": nodes}}})
		case strings.Contains(gql.Query, "ListLibrary"):
			var nodes []map[string]any
			for _, m := range suwLibrary {
				nodes = append(nodes, map[string]any{
					"id":            m.ID,
					"sourceId":      "2499",
					"url":           m.URL,
					"title":         m.Title,
					"thumbnailUrl":  "/api/v1/manga/100/thumbnail",
					"unreadCount":   m.ChapterCount,
					"downloadCount": 0,
					"chapters":      map[string]any{"totalCount": m.ChapterCount},
					"categories":    map[string]any{"nodes": []any{}},
				})
			}
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": []any{}}, "mangas": map[string]any{"nodes": nodes}}})
		case strings.Contains(gql.Query, "FetchManga("):
			if idf, ok := gql.Variables["id"].(float64); ok {
				fetchMangaIDs = append(fetchMangaIDs, int64(idf))
			}
			writeJSON(w, map[string]any{"data": map[string]any{"fetchManga": map[string]any{"manga": map[string]any{"id": gql.Variables["id"]}}}})
		case strings.Contains(gql.Query, "FetchChapters("):
			if idf, ok := gql.Variables["mangaId"].(float64); ok {
				fetchChapterIDs = append(fetchChapterIDs, int64(idf))
			}
			writeJSON(w, map[string]any{"data": map[string]any{"fetchChapters": map[string]any{"chapters": []any{}}}})
		default:
			writeJSON(w, map[string]any{"data": map[string]any{}})
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

	// 1. Visiting GET /browse?tab=extensions when a bare repo exists and 0 extensions are cached
	// upgrades the bare repo to .../index.min.json, calls FetchExtensions, and populates AvailableExts immediately!
	extPageResp, err := http.Get(srv.URL + "/browse?tab=extensions")
	if err != nil {
		t.Fatal(err)
	}
	extPageHTML, _ := io.ReadAll(extPageResp.Body)
	extPageResp.Body.Close()
	if !strings.Contains(string(extPageHTML), "MangaDex") || !strings.Contains(string(extPageHTML), "1.4.200") {
		t.Fatalf("expected /browse?tab=extensions to auto-fetch and populate MangaDex:\n%s", string(extPageHTML))
	}

	// 2. POST /api/sync/reconcile from Karasu materializes missing favorite manga into Suwayomi
	// synchronously AND installs Karasu's extensions + initializes 0-chapter manga in background!
	syncReq := SyncPayload{
		DeviceID:       "karasu-android",
		Categories:     []SyncCategory{{Name: "Reading", Order: 1}},
		ExtensionRepos: []string{"https://example.org/second-repo"},
		InstalledExtensions: []SyncExtension{
			{PkgName: "eu.kanade.tachiyomi.extension.en.mangadex", Name: "MangaDex", Lang: "en", VersionCode: 200},
		},
		Mangas: []SyncManga{
			{
				SourceID:   2499,
				URL:        "/manga/frieren",
				Title:      "Sousou no Frieren",
				Favorite:   true,
				Categories: []string{"Reading"},
				UpdatedAt:  5000,
			},
		},
	}
	reqBytes, _ := json.Marshal(syncReq)
	recResp, err := http.Post(srv.URL+"/api/sync/reconcile", "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		t.Fatal(err)
	}
	recResp.Body.Close()
	if recResp.StatusCode != http.StatusOK {
		t.Fatalf("reconcile status %d", recResp.StatusCode)
	}

	// Immediately after reconcile returns, Suwayomi already has Frieren materialized!
	mu.Lock()
	matLen := len(suwLibrary)
	var hasSecondNorm bool
	for _, stURL := range stores {
		if stURL == "https://example.org/second-repo/index.min.json" {
			hasSecondNorm = true
		}
	}
	mu.Unlock()
	if matLen != 1 {
		t.Fatalf("expected Frieren synchronously materialized into Suwayomi during reconcile, got %d mangas", matLen)
	}
	if !hasSecondNorm {
		t.Fatalf("expected bare repo https://example.org/second-repo normalized to /index.min.json in extensionStores")
	}

	// Wait briefly for background extension auto-install + 0-chapter manga fetch.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := installedPkgs["eu.kanade.tachiyomi.extension.en.mangadex"] && len(fetchMangaIDs) == 1 && len(fetchChapterIDs) == 1
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	mu.Lock()
	if !installedPkgs["eu.kanade.tachiyomi.extension.en.mangadex"] {
		t.Errorf("expected background worker to auto-install MangaDex extension")
	}
	if len(fetchMangaIDs) != 1 || len(fetchChapterIDs) != 1 {
		t.Errorf("expected background worker to initialize newly materialized 0-chapter manga: fetchManga=%v fetchChapters=%v", fetchMangaIDs, fetchChapterIDs)
	}
	mu.Unlock()

	// 3. GraphQL multipart InstallExternalExtension works via POST /browse/extensions/install-apk
	var apkBuf bytes.Buffer
	mw := multipart.NewWriter(&apkBuf)
	part, _ := mw.CreateFormFile("file", "gql-ext.apk")
	_, _ = part.Write([]byte("APK-GRAPHQL-UPLOAD"))
	_ = mw.Close()

	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	apkReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/browse/extensions/install-apk", &apkBuf)
	apkReq.Header.Set("Content-Type", mw.FormDataContentType())
	apkResp, err := noRedir.Do(apkReq)
	if err != nil || apkResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("install-apk failed: %v %v", err, apkResp)
	}
	apkResp.Body.Close()

	mu.Lock()
	if uploadedAPKName != "gql-ext.apk" || string(uploadedAPKData) != "APK-GRAPHQL-UPLOAD" {
		t.Fatalf("expected GraphQL InstallExternalExtension multipart upload, got name=%q bytes=%q", uploadedAPKName, string(uploadedAPKData))
	}
	mu.Unlock()
}

func TestLibraryUpdaterDefaultsStatusBannerStopAndEmptyWarning(t *testing.T) {
	var (
		mu                  sync.Mutex
		suwMangaCount       int
		userSettingsSet     bool
		updaterRunning      bool
		totalJobs           int
		finishedJobs        int
		skippedMangas       int
		runningTitles       []string
		stopCalled          int
		materializedOnUpd   int
	)

	mockSuw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q, _, _, fbytes, ok := parseMultipartGraphQLUpload(r); ok && strings.Contains(q, "RestoreBackup") {
			decoded, err := decodeTachibkToSyncPayload(fbytes)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			materializedOnUpd += len(decoded.Mangas)
			suwMangaCount += len(decoded.Mangas)
			mu.Unlock()
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"restoreBackup": map[string]any{
						"id":     "upd-mat-1",
						"status": map[string]any{"state": "SUCCESS", "mangaProgress": len(decoded.Mangas), "totalManga": len(decoded.Mangas)},
					},
				},
			})
			return
		}

		var gql struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&gql)
		mu.Lock()
		defer mu.Unlock()

		buildStatus := func() map[string]any {
			var mUpdates []map[string]any
			for i, title := range runningTitles {
				mUpdates = append(mUpdates, map[string]any{
					"status": "RUNNING",
					"manga":  map[string]any{"id": i + 1, "title": title},
				})
			}
			return map[string]any{
				"jobsInfo": map[string]any{
					"isRunning":              updaterRunning,
					"totalJobs":              totalJobs,
					"finishedJobs":           finishedJobs,
					"skippedMangasCount":     skippedMangas,
					"skippedCategoriesCount": 0,
				},
				"mangaUpdates": mUpdates,
			}
		}

		switch {
		case strings.Contains(gql.Query, "aboutServer"):
			writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.4.2373"}}})
		case strings.Contains(gql.Query, "EnsureUpdaterSettings"):
			// Simulate newer Suwayomi where excludeUnreadChapters moved to setUserSettings!
			writeJSON(w, map[string]any{"errors": []map[string]any{{"message": "Validation error: FieldUndefined excludeUnreadChapters on SettingsInput"}}})
		case strings.Contains(gql.Query, "EnsureUpdaterUserSettings"):
			if strings.Contains(gql.Query, "excludeUnreadChapters: false") && strings.Contains(gql.Query, "excludeNotStarted: false") {
				userSettingsSet = true
			}
			writeJSON(w, map[string]any{"data": map[string]any{"setUserSettings": map[string]any{"userSettings": map[string]any{"excludeNotStarted": false}}}})
		case strings.Contains(gql.Query, "ExistingLibraryKeys"):
			writeJSON(w, map[string]any{"data": map[string]any{"mangas": map[string]any{"nodes": []any{}}}})
		case strings.Contains(gql.Query, "ListLibrary"):
			var nodes []map[string]any
			for i := 0; i < suwMangaCount; i++ {
				nodes = append(nodes, map[string]any{
					"id":           i + 1,
					"sourceId":     "101",
					"url":          "/m/" + string(rune('a'+i)),
					"title":        "Manga",
					"thumbnailUrl": "",
					"chapters":     map[string]any{"totalCount": 10},
					"categories":   map[string]any{"nodes": []any{}},
				})
			}
			writeJSON(w, map[string]any{"data": map[string]any{"categories": map[string]any{"nodes": []any{}}, "mangas": map[string]any{"nodes": nodes}}})
		case strings.Contains(gql.Query, "UpdateLibrary"):
			writeJSON(w, map[string]any{"data": map[string]any{"updateLibrary": map[string]any{"updateStatus": buildStatus()}}})
		case strings.Contains(gql.Query, "LibraryUpdateStatus"):
			writeJSON(w, map[string]any{"data": map[string]any{"libraryUpdateStatus": buildStatus()}})
		case strings.Contains(gql.Query, "StopUpdate"):
			stopCalled++
			updaterRunning = false
			writeJSON(w, map[string]any{"data": map[string]any{"updateStop": map[string]any{"clientMutationId": "stopped"}}})
		case strings.Contains(gql.Query, "RecentUpdates"):
			writeJSON(w, map[string]any{"data": map[string]any{"chapters": map[string]any{"nodes": []any{}}}})
		default:
			writeJSON(w, map[string]any{"data": map[string]any{}})
		}
	}))
	defer mockSuw.Close()

	dir := t.TempDir()
	cfg := loadConfig(dir)
	s := shelf{dir: dir, cfg: cfg}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	// When Suwayomi is NOT configured, GET /api/suwayomi/update-status returns {"isRunning":false}.
	unconfResp, err := http.Get(srv.URL + "/api/suwayomi/update-status")
	if err != nil {
		t.Fatal(err)
	}
	var unconfSt SuwayomiUpdateStatus
	_ = json.NewDecoder(unconfResp.Body).Decode(&unconfSt)
	unconfResp.Body.Close()
	if unconfSt.IsRunning {
		t.Fatalf("expected isRunning=false when Suwayomi not configured")
	}

	// Configure Suwayomi.
	stCfg := cfg.get()
	stCfg.SuwayomiURL = mockSuw.URL
	_ = cfg.save(stCfg)

	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// 1. Empty library -> redirects with helpful problem= message!
	emptyResp, err := noRedir.PostForm(srv.URL+"/downloads/control", url.Values{
		"action":   {"update-library"},
		"redirect": {"/library"},
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyResp.Body.Close()
	if loc := emptyResp.Header.Get("Location"); !strings.Contains(loc, "problem=Your+library+is+empty") {
		t.Fatalf("expected empty library warning redirect, got %q", loc)
	}

	// 2. Seed sync/state.json with 2 favorite mangas not in Suwayomi yet, and configure mock updater to queue 2 jobs.
	if err := s.saveSyncState(persistedSyncState{
		Categories: map[string]SyncCategory{},
		Mangas: map[string]SyncManga{
			"101|/m/1": {SourceID: 101, URL: "/m/1", Title: "Kingdom", Favorite: true},
			"101|/m/2": {SourceID: 101, URL: "/m/2", Title: "Real", Favorite: true},
		},
		DeletedAt:  map[string]int64{},
		Repos:      map[string]bool{},
		Extensions: map[string]SyncExtension{},
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	updaterRunning = true
	totalJobs = 2
	finishedJobs = 1
	runningTitles = []string{"Kingdom"}
	mu.Unlock()

	updResp, err := noRedir.PostForm(srv.URL+"/downloads/control", url.Values{
		"action":   {"update-library"},
		"redirect": {"/library"},
	})
	if err != nil {
		t.Fatal(err)
	}
	updResp.Body.Close()
	loc := updResp.Header.Get("Location")
	if !strings.Contains(loc, "2+manga+queued") || !strings.Contains(loc, "updating=1") {
		t.Fatalf("expected queued count + updating=1 redirect, got %q", loc)
	}

	mu.Lock()
	if materializedOnUpd != 2 || !userSettingsSet {
		t.Fatalf("expected 2 mangas materialized before update and excludeUnreadChapters/excludeNotStarted set false: mat=%d userSet=%v", materializedOnUpd, userSettingsSet)
	}
	mu.Unlock()

	// 3. GET /api/suwayomi/update-status returns live progress JSON, and /library + /updates show #libUpdateBanner.
	stResp, err := http.Get(srv.URL + "/api/suwayomi/update-status")
	if err != nil {
		t.Fatal(err)
	}
	var liveSt SuwayomiUpdateStatus
	_ = json.NewDecoder(stResp.Body).Decode(&liveSt)
	stResp.Body.Close()
	if !liveSt.IsRunning || liveSt.TotalJobs != 2 || liveSt.FinishedJobs != 1 || len(liveSt.RunningMangas) != 1 || liveSt.RunningMangas[0] != "Kingdom" {
		t.Fatalf("unexpected live update-status JSON: %+v", liveSt)
	}

	for _, pagePath := range []string{"/library", "/updates"} {
		pr, err := http.Get(srv.URL + pagePath)
		if err != nil {
			t.Fatal(err)
		}
		pb, _ := io.ReadAll(pr.Body)
		pr.Body.Close()
		html := string(pb)
		for _, want := range []string{`id="libUpdateBanner"`, `data-running="true"`, "Updating library: 1 / 2", "Kingdom", `value="stop-update"`, `/api/suwayomi/update-status`} {
			if !strings.Contains(html, want) {
				t.Errorf("%s missing %q", pagePath, want)
			}
		}
	}

	// 4. POST /downloads/control action=stop-update stops the running update.
	stopResp, err := noRedir.PostForm(srv.URL+"/downloads/control", url.Values{
		"action":   {"stop-update"},
		"redirect": {"/updates"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stopResp.Body.Close()
	if !strings.Contains(stopResp.Header.Get("Location"), "Library+update+stopped") {
		t.Fatalf("unexpected stop-update redirect: %q", stopResp.Header.Get("Location"))
	}
	mu.Lock()
	sc := stopCalled
	mu.Unlock()
	if sc != 1 {
		t.Fatalf("expected StopUpdate called once, got %d", sc)
	}

	// 5. Skipped mangas case (TotalJobs == 0 && !IsRunning && SkippedMangasCount > 0) reports filter note.
	mu.Lock()
	updaterRunning = false
	totalJobs = 0
	finishedJobs = 0
	skippedMangas = 3
	runningTitles = nil
	mu.Unlock()
	// Clear sync state so materializeMissingMangas doesn't need to run again.
	_ = s.saveSyncState(persistedSyncState{})

	skipResp, err := noRedir.PostForm(srv.URL+"/downloads/control", url.Values{
		"action":   {"update-library"},
		"redirect": {"/library"},
	})
	if err != nil {
		t.Fatal(err)
	}
	skipResp.Body.Close()
	if loc := skipResp.Header.Get("Location"); !strings.Contains(loc, "3+manga+skipped+by+filters") {
		t.Fatalf("expected skipped mangas note redirect, got %q", loc)
	}
}

func TestEntrypointAllInOneIdempotentUpdaterDefaults(t *testing.T) {
	// Verify entrypoint-allinone.sh syntax and idempotent server.conf updater defaults.
	if out, err := exec.Command("bash", "-n", "entrypoint-allinone.sh").CombinedOutput(); err != nil {
		t.Fatalf("bash -n entrypoint-allinone.sh failed: %v\n%s", err, out)
	}
	rawScript, err := os.ReadFile("entrypoint-allinone.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"server.excludeUnreadChapters = false", "server.excludeNotStarted = false"} {
		if !strings.Contains(string(rawScript), want) {
			t.Fatalf("entrypoint-allinone.sh missing %q", want)
		}
	}

	// Simulate existing server.conf without excludeUnreadChapters and run the config block twice.
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "server.conf")
	if err := os.WriteFile(confPath, []byte("server.ip = \"127.0.0.1\"\nserver.port = 4567\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snippet := `
SUWAYOMI_CONF="$1"
if ! grep -q 'server\.excludeUnreadChapters' "${SUWAYOMI_CONF}"; then
  printf '\nserver.excludeUnreadChapters = false\n' >> "${SUWAYOMI_CONF}"
fi
if ! grep -q 'server\.excludeNotStarted' "${SUWAYOMI_CONF}"; then
  printf 'server.excludeNotStarted = false\n' >> "${SUWAYOMI_CONF}"
fi
`
	for range 2 {
		if out, err := exec.Command("bash", "-c", snippet, "_", confPath).CombinedOutput(); err != nil {
			t.Fatalf("snippet failed: %v %s", err, out)
		}
	}
	gotConf, _ := os.ReadFile(confPath)
	if strings.Count(string(gotConf), "server.excludeUnreadChapters") != 1 || strings.Count(string(gotConf), "server.excludeNotStarted") != 1 {
		t.Fatalf("expected each setting appended exactly once idempotently, got:\n%s", string(gotConf))
	}
}

// Ensure concurrent sync + library reads have zero data races under -race.
func TestConcurrentSyncAndLibraryReadsRaceFree(t *testing.T) {
	var okFlag atomic.Bool
	okFlag.Store(true)

	dir := t.TempDir()
	s := shelf{dir: dir, cfg: loadConfig(dir)}
	srv := httptest.NewServer(router(s))
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := SyncPayload{
				DeviceID: "dev",
				Mangas: []SyncManga{
					{SourceID: 1, URL: "/m/1", Title: "Test", ThumbnailURL: "https://example.org/c.jpg", Favorite: true, UpdatedAt: int64(1000 + idx)},
				},
			}
			b, _ := json.Marshal(payload)
			r1, err := http.Post(srv.URL+"/api/sync/reconcile", "application/json", bytes.NewReader(b))
			if err != nil {
				okFlag.Store(false)
				return
			}
			r1.Body.Close()

			r2, err := http.Get(srv.URL + "/library")
			if err != nil {
				okFlag.Store(false)
				return
			}
			_, _ = io.Copy(io.Discard, r2.Body)
			r2.Body.Close()
		}(i)
	}
	wg.Wait()
	if !okFlag.Load() {
		t.Fatalf("concurrent requests failed")
	}
}
