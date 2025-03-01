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

type TCPApplication string

const (
	RDP       TCPApplication = "rdp"
	DB        TCPApplication = "db"
	SQLSERVER TCPApplication = "sqlserver"
)

type RDPActionParams struct {
	SchemaVersion string `json:"schemaVersion"`
	RemotePort    int    `json:"remotePort"`
	RemoteHost    string `json:"remoteHost"`
}

type SQLServerActionParams struct {
	SchemaVersion string `json:"schemaVersion"`
	RemotePort    int    `json:"remotePort"`
	RemoteHost    string `json:"remoteHost"`
}
