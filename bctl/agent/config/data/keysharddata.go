package data

type KeyShardData []MappedKeyEntry

type MappedKeyEntry struct {
	KeyData   KeyEntry `json:"keyData"`
	TargetIds []string `json:"targetIds"`
}

type KeyEntry struct {
	KeyShardPem string `json:"keyShardPem"`
	CaCertPem   string `json:"caCertPem"`
}
