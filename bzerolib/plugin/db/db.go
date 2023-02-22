package db

type DbAction string

const (
	Dial DbAction = "dial"
	Pwdb DbAction = "pwdb"
)

type DbActionParams struct {
	SchemaVersion string `json:"schemaVersion"`
	RemotePort    int    `json:"remotePort"`
	RemoteHost    string `json:"remoteHost"`
}
