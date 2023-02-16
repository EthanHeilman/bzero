package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/agenttype"
	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/cenkalti/backoff/v4"
)

const (
	whereEndpoint = "status/where"

	// Register info
	activationTokenEndpoint      = "/api/v2/agent/token"
	registerEndpoint             = "/api/v2/agent/register"
	getConnectionServiceEndpoint = "/api/v2/connection-service/url"
)

type RegistrationConfig interface {
	SetRegistrationData(
		serviceUrl string,
		publicKey *keypair.PublicKey,
		privateKey *keypair.PrivateKey,
		idpProvider string,
		idpOrgId string,
		targetId string,
		jwksUrlPatterns []string,
	) error
}

type Registration struct {
	ctx    context.Context
	logger *logger.Logger

	agentType       agenttype.AgentType
	serviceUrl      string
	activationToken string
	registrationKey string
	targetId        string
	version         string
	environmentId   string
	environmentName string
	targetName      string
	idpProvider     string
	idpOrgId        string
	jwksUrlPatterns []string
}

func New(
	agentType agenttype.AgentType,
	serviceUrl string,
	activationToken string,
	apiKey string,
	targetId string,
	version string,
	environmentId string,
	environmentName string,
	targetName string,
	idpProvider string,
	idpOrgId string,
) *Registration {
	var jwksUrlPatterns []string
	return &Registration{
		agentType:       agentType,
		serviceUrl:      serviceUrl,
		activationToken: activationToken,
		registrationKey: apiKey,
		targetId:        targetId,
		version:         version,
		environmentId:   environmentId,
		environmentName: environmentName,
		targetName:      targetName,
		idpProvider:     idpProvider,
		idpOrgId:        idpOrgId,
		jwksUrlPatterns: jwksUrlPatterns,
	}
}

func (r *Registration) Register(ctx context.Context, logger *logger.Logger, config RegistrationConfig) error {
	r.ctx = ctx

	// Check we have all our requried args
	if r.activationToken == "" && r.registrationKey == "" {
		return fmt.Errorf("in order to register, we need either an api key or an activation token")
	}

	r.logger = logger
	r.logger.Infof("Registering agent with %s", r.serviceUrl)

	// Generate and store our public, private key pair and add to config
	publicKey, privateKey, err := keypair.GenerateKeyPair()
	if err != nil {
		return err
	}
	r.logger.Info("Generated cryptographic identity")

	r.logger.Info("Phoning home to BastionZero...")
	// Complete registration with the Bastion
	if err := r.phoneHome(publicKey.String()); err != nil {
		return err
	}

	r.logger.Info("Agent successfully Registered.  BastionZero says hi.")

	// If the registration went ok, save the config
	if err := config.SetRegistrationData(r.serviceUrl, publicKey, privateKey, r.idpProvider, r.idpOrgId, r.targetId, r.jwksUrlPatterns); err != nil {
		return fmt.Errorf("failed to persist new registration data: %w", err)
	}

	r.logger.Info("Registration complete!")
	return nil
}

func (r *Registration) phoneHome(publickey string) error {
	// If we don't have an activation token, use api key to get one
	if r.activationToken == "" {
		if token, err := r.getActivationToken(r.registrationKey); err != nil {
			return err
		} else {
			r.activationToken = token
		}
	}

	// Register with Bastion
	resp, err := r.getRegistrationResponse(publickey)
	if err != nil {
		return err
	}

	// only replace, if values were undefined by user
	if r.idpProvider == "" {
		r.idpProvider = resp.OrgProvider
	}
	if r.idpOrgId == "" {
		r.idpOrgId = resp.OrgID
	}

	// set our remaining values
	r.targetName = resp.TargetName

	r.logger.Infof("Setting up this agent to accept service accounts from the following JWKS URL patterns %v", resp.JwksUrlPatterns)
	r.jwksUrlPatterns = resp.JwksUrlPatterns

	return nil
}

