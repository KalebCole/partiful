package version

var CLIVersion = "0.1.0"

const (
	CommandContractRevision   = "1"
	TransportContractRevision = "2026-08-12.7"
)

// Info is the immutable public version result.
type Info struct {
	CLIVersion                string
	CommandContractRevision   string
	TransportContractRevision string
}

// Current returns the reviewed CLI and contract revisions.
func Current() Info {
	return Info{
		CLIVersion:                CLIVersion,
		CommandContractRevision:   CommandContractRevision,
		TransportContractRevision: TransportContractRevision,
	}
}
