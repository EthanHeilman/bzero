package registration

import (
	"crypto/ed25519"
	ed "crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"bastionzero.com/bctl/v1/bzerolib/bzhttp"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	whereEndpoint = "status/where"

	// Register info
	activationTokenEndpoint      = "/api/v2/agent/token"
	registerEndpoint             = "/api/v2/agent/register"
	getConnectionServiceEndpoint = "/api/v2/connection-service/url"
)

type IRegistration interface {
	Register(logger *logger.Logger) error
}

type RegistrationConfig interface {
	SetKeyPair(publickey ed25519.PublicKey, privateKey ed25519.PrivateKey) error
	SetIdpProvider(provider string) error
	SetIdPOrg(org string) error
}

type Registration struct {
	logger *logger.Logger
	config RegistrationConfig

	serviceUrl      string
	activationToken string
	apiKey          string
	targetId        string
	version         string
	environmentId   string
	environmentName string
	targetName      string
	idpProvider     string
	idpOrgId        string
}

func (r *Registration) Register(logger *logger.Logger) error {
	// Check we have all our requried args
	if r.activationToken == "" && r.apiKey == "" {
		return fmt.Errorf("in order to register, we need either an api or activation token")
	}

	logger.Infof("Registering agent with %s", r.serviceUrl)

	// Generate and store our public, private key pair and add to config
	r.logger.Info("Generated cryptographic identity")
	if publicKey, privateKey, err := ed.GenerateKey(nil); err != nil {
		return err
	}

	// Complete registration with the Bastion
	if err := r.phoneHome(); err != nil {
		return err
	}

	r.logger.Info("Agent successfully Registered.  BastionZero says hi.")

	// If the registration went ok, save the config
	if err := r.saveRegistration(); err != nil {
		return fmt.Errorf("error saving config: %w", err)
	}

	logger.Info("Registration complete!")
	return nil
}

func (r *Registration) saveRegistration() error {
	return nil
}

func (r *Registration) phoneHome(activationToken string, apiKey string, targetId string) error {
	// If we don't have an activation token, use api key to get one
	if activationToken == "" {
		if token, err := r.getActivationToken(apiKey); err != nil {
			return err
		} else {
			activationToken = token
		}
	}

	// Register with Bastion
	r.logger.Info("Phoning home to BastionZero...")

	if resp, err := r.getRegistrationResponse(activationToken, targetId); err != nil {
		return err
	} else {
		// only replace, if values were undefined by user
		if r.idpProvider == "" {
			r.idpProvider = resp.OrgProvider
		}
		if r.idpOrgId == "" {
			r.idpOrgId = resp.OrgID
		}

		// set our remaining values
		r.targetName = resp.TargetName

		// If targetId is empty, that means to use the activationToken as the id of the target
		if targetId == "" {
			targetId = activationToken
		}
		r.targetId = targetId

		return nil
	}
}

func (r *Registration) getActivationToken(apiKey string) (string, error) {
	r.logger.Infof("Requesting activation token from Bastion")
	tokenEndpoint, err := bzhttp.BuildEndpoint(r.serviceUrl, activationTokenEndpoint)
	if err != nil {
		return "", err
	}

	req := ActivationTokenRequest{
		TargetName: r.targetName,
	}

	// Marshall the request
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("error marshalling activation token request: %+v", req)
	}

	headers := map[string]string{
		"X-API-KEY": apiKey,
	}
	params := map[string]string{} // no params

	resp, err := bzhttp.Post(r.logger, tokenEndpoint, "application/json", reqBytes, headers, params)
	if err != nil {
		return "", fmt.Errorf("failed to get activation token: %s. {Endpoint: %s, Request: %+v, Response: %+v}", err, tokenEndpoint, req, resp)
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

	// determine agent location
	region, err := r.whereAmI()
	if err != nil {
		return regResponse, fmt.Errorf("failed to get agent region: %s", err)
	}

	// If we pass no targetId to the container, this means that our Id is the same as our activationToken
	if targetId == "" {
		targetId = activationToken
	}

	// Create our request
	req := RegistrationRequest{
		PublicKey:       r.publicKey,
		ActivationCode:  activationToken,
		Version:         r.version,
		EnvironmentId:   r.environmentId,
		EnvironmentName: r.environmentName,
		TargetName:      r.targetName,
		TargetHostName:  hostname,
		TargetId:        targetId,
		Region:          region,
	}

	// Build the endpoint we want to hit
	registrationEndpoint, err := bzhttp.BuildEndpoint(r.serviceUrl, registerEndpoint)
	if err != nil {
		return regResponse, fmt.Errorf("error building registration url: {serviceUrl: %s, registerEndpoint: %s", r.serviceUrl, registerEndpoint)
	}

	// Marshal the request
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return regResponse, fmt.Errorf("error marshalling register agent message for agent: %+v", req)
	}

	// Perform the request
	resp, err := bzhttp.Post(r.logger, registrationEndpoint, "application/json", reqBytes, map[string]string{}, map[string]string{})
	if err != nil {
		return regResponse, fmt.Errorf("error registering agent with bastion: %s. {Endpoint: %s, Request: %+v, Response: %+v}", err, registrationEndpoint, req, resp)
	}

	if respBytes, err := io.ReadAll(resp.Body); err != nil {
		return regResponse, fmt.Errorf("could not read http response: %s", err)
	} else {
		if err := json.Unmarshal(respBytes, &regResponse); err != nil {
			return regResponse, fmt.Errorf("malformed registration response: %s", err)
		} else {
			return regResponse, nil
		}
	}
}

func (r *Registration) whereAmI() (string, error) {
	// Get our region by pinging out connection-service
	connectionServiceUrl, err := r.getConnectionServiceUrlFromServiceUrl() //TODO: Question: This seems like a lot
	if err != nil {
		return "", err
	}

	whereEndpoint, err := bzhttp.BuildEndpoint(connectionServiceUrl, whereEndpoint)
	if err != nil {
		return "", err
	}

	regionResponse, err := bzhttp.Get(r.logger, whereEndpoint, map[string]string{}, map[string]string{})
	if err != nil {
		return "", err
	}

	if regionBodyBytes, err := io.ReadAll(regionResponse.Body); err != nil {
		return "", err
	} else {
		return string(regionBodyBytes), nil
	}
}

func (r *Registration) getConnectionServiceUrlFromServiceUrl() (string, error) {
	// build our endpoint
	endpointToHit, err := bzhttp.BuildEndpoint(r.serviceUrl, getConnectionServiceEndpoint)
	if err != nil {
		return "", fmt.Errorf("error building endpoint for get connection service request")
	}

	// make our request
	resp, err := bzhttp.Get(r.logger, endpointToHit, map[string]string{}, map[string]string{})
	if err != nil {
		return "", fmt.Errorf("error making get request to get connection service url")
	}

	// read the response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading body on get connection service url requets")
	}

	// unmarshal the response into struct
	var getConnectionServiceResponse GetConnectionServiceResponse
	if err := json.Unmarshal(respBytes, &getConnectionServiceResponse); err != nil {
		return "", fmt.Errorf("malformed getConnectionService response: %s", err)
	}

	return getConnectionServiceResponse.ConnectionServiceUrl, nil
}