func (r *Registration) getActivationToken(apiKey string) (string, error) {
	r.logger.Infof("Requesting activation token from Bastion")
	req := ActivationTokenRequest{
		TargetName: r.targetName,
		AgentType:  r.agentType,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("error marshalling activation token request: %s", err)
	}

	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = 10 * time.Minute
	backoff.MaxInterval = time.Minute

	opts := httpclient.HTTPOptions{
		Endpoint: activationTokenEndpoint,
		Body:     bytes.NewBuffer(reqBytes),
		Headers: http.Header{
			"X-API-KEY":    {apiKey},
			"Content-Type": {"application/json"},
		},
		ExponentialBackoff: backoff,
	}

	client, err := httpclient.New(r.serviceUrl, opts)
	if err != nil {
		return "", fmt.Errorf("failed to create to http client: %s", err)
	}

	resp, err := client.Post(r.ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get activation token: %w", err)
	}

	// read our activation token request body
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResponse ActivationTokenResponse
	if err := json.Unmarshal(respBytes, &tokenResponse); err != nil {
		return "", fmt.Errorf("malformed activation token response: %s", err)
	}

	if tokenResponse.ActivationToken == "" {
		return "", fmt.Errorf("activation request returned empty response")
	} else {
		// re-use an existing targetId if a cluster with this name already exists
		if tokenResponse.ExistingClusterId != "" {
			r.targetId = tokenResponse.ExistingClusterId
		}

		return tokenResponse.ActivationToken, nil
	}
}

func (r *Registration) getRegistrationResponse(publickey string) (RegistrationResponse, error) {
	var regResponse RegistrationResponse

	// if the target name was never previously set, then we default to hostname, but only Bastion knows
	// if the target name was previously set, so we send it as an additional value
	hostname, err := os.Hostname()
	if err != nil {
		return regResponse, fmt.Errorf("could not resolve hostname: %s", err)
	}

	// determine agent's location
	region, err := r.whereAmI()
	if err != nil {
		return regResponse, fmt.Errorf("failed to get agent region: %s", err)
	}

	// If we pass no targetId to the container, this means that our Id is the same as our activationToken
	if r.targetId == "" {
		r.targetId = r.activationToken
	}

	// Create our request
	req := RegistrationRequest{
		PublicKey:       publickey,
		ActivationCode:  r.activationToken,
		Version:         r.version,
		EnvironmentId:   r.environmentId,
		EnvironmentName: r.environmentName,
		TargetName:      r.targetName,
		TargetHostName:  hostname,
		TargetId:        r.targetId,
		Region:          region,
	}

	// Marshal the request
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return regResponse, fmt.Errorf("error marshalling register agent message for agent: %s", err)
	}

	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = 10 * time.Minute
	backoff.MaxInterval = time.Minute

	opts := httpclient.HTTPOptions{
		Endpoint: registerEndpoint,
		Body:     bytes.NewBuffer(reqBytes),
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
		ExponentialBackoff: backoff,
	}

	client, err := httpclient.New(r.serviceUrl, opts)
	if err != nil {
		return regResponse, fmt.Errorf("failed to create to http client: %s", err)
	}

	resp, err := client.Post(r.ctx)
	if err != nil {
		return regResponse, fmt.Errorf("failed to get activation token: %w", err)
	}

	if respBytes, err := io.ReadAll(resp.Body); err != nil {
		return regResponse, fmt.Errorf("could not read http response: %s", err)
	} else if err := json.Unmarshal(respBytes, &regResponse); err != nil {
		return regResponse, fmt.Errorf("malformed registration response: %s", err)
	} else {
		return regResponse, nil
	}
}

func (r *Registration) whereAmI() (string, error) {
	// Get our region by pinging out connection-service
	connectionServiceUrl, err := r.getConnectionServiceUrlFromServiceUrl() //TODO: Question: This seems like a lot
	if err != nil {
		return "", err
	}

	client, err := httpclient.New(connectionServiceUrl, httpclient.HTTPOptions{
		Endpoint: whereEndpoint,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create to http client: %s", err)
	}

	resp, err := client.Get(r.ctx)
	if err != nil {
		return "", fmt.Errorf("failed to hit location endpoint: %s", err)
	}

	if regionBodyBytes, err := io.ReadAll(resp.Body); err != nil {
		return "", err
	} else {
		return string(regionBodyBytes), nil
	}
}

func (r *Registration) getConnectionServiceUrlFromServiceUrl() (string, error) {
	client, err := httpclient.New(r.serviceUrl, httpclient.HTTPOptions{
		Endpoint: getConnectionServiceEndpoint,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create to http client: %s", err)
	}

	resp, err := client.Get(r.ctx)
	if err != nil {
		return "", fmt.Errorf("error making request to connection service: %s", err)
	}

	// read the response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading body on get connection service url requets: %s", err)
	}

	// unmarshal the response into struct
	var getConnectionServiceResponse GetConnectionServiceResponse
	if err := json.Unmarshal(respBytes, &getConnectionServiceResponse); err != nil {
		return "", fmt.Errorf("malformed getConnectionService response: %s", err)
	}

	return getConnectionServiceResponse.ConnectionServiceUrl, nil
}
