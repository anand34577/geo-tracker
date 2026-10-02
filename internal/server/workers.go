package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/store"
	"geotracker/internal/timeline"
	"geotracker/internal/transfer"
)

// Run starts the background workers. They are goroutines with state in SQLite, so no
// Redis or queue broker is needed and interrupted work resumes after a restart (ADR-003).
func (s *Server) Run(ctx context.Context) error {
	if err := s.db.MarkAllDirty(ctx); err != nil {
		return err
	}
	if err := s.db.RequeueImports(ctx); err != nil {
		return err
	}
	go s.timelineLoop(ctx)
	go s.importLoop(ctx)
	go s.maintenanceLoop(ctx)
	return nil
}

func (s *Server) wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// ── Timeline & geocoding ─────────────────────────────────────

func (s *Server) timelineLoop(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	failed := map[int64]bool{} // visits the geocoder could not resolve this run
	for {
		s.processTimelines(ctx)
		s.geocodeVisits(ctx, failed)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.wakeTimeline:
			time.Sleep(3 * time.Second) // debounce bursts of points
		}
	}
}

func (s *Server) processTimelines(ctx context.Context) {
	dirty, err := s.db.TakeDirty(ctx)
	if err != nil {
		slog.Error("timeline: take dirty", "err", err)
		return
	}
	for userID, from := range dirty {
		start := time.Now()
		if err := s.rebuild(ctx, userID, from); err != nil {
			slog.Error("timeline rebuild failed", "user", userID, "err", err)
			s.db.MarkDirty(ctx, userID, from) // retry next round
			continue
		}
		slog.Debug("timeline rebuilt", "user", userID, "ms", time.Since(start).Milliseconds())
		s.hub.Publish(userID, "timeline", map[string]int64{"from": from})
	}
}

func (s *Server) rebuild(ctx context.Context, userID, dirtyFrom int64) error {
	start, err := s.db.WindowStart(ctx, userID, dirtyFrom)
	if err != nil {
		return err
	}
	b := timeline.New(timeline.Default)
	err = s.db.ForEachPoint(ctx, userID, start, math.MaxInt64, 0, func(p geo.Point) error {
		b.Add(p)
		return nil
	})
	if err != nil {
		return err
	}
	b.Finish()
	return s.db.ReplaceTimeline(ctx, userID, start, b.Visits, b.Trips)
}

// geocodeVisits names visits from the shared cache first, then the configured provider
// at most ~1 request/second (Nominatim's usage policy).
func (s *Server) geocodeVisits(ctx context.Context, failed map[int64]bool) {
	st, err := s.db.Settings(ctx)
	if err != nil || st["geocoder"] == "none" {
		return
	}
	visits, err := s.db.UngeocodedVisits(ctx, 500)
	if err != nil {
		slog.Error("geocode: list", "err", err)
		return
	}
	if len(failed) > 10_000 {
		clear(failed)
	}
	calls, errs := 0, 0
	for _, v := range visits {
		if failed[v.ID] || ctx.Err() != nil {
			continue
		}
		if id, ok, err := s.db.GeocodeNear(ctx, v.Lat, v.Lon, 40); err == nil && ok {
			s.db.SetVisitGeocode(ctx, v.ID, id)
			continue
		}
		if calls >= 25 || errs >= 3 { // stay inside the 30 s loop; resume next round
			continue
		}
		calls++
		select {
		case <-ctx.Done():
			return
		case <-time.After(1100 * time.Millisecond):
		}
		res, err := s.geo.Reverse(ctx, st["geocoder"], st["geocoder_url"], v.Lat, v.Lon)
		if err != nil {
			errs++
			failed[v.ID] = true
			slog.Warn("geocode failed", "provider", st["geocoder"], "err", err)
			continue
		}
		id, err := s.db.InsertGeocode(ctx, store.Geocode{Lat: v.Lat, Lon: v.Lon, Provider: st["geocoder"],
			Name: res.Name, Address: res.Address, City: res.City, CountryCode: res.CountryCode})
		if err == nil {
			s.db.SetVisitGeocode(ctx, v.ID, id)
		}
	}
}

// ── Imports ──────────────────────────────────────────────────

func (s *Server) importLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		imp, err := s.db.NextImport(ctx)
		if err != nil {
			slog.Error("import: next", "err", err)
		}
		if imp != nil {
			s.runImport(ctx, imp)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.wakeImports:
		}
	}
}

