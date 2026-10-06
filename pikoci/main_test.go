package pikoci_test

import (
	"os"
	"testing"

	"github.com/pikoci/pikoci/pikoci/utils"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	// Hash at the minimum bcrypt cost so password tests take milliseconds.
	// Cost 14 (production default) takes ~1s per hash locally and far longer
	// on the loaded arm64 CI runner, pushing the package past its 120s timeout.
	utils.BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
