package providedtime

import (
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type detailed struct{ render func(string) error }

func (r detailed) Error() string                  { return r.render("").Error() }
func (r detailed) Unwrap() error                  { return r.render("") }
func (r detailed) WithDetail(detail string) error { return r.render(detail) }

const sourceEntry = `[0-9A-Za-z.:-]{1,253}@[spl][SCNUFV][0-7]{1,3}`

var (
	conflictUnits = []string{"systemd-timesyncd.service", "ntpd.service", "ntpsec.service", "openntpd.service", "timemaster.service"}
	sourcesDetail = regexp.MustCompile(`^(?:none|unavailable|` + sourceEntry + `(?:_` + sourceEntry + `)*)$`)
)

func refusals(request Request) map[string]error {
	name, servers := "Machine/"+request.Identity.Object, strings.Join(request.Servers, ", ")
	refuse := func(stop diagnostics.Stop, message, remedy string) error { return stop.Fail(message, remedy) }
	return map[string]error{
		"time-service-conflict": detailed{func(detail string) error {
			unit := "unknown"
			for _, known := range conflictUnits {
				if detail == known {
					unit = detail
				}
			}
			return refuse(timeRefused, name+" runs the time service "+unit+", which Bootwright never stops",
				"disable the time service "+unit+" on "+name+", such as with systemctl disable --now "+unit+", and repeat apply")
		}},
		"time-package": refuse(timeRefused, name+" has no chrony package and could not install it from its enabled repositories",
			"enable a repository that provides chrony on "+name+", or install it, and repeat apply"),
		"time-config": refuse(timeRefused, "/etc/chrony.conf on "+name+" is a link, is not a regular file or is larger than 64 KiB",
			"replace /etc/chrony.conf on "+name+" with a regular file of at most 64 KiB, and repeat apply"),
		"time-unproved": detailed{func(detail string) error {
			if !sourcesDetail.MatchString(detail) {
				detail = "unknown"
			}
			return refuse(timeRefused, name+" did not report a synchronized clock within 180 seconds of chronyd using "+servers+
				"; chronyc -n sources, as address@mode state reach with mode s server, p peer or l local clock and state S selected, C combined, N not combined, U unreachable, F falseticker or V variable: "+detail,
				"make "+servers+" reachable from "+name+" on UDP port 123, or name servers it can reach in spec.os.provided.ntp, and repeat apply; the host keeps the configuration it had")
		}},
		"time-unavailable": refuse(timeRefused, name+" could not run chronyd's tools or read, change or restore its chrony configuration",
			"restore chronyd and the route to its tools on "+name+", or, when /etc/bootwright/provided-time.json is unreadable or outstanding, restore /etc/chrony.conf and chronyd from it by hand and remove it, and repeat apply"),
	}
}
