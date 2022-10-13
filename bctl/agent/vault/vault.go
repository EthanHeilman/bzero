package vault

import "crypto/ed25519"

type AgentType string

const (
	Kubernetes AgentType = "kubernetes"
	SystemD    AgentType = "systemd"
)

type Config interface {
	GetPublicKey() string
	GetPrivateKey() ed25519.PrivateKey
	GetIdpOrgId() string
	GetIdpProvider() string
	GetAgentIdentityToken() string

	SetVersion(version string) error
	SetShutdown(reason string, state map[string]string) error
	SetAgentIdentityToken(token string) error
}

type vault struct {
	Version string

	// Who is in charge of this agent? Kubernetes or SystemD
	AgentType string

	// Agent signature key pair
	// Our public key is stored as a base64 encoded string because
	// it is only ever used to send in that format
	// The private key is used to sign and therefore is stored in its
	// most usable format
	PublicKey  string
	PrivateKey ed25519.PrivateKey

	// OIDC-compliant token used for authenticating to BastionZero
	AgentIdentityToken string

	// This is the primary key of the agent table, we use this because
	// we currently have no way to guarantee unique public keys
	TargetId string

	// These values are compared against the user's id token to verify
	// they belong to the same org as the agent
	IdpProvider string
	IdpOrgId    string

	// URL of our Bastion
	ServiceUrl string

	// Policy environment identifiers
	EnvironmentId   string
	EnvironmentName string

	// For reporting back to BastionZero why the agent shutdown
	ShutdownReason string
	ShutdownState  map[string]string
}

// func WaitForNewRegistration(logger *logger.Logger) error {
// 	watcher, err := fsnotify.NewWatcher()
// 	if err != nil {
// 		return fmt.Errorf("error starting new file watcher: %s", err)
// 	}
// 	defer watcher.Close()

// 	done := make(chan error)
// 	go func() {
// 		for {
// 			select {
// 			case event, ok := <-watcher.Events:
// 				if !ok {
// 					done <- fmt.Errorf("file watcher closed events channel")
// 				}

// 				if event.Op&fsnotify.Write == fsnotify.Write {
// 					var config vault
// 					if file, err := ioutil.ReadFile(defaultPath); err != nil {
// 						continue
// 					} else if err := json.Unmarshal([]byte(file), &config); err != nil {
// 						continue
// 					} else {
// 						// if we haven't completed registration yet, continue waiting
// 						if config.PublicKey == "" {
// 							continue
// 						} else {
// 							done <- nil
// 						}
// 					}
// 				}
// 			case err, ok := <-watcher.Errors:
// 				if !ok {
// 					done <- fmt.Errorf("file watcher closed errors channel")
// 				}
// 				done <- fmt.Errorf("file watcher caught error: %s", err)
// 			}
// 		}
// 	}()

// 	if err := watcher.Add(defaultPath); err != nil {
// 		return fmt.Errorf("unable to watch file: %s, error: %s", defaultPath, err)
// 	}

// 	return <-done
// }

// func (v *Vault) GetMessageSigner() (*messagesigner.MessageSigner, error) {
// 	privKey, _ := base64.StdEncoding.DecodeString(v.GetPrivateKey())
// 	return messagesigner.New(privKey)
// }
