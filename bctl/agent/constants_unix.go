//go:build unix

package main

func initConstants() {
	configDir = "/etc/bzero"
	defaultLogPath = "/var/log/bzero/bzero-agent.log"
}
