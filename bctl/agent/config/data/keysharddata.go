package data

type KeyShardData []KeyEntry

type PublicKey struct {
	N string `json:"n" yaml:"n"`
	E int    `json:"e" yaml:"e"`
}

type SplitPrivateKey struct {
	PublicKey PublicKey `json:"associatedPublicKey" yaml:"associatedPublicKey"`
	D         string    `json:"d" yaml:"d"`
	E         string    `json:"e" yaml:"e"`
}

type KeyEntry struct {
	Key       SplitPrivateKey `json:"key" yaml:"key"`
	TargetIds []string        `json:"targetIds" yaml:"targetIds"`
}
