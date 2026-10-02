package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/store"
	"geotracker/internal/transfer"
)

// Background exports: the request only queues a job. A worker writes the file under
// DATA_DIR/exports, and the person downloads it when it is ready. Files expire after a week.

const exportKeep = 7 * 24 * time.Hour

func (s *Server) exportPath(id int64, format string) string {
	return filepath.Join(s.cfg.DataDir, "exports", strconv.FormatInt(id, 10)+"."+transfer.Formats[format][0])
}

func (s *Server) listExports(w http.ResponseWriter, r *http.Request, u *store.User) {
	es, err := s.db.Exports(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, es)
}

func (s *Server) createExport(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Format string `json:"format"`
		From   *int64 `json:"from"`
		To     *int64 `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, ok := transfer.Formats[req.Format]; !ok || (req.From == nil) != (req.To == nil) || (req.From != nil && *req.From > *req.To) {
		fail(w, http.StatusBadRequest, "choose a format (native, gpx, geojson, csv) and a valid range")
		return
	}
	id, err := s.db.CreateExport(r.Context(), u.ID, req.Format, req.From, req.To)
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, fmt.Sprintf("you already have %d exports in progress; wait for one to finish", store.MaxActiveExports))
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	s.wake(s.wakeExports)
	s.audit(r, u.ID, "export", req.Format)
	writeJSON(w, http.StatusAccepted, map[string]int64{"id": id})
}

func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request, u *store.User) {
	e, err := s.db.ExportByID(r.Context(), u.ID, pathID(r))
	if err != nil || e.Status != "done" {
		fail(w, http.StatusNotFound, "this export is not ready or has expired")
		return
	}
	f, err := os.Open(s.exportPath(e.ID, e.Format))
	if err != nil {
		fail(w, http.StatusNotFound, "this export has expired; create a new one")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		internal(w, r, err)
		return
	}
	name := fmt.Sprintf("geotracker-%s.%s", time.UnixMilli(e.CreatedAt).Format("2006-01-02"), transfer.Formats[e.Format][0])
	w.Header().Set("Content-Type", transfer.Formats[e.Format][1])
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func (s *Server) deleteExport(w http.ResponseWriter, r *http.Request, u *store.User) {
	e, err := s.db.ExportByID(r.Context(), u.ID, pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "export not found")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	if e.Status == "running" {
		fail(w, http.StatusConflict, "this export is still being built")
		return
	}
	if err := s.db.DeleteExport(r.Context(), u.ID, e.ID); err != nil {
		internal(w, r, err)
		return
	}
	os.Remove(s.exportPath(e.ID, e.Format))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) exportLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		e, err := s.db.NextExport(ctx)
		if err != nil {
			slog.Error("export: next", "err", err)
		}
		if e != nil {
			s.runExport(ctx, e)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.wakeExports:
		}
	}
}

func (s *Server) runExport(ctx context.Context, e *store.Export) {
	e.Status = "running"
	s.db.UpdateExport(ctx, e)
	s.hub.Publish(e.UserID, "export", e)
	err := s.buildExport(ctx, e)
	if errors.Is(err, context.Canceled) {
		return // shutting down; RequeueExports resumes it on next start
	}
	now := time.Now().UnixMilli()
	e.FinishedAt = &now
	e.Status = "done"
	if err != nil {
		e.Status, e.Error = "failed", err.Error()
		slog.Error("export failed", "id", e.ID, "err", err)
	}
	s.db.UpdateExport(ctx, e)
	s.hub.Publish(e.UserID, "export", e)
	if e.Status == "done" {
		s.notifyUser(e.UserID, "import_done", "Your export is ready", "Open GeoTracker → Settings → Import & export to download it. It is kept for 7 days.")
	}
}

func (s *Server) buildExport(ctx context.Context, e *store.Export) error {
	u, err := s.db.UserByID(ctx, e.UserID)
	if err != nil {
		return err
	}
	from, to := int64(0), int64(1<<62)
	if e.From != nil {
		from, to = *e.From, *e.To
	}
	if err := os.MkdirAll(filepath.Join(s.cfg.DataDir, "exports"), 0o750); err != nil {
		return err
	}
	final := s.exportPath(e.ID, e.Format)
	tmp := final + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = s.writeExport(ctx, f, u, e.Format, from, to)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, final)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if st, err := os.Stat(final); err == nil {
		e.Size = st.Size()
	}
	return nil
}

// cleanExports removes expired exports and files whose row is gone (user deleted, restore).
func (s *Server) cleanExports(ctx context.Context) {
	live, err := s.db.PruneExports(ctx, exportKeep)
	if err != nil {
		slog.Error("export cleanup", "err", err)
		return
	}
	entries, err := os.ReadDir(filepath.Join(s.cfg.DataDir, "exports"))
	if err != nil {
		return
	}
	for _, en := range entries {
		id, _ := strconv.ParseInt(strings.SplitN(en.Name(), ".", 2)[0], 10, 64)
		if !live[id] {
			os.Remove(filepath.Join(s.cfg.DataDir, "exports", en.Name()))
		}
	}
}
