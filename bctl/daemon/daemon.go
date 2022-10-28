package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"bastionzero.com/bctl/v1/bctl/daemon/exit"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/dataconnection"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/dbserver"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/kubeserver"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/shellserver"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/sshserver"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/webserver"
	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/report"

	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert/zliconfig"
	bzlogger "bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
)

const (
	daemonVersion  = "$DAEMON_VERSION"
	prodServiceUrl = "https://cloud.bastionzero.com"
)

type Server interface {
	Start() error
	Close(err error)
}

func main() {
	envErr := loadEnvironment()

	if logger, err := createLogger(config[DEBUG].Value == "true"); err != nil {
		reportError(logger, err)
	} else {
		// print out loadEnvironment error now
		if envErr != nil {
			reportError(logger, envErr)
		} else {

			// how the daemon tells the server to stop
			daemonShutdownChan := make(chan struct{})
			// any server that experiences a fatal error writes to this channel
			// additionally, ephemeral servers write to this channel when their datachannel is done
			serverErrChan := make(chan error)
			go startServer(logger, daemonShutdownChan, serverErrChan)

			for {
				select {
				// we should never exit without allowing our server to shutdown gracefully
				// therefore our response to a SIGINT or SIGTERM is to tell the server to say its goodbyes
				case signal := <-bzos.OsShutdownChan():
					logger.Errorf("received shutdown signal: %s", signal.String())
					close(daemonShutdownChan)
					// but we still wait for it to signal that it's ready to die
				case err := <-serverErrChan:
					/* "If your daemon cleanup code isn't in this block, it's in the wrong place!" -management */
					exit.HandleDaemonExit(err, logger)
				}
			}
		}
	}
}

func createLogger(debug bool) (*bzlogger.Logger, error) {
	options := &bzlogger.Config{
		FilePath: config[LOG_PATH].Value,
	}

	// For ssh plugins we proxy the ssh protocol directly from Stdin/Stdout so
	// we dont want our logs to show up there

	// Otherwise log output to stdout if the daemon is started up in debug mode
	plugin := config[PLUGIN].Value
	if debug && plugin != string(bzplugin.Ssh) {
		options.ConsoleWriters = []io.Writer{os.Stdout}
	}

	logger, err := bzlogger.New(options)
	logger.AddDaemonVersion(daemonVersion)
	return logger, err
}

func reportError(logger *bzlogger.Logger, err error) {
	if logger != nil {
		logger.Error(err)
	}

	errReport := report.ErrorReport{
		Reporter:  "daemon-" + daemonVersion,
		Timestamp: fmt.Sprint(time.Now().UTC().Unix()),
		Message:   err.Error(),
		State: map[string]string{
			"targetHostName": "",
			"goos":           runtime.GOOS,
			"goarch":         runtime.GOARCH,
		},
	}

	if err := report.ReportError(context.TODO(), config[SERVICE_URL].Value, errReport); err != nil && logger != nil {
		logger.Errorf("failed to report error to BastionZero: %s", err)
	}
}

