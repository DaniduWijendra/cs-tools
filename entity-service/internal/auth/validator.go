// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package auth validates the Asgardeo-issued tokens entity-service receives and
// carries the verified caller identity to the services that scope by it.
//
// Two tokens can arrive on a request:
//   - x-user-id-token: the end user's ID token (email claim, audience = one of
//     the accepted application client ids). Present when a portal backend acts
//     for a user.
//   - Authorization: Bearer: the calling application's client-credentials
//     access token (client_id/azp claim). Present on every service-to-service
//     call; it is the ONLY token a machine-to-machine caller (e.g.
//     csm-integration-service) sends.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// Config holds token validation settings. See config.Config.Auth* for what
// each means.
type Config struct {
	Issuer             string
	JWKSURL            string
	UserTokenAudiences []string
	ClockSkew          time.Duration
}

// signingMethods restricts accepted algorithms to asymmetric ones, so a token
// can never be "verified" by treating a public key as an HMAC secret.
var signingMethods = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512"}

// UserClaims is what a validated x-user-id-token yields.
type UserClaims struct {
	Email   string
	Subject string
	// UserID is the token's "userid" claim -- Asgardeo's stable, per-account
	// user identifier. Unlike Subject ("sub"), which csm-portal-backend's own
	// frontend has separately documented as per-session rather than stable
	// (see that repo's IdTokenClaims doc comment), UserID is the same value
	// both csm-portal-backend and customer-portal backend-v2 already decode
	// from this identical token into their own UserInfo.UserID and log on
	// every request they handle -- see that value's own doc comment for why
	// this one, not Subject, is the field to correlate a request across
	// services by.
	UserID string
}

// ClientClaims is what a validated client-credentials access token yields.
type ClientClaims struct {
	ClientID string
}

type tokenClaims struct {
	Email    string `json:"email"`
	UserID   string `json:"userid"`
	ClientID string `json:"client_id"`
	AZP      string `json:"azp"`
	jwt.RegisteredClaims
}

// Validator verifies signatures against the issuer's JWKS and checks issuer,
// expiry and (for user tokens) audience.
type Validator struct {
	cfg     Config
	keyFunc jwt.Keyfunc
}

// NewValidator fetches the issuer's JWKS and fails unless at least one key
// loaded, so a wrong or unreachable JWKS URL stops the process at startup.
// That check is deliberate: keyfunc itself only logs a failed initial fetch
// and keeps retrying in the background, which would otherwise leave a
// misconfigured deployment up while silently rejecting every token.
func NewValidator(ctx context.Context, cfg Config) (*Validator, error) {
	client := &http.Client{Transport: &x5cStrippingTransport{base: http.DefaultTransport}, Timeout: 15 * time.Second}
	jwks, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{cfg.JWKSURL}, keyfunc.Override{Client: client})
	if err != nil {
		return nil, fmt.Errorf("auth: initialise JWKS from %s: %w", cfg.JWKSURL, err)
	}
	keys, err := jwks.Storage().KeyReadAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: read JWKS from %s: %w", cfg.JWKSURL, err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("auth: no signing keys loaded from %s", cfg.JWKSURL)
	}
	return &Validator{cfg: cfg, keyFunc: jwks.Keyfunc}, nil
}

// NewValidatorWithKeyfunc builds a Validator around an existing key function
// (tests supply a static key rather than a JWKS endpoint).
func NewValidatorWithKeyfunc(cfg Config, kf jwt.Keyfunc) *Validator {
	return &Validator{cfg: cfg, keyFunc: kf}
}

func (v *Validator) parse(raw string) (*tokenClaims, error) {
	var c tokenClaims
	tok, err := jwt.ParseWithClaims(raw, &c, v.keyFunc,
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithLeeway(v.cfg.ClockSkew),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods(signingMethods),
	)
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return &c, nil
}

// ValidateUserToken validates an end user's ID token: signature, issuer,
// expiry, an audience among the configured application client ids, and a
// non-empty email claim.
func (v *Validator) ValidateUserToken(raw string) (UserClaims, error) {
	c, err := v.parse(raw)
	if err != nil {
		return UserClaims{}, err
	}
	if !hasAnyAudience(c.Audience, v.cfg.UserTokenAudiences) {
		return UserClaims{}, errors.New("token audience not accepted")
	}
	if strings.TrimSpace(c.Email) == "" {
		return UserClaims{}, errors.New("token missing email claim")
	}
	return UserClaims{Email: strings.TrimSpace(c.Email), Subject: c.Subject, UserID: strings.TrimSpace(c.UserID)}, nil
}

// ValidateClientToken validates an application's client-credentials access
// token: signature, issuer and expiry, then extracts the client id from the
// client_id claim (falling back to azp). No audience is enforced -- the client
// id is what gets authorized, by AccessService checking it against
// AUTH_INTERNAL_CLIENT_IDS.
func (v *Validator) ValidateClientToken(raw string) (ClientClaims, error) {
	c, err := v.parse(raw)
	if err != nil {
		return ClientClaims{}, err
	}
	id := strings.TrimSpace(c.ClientID)
	if id == "" {
		id = strings.TrimSpace(c.AZP)
	}
	if id == "" {
		return ClientClaims{}, errors.New("token has neither client_id nor azp claim")
	}
	return ClientClaims{ClientID: id}, nil
}

func hasAnyAudience(tokenAuds jwt.ClaimStrings, accepted []string) bool {
	for _, want := range accepted {
		for _, got := range tokenAuds {
			if got == want {
				return true
			}
		}
	}
	return false
}
