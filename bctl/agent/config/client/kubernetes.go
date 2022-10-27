package client

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreV1Types "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	configKey          = "secret"
	defaultSecretValue = "coolbeans"
	secretName         = "bctl-%s-secret" // used for formatting with the target name
)

type kubernetesClient struct {
	client     coreV1Types.SecretInterface
	secretName string

	// Used to keep track of changes between fetches and saves
	lastVersion string
}

func NewKubernetesClient(ctx context.Context, namespace string, targetName string) (*kubernetesClient, error) {
	// Create our api object
	kubeConf, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error grabbing cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(kubeConf)
	if err != nil {
		return nil, fmt.Errorf("error creating new config: %w", err)
	}

	// Create our secrets client
	config := kubernetesClient{
		client:     clientset.CoreV1().Secrets(namespace),
		secretName: fmt.Sprintf(secretName, targetName),
	}

	// Get our secrets object
	if _, err := config.client.Get(ctx, config.secretName, metaV1.GetOptions{}); err != nil {
		// If there is no secret there, create it
		emptyData, err := json.Marshal(data.DataV2{})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal empty data: %w", err)
		}

		configData := map[string][]byte{
			configKey: emptyData,
		}

		object := metaV1.ObjectMeta{Name: config.secretName}
		secret := &coreV1.Secret{Data: configData, ObjectMeta: object}

		if _, err := config.client.Create(ctx, secret, metaV1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("error creating secrets client: %w", err)
		}
	}

	return &config, nil
}

func (k *kubernetesClient) Fetch() (data.DataV2, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return data.DataV2{}, fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	k.lastVersion = secret.ResourceVersion

	rawData, ok := secret.Data[configKey]
	if !ok {
		return data.DataV2{}, fmt.Errorf("config does not exist")
	}

	if bytes.Equal(rawData, []byte(defaultSecretValue)) {
		return data.DataV2{}, nil
	}

	// Grab and decode the data from the secrets store
	if config, err := decode(rawData); err != nil {
		return data.DataV2{}, err
	} else {
		return config, nil
	}
}

func (k *kubernetesClient) Save(d data.DataV2) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	// Make sure we're not overwriting any data
	if secret.ResourceVersion != k.lastVersion {
		return fmt.Errorf("the config has changed since it was last fetched")
	}

	// Now encode the secretConfig
	dataBytes, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("failed to marshal data object: %w", err)
	}

	// Now update the kube secret object
	secret.Data[configKey] = dataBytes

	// Update the secret
	if _, err := k.client.Update(ctx, secret, metaV1.UpdateOptions{}); err != nil {
		return fmt.Errorf("could not update secret client: %w", err)
	}
	return nil
}

func decode(s []byte) (data.DataV2, error) {
	var old data.DataV1
	var new data.DataV2

	// first we try to gob decode, it only speaks old
	dec := gob.NewDecoder(bytes.NewReader(s))
	if err := dec.Decode(&old); err != nil {
		// if we failed to decode above, we've already done the json conversion
		// and so we can just unmarshal that
		err = json.Unmarshal(s, &new)
		return new, err
	} else {
		// now we need to convert this to our new version
		oldBytes, _ := json.Marshal(old)
		err = json.Unmarshal(oldBytes, &new)
		return new, err
	}
}