package zliconfig

import (
	"fmt"
	"os/exec"
	"strings"
)

type ZLIConfig struct {
	CertConfig BZCertConfig `json:"mrtap"`
	TokenSet   IdPTokenSet  `json:"tokenSet"`

	// unexported members
	configPath     string
	refreshCommand string
}
type IdPTokenSet struct {
	CurrentIdToken string `json:"id_token"`
}

type BZCertConfig struct {
	PrivateKey       string `json:"privateKey"`
	PublicKey        string `json:"publicKey"`
	CerRand          string `json:"cerRand"`
	CerRandSignature string `json:"cerRandSig"`
	InitialIdToken   string `json:"initialIdToken"`
	OrgIssuerId      string `json:"orgIssuerId"`
	OrgProvider      string `json:"orgProvider"`
}

func New(configPath string, refreshCommand string) (*ZLIConfig, error) {
	if configPath == "" {
		return nil, fmt.Errorf("no config path provided")
	} else if splits := strings.Split(refreshCommand, " "); len(splits) < 2 {
		return nil, fmt.Errorf("malformed refresh command")
	}

	config := &ZLIConfig{
		configPath:     configPath,
		refreshCommand: refreshCommand,
	}

	if err := config.Load(); err != nil {
		return nil, fmt.Errorf("failed to load zli config: %w", err)
	} else {
		return config, nil
	}
}

func (z *ZLIConfig) Load() error {
	// load based on operating system defined in the appropriate files
	return z.load()
}

func (z *ZLIConfig) Refresh() error {
	if z.refreshCommand == "" {
		return fmt.Errorf("could not refresh zli config, because no refresh command was found")
	}

	// Update the id token by calling the passed in zli refresh command
	if err := runRefreshCommand(z.refreshCommand); err != nil {
		return err
	}

	// Reload the zli config
	if err := z.Load(); err != nil {
		return fmt.Errorf("failed to load zli config: %w", err)
	}

	return nil
}

func runRefreshCommand(refreshCommand string) error {
	if splits := strings.Split(refreshCommand, " "); len(splits) >= 2 {
		if out, err := exec.Command(splits[0], splits[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to execute zli refresh token command: {Command Output: %s, Error: %w}", string(out), err)
		}
	} else {
		return fmt.Errorf("not enough arguments to refresh token zli command: %d", len(splits))
	}
	return nil
}
