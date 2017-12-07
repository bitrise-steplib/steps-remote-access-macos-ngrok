package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/log"
	"github.com/pkg/errors"
)

// ConfigsModel ...
type ConfigsModel struct {
	SSHPublicKey    string
	PasswordToSet   string
	NgrokAuthToken  string
	IsStepDebugMode bool
}

func createConfigsModelFromEnvs() ConfigsModel {
	return ConfigsModel{
		NgrokAuthToken:  os.Getenv("ngrok_auth_token"),
		SSHPublicKey:    os.Getenv("ssh_public_key"),
		PasswordToSet:   os.Getenv("user_and_screen_share_password"),
		IsStepDebugMode: os.Getenv("is_step_debug_mode") == "true",
	}
}

func (configs ConfigsModel) print() {
	fmt.Println()
	log.Infof("Ngrok Configs:")
	log.Printf("- IsStepDebugMode: %t", configs.IsStepDebugMode)
	log.Printf("- SSHPublicKey: %s", configs.SSHPublicKey)
	if configs.IsStepDebugMode {
		log.Printf("- PasswordToSet: %s", configs.PasswordToSet)
	} else {
		log.Printf("- PasswordToSet: ***")
	}
	if configs.IsStepDebugMode {
		log.Printf("- NgrokAuthToken: %s", configs.NgrokAuthToken)
	} else {
		log.Printf("- NgrokAuthToken: ***")
	}
	fmt.Println()
}

func (configs ConfigsModel) validate() error {
	if configs.NgrokAuthToken == "" {
		return errors.New("No NgrokAuthToken parameter specified")
	}
	if configs.PasswordToSet == "" && configs.SSHPublicKey == "" {
		return errors.New("Neither SSHPublicKey nor (VNC) PasswordToSet specified. At least one is required")
	}

	return nil
}
