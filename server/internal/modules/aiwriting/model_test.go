package aiwriting

import "testing"

func TestCanManuallyRetry(t *testing.T) {
	tests := []struct {
		name string
		job  Job
		want bool
	}{
		{name: "temporary failure", job: Job{Status: JobFailed, Retryable: true}, want: true},
		{name: "invalid model output", job: Job{Status: JobFailed, ErrorCode: ErrorCodeOutputInvalid}, want: true},
		{name: "invalid output still running", job: Job{Status: JobRunning, ErrorCode: ErrorCodeOutputInvalid}, want: false},
		{name: "invalid parameters", job: Job{Status: JobFailed, ErrorCode: "AI_INVALID_PARAMETERS"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanManuallyRetry(test.job); got != test.want {
				t.Fatalf("CanManuallyRetry() = %t, want %t", got, test.want)
			}
		})
	}
}