func startServer(logger *bzlogger.Logger, daemonShutdownChan chan struct{}, errChan chan error) {
	plugin := config[PLUGIN].Value
	logger.Infof("Opening connection to the Connection Node: %s for %s plugin", config[CONNECTION_SERVICE_URL].Value, plugin)

	// create our MrZAP object
	zliConfig, err := zliconfig.New(config[CONFIG_PATH].Value, config[REFRESH_TOKEN_COMMAND].Value)
	if err != nil {
		errChan <- err
		return
	}

	publicKey, err := keypair.PublicKeyFromString(config[AGENT_PUB_KEY].Value)
	if err != nil {
		errChan <- err
		return
	}

	// This validates the bzcert before creating the server so we can fail
	// fast if the cert is no longer valid. This may result in prompting the
	// user to login again if the cert contains expired IdP id tokens
	logger.Debug("verifying bzcert")
	cert, err := bzcert.New(zliConfig)
	if err != nil {
		// don't attach a message here because we read this error type
		errChan <- err
		return
	}
	logger.Debug("done verifying bzcert")

	// Create our headers, these are shared by everyone
	headers := http.Header{
		"Authorization": {config[AUTH_HEADER].Value},
		"Cookie":        {fmt.Sprintf("sessionId=%s", config[SESSION_ID].Value), fmt.Sprintf("sessionToken=%s", config[SESSION_TOKEN].Value)},
	}

	params := url.Values{
		"version":      {daemonVersion},
		"connectionId": {config[CONNECTION_ID].Value},
		"authToken":    {config[CONNECTION_SERVICE_AUTH_TOKEN].Value},
	}

	var server Server

	switch bzplugin.PluginName(plugin) {
	case bzplugin.Db:
		server, err = newDbServer(logger, publicKey, errChan, headers, params, cert)
	case bzplugin.Kube:
		server, err = newKubeServer(logger, publicKey, errChan, headers, params, cert)
	case bzplugin.Shell:
		server, err = newShellServer(logger, publicKey, errChan, headers, params, cert)
	case bzplugin.Ssh:
		server, err = newSshServer(logger, publicKey, errChan, headers, params, cert)
	case bzplugin.Web:
		server, err = newWebServer(logger, publicKey, errChan, headers, params, cert)
	default:
		errChan <- fmt.Errorf("unhandled plugin passed when trying to start server: %s", plugin)
	}

	if err != nil {
		errChan <- fmt.Errorf("failed to initialize %s server: %s", plugin, err)
	} else {
		// await external shutdown
		go listenForShutdown(daemonShutdownChan, server)

		// start accepting requests
		if err := server.Start(); err != nil {
			errChan <- fmt.Errorf("failed to start running %s server: %s", plugin, err)
		}
	}
}

func listenForShutdown(shutdownChan <-chan struct{}, server Server) {
	if _, ok := <-shutdownChan; !ok {
		server.Close(&bzos.OsInterruptError{})
	}
}

func newSshServer(logger *bzlogger.Logger, publicKey *keypair.PublicKey, errChan chan error, headers http.Header, params url.Values, cert *bzcert.DaemonBZCert) (*sshserver.SshServer, error) {
	subLogger := logger.GetComponentLogger("sshserver")

	// Check if remote port is valid
	remotePort, err := strconv.Atoi(config[REMOTE_PORT].Value)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote port: %s", err)
	}

	params["connectionType"] = []string{string(dataconnection.Ssh)}
	params["target_id"] = []string{config[TARGET_ID].Value}
	params["target_user"] = []string{config[TARGET_USER].Value}
	params["remote_host"] = []string{config[REMOTE_HOST].Value}
	params["remote_port"] = []string{config[REMOTE_PORT].Value}

	return sshserver.New(
		subLogger,
		errChan,
		config[TARGET_USER].Value,
		config[DATACHANNEL_ID].Value,
		cert,
		config[CONNECTION_SERVICE_URL].Value,
		params,
		headers,
		publicKey,
		config[IDENTITY_FILE].Value,
		config[KNOWN_HOSTS_FILE].Value,
		strings.Split(config[HOSTNAMES].Value, ","),
		config[REMOTE_HOST].Value,
		remotePort,
		config[LOCAL_PORT].Value,
		config[SSH_ACTION].Value,
	)
}

func newShellServer(logger *bzlogger.Logger, publicKey *keypair.PublicKey, errChan chan error, headers http.Header, params url.Values, cert *bzcert.DaemonBZCert) (*shellserver.ShellServer, error) {
	subLogger := logger.GetComponentLogger("shellserver")

	params["connectionType"] = []string{string(dataconnection.Shell)}

	return shellserver.New(
		subLogger,
		errChan,
		config[TARGET_USER].Value,
		config[DATACHANNEL_ID].Value,
		cert,
		config[CONNECTION_SERVICE_URL].Value,
		params,
		headers,
		publicKey,
	)
}

