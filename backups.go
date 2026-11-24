package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type BackupInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"createdAt"`
}

func (s shelf) backupsDir() string {
	return filepath.Join(s.dir, "backups")
}

func sanitizeBackupID(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	res := strings.Trim(b.String(), "._")
	if res == "" {
		return "karasu.tachibk"
	}
	if !strings.HasSuffix(res, ".tachibk") && !strings.HasSuffix(res, ".proto.gz") {
		res += ".tachibk"
	}
	return res
}

func (s shelf) validBackupPath(id string) (string, bool) {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", false
	}
	return filepath.Join(s.backupsDir(), id), true
}

func (s shelf) listBackups() ([]BackupInfo, error) {
	dir := s.backupsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]BackupInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			ID:        e.Name(),
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s shelf) pruneBackups() {
	maxKeep := s.cfg.get().MaxBackups
	if maxKeep <= 0 {
		maxKeep = 10
	}
	backups, err := s.listBackups()
	if err != nil || len(backups) <= maxKeep {
		return
	}
	for _, b := range backups[maxKeep:] {
		if p, ok := s.validBackupPath(b.ID); ok {
			os.Remove(p)
		}
	}
}

func (s shelf) handleListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := s.listBackups()
	if err != nil {
		http.Error(w, "list backups failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"backups": backups})
}

func (s shelf) handleUploadBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUpload)
	if err := os.MkdirAll(s.backupsDir(), 0o755); err != nil {
		http.Error(w, "cannot create backups dir", http.StatusInternalServerError)
		return
	}

	var (
		src      io.Reader
		filename string
	)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "bad multipart body", http.StatusBadRequest)
			return
		}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, "bad multipart body", http.StatusBadRequest)
				return
			}
			if p.FormName() == "file" {
				filename = p.FileName()
				src = p
				defer p.Close()
				break
			}
			p.Close()
		}
		if src == nil {
			http.Error(w, "missing file part", http.StatusBadRequest)
			return
		}
	} else {
		filename = r.Header.Get("X-Backup-Filename")
		src = r.Body
	}

	id := fmt.Sprintf("%d_%s", now(), sanitizeBackupID(filename))
	dstPath, ok := s.validBackupPath(id)
	if !ok {
		http.Error(w, "invalid backup name", http.StatusBadRequest)
		return
	}

	tmp, err := os.CreateTemp(s.backupsDir(), "backup-*.tmp")
	if err != nil {
		http.Error(w, "cannot store backup", http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	n, err := io.Copy(tmp, src)
	tmp.Close()
	if err != nil || n == 0 {
		os.Remove(tmpPath)
		http.Error(w, "empty or interrupted backup upload", http.StatusBadRequest)
		return
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		http.Error(w, "cannot finalize backup", http.StatusInternalServerError)
		return
	}
	s.pruneBackups()

	writeJSON(w, BackupInfo{
		ID:        id,
		Name:      id,
		Size:      n,
		CreatedAt: now(),
	})
}

func (s shelf) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := s.validBackupPath(id)
	if !ok {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(p); err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, id))
	http.ServeFile(w, r, p)
}

func (s shelf) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := s.validBackupPath(id)
	if !ok {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	os.Remove(p)
	w.WriteHeader(http.StatusNoContent)
}

const maxBackupUpload = 256 << 20 // 256 MiB
