package handler

import "testing"

func TestWorkspaceAction_TransitionalStatus(t *testing.T) {
	cases := map[workspaceAction]string{
		actionStart:   "starting",
		actionStop:    "stopping",
		actionRebuild: "rebuilding",
		actionDelete:  "deleting",
	}
	for action, want := range cases {
		if got := action.transitionalStatus(); got != want {
			t.Errorf("%s.transitionalStatus() = %q, want %q", action, got, want)
		}
	}
}

func TestIsTransitionalStatus(t *testing.T) {
	transitional := []string{"starting", "stopping", "rebuilding", "deleting"}
	for _, s := range transitional {
		if !isTransitionalStatus(s) {
			t.Errorf("expected %q to be transitional", s)
		}
	}
	terminal := []string{"ready", "stopped", "missing", "failed", "", "bogus"}
	for _, s := range terminal {
		if isTransitionalStatus(s) {
			t.Errorf("expected %q to be NON-transitional", s)
		}
	}
}
