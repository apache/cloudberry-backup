package textmsg

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTextmsg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Textmsg Suite")
}
