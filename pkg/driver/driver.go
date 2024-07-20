package driver

import "fmt"

var (
	ErrPathRequired = fmt.Errorf("path is required")

	DriverNames = []DriverName{
		DriverNameAws,
		DriverNameGcp,
		DriverNameGitHub,
		DriverNameVault,
		DriverNameHttp,
	}
)

type DriverName string

const (
	DriverNameAws    DriverName = "aws"
	DriverNameGcp    DriverName = "gcp"
	DriverNameGitHub DriverName = "github"
	DriverNameVault  DriverName = "vault"
	DriverNameHttp   DriverName = "http"
)

func DriverIsSupported(driver DriverName) bool {
	for _, d := range DriverNames {
		if d == driver {
			return true
		}
	}
	return false
}
