package gpbckpconfig

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGpbckpconfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gpbckpconfig Suite")
}
