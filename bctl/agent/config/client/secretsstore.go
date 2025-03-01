package client

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"time"

	agentdata "bastionzero.com/agent/config/agentconfig/data"
	ksdata "bastionzero.com/agent/config/keyshardconfig/data"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreV1Types "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	agentConfigKey     = "secret"
	agentSecretFormula = "bctl-%s-secret" // used for formatting with the target name
	defaultSecretValue = "coolbeans"

	keyShardConfigKey     = "keyshards"
	keyShardSecretFormula = "bctl-%s-keyshards-secret" // used for formatting with the target name
)

type secretsStore struct {
	client     coreV1Types.SecretInterface
	secretName string
	configType ConfigType
	configKey  string

	// Used to keep track of changes between fetches and saves
	lastAgentVersion    string
	lastKeyShardVersion string
}

func NewSecretsStore(ctx context.Context, namespace string, targetName string, configType ConfigType) (*secretsStore, error) {
	// Create our api object
	kubeConf, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error grabbing cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(kubeConf)
	if err != nil {
		return nil, fmt.Errorf("error creating new config: %w", err)
	}

	var configKey, secretFormula string
	var emptyData []byte
	switch configType {
	case Agent:
		configKey = agentConfigKey
		secretFormula = agentSecretFormula
		emptyData, err = json.Marshal(agentdata.AgentDataV2{})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal empty agent config data: %w", err)
		}
	case KeyShard:
		configKey = keyShardConfigKey
		secretFormula = keyShardSecretFormula
		emptyData, err = json.Marshal(ksdata.KeyShardData{})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal empty keyshard config data: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config type: %s", configType)
	}

	// Create our secrets client
	config := secretsStore{
		client:     clientset.CoreV1().Secrets(namespace),
		secretName: fmt.Sprintf(secretFormula, targetName),
		configType: configType,
		configKey:  configKey,
	}

	// Get our secrets object
	if existingSecret, err := config.client.Get(ctx, config.secretName, metaV1.GetOptions{}); err != nil {
		// If there is no secret there, create one
		configData := map[string][]byte{
			configKey: emptyData,
		}

		object := metaV1.ObjectMeta{Name: config.secretName}
		newSecret := &coreV1.Secret{ObjectMeta: object, Data: configData}

		if _, err := config.client.Create(ctx, newSecret, metaV1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("error creating secrets client: %w", err)
		}
	} else {
		// if the secret already exists, we need to make sure it is initialized
		if _, ok := existingSecret.Data[configKey]; !ok {
			existingSecret.Data[configKey] = emptyData
		}

		if _, err := config.client.Update(ctx, existingSecret, metaV1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("could not update secret client: %w", err)
		}
	}

	return &config, nil
}

func (k *secretsStore) FetchAgentData() (agentdata.AgentDataV2, error) {
	if k.configType != Agent {
		return agentdata.AgentDataV2{}, fmt.Errorf("cannot fetch agent data with %s client", k.configType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return agentdata.AgentDataV2{}, fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	k.lastAgentVersion = secret.ResourceVersion

	rawData, ok := secret.Data[k.configKey]
	if !ok {
		return agentdata.AgentDataV2{}, fmt.Errorf("agent config does not exist")
	}

	if bytes.Equal(rawData, []byte(defaultSecretValue)) {
		return agentdata.AgentDataV2{}, nil
	}

	// Grab and decode the data from the secrets store
	if config, err := decodeAgentData(rawData); err != nil {
		return agentdata.AgentDataV2{}, err
	} else {
		return config, nil
	}
}

func (k *secretsStore) FetchKeyShardData() (ksdata.KeyShardData, error) {
	if k.configType != KeyShard {
		return ksdata.KeyShardData{}, fmt.Errorf("cannot fetch key shard data with %s client", k.configType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return ksdata.KeyShardData{}, fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	k.lastKeyShardVersion = secret.ResourceVersion

	rawData, ok := secret.Data[k.configKey]
	if !ok {
		return ksdata.KeyShardData{}, fmt.Errorf("key shard config does not exist")
	}

	if bytes.Equal(rawData, []byte(defaultSecretValue)) {
		return ksdata.KeyShardData{}, nil
	}

	var config ksdata.KeyShardData
	err = json.Unmarshal(rawData, &config)
	return config, err
}

func (k *secretsStore) Save(d interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	secret, err := k.client.Get(ctx, k.secretName, metaV1.GetOptions{})
	if err != nil {
		return fmt.Errorf("config secret %s does not exist", k.secretName)
	}

	// Make sure we're not overwriting any data
	if (k.configType == Agent && secret.ResourceVersion != k.lastAgentVersion) ||
		(k.configType == KeyShard && secret.ResourceVersion != k.lastKeyShardVersion) {
		return fmt.Errorf("the config has changed since it was last fetched")
	}

	// Now encode the secretConfig
	dataBytes, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("failed to marshal data object: %w", err)
	}

	// Now update the kube secret object
	secret.Data[k.configKey] = dataBytes

	// Update the secret
	if _, err := k.client.Update(ctx, secret, metaV1.UpdateOptions{}); err != nil {
		return fmt.Errorf("could not update secret client: %w", err)
	}
	return nil
}

func decodeAgentData(s []byte) (agentdata.AgentDataV2, error) {
	var old agentdata.AgentDataV1
	var new agentdata.AgentDataV2

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
