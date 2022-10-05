module bastionzero.com/bctl/v1/bzerolib

go 1.18

replace bastionzero.com/bctl/v1/bctl => ../bctl

replace bastionzero.com/bctl/v1/bzerolib => ./

require (
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/cenkalti/backoff/v4 v4.1.3
	github.com/coreos/go-oidc/v3 v3.4.0
	github.com/gofrs/flock v0.8.1
	github.com/gorilla/websocket v1.5.0
	github.com/onsi/ginkgo/v2 v2.2.0
	github.com/onsi/gomega v1.20.2
	github.com/rs/zerolog v1.28.0
	github.com/stretchr/testify v1.8.0
	golang.org/x/crypto v0.0.0-20220926161630-eccd6366d1be
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/tomb.v2 v2.0.0-20161208151619-d5d1b5820637
	k8s.io/apimachinery v0.25.2
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/objx v0.4.0 // indirect
	golang.org/x/net v0.0.0-20220826154423-83b083e8dc8b // indirect
	golang.org/x/oauth2 v0.0.0-20220822191816-0ebed06d0094 // indirect
	golang.org/x/sys v0.0.0-20220728004956-3c1f35247d10 // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
