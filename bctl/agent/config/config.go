package config

import (
	"context"
	"encoding/base64"
	"sync"

	"bastionzero.com/bctl/v1/bzerolib/messagesigner"
)

type configFetchError string

func (e configFetchError) Error() string {
	return "failed to fetch config: " + string(e)
}

type configSaveError string

func (e configSaveError) Error() string {
	return "failed to save config: " + string(e)
}

type client interface {
	Fetch() (data, error)
	Save(d data) error
	WaitForRegistration(ctx context.Context) error
}

type Config struct {
	lock   sync.RWMutex
	data   data
	client client
}

func LoadSystemdConfig(configDir string) (*Config, error) {
	config := &Config{}
	var err error

	if config.client, err = newSystemdClient(configDir); err != nil {
		return nil, err
	}

	if config.data, err = config.client.Fetch(); err != nil {
		return nil, configFetchError(err.Error())
	}

	return config, nil
}

func LoadKubernetesConfig(ctx context.Context, namespace string, targetName string) (*Config, error) {
	config := &Config{}
	var err error

	if config.client, err = newKubernetesClient(ctx, namespace, targetName); err != nil {
		return nil, err
	}

	if config.data, err = config.client.Fetch(); err != nil {
		return nil, configFetchError(err.Error())
	}

	return config, nil
}

func (c *Config) WaitForRegistration(ctx context.Context) error {
	return c.client.WaitForRegistration(ctx)
}

func (c *Config) Reload() error {
	if newData, err := c.client.Fetch(); err != nil {
		return err
	} else {
		c.data = newData
	}
	return nil
}

func (c *Config) GetPublicKey() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.data.PublicKey
}

func (c *Config) GetPrivateKey() string {
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

func (c *Config) GetMessageSigner() (*messagesigner.MessageSigner, error) {
	privKey, _ := base64.StdEncoding.DecodeString(c.GetPrivateKey())
	return messagesigner.New(privKey)
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

func (c *Config) SetRegistrationData(
	serviceUrl string,
	publickey string,
	privateKey string,
	idpProvider string,
	idpOrgId string,
	targetId string,
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

	// Vacate our agent identity token because a new registration means we need a new
	// one even if the previous one remains verifiable
	current.AgentIdentityToken = ""

	c.data = current
	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}
