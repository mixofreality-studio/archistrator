// Package actoridentity recognizes the team member behind a presented proof, or refuses it.
package actoridentity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

const (
	refusalUnrecognized = "unrecognized"
	refusalUnavailable  = "unavailable"

	// providerWait bounds each call to the identity provider.
	providerWait = 2 * time.Second
	// maxResponseBytes bounds the untrusted provider response we will read.
	maxResponseBytes = 64 * 1024
)

type externalHTTPActorIdentityAccess struct {
	endpoint string
	client   *http.Client
}

// providerRequest/providerVerdict are the provider's wire vocabulary; they stay inside this package.
type providerRequest struct {
	Credential string `json:"credential"`
}

type providerVerdict struct {
	Valid    bool   `json:"valid"`
	MemberID string `json:"memberId"`
	OnBehalf bool   `json:"onBehalf"`
}

func newExternalHTTPActorIdentityAccess(baseURL string, client *http.Client) (ActorIdentityAccess, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("actoridentity: invalid identity provider base URL")
	}
	if client == nil {
		return nil, errors.New("actoridentity: nil HTTP client")
	}
	return &externalHTTPActorIdentityAccess{endpoint: u.JoinPath("verify").String(), client: client}, nil
}

func refuse(reason string) (Recognition, error) {
	return Recognition{Refusal: reason}, nil
}

// RecognizeActor verifies the proof with the identity provider, failing closed.
func (a *externalHTTPActorIdentityAccess) RecognizeActor(rc fwra.Context, proof ActorProof) (Recognition, error) {
	if proof.Credential == "" {
		return refuse(refusalUnrecognized)
	}
	body, err := json.Marshal(providerRequest(proof))
	if err != nil {
		return Recognition{}, errors.New("actoridentity: cannot encode provider request")
	}
	parent := rc.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, providerWait)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return Recognition{}, errors.New("actoridentity: cannot build provider request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return refuse(refusalUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusBadRequest:
		return refuse(refusalUnrecognized)
	case resp.StatusCode != http.StatusOK:
		return refuse(refusalUnavailable)
	}

	var v providerVerdict
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&v); err != nil {
		return refuse(refusalUnrecognized)
	}
	if !v.Valid || v.MemberID == "" {
		return refuse(refusalUnrecognized)
	}
	return Recognition{Recognized: true, MemberID: v.MemberID, OnBehalf: v.OnBehalf}, nil
}
