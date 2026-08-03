package routes

import "testing"

func TestValidateRetentionDays(t *testing.T) {
	validAudit := 1
	validJob := 7
	validTerminal := 30
	zero := 0
	negative := -1

	tests := []struct {
		name         string
		auditDays    *int
		jobDays      *int
		terminalDays *int
		wantErr      bool
	}{
		{
			name:    "missing values are allowed for backwards compatible partial updates",
			wantErr: false,
		},
		{
			name:         "positive values",
			auditDays:    &validAudit,
			jobDays:      &validJob,
			terminalDays: &validTerminal,
			wantErr:      false,
		},
		{
			name:         "zero audit retention is rejected",
			auditDays:    &zero,
			jobDays:      &validJob,
			terminalDays: &validTerminal,
			wantErr:      true,
		},
		{
			name:         "negative job retention is rejected",
			auditDays:    &validAudit,
			jobDays:      &negative,
			terminalDays: &validTerminal,
			wantErr:      true,
		},
		{
			name:         "negative terminal retention is rejected",
			auditDays:    &validAudit,
			jobDays:      &validJob,
			terminalDays: &negative,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRetentionDays(tc.auditDays, tc.jobDays, tc.terminalDays)
			if (err != nil) != tc.wantErr {
				t.Fatalf("expected error=%v, got %v", tc.wantErr, err)
			}
		})
	}
}
