module github.com/crmarques/bootwright/tools

go 1.26.11

tool (
	golang.org/x/tools/cmd/goimports
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)

require (
	github.com/crmarques/bootwright v0.0.0
	golang.org/x/mod v0.43.0
	golang.org/x/vuln v1.10.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/telemetry v0.0.0-20261116191925-e77a43154acb // indirect
	golang.org/x/tools v0.53.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
)

replace github.com/crmarques/bootwright => ../..
