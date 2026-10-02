package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/notify"
	"geotracker/internal/store"
)

// Immich integration: photos taken during a day appear on the timeline. All requests go
// through the server, so the user's Immich API key never reaches the browser.
// ponytail: Immich only; PhotoPrism can follow the same three calls if asked for.

type immichCfg struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

type integrations struct {
	Immich immichCfg `json:"immich"`
}

func (s *Server) loadIntegrations(ctx context.Context, userID int64) integrations {
	var in integrations
	if raw, err := s.db.Integrations(ctx, userID); err == nil {
		json.Unmarshal([]byte(raw), &in)
	}
	return in
}

var immichHTTP = notify.UserHTTP

func immichDo(ctx context.Context, c immichCfg, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return immichHTTP.Do(req)
}

func (s *Server) getIntegrations(w http.ResponseWriter, r *http.Request, u *store.User) {
	in := s.loadIntegrations(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"immich": map[string]any{"url": in.Immich.URL, "connected": in.Immich.URL != "" && in.Immich.APIKey != ""}})
}

// putIntegrations saves (and verifies) the Immich connection; an empty URL disconnects.
func (s *Server) putIntegrations(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req integrations
	if !decode(w, r, &req) {
		return
	}
	cur := s.loadIntegrations(r.Context(), u.ID)
	c := immichCfg{URL: strings.TrimRight(strings.TrimSpace(req.Immich.URL), "/"), APIKey: strings.TrimSpace(req.Immich.APIKey)}
	if c.APIKey == "" || c.APIKey == secretMask {
		c.APIKey = cur.Immich.APIKey
	}
	if c.URL != "" {
		if pu, err := url.Parse(c.URL); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			fail(w, http.StatusBadRequest, "enter your Immich address, e.g. https://photos.example.com")
			return
		}
		resp, err := immichDo(r.Context(), c, http.MethodGet, "/api/users/me", nil)
		if err != nil {
			fail(w, http.StatusBadGateway, "could not reach Immich: "+err.Error())
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fail(w, http.StatusBadRequest, fmt.Sprintf("Immich refused the API key (HTTP %d)", resp.StatusCode))
			return
		}
	} else {
		c = immichCfg{}
	}
	b, _ := json.Marshal(integrations{Immich: c})
	if err := s.db.SetIntegrations(r.Context(), u.ID, string(b)); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "integration.immich", map[bool]string{true: "connected", false: "disconnected"}[c.URL != ""])
	s.getIntegrations(w, r, u)
}

type photo struct {
	ID  string   `json:"id"`
	TS  int64    `json:"ts"`
	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`
}

// photos lists the user's Immich photos taken in [from, to] (at most 200).
func (s *Server) photos(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok || r.URL.Query().Get("from") == "" || r.URL.Query().Get("to") == "" {
		fail(w, http.StatusBadRequest, "from and to are required")
		return
	}
	c := s.loadIntegrations(r.Context(), u.ID).Immich
	if c.URL == "" {
		writeJSON(w, http.StatusOK, []photo{})
		return
	}
	resp, err := immichDo(r.Context(), c, http.MethodPost, "/api/search/metadata", map[string]any{
		"takenAfter":  time.UnixMilli(from).UTC().Format(time.RFC3339),
		"takenBefore": time.UnixMilli(to).UTC().Format(time.RFC3339),
		"type":        "IMAGE",
		"size":        200,
		"withExif":    true,
		"order":       "asc",
	})
	if err != nil {
		fail(w, http.StatusBadGateway, "could not reach Immich")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(w, http.StatusBadGateway, fmt.Sprintf("Immich answered HTTP %d", resp.StatusCode))
		return
	}
	var res struct {
		Assets struct {
			Items []struct {
				ID            string `json:"id"`
				FileCreatedAt string `json:"fileCreatedAt"`
				ExifInfo      *struct {
					DateTimeOriginal string   `json:"dateTimeOriginal"`
					Latitude         *float64 `json:"latitude"`
					Longitude        *float64 `json:"longitude"`
				} `json:"exifInfo"`
			} `json:"items"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		fail(w, http.StatusBadGateway, "unexpected answer from Immich")
		return
	}
	out := make([]photo, 0, len(res.Assets.Items))
	for _, a := range res.Assets.Items {
		p := photo{ID: a.ID}
		when := a.FileCreatedAt
		if a.ExifInfo != nil {
			if a.ExifInfo.DateTimeOriginal != "" {
				when = a.ExifInfo.DateTimeOriginal
			}
			if a.ExifInfo.Latitude != nil && a.ExifInfo.Longitude != nil && (*a.ExifInfo.Latitude != 0 || *a.ExifInfo.Longitude != 0) {
				p.Lat, p.Lon = a.ExifInfo.Latitude, a.ExifInfo.Longitude
			}
		}
		p.TS, _ = geo.ParseTime(when)
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

var assetID = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// photoThumb proxies a thumbnail (size=thumbnail) or larger preview (size=preview).
func (s *Server) photoThumb(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("pid")
	size := r.URL.Query().Get("size")
	if size != "preview" {
		size = "thumbnail"
	}
	c := s.loadIntegrations(r.Context(), u.ID).Immich
	if !assetID.MatchString(id) || c.URL == "" {
		http.NotFound(w, r)
		return
	}
	resp, err := immichDo(r.Context(), c, http.MethodGet, "/api/assets/"+id+"/thumbnail?size="+size, nil)
	if err != nil {
		fail(w, http.StatusBadGateway, "could not reach Immich")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	io.Copy(w, resp.Body)
}
