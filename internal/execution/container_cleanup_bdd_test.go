package execution

import (
	"testing"
)

func TestContainerBDD_AutomaticRemovalIsNotAnExecutionFailure(t *testing.T) {
	for _, scenario := range []string{"already_removed", "still_present", "daemon_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			p := containerProfile(t)
			body := "case \"$1\" in\nrm) exit 1 ;;\ncontainer)\n"
			switch scenario {
			case "already_removed":
				body += "exit 0 ;;\n"
			case "still_present":
				body += "printf 'research-attempt-still-present\\n'; exit 0 ;;\n"
			case "daemon_unavailable":
				body += "exit 1 ;;\n"
			}
			body += "*) exit 2 ;;\nesac\n"
			p.External.Container.Runtime = fixture(t, body)
			_, _, cleanup, err := coddyContainer(*p.External, t.TempDir(), t.TempDir(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = cleanup()
			if (err == nil) != (scenario == "already_removed") {
				t.Fatalf("scenario %s: %v", scenario, err)
			}
		})
	}
}
