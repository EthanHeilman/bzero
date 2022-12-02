package data

type KeyShardData []KeyEntry

type PublicKey struct {
	N []byte `json:"n" yaml:"n"`
	E int    `json:"e" yaml:"e"`
}

type SplitPrivateKey struct {
	PublicKey PublicKey `json:"associatedPublicKey" yaml:"associatedPublicKey"`
	D         []byte    `json:"d" yaml:"d"`
	E         []byte    `json:"e" yaml:"e"`
}

type KeyEntry struct {
	Key       SplitPrivateKey `json:"key" yaml:"key"`
	TargetIds []string        `json:"targetIds" yaml:"targetIds"`
}
