package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"geotracker/internal/store"
)

// OpenID Connect single sign-on (Authelia, Authentik, Keycloak, Pocket ID, Zitadel, …).
// Authorization-code flow with PKCE and nonce; users are linked by the stable `sub` claim.

type oidcClient struct {
	key      string
	provider *oidc.Provider
	cfg      oauth2.Config
	verifier *oidc.IDTokenVerifier
}

type oidcCache struct {
	mu sync.Mutex
	c  *oidcClient
}

const oidcCookie = "gt_oidc"

func (s *Server) oidcCallbackURL(r *http.Request) string {
	return s.baseURL(r) + "/api/v1/auth/oidc/callback"
}

func (s *Server) oidcClient(r *http.Request, st map[string]string) (*oidcClient, error) {
	if st["oidc_enabled"] != "true" || st["oidc_issuer"] == "" || st["oidc_client_id"] == "" {
		return nil, errors.New("single sign-on is not configured")
	}
	redirect := s.oidcCallbackURL(r)
	key := strings.Join([]string{st["oidc_issuer"], st["oidc_client_id"], st["oidc_client_secret"], redirect}, "\x00")
	s.oidc.mu.Lock()
	defer s.oidc.mu.Unlock()
	if s.oidc.c != nil && s.oidc.c.key == key {
		return s.oidc.c, nil
	}
	// Discovery happens once per configuration. Background context: the provider keeps it for key refreshes.
	p, err := oidc.NewProvider(oidc.ClientContext(context.Background(), &http.Client{Timeout: 15 * time.Second}), st["oidc_issuer"])
	if err != nil {
		return nil, err
	}
	c := &oidcClient{
		key:      key,
		provider: p,
		cfg: oauth2.Config{ClientID: st["oidc_client_id"], ClientSecret: st["oidc_client_secret"], Endpoint: p.Endpoint(),
			RedirectURL: redirect, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}},
		verifier: p.Verifier(&oidc.Config{ClientID: st["oidc_client_id"]}),
	}
	s.oidc.c = c
	return c, nil
}

// authMethods tells the login screen (and apps) which sign-in options exist.
func (s *Server) authMethods(w http.ResponseWriter, r *http.Request) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"password":   true,
		"oidc":       st["oidc_enabled"] == "true" && st["oidc_issuer"] != "",
		"oidc_label": st["oidc_label"],
	})
}

type oidcState struct {
	State, Nonce, Verifier, Next string
}

func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

func loginError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusFound)
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	c, err := s.oidcClient(r, st)
	if err != nil {
		loginError(w, r, "Single sign-on is unavailable: "+err.Error())
		return
	}
	state := oidcState{State: newToken(), Nonce: newToken(), Verifier: oauth2.GenerateVerifier(), Next: safeNext(r.URL.Query().Get("next"))}
	b, _ := json.Marshal(state)
	http.SetCookie(w, &http.Cookie{
		Name: oidcCookie, Value: base64.RawURLEncoding.EncodeToString(b), Path: "/api/v1/auth/oidc", MaxAge: 600,
		// Lax is required: the identity provider sends the browser back with a top-level GET.
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r) || strings.HasPrefix(s.cfg.BaseURL, "https://"),
	})
	http.Redirect(w, r, c.cfg.AuthCodeURL(state.State, oidc.Nonce(state.Nonce), oauth2.S256ChallengeOption(state.Verifier)), http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var state oidcState
	if ck, err := r.Cookie(oidcCookie); err == nil {
		if b, err := base64.RawURLEncoding.DecodeString(ck.Value); err == nil {
			json.Unmarshal(b, &state)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Path: "/api/v1/auth/oidc", MaxAge: -1})
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		loginError(w, r, "Sign-in was cancelled or refused: "+strings.TrimSpace(e+" "+q.Get("error_description")))
		return
	}
	if state.State == "" || q.Get("state") != state.State {
		loginError(w, r, "The sign-in link expired. Please try again.")
		return
	}
	st, err := s.db.Settings(ctx)
	if err != nil {
		internal(w, r, err)
		return
	}
	c, err := s.oidcClient(r, st)
	if err != nil {
		loginError(w, r, err.Error())
		return
	}
	tok, err := c.cfg.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(state.Verifier))
	if err != nil {
		loginError(w, r, "The identity provider rejected the sign-in: "+err.Error())
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := c.verifier.Verify(ctx, raw)
	if err != nil || idt.Nonce != state.Nonce {
		loginError(w, r, "Could not verify the identity token.")
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
		Name          string `json:"name"`
		Username      string `json:"preferred_username"`
	}
	idt.Claims(&claims)
	if claims.Email == "" { // some providers only put email in userinfo
		if ui, err := c.provider.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
			ui.Claims(&claims)
		}
	}

	u, err := s.db.UserByOIDC(ctx, idt.Subject)
	if errors.Is(err, store.ErrNotFound) {
		u, err = s.linkOrCreateOIDCUser(ctx, st, idt.Subject, claims.Email, claims.EmailVerified, firstNonEmpty(claims.Name, claims.Username))
		if err != nil {
			loginError(w, r, err.Error())
			return
		}
	} else if err != nil {
		internal(w, r, err)
		return
	}
	if u.Disabled {
		loginError(w, r, "This account is disabled.")
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "login", "sso")
	http.Redirect(w, r, state.Next, http.StatusFound)
}

// linkOrCreateOIDCUser links an existing account with the same verified email, or
// creates one when auto-registration is enabled.
func (s *Server) linkOrCreateOIDCUser(ctx context.Context, st map[string]string, sub, email string, verified *bool, name string) (*store.User, error) {
	email, ok := cleanEmail(email)
	if !ok {
		return nil, errors.New("Your identity provider did not share an email address.")
	}
	if verified != nil && !*verified {
		return nil, errors.New("Your email address is not verified at your identity provider.")
	}
	if u, err := s.db.UserByEmail(ctx, email); err == nil {
		// Linking takes over an existing (maybe admin) account, so the provider must vouch
		// for the address; an absent claim is not enough.
		if verified == nil {
			return nil, errors.New("Your identity provider did not confirm that " + email + " is verified, so it can't be linked to the existing account. Turn on the email_verified claim at the provider.")
		}
		return u, s.db.LinkOIDC(ctx, u.ID, sub)
	}
	if st["oidc_auto_register"] != "true" {
		return nil, errors.New("There is no account for " + email + ". Ask your admin to create one.")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	return s.db.CreateOIDCUser(ctx, email, name, sub)
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
