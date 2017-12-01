package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/log"
)

// ConfigsModel ...
type ConfigsModel struct {
	SSHPublicKey  string
	ScreenSharePW string
	AuthToken     string
}

func createConfigsModelFromEnvs() ConfigsModel {
	return ConfigsModel{
		SSHPublicKey:  os.Getenv("ssh_public_key"),
		ScreenSharePW: os.Getenv("screen_share_pw"),
		AuthToken:     os.Getenv("auth_token"),
	}
}

func (configs ConfigsModel) print() {
	log.Infof("\nNgrok Configs:")
	log.Printf("- SSHPublicKey: %s", configs.SSHPublicKey)
	log.Printf("- ScreenSharePW: %s", configs.ScreenSharePW)
	log.Printf("- AuthToken: %s", configs.AuthToken)
}

func (configs ConfigsModel) validate() error {
	for k, v := range map[string]string{
		"SSHPublicKey":  configs.SSHPublicKey,
		"ScreenSharePw": configs.ScreenSharePW,
		"AuthToken":     configs.AuthToken,
	} {
		if v == "" {
			return fmt.Errorf("no %s parameter specified", k)
		}
	}
	return nil
}
