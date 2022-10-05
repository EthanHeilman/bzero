package agentidentity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/messagesigner"
	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	agentIdentityEndpoint = "/api/v2/agent/identity/%s" // targetId
)

type IAgentIdentityTokenStore interface {
	GetAgentIdentityToken() string
	SaveAgentIdentityToken(string) error
}

type IAgentIdentityProvider interface {
	GetToken(ctx context.Context) (string, error)
}

type AgentIdentityProvider struct {
	logger                *logger.Logger
	serviceUrl            string
	targetId              string
	store                 IAgentIdentityTokenStore
	agentIdentityProvider *oidc.Provider
	messageSigner         messagesigner.IMessageSigner
}

func New(
	logger *logger.Logger,
	serviceUrl string,
	targetId string,
	agentIdentityTokenStore IAgentIdentityTokenStore,
	messageSigner messagesigner.IMessageSigner,
) *AgentIdentityProvider {
	return &AgentIdentityProvider{
		logger:        logger,
		serviceUrl:    serviceUrl,
		targetId:      targetId,
		store:         agentIdentityTokenStore,
		messageSigner: messageSigner,
	}
}

func (a *AgentIdentityProvider) GetToken(ctx context.Context) (string, error) {
	idToken := a.store.GetAgentIdentityToken()

	if idToken == "" {
		return a.refreshToken(ctx)
	} else {
		// Check that the identity token is still valid and refresh it otherwise
		_, err := a.verifyToken(idToken, ctx)
		if err != nil {
			a.logger.Infof("Agent Identity token invalid: %s. Attempting to refresh.", err)
			return a.refreshToken(ctx)
		} else {
			return idToken, nil
		}
	}
}

func (a *AgentIdentityProvider) refreshToken(ctx context.Context) (string, error) {
	if res, err := a.getTokenFromBastion(ctx); err != nil {
		return "", err
	} else {
		if err = a.store.SaveAgentIdentityToken(res.Token); err != nil {
			return "", err
		} else {
			return res.Token, nil
		}
	}
}

func (a *AgentIdentityProvider) verifyToken(idToken string, ctx context.Context) (*oidc.IDToken, error) {
	// create the oidc provider if its not yet created. Using a single provider
	// object will cache jwks so that they don't need to be refreshed each time
	// we call verify
	if a.agentIdentityProvider == nil {
		issuerUrl := a.serviceUrl
		// trim any trailing slash from the url as the oidc will not
		// treat these as identical urls if the provider returns a url without
		// the trailing slash
		// https://github.com/coreos/go-oidc/issues/203
		issuerUrl = strings.TrimSuffix(issuerUrl, "/")
		agentIdentityProvider, err := oidc.NewProvider(ctx, issuerUrl)
		if err != nil {
			return nil, fmt.Errorf("failed to establish connection bzero provider %s: %w", issuerUrl, err)
		}

		a.agentIdentityProvider = agentIdentityProvider
	}

	config := &oidc.Config{
		ClientID:             "connection-service",
		SupportedSigningAlgs: []string{oidc.ES256},
	}
	verifier := a.agentIdentityProvider.Verifier(config)
	token, err := verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify id token with provider: %w", err)
	}

	return token, nil
}

func (a *AgentIdentityProvider) getTokenFromBastion(ctx context.Context) (*GetAgentIdentityTokenResponse, error) {
	// Create a new getAgentIdentityToken message
	getAgentIdentityToken := GetAgentIdentityTokenRequest{
		BackendAgentMessage: am.BackendAgentMessage{
			MessageType: am.GetAgentIdentityToken,
			Timestamp:   time.Now().Unix(),
		},
	}

	// Serialize the message
	getAgentIdentityTokenPayload, err := json.Marshal(getAgentIdentityToken)
	if err != nil {
		return nil, fmt.Errorf("error marshalling getAgentIdentityToken message: %w", err)
	}

	// Sign the message
	sig, err := a.messageSigner.SignMessage(getAgentIdentityTokenPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to sign getAgentIdentityToken message %w", err)
	}

	// Build the http client and request
	options := httpclient.HTTPOptions{
		Endpoint: fmt.Sprintf(agentIdentityEndpoint, a.targetId),
		Params: url.Values{
			"message":   {base64.StdEncoding.EncodeToString(getAgentIdentityTokenPayload)},
			"signature": {sig},
		},
	}

	client, err := httpclient.New(a.logger, a.serviceUrl, options)
	if err != nil {
		return nil, err
	}

	// Send the request
	response, err := client.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("error making get request for agent identity token. Request: %+v Error: %s", getAgentIdentityToken, err)
	}

	// Decode and return response
	defer response.Body.Close()
	responseDecoded := GetAgentIdentityTokenResponse{}
	json.NewDecoder(response.Body).Decode(&responseDecoded)
	return &responseDecoded, nil
}
