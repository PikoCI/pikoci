package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
)

// --- Auth Methods (unauthenticated) ---

type GetAuthMethodsResponse struct {
	Err  string             `json:"error,omitempty"`
	Data *oauthprovider.AuthMethods `json:"data,omitempty"`
}

func (r GetAuthMethodsResponse) Error() string { return r.Err }

func getAuthMethods(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		methods, err := s.GetAuthMethods(r.Context())
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetAuthMethodsResponse{Data: methods, Err: errs}, w)
	}
}

// --- OAuth Start (unauthenticated, returns redirect) ---

func oauthStart(s pikoci.Service, externalURL string, stateStore *pikoci.OAuthStateStore, ts []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		canonical := vars["canonical"]

		p, err := s.GetOAuthProvider(r.Context(), canonical)
		if err != nil || !p.Enabled {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}

		callbackURL := fmt.Sprintf("%s/auth/oauth/%s/callback", strings.TrimRight(externalURL, "/"), canonical)

		flowCfg, err := pikoci.BuildOAuth2Config(r.Context(), p, callbackURL)
		if err != nil {
			http.Error(w, "failed to build OAuth config: "+err.Error(), http.StatusInternalServerError)
			return
		}

		state, err := pikoci.GenerateState()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		oauthState := &pikoci.OAuthState{
			CreatedAt: time.Now(),
		}

		// Check for account linking
		linkParam := r.URL.Query().Get("link")
		if linkParam == "true" {
			// Verify the user is authenticated
			reqToken := r.Header.Get("Authorization")
			if reqToken == "" {
				// Try cookie or query param for link flow
				reqToken = "Bearer " + r.URL.Query().Get("token")
			}
			splitToken := strings.Split(reqToken, " ")
			if len(splitToken) == 2 {
				token, err := jwt.Parse(splitToken[1], func(token *jwt.Token) (any, error) {
					return ts, nil
				}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
				if err == nil {
					claims, ok := token.Claims.(jwt.MapClaims)
					if ok {
						if userClaim, ok := claims["user"].(map[string]interface{}); ok {
							if idFloat, ok := userClaim["id"].(float64); ok {
								oauthState.UserID = uint32(idFloat)
								oauthState.Link = true
							}
						}
					}
				}
			}
		}

		stateStore.Set(state, oauthState)

		authURL := flowCfg.OAuth2.AuthCodeURL(state)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// --- OAuth Callback (unauthenticated, returns redirect to SPA) ---

func oauthCallback(s pikoci.Service, externalURL string, stateStore *pikoci.OAuthStateStore, ts []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		canonical := vars["canonical"]

		code := r.URL.Query().Get("code")
		stateParam := r.URL.Query().Get("state")

		if code == "" || stateParam == "" {
			redirectWithError(w, r, externalURL, "missing code or state")
			return
		}

		oauthState, ok := stateStore.Get(stateParam)
		if !ok {
			redirectWithError(w, r, externalURL, "invalid or expired state")
			return
		}

		p, err := s.GetOAuthProvider(r.Context(), canonical)
		if err != nil || !p.Enabled {
			redirectWithError(w, r, externalURL, "provider not found")
			return
		}

		callbackURL := fmt.Sprintf("%s/auth/oauth/%s/callback", strings.TrimRight(externalURL, "/"), canonical)

		flowCfg, err := pikoci.BuildOAuth2Config(r.Context(), p, callbackURL)
		if err != nil {
			redirectWithError(w, r, externalURL, "failed to build OAuth config")
			return
		}

		token, err := flowCfg.OAuth2.Exchange(r.Context(), code)
		if err != nil {
			redirectWithError(w, r, externalURL, "token exchange failed: "+err.Error())
			return
		}

		subject, email, fullName, username, err := pikoci.FetchOAuthUserInfo(r.Context(), p, token, flowCfg)
		if err != nil {
			redirectWithError(w, r, externalURL, "failed to fetch user info: "+err.Error())
			return
		}

		ctx := r.Context()

		// Account linking flow
		if oauthState.Link && oauthState.UserID > 0 {
			_, err := s.FindUserByID(ctx, oauthState.UserID)
			if err != nil {
				redirectWithError(w, r, externalURL, "user not found")
				return
			}
			existingLink, _ := s.FindOAuthUserLink(ctx, p.ID, subject)
			if existingLink != nil {
				if existingLink.UserID != oauthState.UserID {
					redirectWithError(w, r, externalURL, "this identity is already linked to another account")
					return
				}
				redirectURL := fmt.Sprintf("%s/?oauth_action=linked&provider=%s", strings.TrimRight(externalURL, "/"), canonical)
				http.Redirect(w, r, redirectURL, http.StatusFound)
				return
			}
			_, err = s.CreateOAuthUserLink(ctx, oauthprovider.UserLink{
				UserID:     oauthState.UserID,
				ProviderID: p.ID,
				Subject:    subject,
				Email:      email,
			})
			if err != nil {
				redirectWithError(w, r, externalURL, "failed to link account: "+err.Error())
				return
			}
			redirectURL := fmt.Sprintf("%s/?oauth_action=linked&provider=%s", strings.TrimRight(externalURL, "/"), canonical)
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}

		// Login flow: check if user link exists
		existingLink, _ := s.FindOAuthUserLink(ctx, p.ID, subject)
		if existingLink != nil {
			// Returning user: find user by ID and generate JWT
			matchedUser, err := s.FindUserByID(ctx, existingLink.UserID)
			if err != nil {
				redirectWithError(w, r, externalURL, "linked user not found")
				return
			}

			_, jwtStr, err := s.RefreshToken(ctx, matchedUser.Username)
			if err != nil {
				redirectWithError(w, r, externalURL, "failed to generate token")
				return
			}

			redirectURL := fmt.Sprintf("%s/?oauth_action=callback&jwt=%s",
				strings.TrimRight(externalURL, "/"),
				url.QueryEscape(jwtStr))
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}

		// New user: generate temp token for profile completion
		tempToken, err := pikoci.GenerateTempToken(ts, canonical, subject, email, username, fullName)
		if err != nil {
			redirectWithError(w, r, externalURL, "failed to generate temp token")
			return
		}

		redirectURL := fmt.Sprintf("%s/?oauth_action=complete-profile&token=%s&username=%s&full_name=%s&email=%s",
			strings.TrimRight(externalURL, "/"),
			url.QueryEscape(tempToken),
			url.QueryEscape(username),
			url.QueryEscape(fullName),
			url.QueryEscape(email))
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

func redirectWithError(w http.ResponseWriter, r *http.Request, externalURL, msg string) {
	redirectURL := fmt.Sprintf("%s/?oauth_error=%s", strings.TrimRight(externalURL, "/"), url.QueryEscape(msg))
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// --- OAuth Complete Profile (unauthenticated, JSON) ---

type OAuthCompleteProfileRequest struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
}

type OAuthCompleteProfileResponse struct {
	Err  string `json:"error,omitempty"`
	Data struct {
		User interface{} `json:"user,omitempty"`
		JWT  string      `json:"jwt,omitempty"`
	} `json:"data,omitempty"`
}

func (r OAuthCompleteProfileResponse) Error() string { return r.Err }

func oauthCompleteProfile(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req OAuthCompleteProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encodeResponse(OAuthCompleteProfileResponse{Err: err.Error()}, w)
			return
		}

		u, jwtStr, err := s.OAuthCompleteProfile(r.Context(), req.Token, req.Username, req.FullName)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		resp := OAuthCompleteProfileResponse{Err: errs}
		resp.Data.User = u
		resp.Data.JWT = jwtStr
		encodeResponse(resp, w)
	}
}

// --- Admin: OAuth Providers CRUD ---

type ListOAuthProvidersResponse struct {
	Err  string                     `json:"error,omitempty"`
	Data []*oauthprovider.Provider `json:"data,omitempty"`
}

func (r ListOAuthProvidersResponse) Error() string { return r.Err }

func listOAuthProviders(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps, err := s.ListOAuthProviders(r.Context())
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListOAuthProvidersResponse{Data: ps, Err: errs}, w)
	}
}

