module bastionzero.com/bctl/v1/bzerolib

go 1.16

replace bastionzero.com/bctl/v1/bctl => ../bctl

replace bastionzero.com/bctl/v1/bzerolib => ./

require (
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/cenkalti/backoff/v4 v4.1.1
	github.com/coreos/go-oidc/v3 v3.0.0
	github.com/gofrs/flock v0.8.1
	github.com/gorilla/websocket v1.4.2
	github.com/onsi/ginkgo/v2 v2.1.4
	github.com/onsi/gomega v1.19.0
	github.com/rs/zerolog v1.24.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/tomb.v2 v2.0.0-20161208151619-d5d1b5820637
	k8s.io/apimachinery v0.21.3
)

require (
	github.com/google/go-cmp v0.5.6 // indirect
	golang.org/x/oauth2 v0.0.0-20210628180205-a41e5a781914 // indirect
	golang.org/x/sys v0.0.0-20220520151302-bc2c85ada10a // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
)
