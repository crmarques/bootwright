package substrate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
)

const redfishPlugins = "collections/ansible_collections/bootwright/core/plugins/"

// redfishSource is one plugin of the embedded collection, its whitespace runs
// collapsed so a statement reflowed across lines still reads as one.
func redfishSource(t *testing.T, name string) (raw, flowed string) {
	t.Helper()
	data, ok := ansible.Assets()[redfishPlugins+name]
	if !ok {
		t.Fatalf("the embedded collection carries no %s", name)
	}
	return string(data), strings.Join(strings.Fields(string(data)), " ")
}

// Every bound a margin is derived from is the client's own: each constant of
// redfish_control.py, and the poll count redfish_boot gives a power operation
// when its consumer names none. A bound the client moves fails here until the
// margins derived from it move too.
func TestTheControllerBoundsAreTheClientsOwn(t *testing.T) {
	raw, _ := redfishSource(t, "module_utils/redfish_control.py")
	declared := map[string]int{}
	for _, match := range regexp.MustCompile(`(?m)^([A-Z][A-Z_]*) = ([0-9]+)$`).FindAllStringSubmatch(raw, -1) {
		if _, repeated := declared[match[1]]; repeated {
			t.Fatalf("redfish_control.py assigns %s twice", match[1])
		}
		value, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatal(err)
		}
		declared[match[1]] = value
	}
	seconds := func(duration time.Duration) int { return int(duration / time.Second) }
	for name, mirrored := range map[string]int{
		"REQUEST_TIMEOUT": seconds(controllerRequestTimeout), "MEDIA_TIMEOUT": seconds(controllerMediaTimeout),
		"INSERT_ATTEMPTS": controllerInsertAttempts, "INSERT_RETRY_DELAY": seconds(controllerInsertRetryDelay),
		"TASK_POLLS": controllerTaskPolls, "TASK_POLL_DELAY": seconds(controllerTaskPollDelay),
		"MEDIA_PROBES": controllerMediaProbes, "MEDIA_PROBE_DELAY": seconds(controllerMediaProbeDelay),
		"POLL_ATTEMPTS": controllerPowerPolls, "POLL_DELAY": seconds(controllerPowerPollDelay),
		"BOOT_POLLS": controllerBootPolls, "BOOT_POLL_DELAY": seconds(controllerBootPollDelay),
	} {
		value, found := declared[name]
		if !found {
			t.Errorf("redfish_control.py declares no %s", name)
		} else if value != mirrored {
			t.Errorf("redfish_control.py sets %s to %d, but the margins are derived from %d", name, value, mirrored)
		}
	}
	boot, _ := redfishSource(t, "modules/redfish_boot.py")
	defaults := regexp.MustCompile(`"attempts": \{"type": "int", "default": ([0-9]+)\}`).FindAllStringSubmatch(boot, -1)
	if len(defaults) != 1 {
		t.Fatalf("redfish_boot.py declares the attempts default %d times, want once", len(defaults))
	}
	if value, _ := strconv.Atoi(defaults[0][1]); value != controllerPowerPolls {
		t.Errorf("redfish_boot.py polls a power operation %d times by default, but the margins are derived from %d", value, controllerPowerPolls)
	}
}

// Each call's pauses are the worst case the client documents for it, and a
// media read finds its device in the requests the client documents for the
// pinned emulator, so a margin derived here is derived from what the client
// states rather than from a reading of it the client does not share.
func TestTheControllerBoundsAreTheWorstCasesTheClientDocuments(t *testing.T) {
	_, boot := redfishSource(t, "modules/redfish_boot.py")
	for _, stated := range []string{
		"- insert: at most " + secondsText(controllerInsertPauses) + " s of pauses and attach timeouts",
		"- eject: at most " + secondsText(controllerEjectPauses) + " s of pauses",
		"- boot: at most " + secondsText(controllerBootPauses) + " s,",
		"- power-on, power-off and shutdown: at most attempts x " + secondsText(controllerPowerPollDelay) + " s,",
	} {
		if !strings.Contains(boot, stated) {
			t.Errorf("redfish_boot.py does not document %q", stated)
		}
	}
	_, read := redfishSource(t, "modules/redfish_system_read.py")
	words := []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	if controllerMediaReads >= len(words) {
		t.Fatalf("a media read of %d requests has no word here", controllerMediaReads)
	}
	if stated := "The pinned emulator answers in " + words[controllerMediaReads] + " GETs in all."; !strings.Contains(read, stated) {
		t.Errorf("redfish_system_read.py does not document %q", stated)
	}
}

// secondsText renders whole seconds as the client's documentation does, with
// a comma between thousands.
func secondsText(duration time.Duration) string {
	text := strconv.Itoa(int(duration / time.Second))
	for index := len(text) - 3; index > 0; index -= 3 {
		text = text[:index] + "," + text[index:]
	}
	return text
}

// Each bound is its pauses and every request outside its polls at the
// request timeout, so an arithmetic slip in a derivation shows as a figure the
// specifications do not state.
func TestTheControllerBoundsAreTheFiguresTheSpecificationsState(t *testing.T) {
	for name, test := range map[string]struct {
		bound time.Duration
		want  string
	}{
		"a power read":      {ControllerPowerReadBound, "30s"},
		"a media read":      {ControllerMediaReadBound, "2m0s"},
		"an insert":         {ControllerInsertBound, "39m20s"},
		"an eject":          {ControllerEjectBound, "4m30s"},
		"a boot selection":  {ControllerBootSelectionBound, "2m30s"},
		"a power operation": {ControllerPowerBound, "3m30s"},
	} {
		if got := fmt.Sprint(test.bound); got != test.want {
			t.Errorf("%s is bounded by %s, want %s", name, got, test.want)
		}
	}
}