func (s *Server) runImport(ctx context.Context, imp *store.Import) {
	imp.Status = "running"
	s.db.UpdateImport(ctx, imp)
	start := time.Now()
	batch := make([]geo.Point, 0, 5000)
	total := 0
	flush := func() error {
		n, err := s.db.InsertPoints(ctx, imp.UserID, 0, imp.ID, batch)
		imp.Added += n
		total += len(batch)
		imp.Duplicates = total - imp.Added
		batch = batch[:0]
		if err == nil {
			s.db.UpdateImport(ctx, imp)
			s.hub.Publish(imp.UserID, "import", imp)
		}
		return err
	}
	format, err := transfer.Import(s.importPath(imp.ID), imp.Filename, transfer.Sink{
		Point: func(p geo.Point) error {
			if !p.Valid() {
				imp.Rejected++
				return nil
			}
			batch = append(batch, p)
			if len(batch) == cap(batch) {
				return flush()
			}
			return nil
		},
		Place: func(p transfer.Place) error {
			return s.db.CreatePlace(ctx, imp.UserID, &store.Place{Name: p.Name, Icon: p.Icon, Lat: p.Lat, Lon: p.Lon, Radius: p.Radius})
		},
	})
	if err == nil {
		err = flush()
	}
	if errors.Is(err, context.Canceled) {
		return // shutting down; RequeueImports resumes it on next start
	}
	imp.Format, imp.Status = format, "done"
	if err != nil {
		imp.Status, imp.Error = "failed", err.Error()
	}
	now := time.Now().UnixMilli()
	imp.FinishedAt = &now
	s.db.UpdateImport(ctx, imp)
	s.hub.Publish(imp.UserID, "import", imp)
	os.Remove(s.importPath(imp.ID))
	s.wake(s.wakeTimeline)
	if imp.Status == "done" {
		s.notifyUser(imp.UserID, "import_done", "Import finished: "+imp.Filename, fmt.Sprintf("%d points added, %d duplicates skipped.", imp.Added, imp.Duplicates))
	} else {
		s.notifyUser(imp.UserID, "import_done", "Import failed: "+imp.Filename, imp.Error)
	}
	slog.Info("import finished", "id", imp.ID, "status", imp.Status, "added", imp.Added, "dupes", imp.Duplicates, "s", int(time.Since(start).Seconds()))
}

// ── Scheduled backups & housekeeping ─────────────────────────

func (s *Server) maintenanceLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	alertedDay := "" // one backup-failure alert per day, not one per retry
	housekeeping := func() {
		if n, err := s.db.ApplyRetention(ctx); err != nil {
			slog.Error("retention", "err", err)
		} else if n > 0 {
			slog.Info("retention removed old points", "points", n)
		}
		s.db.PruneAudit(ctx)
	}
	housekeeping()
	for i := 1; ; i++ {
		if i%360 == 0 { // every 6 hours
			housekeeping()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := s.scheduledBackup(ctx); err != nil {
			slog.Error("scheduled backup failed", "err", err)
			if today := time.Now().Format(time.DateOnly); alertedDay != today {
				alertedDay = today
				s.notifyAdmins("backup_failed", "GeoTracker backup failed", "The scheduled backup failed: "+err.Error()+"\nCheck disk space and the server log.")
			}
		}
		s.checkSilentDevices(ctx)
	}
}

func (s *Server) scheduledBackup(ctx context.Context) error {
	st, err := s.db.Settings(ctx)
	if err != nil {
		return err
	}
	hour, _ := strconv.Atoi(st["backup_hour"])
	now := time.Now()
	if hour < 0 || now.Hour() != hour {
		return nil
	}
	prefix := "geotracker-" + now.Format("20060102")
	files, _ := s.db.Backups()
	for _, f := range files {
		if strings.HasPrefix(f.Name, prefix) {
			return nil // already done today
		}
	}
	name := fmt.Sprintf("%s-%s.db.gz", prefix, now.Format("150405"))
	if err := s.db.Backup(ctx, filepath.Join(s.cfg.DataDir, "backups", name)); err != nil {
		return err
	}
	keep, _ := strconv.Atoi(st["backup_keep"])
	s.db.Rotate(max(1, keep))
	s.db.PurgeSessions(ctx)
	slog.Info("scheduled backup written", "file", name)
	return nil
}
