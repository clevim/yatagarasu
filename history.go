package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// readEvent is one "finished on the e-reader" moment. It is kept separately
// from the entries because an entry disappears as soon as Karasu prunes it —
// which happens immediately after the chapter is read — and the whole point of
// a history is to outlive the thing it describes.
type readEvent struct {
	At          int64  `json:"t"`
	ChapterID   int64  `json:"chapterId"`
	MangaTitle  string `json:"mangaTitle"`
	ChapterName string `json:"chapterName"`
}

func (s shelf) historyPath() string { return filepath.Join(s.dir, "history.jsonl") }

// appendHistory is best effort: losing a stats line must never fail a sync.
func (s shelf) appendHistory(e Entry, at int64) {
	f, err := os.OpenFile(s.historyPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("yata: cannot append history: %v", err)
		return
	}
	defer f.Close()
	line, err := json.Marshal(readEvent{
		At:          at,
		ChapterID:   e.ChapterID,
		MangaTitle:  e.MangaTitle,
		ChapterName: e.ChapterName,
	})
	if err != nil {
		return
	}
	f.Write(append(line, '\n'))
}

// history returns every read event, oldest first.
//
// ponytail: reads the whole file. At a chapter a day that is ~40 KB a year, so
// the day it needs an index is the day it also needs SQLite — not before.
func (s shelf) history() []readEvent {
	f, err := os.Open(s.historyPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var events []readEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev readEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err == nil {
			events = append(events, ev)
		}
	}
	return events
}
