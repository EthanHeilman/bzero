package registration

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"bastionzero.com/bctl/v1/bzerolib/bzhttp"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
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
	Register(logger *logger.Logger, config RegistrationConfig) error
}

type RegistrationConfig interface {
	SetRegistrationData(serviceUrl string, publickey keypair.PublicKey, privateKey keypair.PrivateKey, idpProvider string, idpOrgId string, targetId string) error
}

type Registration struct {
	logger *logger.Logger

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

func New(
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
) IRegistration {
	return &Registration{
		serviceUrl:      serviceUrl,
		activationToken: activationToken,
		apiKey:          apiKey,
		targetId:        targetId,
		version:         version,
		environmentId:   environmentId,
		environmentName: environmentName,
		targetName:      targetName,
		idpProvider:     idpProvider,
		idpOrgId:        idpOrgId,
	}
}

func (r *Registration) Register(logger *logger.Logger, config RegistrationConfig) error {
	// Check we have all our requried args
	if r.activationToken == "" && r.apiKey == "" {
		return fmt.Errorf("in order to register, we need either an api key or an activation token")
	}

	logger.Infof("Registering agent with %s", r.serviceUrl)

	// Generate and store our public, private key pair and add to config
	publicKey, privateKey, err := keypair.GenerateKeyPair()
	if err != nil {
		return err
	}
	r.logger.Info("Generated cryptographic identity")

	r.logger.Info("Phoning home to BastionZero...")
	// Complete registration with the Bastion
	if err := r.phoneHome(&publicKey); err != nil {
		return err
	}

	r.logger.Info("Agent successfully Registered.  BastionZero says hi.")

	// If the registration went ok, save the config
	if err := config.SetRegistrationData(r.serviceUrl, publicKey, privateKey, r.idpProvider, r.idpOrgId, r.targetId); err != nil {
		return fmt.Errorf("error saving config: %w", err)
	}

	logger.Info("Registration complete!")
	return nil
}

func (r *Registration) phoneHome(publickey *keypair.PublicKey) error {
	// If we don't have an activation token, use api key to get one
	if r.activationToken == "" {
		if token, err := r.getActivationToken(r.apiKey); err != nil {
			return err
		} else {
			r.activationToken = token
		}
	}

	// Register with Bastion
	if resp, err := r.getRegistrationResponse(publickey); err != nil {
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
		if r.targetId == "" {
			r.targetId = r.activationToken
		}

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

func (r *Registration) getRegistrationResponse(publickey *keypair.PublicKey) (RegistrationResponse, error) {
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
	if r.targetId == "" {
		r.targetId = r.activationToken
	}

	// Create our request
	req := RegistrationRequest{
		PublicKey:       publickey.String(),
		ActivationCode:  r.activationToken,
		Version:         r.version,
		EnvironmentId:   r.environmentId,
		EnvironmentName: r.environmentName,
		TargetName:      r.targetName,
		TargetHostName:  hostname,
		TargetId:        r.targetId,
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
