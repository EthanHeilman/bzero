package config

import (
	"fmt"
	"net/url"
	"sync"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
)

type configFetchError string

func (e configFetchError) Error() string {
	return "failed to fetch config: " + string(e)
}

type configSaveError string

func (e configSaveError) Error() string {
	return "failed to save config: " + string(e)
}

type configClient interface {
	Fetch() (data.DataV2, error)
	Save(d data.DataV2) error
}

type Config struct {
	lock   sync.RWMutex
	data   data.DataV2
	client configClient
}

func Load(client configClient) (*Config, error) {
	if data, err := client.Fetch(); err != nil {
		return nil, configFetchError(err.Error())
	} else {
		return &Config{
			client: client,
			data:   data,
		}, nil
	}
}

func (c *Config) Reload() error {
	if newData, err := c.client.Fetch(); err != nil {
		return configFetchError(err.Error())
	} else {
		c.data = newData
	}
	return nil
}

func (c *Config) GetPublicKey() *keypair.PublicKey {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.PublicKey
}

func (c *Config) GetPrivateKey() *keypair.PrivateKey {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.PrivateKey
}

func (c *Config) GetIdpOrgId() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.IdpOrgId
}

func (c *Config) GetIdpProvider() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.IdpProvider
}

func (c *Config) GetServiceAccountJwksUrls() []string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.JwksUrlPatterns
}

func (c *Config) GetAgentIdentityToken() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.AgentIdentityToken
}

func (c *Config) GetShutdownInfo() (string, map[string]string) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.ShutdownReason, c.data.ShutdownState
}

func (c *Config) GetTargetId() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.TargetId
}

func (c *Config) GetServiceUrl() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.ServiceUrl
}

func (c *Config) SetVersion(version string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.Fetch()
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

func (c *Config) SetShutdownInfo(reason string, state map[string]string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.Fetch()
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

func (c *Config) SetAgentIdentityToken(token string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.Fetch()
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

func (c *Config) SetServiceAccountJwksUrl(jwksUrlPattern string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.Fetch()
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

func (c *Config) SetRegistrationData(
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

	current, err := c.client.Fetch()
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
