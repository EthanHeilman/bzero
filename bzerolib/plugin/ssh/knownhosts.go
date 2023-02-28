package ssh

import (
	"fmt"
	"os"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"bastionzero.com/bzerolib/bzio"
)

type IKnownHosts interface {
	AddHostKeyPrivate(privateKey []byte) error
	AddHostKeyPublic(publicKey gossh.PublicKey) error
}

type KnownHosts struct {
	filePath string
	hosts    []string
	fileIo   bzio.BzFileIo
}

func NewKnownHosts(filePath string, hosts []string, fileIo bzio.BzFileIo) *KnownHosts {
	return &KnownHosts{
		filePath: filePath,
		hosts:    hosts,
		fileIo:   fileIo,
	}
}

func (k *KnownHosts) AddHostKeyPrivate(privateKey []byte) error {

	if publicKey, err := ReadPublicKeyRsa(privateKey); err != nil {
		return fmt.Errorf("failed to decode private key: %s", err)
	} else if sshKey, err := gossh.NewPublicKey(publicKey); err != nil {
		return fmt.Errorf("failed to process public key: %s", err)
	} else {
		return k.AddHostKeyPublic(sshKey)
	}
}

func (k *KnownHosts) AddHostKeyPublic(publicKey gossh.PublicKey) error {
	keyLine := knownhosts.Line(k.hosts, publicKey)
	file, err := k.fileIo.OpenFile(k.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	defer file.Close()

	_, err = file.Write([]byte(fmt.Sprintf("%s\n", keyLine)))
	return err
}
