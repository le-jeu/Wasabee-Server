package wasabi_test

import (
	"github.com/cloudkucooland/WASABI"
	"os"
	"testing"
)

func TestConnect(t *testing.T) {
	cs := os.Getenv("DATABASE")
	if cs == "" {
		t.Errorf("DATABASE environment variable unset")
	}
	err := wasabi.Connect(cs)
	if err != nil {
		t.Errorf(err.Error())
	}

	// assumes a whole host of other things already work
	// but needed to pass TestQuery on a new install (e.g. Travis-CI)
	gid := wasabi.GoogleID("118281765050946915735")
	_, err := gid.InitAgent()
	if err != nil {
		t.Errorf(err.Error())
	}
}

func TestQuery(t *testing.T) {
	gid, err := wasabi.SearchAgentName("deviousness")
	if err != nil {
		t.Errorf(err.Error())
	}
	if gid.String() != "118281765050946915735" {
		t.Error("did not find the correct gid for deviousness")
	}
}
