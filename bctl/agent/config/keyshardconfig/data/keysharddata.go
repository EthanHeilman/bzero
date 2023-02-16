package data

// Why wrap the array in a struct? We want to avoid having a primative array as a top-level field
// because it's fairly rigid as JSON. If the structure changes, we can just add fields, which makes it
// much easier for different versions of the bastion, zli, and agent to talk to each other
type KeyShardData struct {
	Keys []MappedKeyEntry `json:"keys"`
}

type MappedKeyEntry struct {
	KeyData   KeyEntry `json:"key"`
	TargetIds []string `json:"targetIds"`
}

type KeyEntry struct {
	KeyShardPem string `json:"keyShardPem"`
	CaCertPem   string `json:"caCertPem"`
}
