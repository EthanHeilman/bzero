package data

type KeyShardData []KeyEntry

type PublicKey struct {
	N []byte `json:"n"`
	E int    `json:"e"`
}

type SplitPrivateKey struct {
	PublicKey PublicKey `json:"associatedPublicKey"`
	D         []byte    `json:"d"`
	E         []byte    `json:"e"`
}

type KeyEntry struct {
	Key       SplitPrivateKey `json:"key"`
	TargetIds []string        `json:"targetIds"`
}
