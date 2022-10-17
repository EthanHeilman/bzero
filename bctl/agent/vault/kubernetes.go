package vault

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"sync"

	"bastionzero.com/bctl/v1/bzerolib/messagesigner"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreV1Types "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	vaultKey           = "secret"
	defaultSecretValue = "coolbeans"
	secretName         = "bctl-%s-secret" // used for formatting with the target name
)

type KubernetesVault struct {
	data      vault
	vaultLock sync.RWMutex

	client coreV1Types.SecretInterface
	secret *coreV1.Secret
}

func LoadKubernetesVault(ctx context.Context, namespace string, targetName string) (*KubernetesVault, error) {
	// Create our api object
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error grabbing cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating new config: %w", err)
	}

	// Create our secrets client
	kubeVault := KubernetesVault{
		client: clientset.CoreV1().Secrets(namespace),
	}

	// Get our secrets object
	kubeVault.secret, err = kubeVault.client.Get(ctx, secretName, metaV1.GetOptions{})
	if err != nil {
		// If there is no secret there, create it
		if err := kubeVault.initVault(ctx, targetName); err != nil {
			return nil, err
		}
		return &kubeVault, nil
	}

	// Our vault exists but it was never initialized so we initialize it
	if _, ok := kubeVault.secret.Data[vaultKey]; !ok {
		if err := kubeVault.initVault(ctx, targetName); err != nil {
			return nil, err
		}
		return &kubeVault, nil
	}

	// Our vault exists so we can load it up
	if kubeVault.data, err = kubeVault.fetchVault(); err != nil {
		return nil, err
	}

	return &kubeVault, nil
}

func (k *KubernetesVault) initVault(ctx context.Context, targetName string) error {
	formattedSecretName := fmt.Sprintf(secretName, targetName)

	emptyVault, err := gobEncode(vault{})
	if err != nil {
		return fmt.Errorf("failed to gob encode empty vault: %w", err)
	}

	vaultData := map[string][]byte{
		vaultKey: emptyVault,
	}

	object := metaV1.ObjectMeta{Name: formattedSecretName}
	secret := &coreV1.Secret{Data: vaultData, ObjectMeta: object}

	if _, err := k.client.Create(ctx, secret, metaV1.CreateOptions{}); err != nil {
		return fmt.Errorf("error creating secrets client: %w", err)
	}

	k.secret = secret
	return nil
}

func (k *KubernetesVault) fetchVault() (vault, error) {
	if rawData, ok := k.secret.Data[vaultKey]; ok {
		// Grab and decode the data from the secrets store
		if vaultData, err := gobDecode(rawData); err != nil {
			return vault{}, err
		} else {
			return vaultData, nil
		}
	} else {
		return vault{}, fmt.Errorf("vault does not exist")
	}
}

func (k *KubernetesVault) GetPublicKey() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.PublicKey
}

func (k *KubernetesVault) GetPrivateKey() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.PrivateKey
}

func (k *KubernetesVault) GetIdpOrgId() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.IdpOrgId
}

func (k *KubernetesVault) GetIdpProvider() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.IdpProvider
}

func (k *KubernetesVault) GetAgentIdentityToken() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.AgentIdentityToken
}

func (k *KubernetesVault) GetTargetId() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.TargetId
}

func (k *KubernetesVault) GetShutdownInfo() (string, map[string]string) {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.ShutdownReason, k.data.ShutdownState
}

func (k *KubernetesVault) GetServiceUrl() string {
	k.vaultLock.RLock()
	defer k.vaultLock.RUnlock()

	return k.data.ServiceUrl
}

func (k *KubernetesVault) GetMessageSigner() (*messagesigner.MessageSigner, error) {
	privKey, _ := base64.StdEncoding.DecodeString(k.GetPrivateKey())
	return messagesigner.New(privKey)
}

func (k *KubernetesVault) SetVersion(version string) error {
	k.vaultLock.Lock()
	defer k.vaultLock.Unlock()

	// Load our vault so we make sure we're never saving old state
	currentVault, err := k.fetchVault()
	if err != nil {
		return err
	}

	// If our private keys are mismatched, it means a new registration
	// has happened and we shouldn't write anything
	// if k.data.PrivateKey != currentVault.PrivateKey {
	// 	return fmt.Errorf("new registration detected, reload vault")
	// }

	currentVault.Version = version

	k.data = currentVault
	return k.save()
}

func (k *KubernetesVault) SetShutdownInfo(reason string, state map[string]string) error {
	k.vaultLock.Lock()
	defer k.vaultLock.Unlock()

	// Load our vault so we make sure we're never saving old state
	currentVault, err := k.fetchVault()
	if err != nil {
		return err
	}

	currentVault.ShutdownReason = reason
	currentVault.ShutdownState = state

	k.data = currentVault
	return k.save()
}

func (k *KubernetesVault) SetAgentIdentityToken(token string) error {
	k.vaultLock.Lock()
	defer k.vaultLock.Unlock()

	k.data.AgentIdentityToken = token

	return k.save()
}

func (k *KubernetesVault) SetRegistrationData(
	serviceUrl string,
	publickey string,
	privateKey string,
	idpProvider string,
	idpOrgId string,
	targetId string,
) error {

	k.vaultLock.Lock()
	defer k.vaultLock.Unlock()

	currentVault, err := k.fetchVault()
	if err != nil {
		return fmt.Errorf("failed to load vault: %w", err)
	}

	// TODO: think through this
	// If our private keys are mismatched, it means a new registration
	// has happened and we shouldn't write anything
	// if k.data.PrivateKey != currentVault.PrivateKey {
	// 	return fmt.Errorf("new registration detected, reload vault")
	// }

	currentVault.ServiceUrl = serviceUrl
	currentVault.PublicKey = publickey
	currentVault.PrivateKey = privateKey
	currentVault.IdpProvider = idpProvider
	currentVault.IdpOrgId = idpOrgId
	currentVault.TargetId = targetId

	k.data = currentVault
	return k.save()
}

func (k *KubernetesVault) save() error {
	// Now encode the secretConfig
	dataBytes, err := gobEncode(k.data)
	if err != nil {
		return err
	}

	// Now update the kube secret object
	k.secret.Data[vaultKey] = dataBytes

	// Update the secret
	if secret, err := k.client.Update(context.Background(), k.secret, metaV1.UpdateOptions{}); err != nil {
		return fmt.Errorf("could not update secret client: %w", err)
	} else {
		k.secret = secret
		return nil
	}
}

func gobEncode(p interface{}) ([]byte, error) {
	// Ref: https://gist.github.com/SteveBate/042960baa7a4795c3565
	// Remove secrets client
	buf := bytes.Buffer{}
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(p)
	return buf.Bytes(), err
}

func gobDecode(s []byte) (vault, error) {
	// Ref: https://gist.github.com/SteveBate/042960baa7a4795c3565
	p := vault{}
	dec := gob.NewDecoder(bytes.NewReader(s))
	err := dec.Decode(&p)
	return p, err
}
