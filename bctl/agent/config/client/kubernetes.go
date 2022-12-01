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
	secretName         = "bctl-%s-secret" // used for formatting with the config type
)

type kubernetesClient struct {
	client     coreV1Types.SecretInterface
	secretName string
	configType ConfigType

	// Used to keep track of changes between fetches and saves
	lastVersion string
}

func NewKubernetesClient(ctx context.Context, namespace string, configType ConfigType) (*kubernetesClient, error) {
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
		secretName: fmt.Sprintf(secretName, configType),
		configType: configType,
	}

	// Get our secrets object
	if _, err := config.client.Get(ctx, config.secretName, metaV1.GetOptions{}); err != nil {
		// If there is no secret there, create it
		var emptyData []byte
		switch configType {
		case Agent:
			emptyData, err = json.Marshal(data.AgentDataV2{})
		case KeyShard:
			emptyData, err = json.Marshal(data.KeyShardData{})
		default:
			return nil, fmt.Errorf("unsupported config type: %s", configType)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to marshal empty %s config data: %w", configType, err)
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

func (k *kubernetesClient) FetchAgentData() (data.AgentDataV2, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return data.AgentDataV2{}, fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	k.lastVersion = secret.ResourceVersion

	rawData, ok := secret.Data[configKey]
	if !ok {
		return data.AgentDataV2{}, fmt.Errorf("config does not exist")
	}

	if bytes.Equal(rawData, []byte(defaultSecretValue)) {
		return data.AgentDataV2{}, nil
	}

	// Grab and decode the data from the secrets store
	if config, err := decodeAgentData(rawData); err != nil {
		return data.AgentDataV2{}, err
	} else {
		return config, nil
	}
}

func (k *kubernetesClient) FetchKeyShardData() (data.KeyShardData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return data.KeyShardData{}, fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	k.lastVersion = secret.ResourceVersion

	rawData, ok := secret.Data[configKey]
	if !ok {
		return data.KeyShardData{}, fmt.Errorf("config does not exist")
	}

	if bytes.Equal(rawData, []byte(defaultSecretValue)) {
		return data.KeyShardData{}, nil
	}

	var config data.KeyShardData
	err = json.Unmarshal(rawData, &config)
	return config, err
}

func (k *kubernetesClient) Save(d interface{}) error {
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

func decodeAgentData(s []byte) (data.AgentDataV2, error) {
	var old data.AgentDataV1
	var new data.AgentDataV2

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