func newWebServer(logger *bzlogger.Logger, publicKey *keypair.PublicKey, errChan chan error, headers http.Header, params url.Values, cert *bzcert.DaemonBZCert) (*webserver.WebServer, error) {
	subLogger := logger.GetComponentLogger("webserver")

	remotePort, err := strconv.Atoi(config[REMOTE_PORT].Value)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote port: %w", err)
	}

	params["connectionType"] = []string{string(dataconnection.Web)}
	params["target_id"] = []string{config[TARGET_ID].Value}

	return webserver.New(
		subLogger,
		errChan,
		config[LOCAL_PORT].Value,
		config[LOCAL_HOST].Value,
		remotePort,
		config[REMOTE_HOST].Value,
		cert,
		config[CONNECTION_SERVICE_URL].Value,
		params,
		headers,
		publicKey,
	)
}

func newDbServer(logger *bzlogger.Logger, publicKey *keypair.PublicKey, errChan chan error, headers http.Header, params url.Values, cert *bzcert.DaemonBZCert) (*dbserver.DbServer, error) {
	subLogger := logger.GetComponentLogger("dbserver")

	remotePort, err := strconv.Atoi(config[REMOTE_PORT].Value)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote port: %s", err)
	}

	params["connectionType"] = []string{string(dataconnection.Db)}
	params["target_id"] = []string{config[TARGET_ID].Value}

	return dbserver.New(
		subLogger,
		errChan,
		config[LOCAL_PORT].Value,
		config[LOCAL_HOST].Value,
		remotePort,
		config[REMOTE_HOST].Value,
		cert,
		config[CONNECTION_SERVICE_URL].Value,
		params,
		headers,
		publicKey,
	)
}

func newKubeServer(logger *bzlogger.Logger, publicKey *keypair.PublicKey, errChan chan error, headers http.Header, params url.Values, cert *bzcert.DaemonBZCert) (*kubeserver.KubeServer, error) {

	subLogger := logger.GetComponentLogger("kubeserver")

	targetGroups := []string{}
	if config[TARGET_GROUPS].Value != "" {
		targetGroups = strings.Split(config[TARGET_GROUPS].Value, ",")
	}

	params["connectionType"] = []string{string(dataconnection.Kube)}
	params["target_id"] = []string{config[TARGET_ID].Value}
	params["target_user"] = []string{config[TARGET_USER].Value}
	params["target_groups"] = []string{config[TARGET_GROUPS].Value}

	return kubeserver.New(
		subLogger,
		errChan,
		config[LOCAL_PORT].Value,
		config[LOCAL_HOST].Value,
		config[CERT_PATH].Value,
		config[KEY_PATH].Value,
		cert,
		config[TARGET_USER].Value,
		targetGroups,
		config[LOCALHOST_TOKEN].Value,
		config[CONNECTION_SERVICE_URL].Value,
		params,
		headers,
		publicKey,
	)
}

// read all environment variables and apply the processing for specific fields that need it
func loadEnvironment() error {
	for varName, entry := range config {
		entry.Value, entry.Seen = os.LookupEnv(varName)
		config[varName] = entry
	}

	// Make sure our service url is correctly formatted
	if err := formatServiceUrl(); err != nil {
		return err
	}

	// Check we have all required flags
	// Depending on the plugin ensure we have the correct required flag values
	var requriedVars []string
	plugin := config[PLUGIN].Value
	if pluginVars, ok := requriedPluginVars[bzplugin.PluginName(plugin)]; !ok {
		return fmt.Errorf("unhandled plugin passed: %s", plugin)
	} else {
		requriedVars = append(requriedGlobalVars, pluginVars...)
	}

	// Check against required dict to find the missing ones
	var missingVars []string
	for _, req := range requriedVars {
		if !config[req].Seen {
			missingVars = append(missingVars, req)
		}
	}

	if len(missingVars) > 0 {
		return fmt.Errorf("the following required environment variables are not set: %v", missingVars)
	}

	return nil
}

func formatServiceUrl() error {
	serviceUrlEntry := config[SERVICE_URL]
	serviceUrl := serviceUrlEntry.Value

	parsed, err := url.Parse(serviceUrl)
	if err != nil {
		return fmt.Errorf("malformatted serviceUrl: %s", serviceUrl)
	}

	parsed.Scheme = "https"

	serviceUrlEntry.Value = parsed.String()
	config[SERVICE_URL] = serviceUrlEntry

	return nil
}
