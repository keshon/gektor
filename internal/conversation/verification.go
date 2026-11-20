package conversation

// VerificationReceipt records one configured check, not task correctness.
// A missing receipt in an older session means its disposition is unknown.
type VerificationReceipt struct {
	Disposition VerificationDisposition `json:"disposition"`
	Command     string                  `json:"command,omitempty"`
	ExitCode    *int                    `json:"exit_code,omitempty"`
	DurationMS  int64                   `json:"duration_ms"`
	Output      string                  `json:"output,omitempty"`
	Error       string                  `json:"error,omitempty"`
}

type VerificationDisposition string

const (
	VerificationNotRequested VerificationDisposition = "not_requested"
	// NotRun means a configured check was skipped because the run stopped first.
	VerificationNotRun    VerificationDisposition = "not_run"
	VerificationPassed    VerificationDisposition = "passed"
	VerificationFailed    VerificationDisposition = "failed"
	VerificationCancelled VerificationDisposition = "cancelled"
	VerificationError     VerificationDisposition = "error"
)