type CreateOAuthProviderRequest struct {
	Name          string `json:"name"`
	Canonical     string `json:"canonical"`
	Type          string `json:"type"`
	IssuerURL     string `json:"issuer_url"`
	AuthURL       string `json:"auth_url"`
	TokenURL      string `json:"token_url"`
	UserinfoURL   string `json:"userinfo_url"`
	Scopes        string `json:"scopes"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	UsernameClaim string `json:"username_claim"`
	Enabled       bool   `json:"enabled"`
}

type CreateOAuthProviderResponse struct {
	Err  string                   `json:"error,omitempty"`
	Data *oauthprovider.Provider `json:"data,omitempty"`
}

func (r CreateOAuthProviderResponse) Error() string { return r.Err }

func createOAuthProvider(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateOAuthProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encodeResponse(CreateOAuthProviderResponse{Err: err.Error()}, w)
			return
		}
		p, err := s.CreateOAuthProvider(r.Context(), oauthprovider.Provider{
			Name:          req.Name,
			Canonical:     req.Canonical,
			Type:          req.Type,
			IssuerURL:     req.IssuerURL,
			AuthURL:       req.AuthURL,
			TokenURL:      req.TokenURL,
			UserinfoURL:   req.UserinfoURL,
			Scopes:        req.Scopes,
			ClientID:      req.ClientID,
			ClientSecret:  req.ClientSecret,
			UsernameClaim: req.UsernameClaim,
			Enabled:       req.Enabled,
		})
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateOAuthProviderResponse{Data: p, Err: errs}, w)
	}
}

type UpdateOAuthProviderResponse struct {
	Err  string                   `json:"error,omitempty"`
	Data *oauthprovider.Provider `json:"data,omitempty"`
}

func (r UpdateOAuthProviderResponse) Error() string { return r.Err }

func updateOAuthProvider(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		canonical := vars["canonical"]

		var req CreateOAuthProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encodeResponse(UpdateOAuthProviderResponse{Err: err.Error()}, w)
			return
		}
		p, err := s.UpdateOAuthProvider(r.Context(), canonical, oauthprovider.Provider{
			Name:          req.Name,
			Canonical:     req.Canonical,
			Type:          req.Type,
			IssuerURL:     req.IssuerURL,
			AuthURL:       req.AuthURL,
			TokenURL:      req.TokenURL,
			UserinfoURL:   req.UserinfoURL,
			Scopes:        req.Scopes,
			ClientID:      req.ClientID,
			ClientSecret:  req.ClientSecret,
			UsernameClaim: req.UsernameClaim,
			Enabled:       req.Enabled,
		})
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateOAuthProviderResponse{Data: p, Err: errs}, w)
	}
}

type DeleteOAuthProviderResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteOAuthProviderResponse) Error() string { return r.Err }

func deleteOAuthProvider(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		canonical := vars["canonical"]
		err := s.DeleteOAuthProvider(r.Context(), canonical)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteOAuthProviderResponse{Err: errs}, w)
	}
}

// --- Admin: Auth Settings ---

type GetAdminAuthSettingsResponse struct {
	Err  string                       `json:"error,omitempty"`
	Data *oauthprovider.AuthSettings `json:"data,omitempty"`
}

func (r GetAdminAuthSettingsResponse) Error() string { return r.Err }

func getAdminAuthSettings(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := s.GetAuthSettings(r.Context())
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetAdminAuthSettingsResponse{Data: settings, Err: errs}, w)
	}
}

type UpdateAdminAuthSettingsRequest struct {
	LocalAuthEnabled bool `json:"local_auth_enabled"`
}

type UpdateAdminAuthSettingsResponse struct {
	Err string `json:"error,omitempty"`
}

func (r UpdateAdminAuthSettingsResponse) Error() string { return r.Err }

func updateAdminAuthSettings(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpdateAdminAuthSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encodeResponse(UpdateAdminAuthSettingsResponse{Err: err.Error()}, w)
			return
		}
		err := s.UpdateAuthSettings(r.Context(), oauthprovider.AuthSettings{
			LocalAuthEnabled: req.LocalAuthEnabled,
		})
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateAdminAuthSettingsResponse{Err: errs}, w)
	}
}

// --- Profile: Linked Accounts ---

type ListLinkedAccountsResponse struct {
	Err  string                  `json:"error,omitempty"`
	Data []*oauthprovider.LinkedAccount `json:"data,omitempty"`
}

func (r ListLinkedAccountsResponse) Error() string { return r.Err }

func listLinkedAccounts(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		un, _ := ctx.Value(UsernameContextKey).(string)
		if un == "" {
			encodeResponse(ListLinkedAccountsResponse{Err: "missing username"}, w)
			return
		}
		um, err := s.GetUser(ctx, un)
		if err != nil {
			encodeResponse(ListLinkedAccountsResponse{Err: err.Error()}, w)
			return
		}
		accounts, err := s.ListLinkedAccounts(ctx, um.ID)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListLinkedAccountsResponse{Data: accounts, Err: errs}, w)
	}
}

type UnlinkAccountResponse struct {
	Err string `json:"error,omitempty"`
}

func (r UnlinkAccountResponse) Error() string { return r.Err }

func unlinkAccount(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		un, _ := ctx.Value(UsernameContextKey).(string)
		if un == "" {
			encodeResponse(UnlinkAccountResponse{Err: "missing username"}, w)
			return
		}
		vars := mux.Vars(r)
		canonical := vars["canonical"]

		um, err := s.GetUser(ctx, un)
		if err != nil {
			encodeResponse(UnlinkAccountResponse{Err: err.Error()}, w)
			return
		}
		err = s.UnlinkAccount(ctx, um.ID, canonical)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UnlinkAccountResponse{Err: errs}, w)
	}
}
