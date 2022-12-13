package config

import (
	"fmt"
	"net/url"
	"sync"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
)

type agentConfigClient interface {
	FetchAgentData() (data.AgentDataV2, error)
	Save(d interface{}) error
}

type AgentConfig struct {
	lock   sync.RWMutex
	data   data.AgentDataV2
	client agentConfigClient
}

func LoadAgentConfig(client agentConfigClient) (*AgentConfig, error) {
	if data, err := client.FetchAgentData(); err != nil {
		return nil, configFetchError(err.Error())
	} else {
		return &AgentConfig{
			client: client,
			data:   data,
		}, nil
	}
}

func (c *AgentConfig) Reload() error {
	if newData, err := c.client.FetchAgentData(); err != nil {
		return configFetchError(err.Error())
	} else {
		c.data = newData
	}
	return nil
}

func (c *AgentConfig) GetPublicKey() *keypair.PublicKey {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.PublicKey
}

func (c *AgentConfig) GetPrivateKey() *keypair.PrivateKey {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.PrivateKey
}

func (c *AgentConfig) GetIdpOrgId() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.IdpOrgId
}

func (c *AgentConfig) GetIdpProvider() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.IdpProvider
}

func (c *AgentConfig) GetServiceAccountJwksUrls() []string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.JwksUrlPatterns
}

func (c *AgentConfig) GetAgentIdentityToken() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.AgentIdentityToken
}

func (c *AgentConfig) GetShutdownInfo() (string, map[string]string) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.ShutdownReason, c.data.ShutdownState
}

func (c *AgentConfig) GetTargetId() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.TargetId
}

func (c *AgentConfig) GetServiceUrl() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.ServiceUrl
}

func (c *AgentConfig) SetVersion(version string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchAgentData()
	if err != nil {
		return configFetchError(err.Error())
	}

	current.Version = version

	c.data = current
	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

func (c *AgentConfig) SetShutdownInfo(reason string, state map[string]string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchAgentData()
	if err != nil {
		return configFetchError(err.Error())
	}

	current.ShutdownReason = reason
	current.ShutdownState = state

	c.data = current
	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

func (c *AgentConfig) SetAgentIdentityToken(token string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchAgentData()
	if err != nil {
		return configFetchError(err.Error())
	}

	current.AgentIdentityToken = token

	c.data = current
	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

func (c *AgentConfig) SetServiceAccountJwksUrl(jwksUrlPattern string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchAgentData()
	if err != nil {
		return configFetchError(err.Error())
	}

	if parsedJwksUrlPattern, err := url.ParseRequestURI(jwksUrlPattern); err != nil {
		return fmt.Errorf("failed to parse as url the provided jwks url %s: %w", jwksUrlPattern, err)
	} else {
		// Ensure that this pattern does not exist already
		for _, existingJwksUrl := range current.JwksUrlPatterns {
			if existingJwksUrl == parsedJwksUrlPattern.String() {
				return nil
			}
		}

		current.JwksUrlPatterns = append(current.JwksUrlPatterns, parsedJwksUrlPattern.String())
	}

	c.data = current
	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

func (c *AgentConfig) SetRegistrationData(
	serviceUrl string,
	publickey *keypair.PublicKey,
	privateKey *keypair.PrivateKey,
	idpProvider string,
	idpOrgId string,
	targetId string,
	jwksUrlPatterns []string,
) error {

	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchAgentData()
	if err != nil {
		return configFetchError(err.Error())
	}

	current.ServiceUrl = serviceUrl
	current.PublicKey = publickey
	current.PrivateKey = privateKey
	current.IdpProvider = idpProvider
	current.IdpOrgId = idpOrgId
	current.TargetId = targetId

	// Sanitize jwksUrlPatterns before adding them to config
	for _, jwksUrl := range jwksUrlPatterns {
		if parsedJwksUrl, err := url.ParseRequestURI(jwksUrl); err != nil {
			return fmt.Errorf("failed to parse as url provided jwks url %s: %w", jwksUrl, err)
		} else {
			current.JwksUrlPatterns = append(current.JwksUrlPatterns, parsedJwksUrl.String())
		}
	}

	// Vacate our agent identity token because a new registration means we need a new
	// one even if the previous one remains verifiable
	current.AgentIdentityToken = ""

	c.data = current
	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}
